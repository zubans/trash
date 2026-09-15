package service

import (
	"context"
	"database/sql"
	"errors"
	"testing"

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
