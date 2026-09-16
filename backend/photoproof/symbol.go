// Package photoproof — фото-подтверждение выполнения заказа: жесты («водяные
// знаки»), которые исполнитель показывает в кадре, сами снимки и их проверка.
//
// Модуль держит свою схему, свои правила и свои обработчики: с остальным
// приложением он связан двумя тонкими местами — заказ спрашивает у него жест
// при взятии и разрешение закрыть заказ при отметке «Исполнил», а арбитраж
// спрашивает снимки. План механики —
// doc/implementation_plan_disputes_penalties_photo_proof.md.
package photoproof

import (
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// Symbol — жест, который исполнитель показывает рядом с объектом заказа.
type Symbol struct {
	ID   uuid.UUID `json:"id"`
	Code string    `json:"code"`
	// Number — постоянный небольшой номер жеста. Назначается один раз и не
	// переиспользуется даже после удаления: на него ссылаются уже сделанные
	// снимки.
	Number       int    `json:"number"`
	Title        string `json:"title"`
	Description  string `json:"description"`
	HintImageURL string `json:"hint_image_url,omitempty"`
	// FitsInSelfie — жест, который помещается в селфи с заказчиком. У жестов
	// ногой его нет, и подсказка к селфи тогда жеста не требует.
	FitsInSelfie bool       `json:"fits_in_selfie"`
	SortOrder    int        `json:"sort_order"`
	DeletedAt    *time.Time `json:"deleted_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

// Live сообщает, участвует ли жест в выдаче новым заказам.
func (s *Symbol) Live() bool { return s != nil && s.DeletedAt == nil }

// Ошибки правки жестов.
var (
	ErrSymbolCode       = errors.New("код жеста: латиница, цифры и подчёркивание, от 2 до 32 символов")
	ErrSymbolTitle      = errors.New("у жеста должно быть название")
	ErrSymbolTooLong    = errors.New("описание жеста слишком длинное")
	ErrSymbolCodeTaken  = errors.New("жест с таким кодом уже есть")
	ErrSymbolNotFound   = errors.New("жест не найден")
	ErrNoSymbolsDefined = errors.New("в справочнике нет ни одного действующего жеста")
)

const maxSymbolDescriptionRunes = 1000

var symbolCodePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{1,31}$`)

// normalize приводит поля к хранимому виду и проверяет их. Код нормализуется к
// нижнему регистру: он попадает в подсказки клиента и в сравнение, и «OK» с
// «ok» не должны оказаться разными жестами.
func (s *Symbol) normalize() error {
	s.Code = strings.ToLower(strings.TrimSpace(s.Code))
	s.Title = strings.TrimSpace(s.Title)
	s.Description = strings.TrimSpace(s.Description)
	s.HintImageURL = strings.TrimSpace(s.HintImageURL)

	if !symbolCodePattern.MatchString(s.Code) {
		return ErrSymbolCode
	}
	if s.Title == "" {
		return ErrSymbolTitle
	}
	if utf8.RuneCountInString(s.Description) > maxSymbolDescriptionRunes {
		return ErrSymbolTooLong
	}
	return nil
}
