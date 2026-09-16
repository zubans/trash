package service

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"healthlogin/backend/repository"
)

func newIntegrationPenaltyService(db *sql.DB, srv *OrderService) *PenaltyService {
	return NewPenaltyService(repository.NewPenaltyRepository(db), srv.settingsRepo, srv.ledger)
}

func (f *disputeFixture) cleanupPenalties(t *testing.T) {
	t.Cleanup(func() {
		for _, id := range []uuid.UUID{f.customerID, f.executorID} {
			_, _ = f.db.Exec(`DELETE FROM penalty_points WHERE user_id = $1`, id)
			_, _ = f.db.Exec(`DELETE FROM user_penalty_status WHERE user_id = $1`, id)
			_, _ = f.db.Exec(`DELETE FROM user_penalty_flags WHERE user_id = $1`, id)
		}
	})
}

func awardTx(t *testing.T, f *disputeFixture, penalties *PenaltyService, awards ...PenaltyAward) ([]*repository.PenaltyPoint, error) {
	t.Helper()
	var points []*repository.PenaltyPoint
	err := f.srv.ledger.RunInTx(context.Background(), func(tx *sql.Tx) error {
		var err error
		points, err = penalties.AwardTx(context.Background(), tx, awards...)
		return err
	})
	return points, err
}

func activePoints(t *testing.T, db *sql.DB, userID uuid.UUID, role string) int {
	t.Helper()
	var n int
	err := db.QueryRow(`SELECT active_points FROM user_penalty_status WHERE user_id = $1 AND role = $2`, userID, role).Scan(&n)
	if errors.Is(err, sql.ErrNoRows) {
		return 0
	}
	if err != nil {
		t.Fatalf("read status: %v", err)
	}
	return n
}

// Журнал и свёрнутое состояние: начисление, повтор по тому же спору, раздельные
// роли, отмена.
func TestPenaltyJournalIntegration(t *testing.T) {
	f := newDisputeFixture(t)
	f.cleanupPenalties(t)
	ctx := context.Background()
	penalties := newIntegrationPenaltyService(f.db, f.srv)

	dispute, err := f.srv.OpenDispute(ctx, f.customerID, f.order.ID, "не вывезли")
	if err != nil {
		t.Fatalf("open dispute: %v", err)
	}
	adminID := seedExecutor(t, f.db)

	award := PenaltyAward{UserID: f.executorID, Role: repository.RoleExecutor, OrderID: &f.order.ID,
		DisputeID: &dispute.ID, AssignedBy: &adminID, Reason: " арбитраж "}
	points, err := awardTx(t, f, penalties, award)
	if err != nil {
		t.Fatalf("award: %v", err)
	}
	if len(points) != 1 || points[0].ID == uuid.Nil || points[0].Reason != "арбитраж" {
		t.Fatalf("points: %+v", points)
	}
	if n := activePoints(t, f.db, f.executorID, repository.RoleExecutor); n != 1 {
		t.Fatalf("active points %d, want 1", n)
	}

	// Тот же спор, та же сторона — второго балла нет, и транзакция откатывается.
	if _, err := awardTx(t, f, penalties, award); !errors.Is(err, repository.ErrConflict) {
		t.Fatalf("second point for the same dispute: %v", err)
	}
	if n := activePoints(t, f.db, f.executorID, repository.RoleExecutor); n != 1 {
		t.Fatalf("active points %d after refused duplicate, want 1", n)
	}

	// Балл без спора (выдан вручную) и балл того же человека в роли заказчика.
	if _, err := awardTx(t, f, penalties,
		PenaltyAward{UserID: f.executorID, Role: repository.RoleExecutor, AssignedBy: &adminID, Reason: "вручную"},
		PenaltyAward{UserID: f.executorID, Role: repository.RoleCustomer, AssignedBy: &adminID, Reason: "как заказчик"},
	); err != nil {
		t.Fatalf("award two: %v", err)
	}
	if n := activePoints(t, f.db, f.executorID, repository.RoleExecutor); n != 2 {
		t.Fatalf("executor points %d, want 2", n)
	}
	if n := activePoints(t, f.db, f.executorID, repository.RoleCustomer); n != 1 {
		t.Fatalf("customer-role points %d, want 1", n)
	}

	if _, err := awardTx(t, f, penalties, PenaltyAward{UserID: f.executorID, Role: repository.RoleAdmin}); !errors.Is(err, ErrPenaltyRole) {
		t.Fatalf("admin role point: %v", err)
	}

	revoked, err := penalties.Revoke(ctx, points[0].ID, adminID)
	if err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if revoked.RevokedAt == nil || revoked.RevokedBy == nil || *revoked.RevokedBy != adminID {
		t.Fatalf("revoked point: %+v", revoked)
	}
	if n := activePoints(t, f.db, f.executorID, repository.RoleExecutor); n != 1 {
		t.Fatalf("executor points after revoke %d, want 1", n)
	}
	if _, err := penalties.Revoke(ctx, points[0].ID, adminID); !errors.Is(err, ErrPenaltyPointNotLive) {
		t.Fatalf("second revoke: %v", err)
	}
	if _, err := penalties.Revoke(ctx, uuid.New(), adminID); !errors.Is(err, ErrPenaltyPointNotFound) {
		t.Fatalf("revoke of a missing point: %v", err)
	}

	journal, err := penalties.repo.ListPoints(ctx, nil, f.executorID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(journal) != 3 {
		t.Fatalf("journal keeps %d rows, want 3 (revoked included)", len(journal))
	}
}

func penaltyStatus(t *testing.T, db *sql.DB, userID uuid.UUID, role string) repository.PenaltyStatus {
	t.Helper()
	statuses, err := repository.NewPenaltyRepository(db).ListStatuses(context.Background(), nil, userID)
	if err != nil {
		t.Fatalf("statuses: %v", err)
	}
	for _, st := range statuses {
		if st.Role == role {
			return st
		}
	}
	return repository.PenaltyStatus{UserID: userID, Role: role}
}

func sameMoment(a *time.Time, b time.Time) bool {
	return a != nil && a.Sub(b).Abs() < time.Second
}

// Порог N включает и продлевает период фото, 2×N включает тихую блокировку,
// отмена ошибочных баллов снимает то, что на них держалось.
func TestPenaltyPeriodAndSilentBlockIntegration(t *testing.T) {
	f := newDisputeFixture(t)
	f.cleanupPenalties(t)
	penalties := newIntegrationPenaltyService(f.db, f.srv)
	penalties.settings = settingsOverride{f.srv.settingsRepo, map[string]string{
		SettingPenaltyPointsThreshold: "2",
		SettingPhotoRequirementMonths: "3",
		SettingSilentBlockMonths:      "6",
	}}
	clock := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	penalties.now = func() time.Time { return clock }
	adminID := seedExecutor(t, f.db)
	award := func() *repository.PenaltyPoint {
		t.Helper()
		points, err := awardTx(t, f, penalties, PenaltyAward{UserID: f.executorID, Role: repository.RoleExecutor, AssignedBy: &adminID})
		if err != nil {
			t.Fatalf("award: %v", err)
		}
		return points[0]
	}
	status := func() repository.PenaltyStatus { return penaltyStatus(t, f.db, f.executorID, repository.RoleExecutor) }

	first := award()
	if st := status(); st.PhotoRequiredUntil != nil || st.SilentBlockEndsAt != nil {
		t.Fatalf("one point below the threshold: %+v", st)
	}

	second := award()
	periodStart := clock
	if st := status(); !sameMoment(st.PhotoRequiredUntil, periodStart.AddDate(0, 3, 0)) || st.SilentBlockEndsAt != nil {
		t.Fatalf("threshold reached: %+v", st)
	}

	// Новый балл через месяц продлевает период на три месяца от себя.
	clock = clock.AddDate(0, 1, 0)
	third := award()
	if st := status(); !sameMoment(st.PhotoRequiredUntil, clock.AddDate(0, 3, 0)) {
		t.Fatalf("period not extended: %+v", st)
	}

	blockStart := clock.Add(time.Hour)
	clock = blockStart
	fourth := award()
	st := status()
	if st.ActivePoints != 4 || !sameMoment(st.SilentBlockStartedAt, blockStart) || !sameMoment(st.SilentBlockEndsAt, blockStart.AddDate(0, 6, 0)) {
		t.Fatalf("silent block at 2×N: %+v", st)
	}

	// Пятый балл идущую блокировку не продлевает.
	clock = clock.AddDate(0, 0, 10)
	award()
	if st := status(); !sameMoment(st.SilentBlockEndsAt, blockStart.AddDate(0, 6, 0)) {
		t.Fatalf("silent block extended by a new point: %+v", st)
	}

	// Отмена ошибочных баллов: ниже 2×N блокировка снимается сразу, ниже N —
	// и период.
	for _, p := range []*repository.PenaltyPoint{fourth, first} {
		if _, err := penalties.Revoke(context.Background(), p.ID, adminID); err != nil {
			t.Fatalf("revoke: %v", err)
		}
	}
	if st := status(); st.ActivePoints != 3 || st.SilentBlockEndsAt != nil || st.PhotoRequiredUntil == nil {
		t.Fatalf("after revoking to 3: %+v", st)
	}
	for _, p := range []*repository.PenaltyPoint{second, third} {
		if _, err := penalties.Revoke(context.Background(), p.ID, adminID); err != nil {
			t.Fatalf("revoke: %v", err)
		}
	}
	if st := status(); st.ActivePoints != 1 || st.PhotoRequiredUntil != nil {
		t.Fatalf("below the threshold after revokes: %+v", st)
	}
}

// Тихая блокировка кончается по сроку: баллы гасятся, ставится флаг. Следующий
// балл — рецидив: аккаунт переходит в SOFT_BANNED.
func TestSilentBlockExpiryAndRelapseIntegration(t *testing.T) {
	f := newDisputeFixture(t)
	f.cleanupPenalties(t)
	penalties := newIntegrationPenaltyService(f.db, f.srv)
	penalties.settings = settingsOverride{f.srv.settingsRepo, map[string]string{
		SettingPenaltyPointsThreshold: "2",
		SettingSilentBlockMonths:      "6",
	}}
	clock := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	penalties.now = func() time.Time { return clock }
	adminID := seedExecutor(t, f.db)
	award := func() {
		t.Helper()
		if _, err := awardTx(t, f, penalties, PenaltyAward{UserID: f.executorID, Role: repository.RoleExecutor, AssignedBy: &adminID}); err != nil {
			t.Fatalf("award: %v", err)
		}
	}
	status := func() repository.PenaltyStatus { return penaltyStatus(t, f.db, f.executorID, repository.RoleExecutor) }
	flags := func() *repository.PenaltyFlags {
		t.Helper()
		fl, err := repository.NewPenaltyRepository(f.db).GetFlags(context.Background(), nil, f.executorID)
		if err != nil {
			t.Fatalf("flags: %v", err)
		}
		return fl
	}

	for i := 0; i < 4; i++ {
		award()
	}
	if st := status(); st.SilentBlockEndsAt == nil {
		t.Fatalf("no silent block at 2×N: %+v", st)
	}
	if flags().HadSilentBlockAt != nil {
		t.Fatal("the relapse flag is set while the block is still running")
	}
	if got := userStatusOf(t, f, f.executorID); got != repository.UserStatusActive {
		t.Fatalf("account status %s during a silent block, want ACTIVE", got)
	}

	// Через полгода срок вышел: первый же балл после этого снимает блокировку,
	// гасит старые баллы и становится рецидивом.
	clock = clock.AddDate(0, 6, 1)
	award()

	st := status()
	if st.SilentBlockEndsAt != nil || st.SilentBlockStartedAt != nil {
		t.Fatalf("expired silent block not lifted: %+v", st)
	}
	if st.ActivePoints != 1 {
		t.Fatalf("active points %d after the lift, want only the new one", st.ActivePoints)
	}
	if flags().HadSilentBlockAt == nil {
		t.Fatal("the relapse flag was not set when the block was lifted")
	}
	if got := userStatusOf(t, f, f.executorID); got != repository.UserStatusSoftBanned {
		t.Fatalf("account status %s after the relapse, want SOFT_BANNED", got)
	}
	if fl := flags(); fl.SoftBannedBy != nil || fl.SoftBanReason == "" {
		t.Fatalf("system soft ban recorded as: %+v", fl)
	}

	// Повторный балл ничего не меняет: аккаунт уже заблокирован.
	award()
	if fl := flags(); fl.SoftBannedAt == nil {
		t.Fatal("soft ban lost after another point")
	}
}

func userStatusOf(t *testing.T, f *disputeFixture, userID uuid.UUID) string {
	t.Helper()
	var status string
	if err := f.db.QueryRow(`SELECT status::text FROM users WHERE id = $1`, userID).Scan(&status); err != nil {
		t.Fatalf("read status: %v", err)
	}
	return status
}

// Проход обслуживания: баллы без новых начислений сгорают вместе с периодом
// фото, а тихая блокировка снимается по сроку — даже если человеку больше
// ничего не начисляли.
func TestPenaltySweepIntegration(t *testing.T) {
	f := newDisputeFixture(t)
	f.cleanupPenalties(t)
	penalties := newIntegrationPenaltyService(f.db, f.srv)
	penalties.settings = settingsOverride{f.srv.settingsRepo, map[string]string{
		SettingPenaltyPointsThreshold: "2",
		SettingPhotoRequirementMonths: "3",
		SettingPenaltyPointsTTLMonths: "3",
		SettingSilentBlockMonths:      "6",
	}}
	clock := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	penalties.now = func() time.Time { return clock }
	ctx := context.Background()
	adminID := seedExecutor(t, f.db)

	// Баллы пишутся временем базы, а тест живёт по своим часам, поэтому
	// начисленному проставляется то же время, которое видит сервис.
	backdate := func(userID uuid.UUID) {
		t.Helper()
		if _, err := f.db.Exec(`UPDATE penalty_points SET created_at = $1
			WHERE user_id = $2 AND revoked_at IS NULL AND expired_at IS NULL`, clock, userID); err != nil {
			t.Fatalf("backdate: %v", err)
		}
	}

	// Заказчику — два балла: порог, период фото, блокировки нет.
	for i := 0; i < 2; i++ {
		if _, err := awardTx(t, f, penalties, PenaltyAward{UserID: f.customerID, Role: repository.RoleCustomer, AssignedBy: &adminID}); err != nil {
			t.Fatalf("award: %v", err)
		}
	}
	backdate(f.customerID)
	// Исполнителю — четыре: тихая блокировка.
	for i := 0; i < 4; i++ {
		if _, err := awardTx(t, f, penalties, PenaltyAward{UserID: f.executorID, Role: repository.RoleExecutor, AssignedBy: &adminID}); err != nil {
			t.Fatalf("award: %v", err)
		}
	}

	backdate(f.executorID)

	// Ничего ещё не просрочено.
	if got, err := penalties.Sweep(ctx); err != nil || got.PointsBurnt != 0 || got.BlocksLifted != 0 {
		t.Fatalf("early sweep: %+v %v", got, err)
	}

	// Через три месяца без новых баллов заказчик очищается.
	clock = clock.AddDate(0, 3, 1)
	got, err := penalties.Sweep(ctx)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if got.PointsBurnt != 6 || got.BlocksLifted != 0 {
		t.Fatalf("sweep at three months: %+v", got)
	}
	if st := penaltyStatus(t, f.db, f.customerID, repository.RoleCustomer); st.ActivePoints != 0 || st.PhotoRequiredUntil != nil {
		t.Fatalf("customer status after burning: %+v", st)
	}
	// У исполнителя баллы тоже сгорели, но блокировка держится своим сроком.
	st := penaltyStatus(t, f.db, f.executorID, repository.RoleExecutor)
	if st.ActivePoints != 0 || st.SilentBlockEndsAt == nil {
		t.Fatalf("executor status after burning: %+v", st)
	}

	// Ещё три месяца — срок блокировки вышел.
	clock = clock.AddDate(0, 3, 0)
	got, err = penalties.Sweep(ctx)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if got.BlocksLifted != 1 {
		t.Fatalf("sweep at the end of the block: %+v", got)
	}
	if st := penaltyStatus(t, f.db, f.executorID, repository.RoleExecutor); st.SilentBlockEndsAt != nil {
		t.Fatalf("silent block still set: %+v", st)
	}
	flags, err := repository.NewPenaltyRepository(f.db).GetFlags(ctx, nil, f.executorID)
	if err != nil {
		t.Fatal(err)
	}
	if flags.HadSilentBlockAt == nil {
		t.Fatal("the relapse flag was not set by the sweep")
	}
	// Повторный проход ничего не делает.
	if got, err := penalties.Sweep(ctx); err != nil || got.PointsBurnt != 0 || got.BlocksLifted != 0 {
		t.Fatalf("repeated sweep: %+v %v", got, err)
	}
}
