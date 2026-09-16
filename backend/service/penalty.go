package service

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"healthlogin/backend/repository"
)

// Штрафные баллы. План механики —
// doc/implementation_plan_disputes_penalties_photo_proof.md, раздел 2.
//
// Источник правды — журнал penalty_points: каждый балл остаётся строкой, отмена
// и сгорание — отметки в ней. user_penalty_status — журнал, свёрнутый в то, что
// нужно читать на каждом запросе, и пересчитывается он всегда в той же
// транзакции, что меняет журнал, под блокировкой своей строки.

var (
	// ErrPenaltyRole — балл можно дать только исполнителю или заказчику.
	ErrPenaltyRole = errors.New("штрафной балл даётся только роли исполнителя или заказчика")
	// ErrPenaltyPointNotLive — балл уже отменён или сгорел.
	ErrPenaltyPointNotLive = errors.New("балл уже отменён или сгорел")
	// ErrPenaltyPointNotFound — балла нет.
	ErrPenaltyPointNotFound = errors.New("балл не найден")
)

// softBanRelapseReason — причина мягкого бана, поставленного системой.
const softBanRelapseReason = "Штрафной балл после снятой тихой блокировки"

// TxRunner выполняет функцию в транзакции. Ему удовлетворяет *Ledger.
type TxRunner interface {
	RunInTx(ctx context.Context, fn func(*sql.Tx) error) error
}

// PenaltyService начисляет и отменяет штрафные баллы и держит свёрнутое
// состояние ролей согласованным с журналом.
type PenaltyService struct {
	repo     repository.PenaltyRepository
	settings repository.SettingsRepository
	tx       TxRunner
	now      func() time.Time
}

// NewPenaltyService создаёт PenaltyService.
func NewPenaltyService(repo repository.PenaltyRepository, settings repository.SettingsRepository, tx TxRunner) *PenaltyService {
	return &PenaltyService{repo: repo, settings: settings, tx: tx, now: time.Now}
}

// PenaltyAward — за что и кому начисляется балл.
type PenaltyAward struct {
	UserID uuid.UUID
	// Role — CUSTOMER или EXECUTOR: баллы ролей не складываются.
	Role       string
	OrderID    *uuid.UUID
	DisputeID  *uuid.UUID
	AssignedBy *uuid.UUID
	Reason     string
}

func validPenaltyRole(role string) bool {
	return role == repository.RoleCustomer || role == repository.RoleExecutor
}

// AwardTx начисляет баллы в транзакции вызывающего и пересчитывает состояние
// каждой затронутой роли. Роли обрабатываются в постоянном порядке — по
// пользователю, затем по роли, — чтобы две транзакции, награждающие одних и тех
// же людей, брали блокировки состояний в одном порядке и не взаимоблокировались.
func (s *PenaltyService) AwardTx(ctx context.Context, tx *sql.Tx, awards ...PenaltyAward) ([]*repository.PenaltyPoint, error) {
	sorted := make([]PenaltyAward, len(awards))
	copy(sorted, awards)
	sort.Slice(sorted, func(i, j int) bool {
		if c := strings.Compare(sorted[i].UserID.String(), sorted[j].UserID.String()); c != 0 {
			return c < 0
		}
		return sorted[i].Role < sorted[j].Role
	})

	points := make([]*repository.PenaltyPoint, 0, len(sorted))
	for _, a := range sorted {
		if !validPenaltyRole(a.Role) {
			return nil, ErrPenaltyRole
		}
		// Истёкшая блокировка снимается до начисления: балл, пришедший после
		// её срока, — уже рецидив, а не продолжение прежнего наказания.
		if _, err := s.liftExpiredSilentBlockTx(ctx, tx, a.UserID, a.Role); err != nil {
			return nil, err
		}
		// Блокировка состояния берётся до записи балла: пересчёт другой
		// транзакции не должен увидеть балл, которого ещё нет в его подсчёте.
		if _, err := s.repo.LockStatus(ctx, tx, a.UserID, a.Role); err != nil {
			return nil, err
		}
		point := &repository.PenaltyPoint{
			UserID:     a.UserID,
			Role:       a.Role,
			OrderID:    a.OrderID,
			DisputeID:  a.DisputeID,
			AssignedBy: a.AssignedBy,
			Reason:     strings.TrimSpace(a.Reason),
		}
		if err := s.repo.AddPoint(ctx, tx, point); err != nil {
			return nil, err
		}
		if _, err := s.recomputeTx(ctx, tx, a.UserID, a.Role, recomputeAfterAward); err != nil {
			return nil, err
		}
		if err := s.softBanOnRelapseTx(ctx, tx, a.UserID); err != nil {
			return nil, err
		}
		points = append(points, point)
	}
	return points, nil
}

// Revoke отменяет ошибочный балл и пересчитывает состояние его роли.
func (s *PenaltyService) Revoke(ctx context.Context, pointID, adminID uuid.UUID) (*repository.PenaltyPoint, error) {
	var point *repository.PenaltyPoint
	err := s.tx.RunInTx(ctx, func(tx *sql.Tx) error {
		var err error
		point, err = s.repo.RevokePoint(ctx, tx, pointID, adminID)
		switch {
		case errors.Is(err, repository.ErrConflict):
			return ErrPenaltyPointNotLive
		case errors.Is(err, sql.ErrNoRows):
			return ErrPenaltyPointNotFound
		case err != nil:
			return err
		}
		if _, err := s.repo.LockStatus(ctx, tx, point.UserID, point.Role); err != nil {
			return err
		}
		_, err = s.recomputeTx(ctx, tx, point.UserID, point.Role, recomputeAfterRevoke)
		return err
	})
	if err != nil {
		return nil, err
	}
	return point, nil
}

// liftExpiredSilentBlockTx снимает тихую блокировку роли, если её срок вышел.
//
// Блокировка живёт свой срок независимо от баллов, поэтому снимает её время, а
// не пересчёт журнала. При снятии оставшиеся баллы роли гасятся — человек
// начинает с чистого листа, — а пользователю навсегда ставится флаг «была тихая
// блокировка»: следующий балл будет рецидивом.
func (s *PenaltyService) liftExpiredSilentBlockTx(ctx context.Context, tx *sql.Tx, userID uuid.UUID, role string) (bool, error) {
	st, err := s.repo.LockStatus(ctx, tx, userID, role)
	if err != nil {
		return false, err
	}
	now := s.now()
	if st.SilentBlockEndsAt == nil || st.SilentBlockEndsAt.After(now) {
		return false, nil
	}
	if _, err := s.repo.ExpirePoints(ctx, tx, userID, role, now); err != nil {
		return false, err
	}
	if err := s.repo.MarkSilentBlockLifted(ctx, tx, userID, now); err != nil {
		return false, err
	}
	st.ActivePoints = 0
	st.PhotoRequiredUntil = nil
	st.SilentBlockStartedAt = nil
	st.SilentBlockEndsAt = nil
	if err := s.repo.SaveStatus(ctx, tx, st); err != nil {
		return false, err
	}
	log.Printf("[AUDIT] silent block of user %s (%s) expired: points burnt, relapse flag set", userID, role)
	return true, nil
}

// softBanOnRelapseTx переводит аккаунт в SOFT_BANNED, если балл пришёл к тому,
// у кого тихая блокировка уже была. Рецидив — любой новый балл в любой роли:
// тихую блокировку такой человек уже отбыл, и вторая была бы тем же наказанием,
// которое не сработало.
func (s *PenaltyService) softBanOnRelapseTx(ctx context.Context, tx *sql.Tx, userID uuid.UUID) error {
	flags, err := s.repo.GetFlags(ctx, tx, userID)
	if err != nil {
		return err
	}
	if flags.HadSilentBlockAt == nil || flags.SoftBannedAt != nil {
		return nil
	}
	if err := s.repo.ApplySoftBan(ctx, tx, userID, nil, softBanRelapseReason); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}
	log.Printf("[AUDIT] user %s soft-banned: penalty point after a lifted silent block", userID)
	return nil
}

// penaltyLimits — настройки механики на момент пересчёта.
type penaltyLimits struct {
	threshold         int
	photoMonths       int
	pointsTTLMonths   int
	silentBlockMonths int
}

func (s *PenaltyService) limits(ctx context.Context) penaltyLimits {
	l := penaltyLimits{
		threshold:         defaultPenaltyPointsThreshold,
		photoMonths:       defaultPhotoRequirementMonths,
		pointsTTLMonths:   defaultPenaltyPointsTTLMonths,
		silentBlockMonths: defaultSilentBlockMonths,
	}
	if s.settings == nil {
		return l
	}
	settings, err := s.settings.GetSettings(ctx)
	if err != nil {
		return l
	}
	read := func(key string, into *int) {
		// Нечитаемое или вне границ значение не должно выключать механику:
		// берётся умолчание, как если бы строки не было.
		if v, err := strconv.Atoi(settings[key]); err == nil && validatePenaltySetting(key, settings[key]) == nil {
			*into = v
		}
	}
	read(SettingPenaltyPointsThreshold, &l.threshold)
	read(SettingPhotoRequirementMonths, &l.photoMonths)
	read(SettingPenaltyPointsTTLMonths, &l.pointsTTLMonths)
	read(SettingSilentBlockMonths, &l.silentBlockMonths)
	return l
}

// recomputeReason — что изменило журнал. От этого зависит, что пересчёт вправе
// сделать с периодом и блокировкой.
type recomputeReason int

const (
	// recomputeAfterAward — начислен балл: период фото включается или
	// продлевается, тихая блокировка может включиться.
	recomputeAfterAward recomputeReason = iota
	// recomputeAfterRevoke — администратор отменил ошибочный балл: то, что
	// держалось на этом балле, снимается сразу — это исправление ошибки, а не
	// срок наказания.
	recomputeAfterRevoke
	// recomputeAfterExpire — баллы сгорели по сроку: снимается период фото, а
	// тихая блокировка живёт свой срок независимо от баллов.
	recomputeAfterExpire
)

// recomputeTx сворачивает журнал роли в её состояние. Вызывающий уже держит
// блокировку строки состояния (LockStatus).
//
//   - Баллов N и больше, и балл только что начислен — период фото тянется до
//     «сейчас + photo_requirement_months», если он кончался раньше.
//   - Баллов меньше N после отмены или сгорания — периода нет.
//   - Баллов 2×N и больше, и блокировки нет — тихая блокировка на
//     silent_block_months от сейчас. Идущая блокировка новым баллом не
//     продлевается: её срок — от включения.
//   - Баллов меньше 2×N после отмены — блокировка снимается. Сгорание баллов
//     её не снимает: это делает только истечение срока (воркер).
func (s *PenaltyService) recomputeTx(ctx context.Context, tx *sql.Tx, userID uuid.UUID, role string, reason recomputeReason) (*repository.PenaltyStatus, error) {
	st, err := s.repo.LockStatus(ctx, tx, userID, role)
	if err != nil {
		return nil, err
	}
	live, err := s.repo.CountLivePoints(ctx, tx, userID, role)
	if err != nil {
		return nil, err
	}
	limits := s.limits(ctx)
	now := s.now()
	st.ActivePoints = live.Count

	switch {
	case live.Count >= limits.threshold && reason == recomputeAfterAward:
		until := now.AddDate(0, limits.photoMonths, 0)
		if st.PhotoRequiredUntil == nil || st.PhotoRequiredUntil.Before(until) {
			st.PhotoRequiredUntil = &until
		}
	case live.Count < limits.threshold:
		st.PhotoRequiredUntil = nil
	}

	blocked := st.SilentBlockEndsAt != nil && st.SilentBlockEndsAt.After(now)
	switch {
	case live.Count >= 2*limits.threshold && reason == recomputeAfterAward && !blocked:
		ends := now.AddDate(0, limits.silentBlockMonths, 0)
		st.SilentBlockStartedAt = &now
		st.SilentBlockEndsAt = &ends
	case live.Count < 2*limits.threshold && reason == recomputeAfterRevoke:
		st.SilentBlockStartedAt = nil
		st.SilentBlockEndsAt = nil
	}

	if err := s.repo.SaveStatus(ctx, tx, st); err != nil {
		return nil, err
	}
	return st, nil
}
