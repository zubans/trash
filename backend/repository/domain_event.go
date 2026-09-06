package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Типы доменных событий. Они называют то, что произошло, и никогда — то, что
// должно произойти: что с этим делать — решение поведения, и два поведения
// вполне могут хотеть от одного события разного.
const (
	EventUserVerified   = "user.verified"
	EventOrderCreated   = "order.created"
	EventOrderAccepted  = "order.accepted"
	EventOrderExecuted  = "order.executed"
	EventOrderConfirmed = "order.confirmed"
	EventOrderCanceled  = "order.canceled"
	// EventOrderSubmission несёт данные, отправленные исполнителем на проверку, и
	// то, как они сравнились, — но никогда значения, с которыми сравнивали.
	EventOrderSubmission = "order.submission"
)

// Типы субъектов события. Субъект решает, кому событие доставят: событие заказа
// уходит поведению этого заказа, событие пользователя — поведениям его открытых
// заказов.
const (
	EventSubjectUser  = "user"
	EventSubjectOrder = "order"
)

// DomainEvent — одна строка транзакционного outbox.
//
// Outbox существует потому, что две половины скриптовой услуги не должны
// разъезжаться. «Модератор отметил визит выполненным» — это изменение
// состояния; «проверяющему заплачено» — то, что поведение решает в ответ. Если
// бы второе делалось в том же запросе, сбой в нём откатил бы и первое; если бы
// делалось после, падение между ними его потеряло бы. Записать событие вместе с
// изменением состояния, а действовать по нему позже — единственная схема, где
// невозможно ни то ни другое.
type DomainEvent struct {
	ID          uuid.UUID              `json:"id"`
	Type        string                 `json:"type"`
	SubjectType string                 `json:"subject_type"`
	SubjectID   uuid.UUID              `json:"subject_id"`
	ActorID     *uuid.UUID             `json:"actor_id,omitempty"`
	Payload     map[string]interface{} `json:"payload,omitempty"`
	CreatedAt   time.Time              `json:"created_at"`
	Attempts    int                    `json:"attempts"`
}

// ErrEffectAlreadyApplied сообщает, что эффект с этим ключом идемпотентности уже
// применён. Это нормальный исход — переотправленное событие, просящее уже
// сделанную выплату, — и вызывающие пропускают эффект, а не падают.
var ErrEffectAlreadyApplied = errors.New("behavior effect already applied")

// Потребители outbox. Каждый читает все события со своим курсором: событие,
// обработанное поведениями услуг, для ачивок всё ещё не обработано, и наоборот.
// Общий курсор означал бы, что кто первый успел, тот у второго событие и забрал.
const (
	ConsumerBehaviors    = "behaviors"
	ConsumerAchievements = "achievements"
)

// EventRepository хранит доменные события и применённые в ответ эффекты.
type EventRepository interface {
	// RunInTx выполняет fn в транзакции — для вызывающих, которым надо опубликовать
	// событие вместе с изменением, которое оно описывает.
	RunInTx(ctx context.Context, fn func(*sql.Tx) error) error
	// Publish добавляет событие. Он принимает Querier, потому что почти всегда
	// вызывается внутри чужой транзакции — в этом весь его смысл.
	Publish(ctx context.Context, q Querier, event *DomainEvent) error
	// ClaimPending возвращает самые старые события, ещё не обработанные этим
	// потребителем, и засчитывает каждому попытку, чтобы событие, всякий раз
	// убивающее диспетчер, нельзя было повторять вечно.
	ClaimPending(ctx context.Context, consumer string, limit, maxAttempts int) ([]*DomainEvent, error)
	MarkProcessed(ctx context.Context, consumer string, id uuid.UUID) error
	MarkFailed(ctx context.Context, consumer string, id uuid.UUID, reason string) error
	// RecordEffect занимает ключ идемпотентности внутри транзакции применителя.
	// Возвращает ErrEffectAlreadyApplied, когда ключ занят, — так вознаграждение
	// платится один раз, сколько бы событий его ни описывало.
	RecordEffect(ctx context.Context, q Querier, key string, eventID uuid.UUID, behaviorCode, kind string, payload map[string]interface{}) error
	// CountPending сообщает размер очереди этого потребителя — для метрик.
	CountPending(ctx context.Context, consumer string) (int, error)
	// PurgeProcessed удаляет события, обработанные раньше заданного окна.
	// Обработанные строки — это история, а история, которую никто не читает, всё
	// равно обязана перестать расти.
	PurgeProcessed(ctx context.Context, olderThan time.Duration) (int64, error)
}

type eventRepo struct {
	db *sql.DB
}

// NewEventRepository создаёт EventRepository.
func NewEventRepository(db *sql.DB) EventRepository {
	return &eventRepo{db: db}
}

func (r *eventRepo) RunInTx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *eventRepo) Publish(ctx context.Context, q Querier, event *DomainEvent) error {
	if event.ID == uuid.Nil {
		event.ID = uuid.New()
	}
	payload := []byte("{}")
	if len(event.Payload) > 0 {
		encoded, err := json.Marshal(event.Payload)
		if err != nil {
			return err
		}
		payload = encoded
	}
	exec := Querier(r.db)
	if q != nil {
		exec = q
	}
	_, err := exec.ExecContext(ctx, `
        INSERT INTO domain_events (id, type, subject_type, subject_id, actor_id, payload)
        VALUES ($1, $2, $3, $4, $5, $6)
    `, event.ID, event.Type, event.SubjectType, event.SubjectID, event.ActorID, payload)
	return err
}

func (r *eventRepo) ClaimPending(ctx context.Context, consumer string, limit, maxAttempts int) ([]*DomainEvent, error) {
	if limit <= 0 {
		limit = 50
	}
	// Захват — это вставка курсора, а не пометка в самом событии: у события их
	// столько, сколько потребителей. Строки курсора у ещё не читанного события
	// нет вовсе, поэтому первый захват её создаёт, а повторный увеличивает
	// счётчик попыток — ON CONFLICT здесь и есть то, что делает второй процесс
	// безвредным, а не дублирующим работу.
	rows, err := r.db.QueryContext(ctx, `
        WITH claimed AS (
            INSERT INTO domain_event_consumers (event_id, consumer, attempts)
            SELECT e.id, $1, 1
            FROM domain_events e
            LEFT JOIN domain_event_consumers c
                   ON c.event_id = e.id AND c.consumer = $1
            WHERE c.event_id IS NULL
               OR (c.processed_at IS NULL AND c.attempts < $3)
            ORDER BY e.created_at
            LIMIT $2
            ON CONFLICT (event_id, consumer)
            DO UPDATE SET attempts = domain_event_consumers.attempts + 1
            RETURNING event_id, attempts
        )
        SELECT e.id, e.type, e.subject_type, e.subject_id, e.actor_id, e.payload, e.created_at, claimed.attempts
        FROM claimed
        JOIN domain_events e ON e.id = claimed.event_id
        ORDER BY e.created_at
    `, consumer, limit, maxAttempts)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []*DomainEvent
	for rows.Next() {
		var e DomainEvent
		var payload []byte
		if err := rows.Scan(&e.ID, &e.Type, &e.SubjectType, &e.SubjectID, &e.ActorID, &payload, &e.CreatedAt, &e.Attempts); err != nil {
			return nil, err
		}
		if len(payload) > 0 {
			_ = json.Unmarshal(payload, &e.Payload)
		}
		events = append(events, &e)
	}
	return events, rows.Err()
}

func (r *eventRepo) MarkProcessed(ctx context.Context, consumer string, id uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `
        UPDATE domain_event_consumers SET processed_at = now(), last_error = NULL
        WHERE event_id = $1 AND consumer = $2
    `, id, consumer)
	return err
}

func (r *eventRepo) MarkFailed(ctx context.Context, consumer string, id uuid.UUID, reason string) error {
	// Курсор намеренно остаётся необработанным: событие повторяют, пока не выйдет
	// или пока не кончатся попытки, а последняя ошибка сохраняется, чтобы причина
	// была видна без чтения логов.
	_, err := r.db.ExecContext(ctx, `
        UPDATE domain_event_consumers SET last_error = $3
        WHERE event_id = $1 AND consumer = $2
    `, id, consumer, reason)
	return err
}

func (r *eventRepo) RecordEffect(ctx context.Context, q Querier, key string, eventID uuid.UUID, behaviorCode, kind string, payload map[string]interface{}) error {
	encoded := []byte("{}")
	if len(payload) > 0 {
		data, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		encoded = data
	}
	exec := Querier(r.db)
	if q != nil {
		exec = q
	}
	err := execExpectingOne(ctx, exec, `
        INSERT INTO behavior_effects (idempotency_key, event_id, behavior_code, kind, payload)
        VALUES ($1, $2, $3, $4, $5)
        ON CONFLICT (idempotency_key) DO NOTHING
    `, key, eventID, behaviorCode, kind, encoded)
	if errors.Is(err, ErrConflict) {
		return ErrEffectAlreadyApplied
	}
	return err
}

func (r *eventRepo) PurgeProcessed(ctx context.Context, olderThan time.Duration) (int64, error) {
	if olderThan <= 0 {
		return 0, nil
	}
	// Эффекты и курсоры удаляются вместе со своим событием (ON DELETE CASCADE).
	// Это безопасно только потому, что окно намного длиннее любой переотправки:
	// ключ идемпотентности обязан пережить каждую повторную попытку занявшего его
	// события.
	//
	// Удаляется только то, что обработали все потребители. Событие без единого
	// курсора не трогается вовсе: отсутствие курсоров означает, что его никто
	// ещё не читал, а не что читать нечего.
	result, err := r.db.ExecContext(ctx, `
        DELETE FROM domain_events e
        WHERE e.created_at < now() - $1::interval
          AND EXISTS (SELECT 1 FROM domain_event_consumers c
                       WHERE c.event_id = e.id AND c.processed_at IS NOT NULL)
          AND NOT EXISTS (SELECT 1 FROM domain_event_consumers c
                           WHERE c.event_id = e.id AND c.processed_at IS NULL)
    `, fmt.Sprintf("%d seconds", int64(olderThan.Seconds())))
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (r *eventRepo) CountPending(ctx context.Context, consumer string) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx, `
        SELECT COUNT(*)
        FROM domain_events e
        LEFT JOIN domain_event_consumers c
               ON c.event_id = e.id AND c.consumer = $1
        WHERE c.event_id IS NULL OR c.processed_at IS NULL
    `, consumer).Scan(&count)
	return count, err
}
