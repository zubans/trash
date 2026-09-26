package repository

import (
	"context"
	"database/sql"
	"time"

	"github.com/google/uuid"

	"healthlogin/backend/money"
)

// Заявки на пополнение и вывод: ручные движения денег через администратора.
// Сами деньги двигает реестр (service.Ledger); здесь только заявки — их
// создание, заблокированное чтение и решение.

// TopUpRequest представляет ручную заявку на пополнение баланса.
type TopUpRequest struct {
	ID        uuid.UUID    `json:"id"`
	UserID    uuid.UUID    `json:"user_id"`
	UserPhone string       `json:"user_phone"` // Заполняется через JOIN
	Amount    money.Amount `json:"amount"`
	Status    string       `json:"status"`
	AdminID   *uuid.UUID   `json:"admin_id,omitempty"`
	CreatedAt time.Time    `json:"created_at"`
	UpdatedAt *time.Time   `json:"updated_at,omitempty"`
}

// WithdrawalRequest представляет ручную заявку на вывод средств.
type WithdrawalRequest struct {
	ID        uuid.UUID    `json:"id"`
	UserID    uuid.UUID    `json:"user_id"`
	UserPhone string       `json:"user_phone"` // Заполняется через JOIN
	Amount    money.Amount `json:"amount"`
	Status    string       `json:"status"`
	AdminID   *uuid.UUID   `json:"admin_id,omitempty"`
	CreatedAt time.Time    `json:"created_at"`
	UpdatedAt *time.Time   `json:"updated_at,omitempty"`
}

// PayoutRepository хранит заявки на пополнение и вывод.
type PayoutRepository interface {
	GetTopUpRequests(ctx context.Context, limit, offset int) ([]*TopUpRequest, error)
	CreateTopUpRequest(ctx context.Context, q Querier, userID uuid.UUID, amount money.Amount) (*TopUpRequest, error)
	LockTopUpRequest(ctx context.Context, q Querier, requestID uuid.UUID) (*TopUpRequest, error)
	SetTopUpStatus(ctx context.Context, q Querier, requestID, adminID uuid.UUID, status string) error

	GetWithdrawalRequests(ctx context.Context, limit, offset int) ([]*WithdrawalRequest, error)
	// Выводы — денежный процесс и живут в WalletService/AdminService; репозиторий
	// предоставляет заблокированное чтение и отдельные записи, которые им нужны.
	CreateWithdrawalRequest(ctx context.Context, q Querier, userID uuid.UUID, amount money.Amount) (*WithdrawalRequest, error)
	LockWithdrawalRequest(ctx context.Context, q Querier, requestID uuid.UUID) (*WithdrawalRequest, error)
	SetWithdrawalStatus(ctx context.Context, q Querier, requestID, adminID uuid.UUID, status string) error
	// HasPendingWithdrawal сообщает, есть ли у пользователя уже открытая заявка.
	// Внутри транзакции вызывающего он сперва берёт advisory-блокировку по
	// пользователю, поэтому две параллельные заявки одного человека
	// выстраиваются в очередь: вторая увидит первую уже закоммиченной.
	HasPendingWithdrawal(ctx context.Context, q Querier, userID uuid.UUID) (bool, error)
}

type payoutRepo struct {
	db *sql.DB
}

// NewPayoutRepository создаёт PayoutRepository.
func NewPayoutRepository(db *sql.DB) PayoutRepository {
	return &payoutRepo{db: db}
}

func (r *payoutRepo) GetTopUpRequests(ctx context.Context, limit, offset int) ([]*TopUpRequest, error) {
	query := `
		SELECT r.id, r.user_id, u.phone, r.amount, r.status, r.admin_id, r.created_at, r.updated_at
		FROM balance_topup_requests r
		JOIN users u ON r.user_id = u.id
		ORDER BY r.created_at DESC
		LIMIT $1 OFFSET $2`

	rows, err := r.db.QueryContext(ctx, query, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var reqs []*TopUpRequest
	for rows.Next() {
		var req TopUpRequest
		err := rows.Scan(&req.ID, &req.UserID, &req.UserPhone, &req.Amount, &req.Status, &req.AdminID, &req.CreatedAt, &req.UpdatedAt)
		if err != nil {
			return nil, err
		}
		reqs = append(reqs, &req)
	}
	return reqs, rows.Err()
}

func (r *payoutRepo) CreateTopUpRequest(ctx context.Context, q Querier, userID uuid.UUID, amount money.Amount) (*TopUpRequest, error) {
	id := uuid.New()
	query := `
		INSERT INTO balance_topup_requests (id, user_id, amount, status, created_at)
		VALUES ($1, $2, $3, 'PENDING', now())
		RETURNING id, user_id, amount, status, created_at`

	var req TopUpRequest
	err := exec(r.db, q).QueryRowContext(ctx, query, id, userID, amount).Scan(&req.ID, &req.UserID, &req.Amount, &req.Status, &req.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &req, nil
}

// LockTopUpRequest читает заявку, беря блокировку строки, чтобы два админа,
// решающих одновременно, сериализовались, а не зачислили баланс оба.
func (r *payoutRepo) LockTopUpRequest(ctx context.Context, q Querier, requestID uuid.UUID) (*TopUpRequest, error) {
	var req TopUpRequest
	err := exec(r.db, q).QueryRowContext(ctx, `
		SELECT id, user_id, amount, status, admin_id, created_at, updated_at
		FROM balance_topup_requests WHERE id = $1 FOR UPDATE`, requestID).Scan(
		&req.ID, &req.UserID, &req.Amount, &req.Status, &req.AdminID, &req.CreatedAt, &req.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &req, nil
}

// SetTopUpStatus решает судьбу ожидающей заявки; охрана не даёт второму решению
// зачислить баланс дважды.
func (r *payoutRepo) SetTopUpStatus(ctx context.Context, q Querier, requestID, adminID uuid.UUID, status string) error {
	return execExpectingOne(ctx, exec(r.db, q), `
		UPDATE balance_topup_requests
		SET status = $1::topup_status, admin_id = $2, updated_at = now()
		WHERE id = $3 AND status = 'PENDING'`, status, adminID, requestID)
}

func (r *payoutRepo) GetWithdrawalRequests(ctx context.Context, limit, offset int) ([]*WithdrawalRequest, error) {
	query := `
		SELECT r.id, r.user_id, u.phone, r.amount, r.status, r.admin_id, r.created_at, r.updated_at
		FROM balance_withdrawal_requests r
		JOIN users u ON r.user_id = u.id
		ORDER BY r.created_at DESC
		LIMIT $1 OFFSET $2`

	rows, err := r.db.QueryContext(ctx, query, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var reqs []*WithdrawalRequest
	for rows.Next() {
		var req WithdrawalRequest
		err := rows.Scan(&req.ID, &req.UserID, &req.UserPhone, &req.Amount, &req.Status, &req.AdminID, &req.CreatedAt, &req.UpdatedAt)
		if err != nil {
			return nil, err
		}
		reqs = append(reqs, &req)
	}
	return reqs, rows.Err()
}

func (r *payoutRepo) CreateWithdrawalRequest(ctx context.Context, q Querier, userID uuid.UUID, amount money.Amount) (*WithdrawalRequest, error) {
	id := uuid.New()
	query := `
		INSERT INTO balance_withdrawal_requests (id, user_id, amount, status, created_at)
		VALUES ($1, $2, $3, 'PENDING', now())
		RETURNING id, user_id, amount, status, created_at`

	var req WithdrawalRequest
	err := exec(r.db, q).QueryRowContext(ctx, query, id, userID, amount).Scan(&req.ID, &req.UserID, &req.Amount, &req.Status, &req.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &req, nil
}

// LockWithdrawalRequest читает заявку, беря блокировку строки, чтобы два
// действующих одновременно админа сериализовались, а не увидели её оба как PENDING.
func (r *payoutRepo) LockWithdrawalRequest(ctx context.Context, q Querier, requestID uuid.UUID) (*WithdrawalRequest, error) {
	var req WithdrawalRequest
	err := exec(r.db, q).QueryRowContext(ctx, `
		SELECT id, user_id, amount, status, admin_id, created_at, updated_at
		FROM balance_withdrawal_requests WHERE id = $1 FOR UPDATE`, requestID).Scan(
		&req.ID, &req.UserID, &req.Amount, &req.Status, &req.AdminID, &req.CreatedAt, &req.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &req, nil
}

// SetWithdrawalStatus решает судьбу ожидающей заявки. Охрана заставляет второе
// решение по той же заявке упасть, а не переписать первое.
func (r *payoutRepo) SetWithdrawalStatus(ctx context.Context, q Querier, requestID, adminID uuid.UUID, status string) error {
	return execExpectingOne(ctx, exec(r.db, q), `
		UPDATE balance_withdrawal_requests
		SET status = $1::withdrawal_status, admin_id = $2, updated_at = now()
		WHERE id = $3 AND status = 'PENDING'`, status, adminID, requestID)
}

// HasPendingWithdrawal — проверка «уже есть открытая заявка» под блокировкой.
//
// Раньше проверка шла до транзакции, и две заявки, отправленные одновременно
// (двойное нажатие, два устройства), обе видели «открытых нет» и обе
// создавались — очередь выплат тогда содержала суммы, которые нельзя было
// выполнить все. Advisory-блокировка транзакционная: она отпускается вместе с
// коммитом, и вторая транзакция, дождавшись его, видит первую заявку. Ключ
// пространством «withdrawal:» отделён от других advisory-блокировок по тому же
// пользователю (взятие заказа исполнителем), чтобы вывод денег не стоял в
// очереди за ними. Вне транзакции блокировка отпускается сразу и проверка
// работает как обычный EXISTS.
func (r *payoutRepo) HasPendingWithdrawal(ctx context.Context, q Querier, userID uuid.UUID) (bool, error) {
	db := exec(r.db, q)
	if _, err := db.ExecContext(ctx,
		`SELECT pg_advisory_xact_lock(hashtext('withdrawal:' || $1::text))`, userID); err != nil {
		return false, err
	}
	var exists bool
	err := db.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM balance_withdrawal_requests WHERE user_id = $1 AND status = 'PENDING')`,
		userID,
	).Scan(&exists)
	return exists, err
}
