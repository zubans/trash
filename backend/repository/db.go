package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// Querier — подмножество *sql.DB / *sql.Tx, которым пользуются репозитории.
// Приём этого интерфейса позволяет вызывающему выполнить метод репозитория
// внутри открытой транзакции, а не на неявном автокоммит-соединении.
// Используются методы с контекстом, чтобы отмена доходила до драйвера.
type Querier interface {
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
}

var (
	// ErrConflict возвращается, когда охраняемый UPDATE не затронул ни одной
	// строки, то есть сущность была не в том состоянии, какого ждал вызывающий. Обработчики отображают её в 409.
	ErrConflict = errors.New("state changed concurrently, operation not applied")

	// ErrInsufficientFunds возвращается, когда списание увело бы баланс ниже нуля.
	ErrInsufficientFunds = errors.New("insufficient balance")

	// ErrNotFound — общий предок всех «не найдено» репозиториев. Доменные
	// ошибки вроде ErrShopOrderNotFound оборачивают его, поэтому обработчик,
	// которому всё равно, чего именно нет, проверяет errors.Is(err, ErrNotFound)
	// один раз, а не перечисляет каждую сущность.
	ErrNotFound = errors.New("not found")
)

// rowScanner удовлетворяется и *sql.Row, и *sql.Rows: одна функция сканирования
// обслуживает и чтение одной строки, и цикл по выборке.
type rowScanner interface {
	Scan(dest ...interface{}) error
}

// unmarshalJSON разбирает JSON-колонку в dst; пустое значение (NULL или ”)
// оставляет dst как есть. Ошибка разбора возвращается: прежде она молча
// отбрасывалась, и повреждённое поле читалось как пустое.
func unmarshalJSON(raw []byte, dst interface{}) error {
	if len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, dst)
}

// exec выбирает Querier: открытую транзакцию вызывающего, если она передана, и
// пул соединений в противном случае. Единственная реализация на весь пакет:
// раньше у каждого репозитория была своя копия с тем же телом.
func exec(db *sql.DB, q Querier) Querier {
	if q == nil {
		return db
	}
	return q
}

// runInTx выполняет fn в транзакции и коммитит её, если fn вернула nil.
// Rollback отложен, поэтому паника внутри fn не оставит соединение с открытой
// транзакцией; после Commit он безвреден.
func runInTx(ctx context.Context, db *sql.DB, fn func(*sql.Tx) error) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

const (
	// DefaultHistoryPageSize ограничивает списки истории — прошлые заказы
	// исполнителя, проводки пользователя, — которые клиент запрашивает, не говоря,
	// сколько ему нужно. Это экраны, а не выгрузки: они рисуют недавнее окно, и
	// учётка с годами активности не должна уметь заставить один запрос прочитать
	// всё это.
	DefaultHistoryPageSize = 200
	// MaxHistoryPageSize ограничивает то, что может запросить явный limit.
	MaxHistoryPageSize = 1000
)

// historyLimit приводит переданный вызывающим limit к этим границам.
func historyLimit(limit int) int {
	switch {
	case limit <= 0:
		return DefaultHistoryPageSize
	case limit > MaxHistoryPageSize:
		return MaxHistoryPageSize
	default:
		return limit
	}
}

// idList отдаёт дедуплицированный набор id как список позиционных
// плейсхолдеров ("$1, $2, $3") вместе с аргументами для привязки — для
// пакетных чтений, заменяющих построчные запросы в списковых эндпоинтах.
//
// Дедупликация здесь не менее важна, чем пакетность: страница заказов одного
// заказчика иначе привязывала бы его id по разу на строку. Пустой вход даёт
// пустой список, который вызывающие обязаны читать как «нечего доставать», а
// не передавать в запрос — "IN ()" не является допустимым SQL.
func idList(ids []uuid.UUID) (string, []interface{}) {
	seen := make(map[uuid.UUID]struct{}, len(ids))
	placeholders := make([]string, 0, len(ids))
	args := make([]interface{}, 0, len(ids))
	for _, id := range ids {
		if id == uuid.Nil {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		args = append(args, id)
		placeholders = append(placeholders, fmt.Sprintf("$%d", len(args)))
	}
	return strings.Join(placeholders, ", "), args
}

// execExpectingOne выполняет охраняемый оператор и падает с ErrConflict, когда
// охрана в WHERE не совпала. Молча ничего не делающие обновления — корневая
// причина целого класса багов с двойными возвратами, поэтому любой переход
// состояния обязан идти через этого помощника.
func execExpectingOne(ctx context.Context, q Querier, query string, args ...interface{}) error {
	res, err := q.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrConflict
	}
	return nil
}
