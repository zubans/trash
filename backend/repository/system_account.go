package repository

import (
	"context"
	"database/sql"
	"errors"

	"healthlogin/backend/money"
)

// Коды системных счетов. Любое движение денег пользователя смотрит на один из
// них, поэтому у денег всегда есть откуда прийти и куда уйти.
const (
	// AccountEscrow держит то, что взято с заказчиков по ещё выполняющимся
	// заказам. Оно уходит исполнителю при завершении или возвращается
	// заказчику при отмене.
	AccountEscrow = "ESCROW"
	// AccountFines собирает штрафы, наложенные на исполнителей. До появления этого
	// счёта штраф просто уходил с баланса исполнителя и исчезал.
	AccountFines = "FINES"
	// AccountDeposits — внешний мир: отсюда деньги входят в систему.
	AccountDeposits = "DEPOSITS"
	// AccountPayouts держит деньги, зарезервированные ожидающими заявками на вывод,
	// и отпускает их наружу, когда выплата одобрена.
	AccountPayouts = "PAYOUTS"
	// AccountCommission собирает долю платформы с каждого завершённого заказа.
	// Это единственный счёт, чей баланс — собственные деньги платформы, а не
	// деньги, которые она за кого-то держит, и выплатить его может только админ.
	AccountCommission = "COMMISSION"
	// AccountBonuses — то, что платформа выплатила вознаграждениями, которые не
	// финансировал ни один заказчик, например гонорар проверяющего. Как и DEPOSITS,
	// он уходит в минус, и этот минус — текущая стоимость таких вознаграждений.
	AccountBonuses = "BONUSES"
)

// ErrUnknownSystemAccount возвращается для кода, за которым нет счёта.
var ErrUnknownSystemAccount = errors.New("unknown system account")

// SystemAccount — одна сторона книг платформы.
type SystemAccount struct {
	Code    string       `json:"code"`
	Name    string       `json:"name"`
	Balance money.Amount `json:"balance"`
}

// SystemAccountRepository двигает деньги по собственным счетам платформы.
type SystemAccountRepository interface {
	// Credit прибавляет к счёту, Debit вычитает. Оба принимают транзакцию
	// вызывающего, потому что движение по счёту — всегда вторая половина движения
	// баланса пользователя, и коммититься они обязаны вместе.
	Credit(ctx context.Context, q Querier, code string, amount money.Amount) error
	Debit(ctx context.Context, q Querier, code string, amount money.Amount) error
	// DebitAvailable вычитает, только если на счёте столько есть, атомарно.
	// Возвращает ErrInsufficientFunds, когда нет. Debit не охраняется, потому что
	// его вызывающие — вторая половина движения, уже установившего, что деньги на
	// месте; это же нужно для выплаты со счёта по запросу.
	DebitAvailable(ctx context.Context, q Querier, code string, amount money.Amount) error
	List(ctx context.Context) ([]SystemAccount, error)
	Get(ctx context.Context, code string) (*SystemAccount, error)
}

type systemAccountRepo struct {
	db *sql.DB
}

// NewSystemAccountRepository создаёт SystemAccountRepository.
func NewSystemAccountRepository(db *sql.DB) SystemAccountRepository {
	return &systemAccountRepo{db: db}
}

func (r *systemAccountRepo) exec(ctx context.Context, q Querier) Querier {
	if q == nil {
		return r.db
	}
	return q
}

func (r *systemAccountRepo) Credit(ctx context.Context, q Querier, code string, amount money.Amount) error {
	return r.move(ctx, q, code, amount)
}

func (r *systemAccountRepo) Debit(ctx context.Context, q Querier, code string, amount money.Amount) error {
	return r.move(ctx, q, code, amount.Neg())
}

// move применяет дельту. Нулевая сумма — это no-op, а не ошибка: несколько мест
// вызова законно ничего не двигают (возврат нуля, штраф в ноль).
func (r *systemAccountRepo) move(ctx context.Context, q Querier, code string, delta money.Amount) error {
	if delta.IsZero() {
		return nil
	}
	err := execExpectingOne(ctx, r.exec(ctx, q),
		`UPDATE system_accounts SET balance = balance + $1, updated_at = now() WHERE code = $2`,
		delta, code)
	if errors.Is(err, ErrConflict) {
		return ErrUnknownSystemAccount
	}
	return err
}

// DebitAvailable вычитает одним охраняемым оператором, чтобы две параллельные
// выплаты не могли обе пройти проверку баланса и увести счёт в минус.
func (r *systemAccountRepo) DebitAvailable(ctx context.Context, q Querier, code string, amount money.Amount) error {
	if amount.IsNegative() {
		return errors.New("debit amount must not be negative")
	}
	if amount.IsZero() {
		return nil
	}
	err := execExpectingOne(ctx, r.exec(ctx, q),
		`UPDATE system_accounts SET balance = balance - $1, updated_at = now() WHERE code = $2 AND balance >= $1`,
		amount, code)
	if errors.Is(err, ErrConflict) {
		// Либо счёта не существует, либо на нём недостаточно средств. Различаем эти
		// случаи, чтобы неизвестный код не выдавался за проблему с деньгами.
		if _, getErr := r.Get(ctx, code); getErr != nil {
			return getErr
		}
		return ErrInsufficientFunds
	}
	return err
}

func (r *systemAccountRepo) Get(ctx context.Context, code string) (*SystemAccount, error) {
	var a SystemAccount
	err := r.db.QueryRowContext(ctx,
		`SELECT code, name, balance FROM system_accounts WHERE code = $1`, code,
	).Scan(&a.Code, &a.Name, &a.Balance)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrUnknownSystemAccount
		}
		return nil, err
	}
	return &a, nil
}

func (r *systemAccountRepo) List(ctx context.Context) ([]SystemAccount, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT code, name, balance FROM system_accounts ORDER BY code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var accounts []SystemAccount
	for rows.Next() {
		var a SystemAccount
		if err := rows.Scan(&a.Code, &a.Name, &a.Balance); err != nil {
			return nil, err
		}
		accounts = append(accounts, a)
	}
	return accounts, rows.Err()
}
