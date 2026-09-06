package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"healthlogin/backend/behavior"
	"healthlogin/backend/repository"
)

// Отправка данных на проверку и то, к чему ведёт несовпадение.
//
// Форма потока идёт от услуги верификации: модератору показывают адрес и ничего
// больше о заказчике, он вводит то, что написано в документе, и сравнивает это
// *система*. Ни модератор, ни
// скрипт никогда не получают сохранённых значений — сравнение происходит здесь,
// и дальше едет только его результат. Поэтому несовпадение — не информация о
// заказчике; это «это не совпало», а больше ни для каких действий и не нужно.
//
// Что решает поведение: сколько будет попыток, что говорит предупреждение и
// когда случай уходит администратору. Что решает этот файл: кто может
// отправлять, что сравнивается и что сравнение честное.

// Поля, которые ядро умеет сравнивать. Поведению, назвавшему что-то ещё,
// отказывают, а не проверяют молча ничего.
const (
	FieldLastName   = "last_name"
	FieldFirstName  = "first_name"
	FieldPatronymic = "patronymic"
	FieldBirthDate  = "birth_date"
)

// ErrSubmissionNotSupported сообщает, что эта услуга не принимает отправок.
var ErrSubmissionNotSupported = errors.New("для этой услуги проверка данных не предусмотрена")

// ErrSubmissionEscalated сообщает, что случай уже у администратора.
var ErrSubmissionEscalated = errors.New("заказ передан на модерацию администратору")

// SubmissionResult — то, что приложение исполнителя получает сразу.
// Собственные эффекты поведения — предупреждение, эскалация, закрытие заказа —
// применяются до этого возврата, поэтому ответ на экране и есть исход, а не
// обещание исхода.
type SubmissionResult struct {
	Attempt    int      `json:"attempt"`
	Matched    bool     `json:"matched"`
	Escalated  bool     `json:"escalated"`
	Mismatched []string `json:"mismatched_fields,omitempty"`
	// Messages — то, что поведение об этом сказало, в том порядке, в каком сказало.
	Messages []string `json:"messages,omitempty"`
}

// SubmitOrderData записывает отправленное исполнителем по заказу, сравнивает с
// записью заказчика и запускает поведение по результату.
func (d *BehaviorDispatcher) SubmitOrderData(ctx context.Context, orderID, executorID uuid.UUID, fields map[string]string) (*SubmissionResult, error) {
	if d == nil || d.submissions == nil {
		return nil, ErrSubmissionNotSupported
	}

	order, err := d.orders.GetOrderByID(ctx, orderID)
	if err != nil {
		return nil, errors.New("order not found")
	}
	if order.ExecutorID == nil || *order.ExecutorID != executorID {
		return nil, errors.New("order is not assigned to this executor")
	}
	if order.Status != repository.OrderStatusAssigned && order.Status != repository.OrderStatusExecuted {
		return nil, errors.New("order is not in progress")
	}

	variant, err := d.catalog.GetNodeByID(ctx, order.ServiceVariantID)
	if err != nil {
		return nil, err
	}
	manifest, ok := d.behaviors.Manifest(variant)
	if !ok || len(manifest.CheckFields) == 0 {
		return nil, ErrSubmissionNotSupported
	}

	// Случай, уже находящийся у администратора, попыток больше не принимает: в
	// этом и был смысл эскалации.
	escalated, err := d.submissions.HasOpenEscalation(ctx, orderID)
	if err != nil {
		return nil, err
	}
	if escalated {
		return nil, ErrSubmissionEscalated
	}

	customer, err := d.users.FindByID(ctx, order.CustomerID)
	if err != nil {
		return nil, errors.New("customer not found")
	}

	matches, mismatched, err := compareCustomerFields(customer, manifest.CheckFields, fields)
	if err != nil {
		return nil, err
	}

	submission := &repository.OrderSubmission{
		OrderID:    orderID,
		ExecutorID: executorID,
		Matched:    len(mismatched) == 0,
		Fields:     normalizeSubmitted(manifest.CheckFields, fields),
		Mismatches: mismatched,
	}

	event := &repository.DomainEvent{
		Type:        repository.EventOrderSubmission,
		SubjectType: repository.EventSubjectOrder,
		SubjectID:   orderID,
		ActorID:     &executorID,
	}

	// Попытка и порождаемое ею событие коммитятся вместе: попытка, которой
	// поведение не видело, позволила бы исполнителю пробовать бесплатно, а
	// событие без попытки за ним засчитало бы то, чего не было.
	if err := d.ledger.RunInTx(ctx, func(tx *sql.Tx) error {
		if err := d.submissions.Record(ctx, tx, submission); err != nil {
			return err
		}
		// Скрипту сообщается номер попытки в текущем круге, а не сквозной.
		// Круг начинается заново каждый раз, когда администратор снимает заказ с
		// модерации: в этом и смысл снятия — вернуть заказ исполнителю с полным
		// набором попыток. Сквозной номер остаётся в строке, его читает
		// администратор, разбирающий всю историю ввода.
		roundAttempt, err := d.submissions.AttemptsSinceEscalation(ctx, tx, orderID)
		if err != nil {
			return err
		}
		event.Payload = map[string]interface{}{
			"attempt":    roundAttempt,
			"matched":    submission.Matched,
			"matches":    matches,
			"mismatches": mismatched,
		}
		return d.events.Publish(ctx, tx, event)
	}); err != nil {
		return nil, err
	}

	// Обрабатывается здесь, а не на следующем тике воркера: кто-то стоит перед
	// заказчиком и ждёт ответа, совпало ли. Сбой не фатален — событие остаётся
	// необработанным, и воркер его повторит, — но ответ тогда приходит поздно,
	// поэтому о сбое сообщается.
	messages, err := d.dispatch(ctx, event)
	if err != nil {
		_ = d.events.MarkFailed(ctx, repository.ConsumerBehaviors, event.ID, err.Error())
		return nil, err
	}
	if err := d.events.MarkProcessed(ctx, repository.ConsumerBehaviors, event.ID); err != nil {
		return nil, err
	}

	nowEscalated, err := d.submissions.HasOpenEscalation(ctx, orderID)
	if err != nil {
		return nil, err
	}
	return &SubmissionResult{
		Attempt:    submission.Attempt,
		Matched:    submission.Matched,
		Escalated:  nowEscalated,
		Mismatched: mismatched,
		Messages:   messages,
	}, nil
}

// compareCustomerFields сверяет отправленные значения с записью заказчика.
// Имена сравниваются без учёта регистра и со сведением «ё» к «е», потому что у
// несовпадения есть последствия, а привычка набора — не повод для эскалации;
// дата рождения сравнивается как дата.
func compareCustomerFields(customer *repository.User, checkFields []string, submitted map[string]string) (map[string]bool, []string, error) {
	matches := make(map[string]bool, len(checkFields))
	mismatched := []string{}

	for _, field := range checkFields {
		value := strings.TrimSpace(submitted[field])
		var ok bool
		switch field {
		case FieldLastName:
			ok = sameName(value, customer.LastName)
		case FieldFirstName:
			ok = sameName(value, customer.FirstName)
		case FieldPatronymic:
			// Пустое отчество с обеих сторон — совпадение: оно есть не у всех.
			ok = sameName(value, customer.Patronymic)
		case FieldBirthDate:
			ok = sameBirthDate(value, customer.BirthDate)
		default:
			return nil, nil, fmt.Errorf("behavior asks to check unknown field %q", field)
		}
		matches[field] = ok
		if !ok {
			mismatched = append(mismatched, field)
		}
	}
	return matches, mismatched, nil
}

func sameName(submitted, stored string) bool {
	return foldName(submitted) == foldName(stored)
}

func foldName(value string) string {
	return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(value)), "ё", "е")
}

func sameBirthDate(submitted string, stored *time.Time) bool {
	if stored == nil {
		return false
	}
	parsed, err := time.Parse("2006-01-02", strings.TrimSpace(submitted))
	if err != nil {
		return false
	}
	return parsed.Year() == stored.Year() && parsed.Month() == stored.Month() && parsed.Day() == stored.Day()
}

// normalizeSubmitted оставляет только поля, которые запросило поведение, чтобы
// клиент не мог сохранить на заказе произвольный текст, добавив ключи в запрос.
func normalizeSubmitted(checkFields []string, submitted map[string]string) map[string]string {
	out := make(map[string]string, len(checkFields))
	for _, field := range checkFields {
		out[field] = strings.TrimSpace(submitted[field])
	}
	return out
}

// submissionFacts превращает нагрузку события обратно в то, что видит скрипт.
func submissionFacts(event *repository.DomainEvent, escalated bool) *behavior.SubmissionFacts {
	if event.Type != repository.EventOrderSubmission {
		return nil
	}
	facts := &behavior.SubmissionFacts{Matches: map[string]bool{}, Escalated: escalated}
	// Нагрузка — число, каким бы путём оно ни пришло: как float64 из JSONB или
	// как int из транзакции, которая его только что записала. Чтение лишь одного
	// из двух молча делало бы любую попытку «попыткой 0», и эскалация после
	// последней попытки не случилась бы никогда.
	switch attempt := event.Payload["attempt"].(type) {
	case float64:
		facts.Attempt = int(attempt)
	case int:
		facts.Attempt = attempt
	case int64:
		facts.Attempt = int(attempt)
	case json.Number:
		if parsed, err := attempt.Int64(); err == nil {
			facts.Attempt = int(parsed)
		}
	}
	if matched, ok := event.Payload["matched"].(bool); ok {
		facts.AllMatch = matched
	}
	switch matches := event.Payload["matches"].(type) {
	case map[string]interface{}:
		for field, value := range matches {
			result, _ := value.(bool)
			facts.Matches[field] = result
		}
	case map[string]bool:
		for field, value := range matches {
			facts.Matches[field] = value
		}
	}
	return facts
}
