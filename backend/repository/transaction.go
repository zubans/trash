package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"healthlogin/backend/money"
)

// TransactionType представляет тип финансовой транзакции.
type TransactionType string

const (
	TransactionTypeHold       TransactionType = "HOLD"
	TransactionTypePayment    TransactionType = "PAYMENT"
	TransactionTypeReward     TransactionType = "REWARD"
	TransactionTypeRefund     TransactionType = "REFUND"
	TransactionTypeFine       TransactionType = "FINE"
	TransactionTypeTopUp      TransactionType = "TOP_UP"
	TransactionTypeWithdrawal TransactionType = "WITHDRAWAL"
	// TransactionTypeWithdrawalHold резервирует деньги при запросе вывода;
	// TransactionTypeWithdrawalPaid фиксирует выплату этого резерва. Вместе они
	// зеркалят HOLD/PAYMENT на стороне заказа.
	TransactionTypeWithdrawalHold TransactionType = "WITHDRAWAL_HOLD"
	TransactionTypeWithdrawalPaid TransactionType = "WITHDRAWAL_PAID"
	// TransactionTypeTip списывает с заказчика, дающего чаевые исполнителю после
	// завершённого заказа; TransactionTypeTipReward зачисляет исполнителю. Чаевые
	// проходят через ESCROW одной транзакцией, поэтому пара там сводится в ноль.
	TransactionTypeTip       TransactionType = "TIP"
	TransactionTypeTipReward TransactionType = "TIP_REWARD"
	// TransactionTypeCommission фиксирует переход доли платформы с завершённого
	// заказа из эскроу на счёт комиссии;
	// TransactionTypeCommissionPayout фиксирует вывод этого счёта админом из
	// системы. Ни один из них не трогает баланс пользователя.
	TransactionTypeCommission       TransactionType = "COMMISSION"
	TransactionTypeCommissionPayout TransactionType = "COMMISSION_PAYOUT"
	// TransactionTypeBonus зачисляет пользователю из собственного кармана
	// платформы: вознаграждение, которое скрипт поведения платит за услугу, не
	// оплаченную заказчиком. Он смотрит на счёт BONUSES (см. миграцию 043).
	TransactionTypeBonus TransactionType = "BONUS"
	// TransactionTypeDisputeReward — выплата исполнителю по спору, который арбитр
	// решил как «неизвестно». Удержание целиком вернулось заказчику, поэтому её
	// финансирует не заказчик, а счёт платформы DISPUTES.
	TransactionTypeDisputeReward TransactionType = "DISPUTE_REWARD"
	// TransactionTypeShopPurchase списывает с пользователя оплату покупки в
	// магазине на счёт SHOP; TransactionTypeShopRefund возвращает её при отмене
	// покупки (SHOP может уйти в минус, если выручку уже вывели — возврат
	// покупателю обязанность платформы, а не функция остатка);
	// TransactionTypeShopPayout фиксирует вывод выручки админом из системы —
	// как и вывод комиссии, он двигается между системными счетами и не трогает
	// баланс пользователя.
	TransactionTypeShopPurchase TransactionType = "SHOP_PURCHASE"
	TransactionTypeShopRefund   TransactionType = "SHOP_REFUND"
	TransactionTypeShopPayout   TransactionType = "SHOP_PAYOUT"
)

// ledgerSigns объявляет, как каждый тип транзакции двигает баланс пользователя.
// Это соглашение о знаках в реестре, и оно намеренно записано один раз: суммы в
// таблице все положительные, а направление живёт в типе, поэтому без
// единственного объявления правило пришлось бы заново выводить из кода сервисов
// всякий раз, когда кому-то надо сложить журнал.
//
// PAYMENT равен 0 намеренно. Деньги заказчика ушли с его баланса, когда бралось
// удержание; PAYMENT фиксирует расход этого удержания и ничего не двигает.
var ledgerSigns = map[TransactionType]int{
	TransactionTypeTopUp:      +1,
	TransactionTypeReward:     +1,
	TransactionTypeRefund:     +1,
	TransactionTypeHold:       -1,
	TransactionTypeFine:       -1,
	TransactionTypeWithdrawal: -1,
	// Списание — это резервирование денег; выплата ничего не двигает, потому что
	// они ушли с баланса ещё при создании заявки.
	TransactionTypeWithdrawalHold: -1,
	TransactionTypePayment:        0,
	TransactionTypeWithdrawalPaid: 0,
	// Чаевые списывают с заказчика и зачисляют исполнителю ту же сумму, одной
	// транзакцией через ESCROW.
	TransactionTypeTip:       -1,
	TransactionTypeTipReward: +1,
	// Комиссия перемещается между двумя системными счетами. Пользователь, против
	// которого она записана, — исполнитель, с чьего заказа она пришла, админ,
	// который её вывел, — нужен, чтобы запись находилась, а не чтобы двигать баланс.
	TransactionTypeCommission:       0,
	TransactionTypeCommissionPayout: 0,
	// Бонус зачисляет пользователю; счёт платформы BONUSES уходит в минус на ту же
	// сумму, поэтому книги всё равно сходятся.
	TransactionTypeBonus: +1,
	// Выплата по спору с неизвестным исходом устроена как бонус: зачисляет
	// исполнителю, а DISPUTES уходит в минус на ту же сумму.
	TransactionTypeDisputeReward: +1,
	// Покупка в магазине списывает деньги с баланса покупателя, её отмена
	// возвращает их обратно; вывод выручки происходит между системными счетами
	// и баланса пользователя не касается.
	TransactionTypeShopPurchase: -1,
	TransactionTypeShopRefund:   +1,
	TransactionTypeShopPayout:   0,
}

// LedgerSign сообщает, как тип транзакции двигает баланс и известен ли тип
// вообще. Неизвестный тип означает, что соглашение выше не обновили вместе с
// новым видом транзакции, а это делает любой результат сверки бессмысленным —
// вызывающие обязаны трактовать это как ошибку, а не пропускать.
func LedgerSign(t TransactionType) (int, bool) {
	sign, ok := ledgerSigns[t]
	return sign, ok
}

// KnownTransactionTypes перечисляет типы, покрытые соглашением о знаках.
func KnownTransactionTypes() []TransactionType {
	types := make([]TransactionType, 0, len(ledgerSigns))
	for t := range ledgerSigns {
		types = append(types, t)
	}
	return types
}

// Transaction представляет запись финансового журнала.
type Transaction struct {
	ID        uuid.UUID    `json:"id"`
	UserID    uuid.UUID    `json:"user_id"`
	UserPhone string       `json:"user_phone"` // Заполняется через JOIN
	OrderID   *uuid.UUID   `json:"order_id,omitempty"`
	Type      string       `json:"type"`
	Amount    money.Amount `json:"amount"`
	// Counterparty — системный счёт по другую сторону этой проводки.
	// Пусто в строках, записанных до появления системных счетов.
	Counterparty string `json:"counterparty,omitempty"`
	// ShopOrderID — покупка магазина, которой принадлежит проводка. Пусто у
	// проводок заказов: order_id занят ими, а оплату и возвраты покупки иначе
	// не найти.
	ShopOrderID *uuid.UUID `json:"shop_order_id,omitempty"`
	AdminID     *uuid.UUID `json:"admin_id,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	// Direction — как этот тип двигает баланс пользователя: +1, -1 или 0.
	// Берётся из ledgerSigns, чтобы клиент не выводил соглашение о знаках
	// заново: суммы в таблице все положительные, направление живёт в типе.
	Direction int `json:"direction"`
}

// TransactionsFilter описывает одну страницу журнала проводок. Как и фильтр
// завершённых заказов, всё сужение идёт в SQL, поэтому страница, счётчик и
// выгрузка описывают один и тот же набор.
type TransactionsFilter struct {
	// Search — телефон (нестрого, по цифрам) либо полный uuid проводки, заказа
	// или админа (точно). Строка, которая ни то ни другое, ничему не соответствует
	// по id и сравнивается только с телефоном.
	Search string
	Type   string // точный тип проводки
	Period string // YYYY-MM по created_at
	Sort   string // один из transactionSorts; всё прочее откатывается к умолчанию
	Desc   bool
	Page   PageRequest
}

// TransactionFacets — значения, которые предлагают фильтры журнала. Считаются
// по всей таблице, а не по текущей странице.
type TransactionFacets struct {
	Types   []string `json:"types"`
	Periods []string `json:"periods"`
}

// transactionSorts — белый список того, что может дойти до ORDER BY.
var transactionSorts = map[string]string{
	"created_at": "t.created_at",
	"amount":     "t.amount",
	"type":       "t.type",
	"user":       "u.phone",
}

// TransactionJournalRepository — админское чтение журнала: страница с
// фильтрами, значения для фильтров и история одного пользователя. Отделён от
// TransactionRepository, которым двигает деньги Ledger: тот пишет, этот
// только читает, и моки реестра не обязаны уметь листать журнал.
type TransactionJournalRepository interface {
	// GetTransactions — страница журнала. Общий счётчик считается только при
	// f.Page.WithTotal.
	GetTransactions(ctx context.Context, f TransactionsFilter) ([]*Transaction, int, error)
	TransactionFacets(ctx context.Context) (TransactionFacets, error)
	// GetUserTransactions — проводки одного пользователя, новые сверху.
	//
	// Отдельный метод, а не GetTransactions с поиском по телефону: поиск там
	// нестрогий (LIKE по цифрам), поэтому «792» подтянул бы чужие проводки, а
	// на карточке пользователя показывать чужие деньги нельзя. Здесь отбор идёт
	// по user_id.
	GetUserTransactions(ctx context.Context, userID uuid.UUID, limit, offset int) ([]*Transaction, int, error)
}

// TransactionRepository описывает операции хранения финансовых транзакций и баланса.
type TransactionRepository interface {
	GetBalance(ctx context.Context, userID uuid.UUID) (money.Amount, error)
	// UpdateBalance применяет безусловную дельту. Используйте Debit всякий раз,
	// когда баланс обязан остаться неотрицательным.
	UpdateBalance(ctx context.Context, tx *sql.Tx, userID uuid.UUID, delta money.Amount) error
	// Debit вычитает amount, только если баланс это покрывает, атомарно.
	// Возвращает ErrInsufficientFunds, когда не покрывает.
	Debit(ctx context.Context, tx *sql.Tx, userID uuid.UUID, amount money.Amount) error
	CreateTransaction(ctx context.Context, tx *sql.Tx, t *Transaction) error
	// GetTransactionsByUserID возвращает проводки пользователя, сначала новые, не
	// более limit. Limit, равный нулю или меньше, означает DefaultHistoryPageSize:
	// это питает экран истории, и учётка с годами активности не должна уметь
	// заставить один запрос прочитать всё её прошлое.
	GetTransactionsByUserID(ctx context.Context, userID uuid.UUID, limit int) ([]*Transaction, error)
	// HasTip сообщает, давал ли заказчик чаевые по этому заказу, чтобы чаевые
	// списывались не более одного раза. Выполняется внутри транзакции вызывающего,
	// поэтому проверка и запись — один атомарный шаг.
	HasTip(ctx context.Context, q Querier, orderID uuid.UUID) (bool, error)
	RunInTx(ctx context.Context, fn func(*sql.Tx) error) error
}

// transactionRepo реализует TransactionRepository поверх *sql.DB.
type transactionRepo struct {
	db *sql.DB
}

// NewTransactionRepository создаёт новый TransactionRepository.
func NewTransactionRepository(db *sql.DB) TransactionRepository {
	return &transactionRepo{db: db}
}

// NewTransactionJournal создаёт читающую сторону журнала поверх того же хранилища.
func NewTransactionJournal(db *sql.DB) TransactionJournalRepository {
	return &transactionRepo{db: db}
}

func (r *transactionRepo) GetBalance(ctx context.Context, userID uuid.UUID) (money.Amount, error) {
	var balance money.Amount
	err := r.db.QueryRowContext(ctx, `SELECT balance FROM users WHERE id = $1`, userID).Scan(&balance)
	if err != nil {
		return 0, err
	}
	return balance, nil
}

// UpdateBalance и Debit — единственные писатели users.balance. Колонкой владеет
// реестр (service.Ledger) через этот репозиторий, и оба оператора атомарны:
// дельта применяется в самом UPDATE, а не читается и записывается заново,
// поэтому их нельзя переложить на UserRepository без потери охраны. Абсолютной
// записи баланса в кодовой базе нет и быть не должно.
func (r *transactionRepo) UpdateBalance(ctx context.Context, tx *sql.Tx, userID uuid.UUID, delta money.Amount) error {
	return execExpectingOne(ctx, r.querier(tx),
		`UPDATE users SET balance = balance + $1 WHERE id = $2`, delta, userID)
}

// Debit вычитает amount одним охраняемым оператором, поэтому гонка
// «проверил-записал» не может увести баланс ниже нуля.
func (r *transactionRepo) Debit(ctx context.Context, tx *sql.Tx, userID uuid.UUID, amount money.Amount) error {
	if amount.IsNegative() {
		return errors.New("debit amount must not be negative")
	}
	err := execExpectingOne(ctx, r.querier(tx),
		`UPDATE users SET balance = balance - $1 WHERE id = $2 AND balance >= $1`, amount, userID)
	if errors.Is(err, ErrConflict) {
		return ErrInsufficientFunds
	}
	return err
}

// querier — то же, что exec, но для методов, принимающих *sql.Tx: нулевой
// указатель нельзя отдать в exec напрямую, он стал бы ненулевым интерфейсом.
func (r *transactionRepo) querier(tx *sql.Tx) Querier {
	if tx != nil {
		return tx
	}
	return r.db
}

func (r *transactionRepo) RunInTx(ctx context.Context, fn func(*sql.Tx) error) error {
	return runInTx(ctx, r.db, fn)
}

// CreateTransaction записывает проводку. Counterparty пишется вместе с ней: без
// этого столбца строка не говорит, какой системный счёт стоял по другую сторону
// движения, а по NULL в нём scripts/repair_books_gap.sql узнаёт проводку мимо
// Ledger. Доказательством NULL служит только после миграции 060, которая вышла
// вместе с этой записью: до неё столбец терялся у всех строк, поэтому граница
// берётся из schema_migrations, а не из данных. Пустая строка означает «счёт
// не назван» и ложится в NULL, а не в значение, которого нет в system_accounts:
// столбец — внешний ключ, и пустая строка его нарушила бы.
//
// Ветка tx и ветка без неё сведены в одну: раньше это были два одинаковых
// вызова с одним и тем же списком аргументов, и разойтись им ничто не мешало.
func (r *transactionRepo) CreateTransaction(ctx context.Context, tx *sql.Tx, t *Transaction) error {
	if t.ID == uuid.Nil {
		t.ID = uuid.New()
	}
	if t.CreatedAt.IsZero() {
		t.CreatedAt = time.Now()
	}
	// ShopOrderID привязывает проводку к покупке магазина: order_id занят заказами.
	_, err := r.querier(tx).ExecContext(ctx,
		`INSERT INTO transactions (id, user_id, order_id, shop_order_id, type, amount, counterparty, admin_id, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		t.ID, t.UserID, t.OrderID, t.ShopOrderID, t.Type, t.Amount,
		nullableCode(t.Counterparty), t.AdminID, t.CreatedAt)
	return err
}

// HasTip проверяет наличие записи TIP по заказу. Списание с заказчика — это
// строка TIP; TIP_REWARD лежит на исполнителе, поэтому искать достаточно по
// одному типу.
func (r *transactionRepo) HasTip(ctx context.Context, q Querier, orderID uuid.UUID) (bool, error) {
	var exists bool
	err := exec(r.db, q).QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM transactions WHERE order_id = $1 AND type = $2)`,
		orderID, TransactionTypeTip,
	).Scan(&exists)
	return exists, err
}

// transactionColumns — колонки проводки в порядке, который читает
// scanTransaction. Таблица обязана идти под псевдонимом t.
const transactionColumns = `t.id, t.user_id, t.order_id, t.shop_order_id, t.type::text, t.amount,
	COALESCE(t.counterparty, ''), t.admin_id, t.created_at`

// scanTransaction читает проводку из transactionColumns; extra — приёмники
// колонок, которые вызывающий дописал после них (телефон из JOIN и т. п.).
// Direction берётся из единственного объявления соглашения о знаках, чтобы
// ни один из четырёх прежних сканов не выводил его по-своему.
func scanTransaction(row rowScanner, extra ...interface{}) (*Transaction, error) {
	var t Transaction
	dest := []interface{}{&t.ID, &t.UserID, &t.OrderID, &t.ShopOrderID, &t.Type, &t.Amount,
		&t.Counterparty, &t.AdminID, &t.CreatedAt}
	if err := row.Scan(append(dest, extra...)...); err != nil {
		return nil, err
	}
	t.Direction, _ = LedgerSign(TransactionType(t.Type))
	return &t, nil
}

func (r *transactionRepo) GetTransactionsByUserID(ctx context.Context, userID uuid.UUID, limit int) ([]*Transaction, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+transactionColumns+`
		 FROM transactions t WHERE t.user_id = $1 ORDER BY t.created_at DESC LIMIT $2`,
		userID, historyLimit(limit),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []*Transaction
	for rows.Next() {
		t, err := scanTransaction(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, t)
	}
	return result, rows.Err()
}

// GetTransactions — страница журнала с фильтрами. Идентификаторы ищутся точно,
// по разобранному uuid; телефон — по цифрам. Счётчик считается только по
// просьбе: COUNT(*) по всей выборке стоит не меньше самой страницы.
func (r *transactionRepo) GetTransactions(ctx context.Context, f TransactionsFilter) ([]*Transaction, int, error) {
	where := "WHERE 1=1"
	var args []interface{}

	if search := strings.TrimSpace(f.Search); search != "" {
		var conds []string
		if id, ok := searchUUID(search); ok {
			args = append(args, id)
			n := len(args)
			conds = append(conds,
				fmt.Sprintf("t.id = $%d", n),
				fmt.Sprintf("t.order_id = $%d", n),
				fmt.Sprintf("t.admin_id = $%d", n))
		}
		// Телефон хранится как +79997454656, а вводят его с маской, поэтому обе
		// стороны приводятся к цифрам. Цифровое условие добавляется, только если
		// в запросе есть цифры: иначе пустой шаблон совпал бы со всем.
		if digits := digitsOnly(search); digits != "" {
			args = append(args, "%"+digits+"%")
			conds = append(conds, fmt.Sprintf(
				"regexp_replace(u.phone, '[^0-9]', '', 'g') LIKE $%d", len(args)))
		}
		if len(conds) == 0 {
			// Ни uuid, ни цифр: такой строке не соответствует ничего.
			where += " AND FALSE"
		} else {
			where += " AND (" + strings.Join(conds, " OR ") + ")"
		}
	}

	if txType := strings.TrimSpace(f.Type); txType != "" {
		args = append(args, txType)
		where += fmt.Sprintf(" AND t.type = $%d", len(args))
	}

	where, args = periodArgs(where, args, "t.created_at", f.Period)

	from := `
		FROM transactions t
		JOIN users u ON t.user_id = u.id
		` + where

	var total int
	if f.Page.WithTotal {
		if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) "+from, args...).Scan(&total); err != nil {
			return nil, 0, err
		}
	}

	sortExpr, ok := transactionSorts[f.Sort]
	if !ok {
		sortExpr = transactionSorts["created_at"]
	}
	direction := "ASC"
	if f.Desc {
		direction = "DESC"
	}

	limit, offset := clampPage(f.Page.Limit, f.Page.Offset)
	args = append(args, limit, offset)
	query := fmt.Sprintf(`
		SELECT `+transactionColumns+`, u.phone
		%s
		ORDER BY %s %s NULLS LAST, t.created_at DESC
		LIMIT $%d OFFSET $%d`, from, sortExpr, direction, len(args)-1, len(args))

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	txs := make([]*Transaction, 0, limit)
	for rows.Next() {
		var phone string
		tx, err := scanTransaction(rows, &phone)
		if err != nil {
			return nil, 0, err
		}
		tx.UserPhone = phone
		txs = append(txs, tx)
	}
	return txs, total, rows.Err()
}

// TransactionFacets читает всю таблицу дважды (DISTINCT по типу и по месяцу),
// поэтому вызывающий обязан кэшировать результат, а не спрашивать на каждой
// странице: см. service.facetCache.
func (r *transactionRepo) TransactionFacets(ctx context.Context) (TransactionFacets, error) {
	facets := TransactionFacets{Types: []string{}, Periods: []string{}}

	typeRows, err := r.db.QueryContext(ctx,
		`SELECT DISTINCT type FROM transactions ORDER BY type`)
	if err != nil {
		return facets, err
	}
	defer typeRows.Close()
	for typeRows.Next() {
		var t string
		if err := typeRows.Scan(&t); err != nil {
			return facets, err
		}
		facets.Types = append(facets.Types, t)
	}
	if err := typeRows.Err(); err != nil {
		return facets, err
	}

	periodRows, err := r.db.QueryContext(ctx,
		`SELECT DISTINCT to_char(created_at, 'YYYY-MM') AS period
		 FROM transactions
		 ORDER BY period DESC`)
	if err != nil {
		return facets, err
	}
	defer periodRows.Close()
	for periodRows.Next() {
		var p string
		if err := periodRows.Scan(&p); err != nil {
			return facets, err
		}
		facets.Periods = append(facets.Periods, p)
	}
	return facets, periodRows.Err()
}

// GetUserTransactions отдаёт проводки одного пользователя. Отбор строгий, по
// user_id: карточка пользователя показывает его деньги и только его. Счётчик
// здесь считается всегда: выборка по индексу user_id, и карточка листает
// страницы без первой.
func (r *transactionRepo) GetUserTransactions(ctx context.Context, userID uuid.UUID, limit, offset int) ([]*Transaction, int, error) {
	limit, offset = clampPage(limit, offset)

	var total int
	if err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM transactions WHERE user_id = $1`, userID).Scan(&total); err != nil {
		return nil, 0, err
	}

	rows, err := r.db.QueryContext(ctx, `
		SELECT `+transactionColumns+`, u.phone
		FROM transactions t
		JOIN users u ON t.user_id = u.id
		WHERE t.user_id = $1
		ORDER BY t.created_at DESC
		LIMIT $2 OFFSET $3`, userID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	txs := make([]*Transaction, 0, limit)
	for rows.Next() {
		var phone string
		tx, err := scanTransaction(rows, &phone)
		if err != nil {
			return nil, 0, err
		}
		tx.UserPhone = phone
		txs = append(txs, tx)
	}
	return txs, total, rows.Err()
}
