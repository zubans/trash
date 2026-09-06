package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"

	"github.com/google/uuid"

	"healthlogin/backend/metrics"
	"healthlogin/backend/money"
	"healthlogin/backend/repository"
)

// Ledger — единственный способ двигать деньги.
//
// Каждая операция ниже трогает две стороны: баланс пользователя и системный
// счёт или два системных счёта. В этом и смысл: до появления этого типа штраф
// уходил с баланса исполнителя и просто переставал существовать, удержание
// уходило от заказчика и жило лишь числом на заказе, а пополнение возникало из
// ниоткуда. У сервисов больше нет сырого мутатора баланса под рукой, поэтому
// одностороннее движение нельзя записать по случайности.
//
// Инвариант, который это даёт: сумма всех балансов пользователей плюс сумма
// всех балансов системных счетов равна нулю. ReconciliationRepository это проверяет.
type Ledger struct {
	transactions repository.TransactionRepository
	accounts     repository.SystemAccountRepository
	// incidents хранит срабатывания зажимов. Необязателен: установка без него
	// ведёт себя так же, только след остаётся лишь в логе и в счётчике.
	incidents repository.MoneyIncidentRepository
}

// WithIncidents подключает журнал денежных инцидентов.
func (l *Ledger) WithIncidents(incidents repository.MoneyIncidentRepository) *Ledger {
	l.incidents = incidents
	return l
}

// NewLedger создаёт Ledger поверх хранилищ баланса и счетов.
func NewLedger(transactions repository.TransactionRepository, accounts repository.SystemAccountRepository) *Ledger {
	return &Ledger{transactions: transactions, accounts: accounts}
}

// RunInTx выполняет fn в транзакции базы. Каждая парная операция ниже обязана
// вызываться внутри неё.
func (l *Ledger) RunInTx(ctx context.Context, fn func(*sql.Tx) error) error {
	return l.transactions.RunInTx(ctx, fn)
}

// GetBalance читает баланс пользователя.
func (l *Ledger) GetBalance(ctx context.Context, userID uuid.UUID) (money.Amount, error) {
	return l.transactions.GetBalance(ctx, userID)
}

// AccountBalance читает системный счёт. Админские экраны показывают так счёт
// комиссии; сервисам он для движения денег не нужен, потому что каждое движение
// и так называет счёт, на который смотрит.
func (l *Ledger) AccountBalance(ctx context.Context, code string) (*repository.SystemAccount, error) {
	return l.accounts.Get(ctx, code)
}

// History возвращает самые свежие проводки пользователя, не более limit (ноль
// означает размер страницы по умолчанию из репозитория).
func (l *Ledger) History(ctx context.Context, userID uuid.UUID, limit int) ([]*repository.Transaction, error) {
	return l.transactions.GetTransactionsByUserID(ctx, userID, limit)
}

// HasTip сообщает, давали ли уже чаевые по заказу. Вызывается внутри транзакции
// чаевых, чтобы охрана и списание закоммитились вместе.
func (l *Ledger) HasTip(ctx context.Context, tx *sql.Tx, orderID uuid.UUID) (bool, error) {
	return l.transactions.HasTip(ctx, tx, orderID)
}

// entry описывает одну сторону движения в том виде, как она пишется в журнал.
type entry struct {
	UserID  uuid.UUID
	OrderID *uuid.UUID
	AdminID *uuid.UUID
	Type    repository.TransactionType
	Account string
	Amount  money.Amount
}

func (l *Ledger) record(ctx context.Context, tx *sql.Tx, e entry) error {
	err := l.transactions.CreateTransaction(ctx, tx, &repository.Transaction{
		UserID:       e.UserID,
		OrderID:      e.OrderID,
		AdminID:      e.AdminID,
		Type:         string(e.Type),
		Amount:       e.Amount,
		Counterparty: e.Account,
	})
	// Считается здесь, а не в каждом месте вызова: это единственная воронка, через
	// которую проходит любое движение, поэтому итоги не могут разойтись с журналом.
	// Проводку всё ещё может откатить её транзакция, поэтому авторитетным числом
	// остаётся проход сверки, а это — оценка частоты.
	if err != nil {
		metrics.LedgerError(string(e.Type))
		return err
	}
	metrics.LedgerEntry(string(e.Type), e.Account, e.Amount.Rubles())
	return nil
}

// Reserve переносит деньги от пользователя на системный счёт, но только если
// баланс это покрывает. Используется для удержаний по заказам и резервов
// вывода, где тратить деньги, которых у пользователя нет, недопустимо.
//
// Возвращает repository.ErrInsufficientFunds, когда баланса слишком мало.
func (l *Ledger) Reserve(ctx context.Context, tx *sql.Tx, userID uuid.UUID, account string, amount money.Amount, kind repository.TransactionType, orderID *uuid.UUID) error {
	if !amount.IsPositive() {
		return nil
	}
	if err := l.transactions.Debit(ctx, tx, userID, amount); err != nil {
		return err
	}
	if err := l.accounts.Credit(ctx, tx, account, amount); err != nil {
		return err
	}
	return l.record(ctx, tx, entry{UserID: userID, OrderID: orderID, Type: kind, Account: account, Amount: amount})
}

// Charge переносит деньги от пользователя на системный счёт без проверки
// баланса. Используется для штрафов: балансу исполнителя позволено уходить в
// минус, для чего и существует min_balance_limit.
func (l *Ledger) Charge(ctx context.Context, tx *sql.Tx, userID uuid.UUID, account string, amount money.Amount, kind repository.TransactionType, orderID *uuid.UUID) error {
	if !amount.IsPositive() {
		return nil
	}
	if err := l.transactions.UpdateBalance(ctx, tx, userID, -amount); err != nil {
		return err
	}
	if err := l.accounts.Credit(ctx, tx, account, amount); err != nil {
		return err
	}
	return l.record(ctx, tx, entry{UserID: userID, OrderID: orderID, Type: kind, Account: account, Amount: amount})
}

// Release переносит деньги с системного счёта пользователю: возврат из эскроу,
// вознаграждение исполнителя, возвращённый резерв вывода.
func (l *Ledger) Release(ctx context.Context, tx *sql.Tx, account string, userID uuid.UUID, amount money.Amount, kind repository.TransactionType, orderID, adminID *uuid.UUID) error {
	if !amount.IsPositive() {
		return nil
	}
	if err := l.accounts.Debit(ctx, tx, account, amount); err != nil {
		return err
	}
	if err := l.transactions.UpdateBalance(ctx, tx, userID, amount); err != nil {
		return err
	}
	return l.record(ctx, tx, entry{UserID: userID, OrderID: orderID, AdminID: adminID, Type: kind, Account: account, Amount: amount})
}

// Deposit вводит деньги извне: одобренное пополнение. DEPOSITS уходит в минус
// на ту же сумму — так представляется внешний источник.
func (l *Ledger) Deposit(ctx context.Context, tx *sql.Tx, userID uuid.UUID, amount money.Amount, adminID *uuid.UUID) error {
	return l.Release(ctx, tx, repository.AccountDeposits, userID, amount, repository.TransactionTypeTopUp, nil, adminID)
}

// Settle переносит деньги между двумя системными счетами, записывая проводку
// против пользователя, которого она касается. Используется, когда выплата
// покидает систему: резерв уходит через DEPOSITS — счёт, представляющий внешний мир.
func (l *Ledger) Settle(ctx context.Context, tx *sql.Tx, from, to string, userID uuid.UUID, amount money.Amount, kind repository.TransactionType, adminID *uuid.UUID) error {
	if !amount.IsPositive() {
		return nil
	}
	if err := l.accounts.Debit(ctx, tx, from, amount); err != nil {
		return err
	}
	if err := l.accounts.Credit(ctx, tx, to, amount); err != nil {
		return err
	}
	return l.record(ctx, tx, entry{UserID: userID, AdminID: adminID, Type: kind, Account: from, Amount: amount})
}

// Commission переносит долю платформы с завершённого заказа из эскроу на счёт
// комиссии, внутри транзакции подтверждения. Она записывается против
// исполнителя и заказа, чтобы проводку можно было проследить до выплаты, из
// которой её взяли. С эскроу списывается без охраны, как и при любом другом
// движении по заказу: деньги уже удержаны под этот заказ, и вызывающий делит
// ровно то, что удержано, между исполнителем и этим счётом.
func (l *Ledger) Commission(ctx context.Context, tx *sql.Tx, executorID uuid.UUID, amount money.Amount, orderID *uuid.UUID) error {
	if !amount.IsPositive() {
		return nil
	}
	if err := l.accounts.Debit(ctx, tx, repository.AccountEscrow, amount); err != nil {
		return err
	}
	if err := l.accounts.Credit(ctx, tx, repository.AccountCommission, amount); err != nil {
		return err
	}
	return l.record(ctx, tx, entry{
		UserID:  executorID,
		OrderID: orderID,
		Type:    repository.TransactionTypeCommission,
		Account: repository.AccountEscrow,
		Amount:  amount,
	})
}

// Payout отправляет деньги с системного счёта во внешний мир, но только если
// счёт ими действительно располагает. Settle — неохраняемая версия, для случаев,
// когда деньги были зарезервированы раньше и заведомо на месте; эта — для
// выплаты со счёта по запросу, где сумму задаёт вызывающий и неохраняемое
// списание позволило бы счёту уйти в минус.
//
// Возвращает repository.ErrInsufficientFunds, когда на счёте меньше.
func (l *Ledger) Payout(ctx context.Context, tx *sql.Tx, from string, userID uuid.UUID, amount money.Amount, kind repository.TransactionType, adminID *uuid.UUID) error {
	if !amount.IsPositive() {
		return nil
	}
	if err := l.accounts.DebitAvailable(ctx, tx, from, amount); err != nil {
		return err
	}
	if err := l.accounts.Credit(ctx, tx, repository.AccountDeposits, amount); err != nil {
		return err
	}
	return l.record(ctx, tx, entry{UserID: userID, AdminID: adminID, Type: kind, Account: from, Amount: amount})
}

// Bonus платит пользователю из собственного кармана платформы: вознаграждение,
// присуждённое скриптом поведения за работу, которую не оплачивал ни один
// заказчик, например за подтверждение чьей-то личности. Деньги приходят с
// BONUSES, который уходит в минус на выплаченную сумму, — этот баланс и есть
// текущая стоимость таких вознаграждений, а книги сходятся, потому что зачисление и списание — одно движение.
//
// commission обычно равна нулю. Доля платформы существует, чтобы браться из
// уплаченного заказчиком, а за бесплатную услугу никто не платил; её удержание
// из вознаграждения лишь переложило бы деньги платформы с BONUSES на
// COMMISSION. Поведение, желающее считать свои вознаграждения обычным
// заработком, просит об этом явно, и тогда брутто всё равно делится ровно:
//
//	BONUSES -gross = user +(gross - commission) + COMMISSION +commission
//
// Списание намеренно не охраняется. BONUSES — счёт расходов, а не кошелёк:
// отказ в вознаграждении из-за того, что «счёт пуст», означал бы, что первое
// вознаграждение нельзя выплатить никогда. Потолок того, что может присудить
// скрипт, живёт в применителе (behavior_max_bonus), где админ может его
// прочитать и изменить.
func (l *Ledger) Bonus(ctx context.Context, tx *sql.Tx, userID uuid.UUID, gross, commission money.Amount, orderID *uuid.UUID) error {
	if !gross.IsPositive() {
		return nil
	}
	if commission.IsNegative() {
		return errors.New("bonus commission must not be negative")
	}
	if commission > gross {
		commission = gross
	}
	if commission.IsPositive() {
		if err := l.accounts.Debit(ctx, tx, repository.AccountBonuses, commission); err != nil {
			return err
		}
		if err := l.accounts.Credit(ctx, tx, repository.AccountCommission, commission); err != nil {
			return err
		}
		if err := l.record(ctx, tx, entry{
			UserID:  userID,
			OrderID: orderID,
			Type:    repository.TransactionTypeCommission,
			Account: repository.AccountBonuses,
			Amount:  commission,
		}); err != nil {
			return err
		}
	}
	return l.Release(ctx, tx, repository.AccountBonuses, userID, gross.Sub(commission), repository.TransactionTypeBonus, orderID, nil)
}

// OrderSettlement — распределение удержания по завершённому заказу целиком:
// сколько вернуть заказчику, сколько оставить платформе и сколько отдать
// исполнителю.
type OrderSettlement struct {
	OrderID    uuid.UUID
	CustomerID uuid.UUID
	ExecutorID uuid.UUID
	// Hold — то, что эскроу держит по этому заказу. Распределение обязано
	// опустошить его ровно в ноль.
	Hold money.Amount
	// Paid — то, что заказчик за заказ действительно заплатил: остаток
	// удержания возвращается ему.
	Paid money.Amount
	// Commission — доля платформы, как её посчитал вызывающий. Именно её здесь
	// зажимают: всё остальное считается от неё.
	Commission money.Amount
}

// SettleOrder распределяет удержание по заказу: возврат заказчику, комиссия
// платформе, вознаграждение исполнителю.
//
// Существует ради инварианта, который нельзя проверить по частям. Раньше эти
// три движения были тремя вызовами подряд, и ни одному из них не было видно
// целого — значит, «исполнителю досталось не больше уплаченного» и «эскроу
// опустошён ровно в ноль» негде было проверить. Теперь есть где, и это
// единственная причина, по которой метод один.
//
// Зажим, а не отказ. Комиссия зажимается в [0, Paid] до всякого движения, а
// вознаграждение считается остатком, поэтому исполнитель не может получить
// больше, чем заплатил заказчик, какой бы ни оказалась ставка. Отказ здесь был
// бы хуже: он оставил бы заказ подтверждённым в одной половине системы и
// неоплаченным в другой, а повтор упёрся бы в ту же ошибку. По той же логике
// Bonus давно зажимает commission > gross.
//
// Каждый сработавший зажим пишет инцидент — в этой же транзакции, поэтому
// инцидент не может закоммититься без движения, а движение без инцидента.
func (l *Ledger) SettleOrder(ctx context.Context, tx *sql.Tx, s OrderSettlement) error {
	zero := money.Zero
	paid := s.Paid
	if paid.IsNegative() {
		l.incident(ctx, tx, &repository.MoneyIncident{
			Kind: repository.IncidentCommissionOutOfRange, OrderID: &s.OrderID, UserID: &s.ExecutorID,
			Actual: &paid, Applied: &zero,
			Details: map[string]interface{}{"reason": "отрицательная сумма к оплате"},
		})
		paid = money.Zero
	}
	if paid > s.Hold {
		// Заплатить из эскроу больше, чем он держит, — это чужие деньги: эскроу
		// общий, и лишнее было бы взято из удержаний по другим заказам.
		before := paid
		paid = s.Hold
		l.incident(ctx, tx, &repository.MoneyIncident{
			Kind: repository.IncidentRewardExceedsPayment, OrderID: &s.OrderID, UserID: &s.ExecutorID,
			Expected: &s.Hold, Actual: &before, Applied: &paid,
			Details: map[string]interface{}{"reason": "к оплате больше, чем держит эскроу по заказу"},
		})
	}

	commission := s.Commission
	if commission.IsNegative() || commission > paid {
		before := commission
		if commission.IsNegative() {
			commission = money.Zero
		} else {
			commission = paid
		}
		l.incident(ctx, tx, &repository.MoneyIncident{
			Kind: repository.IncidentCommissionOutOfRange, OrderID: &s.OrderID, UserID: &s.ExecutorID,
			Expected: &paid, Actual: &before, Applied: &commission,
			Details: map[string]interface{}{"reason": "доля платформы вне [0, уплаченного заказчиком]"},
		})
	}

	reward := paid.Sub(commission)
	refund := s.Hold.Sub(paid)

	// Последняя проверка — сложением. Она не должна срабатывать никогда: выше
	// каждое слагаемое уже зажато. Но именно эту сумму обязана видеть база, и
	// стоит она одного сравнения.
	if refund.Add(commission).Add(reward) != s.Hold {
		total := refund.Add(commission).Add(reward)
		l.incident(ctx, tx, &repository.MoneyIncident{
			Kind: repository.IncidentSettlementMismatch, OrderID: &s.OrderID, UserID: &s.ExecutorID,
			Expected: &s.Hold, Actual: &total,
			Details: map[string]interface{}{
				"refund": refund.String(), "commission": commission.String(), "reward": reward.String(),
			},
		})
		return fmt.Errorf("settlement of order %s does not drain escrow: %s + %s + %s != %s",
			s.OrderID, refund, commission, reward, s.Hold)
	}

	if err := l.Release(ctx, tx, repository.AccountEscrow, s.CustomerID, refund, repository.TransactionTypeRefund, &s.OrderID, nil); err != nil {
		return err
	}
	// Деньги заказчика ушли с баланса в момент удержания; эта проводка
	// фиксирует расход удержания, а не второе списание.
	if err := l.Note(ctx, tx, s.CustomerID, repository.AccountEscrow, paid, repository.TransactionTypePayment, &s.OrderID); err != nil {
		return err
	}
	if err := l.Commission(ctx, tx, s.ExecutorID, commission, &s.OrderID); err != nil {
		return err
	}
	return l.Release(ctx, tx, repository.AccountEscrow, s.ExecutorID, reward, repository.TransactionTypeReward, &s.OrderID, nil)
}

// incident записывает срабатывание зажима. Сбой самой записи не отменяет
// движения: инцидент — это след, а не условие, и потеря следа не повод оставить
// заказ незакрытым. Он всё равно попадёт в лог и в счётчик.
func (l *Ledger) incident(ctx context.Context, tx *sql.Tx, incident *repository.MoneyIncident) {
	log.Printf("[AUDIT] money incident %s on order %v: expected=%v actual=%v applied=%v %v",
		incident.Kind, incident.OrderID, incident.Expected, incident.Actual, incident.Applied, incident.Details)
	metrics.MoneyIncident(incident.Kind)
	if l.incidents == nil {
		return
	}
	if err := l.incidents.Record(ctx, tx, incident); err != nil {
		log.Printf("[ledger] cannot record money incident %s: %v", incident.Kind, err)
	}
}

// Note записывает проводку, которая не двигает денег, — для шага, который стоит
// видеть в журнале: PAYMENT отмечает расход удержания, а баланс изменился ещё
// когда удержание бралось.
func (l *Ledger) Note(ctx context.Context, tx *sql.Tx, userID uuid.UUID, account string, amount money.Amount, kind repository.TransactionType, orderID *uuid.UUID) error {
	return l.record(ctx, tx, entry{UserID: userID, OrderID: orderID, Type: kind, Account: account, Amount: amount})
}

// Tip переносит чаевые от заказчика исполнителю. Деньги проходят через ESCROW в
// транзакции вызывающего — списываются с заказчика, только если баланс это
// покрывает, затем отпускаются исполнителю, — поэтому они никогда не существуют
// вне счёта, и сверка остаётся сбалансированной. Возвращает
// repository.ErrInsufficientFunds, когда заказчик не может покрыть чаевые.
func (l *Ledger) Tip(ctx context.Context, tx *sql.Tx, customerID, executorID uuid.UUID, amount money.Amount, orderID *uuid.UUID) error {
	if !amount.IsPositive() {
		return nil
	}
	if err := l.Reserve(ctx, tx, customerID, repository.AccountEscrow, amount, repository.TransactionTypeTip, orderID); err != nil {
		return err
	}
	return l.Release(ctx, tx, repository.AccountEscrow, executorID, amount, repository.TransactionTypeTipReward, orderID, nil)
}
