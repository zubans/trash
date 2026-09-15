package service

import (
	"context"
	"database/sql"
	"errors"
	"sort"
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
		if _, err := s.recomputeTx(ctx, tx, a.UserID, a.Role); err != nil {
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
		_, err = s.recomputeTx(ctx, tx, point.UserID, point.Role)
		return err
	})
	if err != nil {
		return nil, err
	}
	return point, nil
}

// recomputeTx сворачивает журнал роли в её состояние. Вызывающий уже держит
// блокировку строки состояния (LockStatus).
func (s *PenaltyService) recomputeTx(ctx context.Context, tx *sql.Tx, userID uuid.UUID, role string) (*repository.PenaltyStatus, error) {
	st, err := s.repo.LockStatus(ctx, tx, userID, role)
	if err != nil {
		return nil, err
	}
	live, err := s.repo.CountLivePoints(ctx, tx, userID, role)
	if err != nil {
		return nil, err
	}
	st.ActivePoints = live.Count
	if err := s.repo.SaveStatus(ctx, tx, st); err != nil {
		return nil, err
	}
	return st, nil
}
