package repository

import "errors"

// Ошибки подтверждения почты. Раньше репозиторий отдавал их голым текстом
// («verification_token_expired»), и обработчик сравнивал строки; теперь у
// каждого исхода свой сентинел, на который смотрят через errors.Is.
var (
	// ErrVerificationTokenExpired — ссылка подтверждения была, но её срок вышел.
	ErrVerificationTokenExpired = errors.New("verification token expired")
	// ErrVerificationTokenInvalid — такой ссылки нет: она уже использована,
	// подделана или никогда не выдавалась. Класс — ErrNotFound.
	ErrVerificationTokenInvalid = &notFoundError{"invalid or expired verification token"}
)

// notFoundError — конкретное «не найдено», через Unwrap принадлежащее классу
// ErrNotFound.
type notFoundError struct{ msg string }

func (e *notFoundError) Error() string { return e.msg }
func (e *notFoundError) Unwrap() error { return ErrNotFound }

// Ошибки сброса пароля по коду. Текст — для человека, сентинел — для
// сервиса: он переводит их в ошибку валидации, а обработчик отвечает по классу.
var (
	// ErrResetCodeInvalid — код не подошёл или его срок вышел.
	ErrResetCodeInvalid = errors.New("неверный или истекший код сброса")
	// ErrResetCodeAttemptsExceeded — попытки кончились, нужен новый код.
	ErrResetCodeAttemptsExceeded = errors.New("превышено число попыток, запросите новый код")
)

// ErrUserNotFound — пользователя с таким id нет. Класс — ErrNotFound.
var ErrUserNotFound = &notFoundError{"user not found"}
