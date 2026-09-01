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

// Submitting data for a check, and what a mismatch leads to.
//
// The shape of the flow comes from the verification service: the moderator is
// shown the address and nothing else about the customer, types in what the
// document says, and the *system* compares it. Neither the moderator nor the
// script ever receives the stored values — the comparison happens here, and only
// its result travels onward. A mismatch is therefore not information about the
// customer; it is "this did not match", which is all anybody needs to act on.
//
// What the behaviour decides: how many attempts there are, what the warning
// says, and when the case goes to an administrator. What this file decides:
// who may submit, what is compared, and that the comparison is honest.

// Fields the core knows how to compare. A behaviour naming anything else is
// refused rather than silently checking nothing.
const (
	FieldLastName   = "last_name"
	FieldFirstName  = "first_name"
	FieldPatronymic = "patronymic"
	FieldBirthDate  = "birth_date"
)

// ErrSubmissionNotSupported reports that this service does not take submissions.
var ErrSubmissionNotSupported = errors.New("для этой услуги проверка данных не предусмотрена")

// ErrSubmissionEscalated reports that the case is already with an administrator.
var ErrSubmissionEscalated = errors.New("заказ передан на модерацию администратору")

// SubmissionResult is what the executor's app gets back immediately. The
// behaviour's own effects — a warning, an escalation, closing the order — are
// applied before this returns, so the answer on screen is the outcome and not a
// promise of one.
type SubmissionResult struct {
	Attempt    int      `json:"attempt"`
	Matched    bool     `json:"matched"`
	Escalated  bool     `json:"escalated"`
	Mismatched []string `json:"mismatched_fields,omitempty"`
	// Messages are what the behaviour said about it, in the order it said them.
	Messages []string `json:"messages,omitempty"`
}

// SubmitOrderData records what an executor submitted for an order, compares it
// with the customer's record, and runs the behaviour on the result.
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

	// A case already with an administrator does not take more attempts: that is
	// what escalating it meant.
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

	// The attempt and the event it produces commit together: an attempt the
	// behaviour never saw would let the executor retry for free, and an event
	// with no attempt behind it would count one that never happened.
	if err := d.ledger.RunInTx(ctx, func(tx *sql.Tx) error {
		if err := d.submissions.Record(ctx, tx, submission); err != nil {
			return err
		}
		event.Payload = map[string]interface{}{
			"attempt":    submission.Attempt,
			"matched":    submission.Matched,
			"matches":    matches,
			"mismatches": mismatched,
		}
		return d.events.Publish(ctx, tx, event)
	}); err != nil {
		return nil, err
	}

	// Processed here rather than on the next worker tick: somebody is standing
	// in front of the customer waiting to hear whether it matched. A failure is
	// not fatal — the event stays unprocessed and the worker retries it — but
	// the answer then arrives late, so it is reported.
	messages, err := d.dispatch(ctx, event)
	if err != nil {
		_ = d.events.MarkFailed(ctx, event.ID, err.Error())
		return nil, err
	}
	if err := d.events.MarkProcessed(ctx, event.ID); err != nil {
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

// compareCustomerFields checks the submitted values against the customer's
// record. Names are compared case-insensitively and with "ё" folded to "е",
// because a mismatch has consequences and a keyboard habit is not one worth
// escalating; the birth date is compared as a date.
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
			// An empty patronymic on both sides is a match: not everybody has one.
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

// normalizeSubmitted keeps only the fields the behaviour asked for, so a client
// cannot store arbitrary text on the order by adding keys to the request.
func normalizeSubmitted(checkFields []string, submitted map[string]string) map[string]string {
	out := make(map[string]string, len(checkFields))
	for _, field := range checkFields {
		out[field] = strings.TrimSpace(submitted[field])
	}
	return out
}

// submissionFacts turns the event payload back into what the script sees.
func submissionFacts(event *repository.DomainEvent, escalated bool) *behavior.SubmissionFacts {
	if event.Type != repository.EventOrderSubmission {
		return nil
	}
	facts := &behavior.SubmissionFacts{Matches: map[string]bool{}, Escalated: escalated}
	// The payload is a number whichever way it arrived: as float64 out of JSONB,
	// or as an int from the transaction that has just written it. Reading only
	// one of the two would silently make every attempt "attempt 0", and the
	// escalation after the last attempt would never happen.
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
