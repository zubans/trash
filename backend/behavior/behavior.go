// Package behavior вычисляет правила «особой» услуги — такой, чьи условия не
// укладываются во флаги каталога, — в виде скрипта, который живёт вне
// Go-кода.
//
// Зачем вообще скрипт. Каждое необычное свойство, когда-либо понадобившееся
// услуге, превращалось в колонку плюс `if`: requires_verification (035),
// moderator_only (040), min_age. Услуге верификации нужны сразу четыре правила —
// видна только неверифицированным заказчикам, заказывается один раз, бесплатна
// и платит выполнившему её модератору, — и ни одно из них не переиспользуется
// как флаг. Следующей такой услуге понадобилось бы ещё четыре.
//
// Что скрипту можно и что нельзя. Скрипт поведения — чистая функция от фактов,
// которые ему передали: он не читает базу, не открывает сокетов, не хранит
// состояния между вызовами и никогда сам не двигает деньги. Он отвечает на
// вопросы («может ли этот пользователь это видеть?», «сколько это стоит?») и для
// событий возвращает список эффектов, которые просит применить. Ядро применяет
// их транзакционно, через тот же реестр, что и любой другой платёж. Эта граница
// и есть весь замысел: ошибочный скрипт может принять неверное решение, но не
// может породить одностороннее движение денег или несведённые книги.
package behavior

import (
	"fmt"
	"time"
)

// EffectKind называет одно действие, которое поведение может попросить у ядра.
// Набор намеренно закрыт: эффект — это Go-функция со своими проверками, а не
// лазейка к произвольным изменениям состояния.
type EffectKind string

const (
	// EffectCompleteOrder закрывает заказ и выплачивает ровно так же, как это
	// сделало бы подтверждение заказчика.
	EffectCompleteOrder EffectKind = "complete_order"
	// EffectCancelOrder отменяет заказ и возвращает всё, что он ещё удерживает.
	EffectCancelOrder EffectKind = "cancel_order"
	// EffectPayBonus начисляет пользователю — исполнителю, заказчику или обоим по
	// очереди — со счёта платформы BONUSES. Он обязан нести ключ идемпотентности:
	// в отличие от прочих, ему нечего проверить в состоянии — выплата дважды
	// выглядит ровно как выплата один раз.
	EffectPayBonus EffectKind = "pay_bonus"
	// EffectVerifyUser выставляет флаг ручной верификации. Ядро откажет, если заказ,
	// из которого он пришёл, выполнял не модератор (см.
	// service/behavior_dispatch.go): скрипт просит, а ядро решает, был ли просящий
	// вправе просить.
	EffectVerifyUser EffectKind = "verify_user"
	// EffectSystemMessage публикует системное сообщение в чат заказа. Применяется
	// после коммита транзакции, как и любое другое уведомление в чате.
	EffectSystemMessage EffectKind = "system_message"
	// EffectEscalate передаёт заказ администратору: исполнитель больше не может его
	// завершить, и заказ появляется на экране эскалаций вместе с тем, что
	// исполнитель отправил. Используется, когда проверка провалилась столько раз,
	// сколько допускает поведение.
	EffectEscalate EffectKind = "escalate"
)

// Effect — одно запрошенное изменение в том виде, в каком его вернул скрипт.
type Effect struct {
	Kind EffectKind
	// OrderID и UserID — субъекты эффекта, пустые, если эффект их не использует.
	// Приходят строками и разбираются применителем: скрипту нельзя доверять
	// возврат корректного идентификатора.
	OrderID string
	UserID  string
	// Amount указана в рублях, ровно так, как её записал скрипт.
	Amount float64
	// Commission просит удержать из этой выплаты долю платформы
	// (order_commission_percent). По умолчанию false и обычно таким и остаётся:
	// вознаграждение — это деньги, которые платит платформа, а не деньги
	// заказчика, и комиссия с него лишь перекладывала бы собственные деньги
	// платформы между её же счетами. Поведение, чьи вознаграждения должны
	// считаться обычным заработком, выставляет флаг явно.
	Commission bool
	// Key — ключ идемпотентности. Обязателен для EffectPayBonus.
	Key    string
	Reason string
	Text   string
}

// Actor — пользователь в том виде, в каком его видит скрипт.
type Actor struct {
	ID         string
	Role       string
	Roles      []string
	IsVerified bool
	Age        int
	Status     string
}

// HasRole повторяет repository.User.HasRole, чтобы скрипт и Go-проверки
// одинаково понимали, что значит обладать ролью.
func (a *Actor) HasRole(role string) bool {
	if a == nil {
		return false
	}
	for _, r := range a.Roles {
		if r == role {
			return true
		}
	}
	return a.Role == role
}

// OrderFacts — заказ в том виде, в каком его видит скрипт.
type OrderFacts struct {
	ID         string
	Status     string
	CustomerID string
	ExecutorID string
	Amount     float64
	IsUrgent   bool
	IsAsap     bool
}

// VariantFacts — узел каталога, о котором принимается решение.
type VariantFacts struct {
	ID        string
	Code      string
	BasePrice float64
}

// SubmissionFacts описывает данные, отправленные исполнителем на проверку: для
// услуги верификации — то, что модератор прочитал в документе заказчика.
//
// Здесь лежит *результат* сравнения и никогда — значения, с которыми
// сравнивали. В этом весь смысл потока: модератору показывают адрес и ничего
// больше о заказчике, поэтому ни он, ни скрипт не могут выведать имя и дату
// рождения простым вопросом. Скрипт решает политику — сколько попыток, когда
// предупредить, когда эскалировать.
type SubmissionFacts struct {
	// Attempt равен 1 для первой отправки по этому заказу.
	Attempt int
	// AllMatch истинно, когда совпали все проверяемые поля.
	AllMatch bool
	// Matches — результат по каждому полю, с ключами из имён полей, объявленных
	// манифестом в check_fields.
	Matches map[string]bool
	// Escalated сообщает, что заказ уже у администратора.
	Escalated bool
}

// Facts — всё, что хуку позволено знать. Ядро заполняет их перед вызовом;
// скрипт не может запросить ничего сверх этого — именно это оставляет хук
// чистой функцией и ограничивает стоимость его выполнения.
type Facts struct {
	// Event заполняется только для on_event, например "order.executed".
	Event string
	// Config — behavior_config узла, наложенный поверх умолчаний скрипта.
	Config map[string]interface{}
	// User — тот, о ком принимается решение: для visible/can_order это заказчик.
	User *Actor
	// Viewer — исполнитель или модератор, оценивающий заказ.
	Viewer *Actor
	// Customer — заказчик заказа, когда заказ есть.
	Customer *Actor
	Order    *OrderFacts
	Variant  *VariantFacts
	// Claims — сколько раз User уже заказывал этот вариант.
	Claims int
	// Submission заполняется на событии отправки и равно nil в остальных случаях.
	Submission *SubmissionFacts
	Now        time.Time
}

// Manifest — статическая половина поведения: свойства, которые ядру нужно знать,
// ничего не запуская, потому что они определяют запись в базу (строку claim),
// форму в интерфейсе или то, какие события вообще стоит доставлять.
type Manifest struct {
	Code string `json:"code"`
	Name string `json:"name"`
	// Description показывается в админ-панели рядом с выбором поведения.
	Description string `json:"description"`
	// OncePerUser заставляет ядро вставлять вместе с заказом строку claim, так что
	// второй заказ того же варианта тем же пользователем отклонит уже база.
	OncePerUser bool `json:"once_per_user"`
	// ReleaseClaimOnCancel возвращает claim при отмене заказа. Отменённый заказ не
	// должен навсегда закрывать пользователю доступ к услуге.
	ReleaseClaimOnCancel bool `json:"release_claim_on_cancel"`
	// Events — события, на которые реагирует скрипт. Событие вне списка не доставляется.
	Events []string `json:"events"`
	// Defaults — значения конфигурации, которые узел наследует, если не задал своих.
	Defaults map[string]interface{} `json:"defaults"`
	// CheckFields перечисляет поля заказчика, которые исполнитель обязан отправить
	// по этой услуге и которые ядро сравнивает за него. Само объявление этих полей
	// и включает шаг отправки: приложение рисует форму ровно по ним, а значения
	// исполнителю не передаются никогда.
	//
	// Поддерживаются: last_name, first_name, patronymic, birth_date.
	CheckFields []string `json:"check_fields,omitempty"`
	// HideCustomerContacts объявляет, что исполнитель, работающий по этой услуге,
	// должен видеть адрес и ничего больше о заказчике. Полезная нагрузка заказа и
	// так не несёт личности заказчика; флаг делает это требование частью поведения,
	// а не случайностью того, что API чего-то не отдаёт, и регрессионный тест
	// опирается именно на него.
	HideCustomerContacts bool `json:"hide_customer_contacts"`
	// Hooks перечисляет, какие функции скрипт на самом деле определяет, — для
	// админ-панели и экрана пробного прогона.
	Hooks []string `json:"hooks"`
	// ConstantsSource и Source — собственный текст скрипта. Конструктор услуг
	// показывает их: поставляемое поведение админ читает, чтобы разобраться, и
	// копирует как стартовый шаблон для нового.
	ConstantsSource string `json:"constants_source,omitempty"`
	Source          string `json:"source,omitempty"`
}

// Handles сообщает, запрашивало ли поведение это событие.
func (m Manifest) Handles(event string) bool {
	for _, e := range m.Events {
		if e == event {
			return true
		}
	}
	return false
}

// DeniedError — отказ, порождённый скриптом и несущий сообщение, которое должен
// увидеть пользователь. Это нормальный исход, а не сбой скрипта.
type DeniedError struct {
	Code    string
	Message string
}

func (e *DeniedError) Error() string { return e.Message }

// Denied собирает отказ.
func Denied(code, message string) error {
	if message == "" {
		message = "услуга недоступна"
	}
	return &DeniedError{Code: code, Message: message}
}

// ErrUnknownBehavior сообщает, что узел ссылается на незагруженное поведение —
// скрипт, который не скомпилировался, или код, оставшийся после отката.
type ErrUnknownBehavior struct{ Code string }

func (e *ErrUnknownBehavior) Error() string {
	return fmt.Sprintf("unknown service behavior %q", e.Code)
}
