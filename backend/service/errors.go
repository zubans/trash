package service

import (
	"database/sql"
	"errors"

	"healthlogin/backend/repository"
)

// Ошибки домена заказов и его соседей: ставок, смен, отзывов, споров.
//
// У каждой ошибки два лица. Класс — один из сентинелов ниже — говорит
// обработчику, какой HTTP-код ответить (handler/errors.go сопоставляет их
// через errors.Is). Текст — то, что увидит человек в приложении. Раньше
// обработчики либо отдавали 422 на всё подряд, либо сравнивали тексты; теперь
// ни то ни другое не нужно: у ошибки спрашивают класс, а текст показывают как есть.

// Классы. Обработчик отвечает по ним:
//
//	repository.ErrNotFound         — 404
//	ErrForbidden, *NotEligible      — 403
//	repository.ErrConflict, ErrOrderState, ErrRule — 409
//	repository.ErrInsufficientFunds, ErrValidation — 422
//	ErrNotConfigured и всё прочее   — 500, без текста наружу
var (
	// ErrForbidden — действие над чужим заказом, чатом или ставкой.
	ErrForbidden = errors.New("доступ запрещён")
	// ErrNotConfigured — сервис собран без зависимости, которая нужна этому
	// действию. Это ошибка сборки, а не запроса, поэтому клиенту она видна как сбой.
	ErrNotConfigured = errors.New("service is not configured")
	// ErrOrderState — заказ не в том состоянии, какого требует действие:
	// подтвердить неназначенный, отменить закрытый, дать чаевые дважды.
	ErrOrderState = errors.New("order is in the wrong state")
	// ErrRule — правило площадки не пускает: лимит заказов, радиус взятия,
	// отсутствие смены, баланс ниже допустимого.
	ErrRule = errors.New("business rule violated")
	// ErrValidation — данные запроса негодны сами по себе: отрицательная сумма,
	// оба флага срочности, пустая претензия.
	ErrValidation = errors.New("invalid input")
)

// DomainError — ошибка с текстом для человека и классом для кода:
// errors.Is(err, Kind) истинно, Error() отдаёт только текст. Так у отказа
// «превышен лимит активных заказов (не более 3)» остаётся своё сообщение, и
// при этом обработчик знает, что это ErrRule, а не сбой базы.
type DomainError struct {
	Kind error
	Msg  string
	// Cause — исходная ошибка, если она несёт больше, чем класс: отказ скрипта
	// поведения (*behavior.DeniedError) остаётся доступным через errors.As.
	Cause error
}

func (e *DomainError) Error() string { return e.Msg }

// Unwrap отдаёт класс и причину, чтобы errors.Is видел класс сквозь текст, а
// errors.As доставал причину.
func (e *DomainError) Unwrap() []error {
	if e.Cause == nil {
		return []error{e.Kind}
	}
	return []error{e.Kind, e.Cause}
}

func notFoundError(msg string) error   { return &DomainError{Kind: repository.ErrNotFound, Msg: msg} }
func forbiddenError(msg string) error  { return &DomainError{Kind: ErrForbidden, Msg: msg} }
func stateError(msg string) error      { return &DomainError{Kind: ErrOrderState, Msg: msg} }
func ruleError(msg string) error       { return &DomainError{Kind: ErrRule, Msg: msg} }
func validationError(msg string) error { return &DomainError{Kind: ErrValidation, Msg: msg} }
func conflictError(msg string) error   { return &DomainError{Kind: repository.ErrConflict, Msg: msg} }

// Конкретные ошибки, на которые смотрят тесты, соседние сервисы и обработчики.
var (
	ErrOrderNotFound   = notFoundError("заказ не найден")
	ErrUserNotFound    = notFoundError("пользователь не найден")
	ErrBidNotFound     = notFoundError("предложение не найдено")
	ErrShiftNotFound   = notFoundError("смена не найдена")
	ErrDisputeNotFound = notFoundError("спор не найден")
	// ErrVerificationRequestNotFound — у исполнителя нет открытой заявки на верификацию.
	ErrVerificationRequestNotFound = notFoundError("заявка на верификацию не найдена")

	// ErrNoActiveShift — действие требует смены, а её нет.
	ErrNoActiveShift = ruleError("executor has no active shift")
	// ErrExecutorPenalized — смена исполнителя закрыта штрафом.
	ErrExecutorPenalized = ruleError("executor is penalized")
	// ErrOwnOrder — исполнитель пытается работать по собственному заказу.
	ErrOwnOrder = ruleError("нельзя брать собственный заказ")
	// ErrWorkPositionUnknown — исполнитель не выбрал район на карте.
	ErrWorkPositionUnknown = ruleError("рабочая позиция не задана: откройте карту и выберите район, чтобы брать заказы")
	// ErrShiftAlreadyActive — вторую смену одновременно открыть нельзя.
	ErrShiftAlreadyActive = stateError("active shift already exists")

	// ErrOrderTaken — заказ достался другому исполнителю раньше.
	ErrOrderTaken = conflictError("заказ уже взят другим исполнителем")
	// ErrOrderNotAssignedToExecutor — заказ не назначен этому исполнителю (или уже не в работе).
	ErrOrderNotAssignedToExecutor = stateError("order is not assigned to this executor")
	// ErrOrderNotConfirmable — подтвердить можно назначенный или исполненный заказ.
	ErrOrderNotConfirmable = stateError("order must be assigned or marked as executed before confirmation")
	// ErrOrderHasNoExecutor — у заказа нет исполнителя, платить некому.
	ErrOrderHasNoExecutor = stateError("order has no executor")
	// ErrOrderNotCancelable — заказ уже нельзя отменить из его статуса.
	ErrOrderNotCancelable = stateError("order cannot be canceled")
	// ErrTipNotAllowed — чаевые только по завершённому заказу.
	ErrTipNotAllowed = stateError("tips can only be sent for completed orders")
	// ErrTipAlreadySent — по заказу чаевые уже были.
	ErrTipAlreadySent = stateError("this order has already been tipped")
	// ErrServiceAlreadyOrdered — услугу «один раз на пользователя» уже заказывали.
	ErrServiceAlreadyOrdered = stateError("услуга уже была заказана")
	// ErrOrderNotOnReview — вернуть в работу можно только заказ на проверке.
	ErrOrderNotOnReview = stateError("вернуть в работу можно только заказ на проверке")
	// ErrManualExecuteDisabled — услуга запрещает отмечать заказ исполненным вручную.
	ErrManualExecuteDisabled = stateError("этот заказ закрывается автоматически после проверки, отметить его исполненным вручную нельзя")
	// ErrReviewAlreadySubmitted — второй отзыв по заказу от того же автора.
	ErrReviewAlreadySubmitted = stateError("you have already submitted a review for this order")
	// ErrOrderNotInProgress — действие возможно только по заказу в работе
	// (назначен или исполнен, ждёт подтверждения).
	ErrOrderNotInProgress = stateError("заказ не в работе")
	// ErrNotAssigned — заказ назначен другому исполнителю.
	ErrNotAssigned = stateError("заказ назначен не вам")
	// ErrOrderNotBiddable — ставки принимаются только по заказу в поиске.
	ErrOrderNotBiddable = stateError("order is not open for bidding")
	// ErrNotAuction — услуга заказа не аукционная: ставок по ней нет.
	ErrNotAuction = ruleError("cannot bid on non-auction orders")

	// ErrInsufficientBalance — на балансе не хватает на удержание или чаевые.
	// Класс — repository.ErrInsufficientFunds, чтобы обработчик и тесты видели
	// ту же ошибку, что отдаёт реестр.
	ErrInsufficientBalance = &DomainError{Kind: repository.ErrInsufficientFunds, Msg: "недостаточно средств"}

	// ErrInvalidServiceVariant — id варианта не указывает на вариант услуги.
	ErrInvalidServiceVariant = validationError("invalid service variant")
	// ErrServiceVariantUnavailable — вариант списан или выключен.
	ErrServiceVariantUnavailable = validationError("service variant is not available")
)

// orderNotFound переводит «нет строки» репозитория в ошибку домена, а любой
// другой сбой чтения оставляет как есть: сбой базы — не «заказ не найден».
func orderNotFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) || errors.Is(err, repository.ErrNotFound) {
		return ErrOrderNotFound
	}
	return err
}

// userNotFound — то же для пользователей.
func userNotFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) || errors.Is(err, repository.ErrNotFound) {
		return ErrUserNotFound
	}
	return err
}
