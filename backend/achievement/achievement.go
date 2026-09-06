// Package achievement вычисляет правила ачивки — условия, при которых
// исполнитель её заслужил, — в виде скрипта, который живёт вне Go-кода.
//
// Устройство то же, что у поведений услуг (backend/behavior), и по той же
// причине: каждое новое условие иначе становится колонкой плюс `if`, а условий
// у геймификации будет ровно столько, сколько придумает маркетинг. Рантайм у
// обоих общий (backend/script); здесь только то, что знает про ачивки.
//
// Отличие от поведения услуги — субъект. Поведение отвечает на вопросы о
// заказе, ачивка — о человеке: сколько он выполнил, как быстро, с какими
// оценками. Поэтому факты несут агрегаты, а не только текущий заказ.
//
// Что скрипту можно и что нельзя. Скрипт — чистая функция от переданных
// фактов: он не читает базу, не хранит состояния между вызовами и не двигает
// деньги. Он отвечает «заслужил» или «нет» и просит начислить баллы и выдать
// подарок. Комиссию он не назначает вовсе — такого эффекта нет: баллы
// складываются в уровень, а уровень превращает в ставку уже ядро, по одной
// формуле в одном месте.
package achievement

import (
	"fmt"
	"time"
)

// EffectKind называет одно действие, которое ачивка может попросить у ядра.
// Набор намеренно закрыт: эффект — это Go-функция со своими проверками, а не
// лазейка к произвольным изменениям состояния.
type EffectKind string

const (
	// EffectGift выдаёт подарок из таблицы подарков: деньги, код магазина,
	// купон на вещь. Подарок обязан существовать и быть активным, а денежный —
	// уложиться в потолок achievement_max_bonus.
	EffectGift EffectKind = "gift"
	// EffectNotify кладёт письмо во внутреннюю почту пользователя. Применяется
	// после коммита: неудавшееся письмо не должно откатывать выдачу.
	EffectNotify EffectKind = "notify"
)

// Effect — одна просьба скрипта в том виде, в каком он её вернул.
type Effect struct {
	Kind EffectKind
	// GiftCode называет строку таблицы gifts. Разбирает и проверяет её
	// применитель: скрипту нельзя доверять возврат существующего кода.
	GiftCode string
	Subject  string
	Text     string
}

// Grant — ответ скрипта «заслужил». Он несёт всё, что ядро запишет одной
// строкой выдачи, и просьбы, которые применит следом.
type Grant struct {
	// Key — ключ выдачи. У разовой ачивки пустой, и ядро подставит её код; у
	// повторяемой — то, что назвал скрипт: id заказа, месяц, номер серии.
	// Уникальность (user, code, key) и есть защита от повторной выдачи.
	Key string
	// Points — вес этой выдачи. Ноль означает «взять вес из настройки ачивки»,
	// и обычно так и есть: вес живёт в конфигурации, а не в решении.
	Points int
	// LifetimeDays ограничивает срок действия баллов. Ноль — вечно.
	LifetimeDays int
	// OrderID — заказ, на котором ачивка сработала, для истории.
	OrderID string
	Reason  string
	Effects []Effect
}

// Actor — пользователь в том виде, в каком его видит скрипт.
type Actor struct {
	ID           string
	Role         string
	Roles        []string
	IsVerified   bool
	Status       string
	RegisteredAt time.Time
	Points       int
	Level        int
}

// OrderFacts — заказ, о котором пришло событие. Отличается от одноимённой
// структуры поведений тем, что несёт метки времени: без них «выполнено за
// двадцать минут» не вычислить, а сам скрипт их взять неоткуда.
type OrderFacts struct {
	ID          string
	Status      string
	CustomerID  string
	ExecutorID  string
	Amount      float64
	IsUrgent    bool
	IsAsap      bool
	CreatedAt   time.Time
	AssignedAt  time.Time
	CompletedAt time.Time
	ConfirmedAt time.Time
	Rating      int
}

// Stats — агрегаты исполнителя, которые ядро приносит скрипту готовыми.
//
// Набор закрыт намеренно. Скрипт, которому позволено спросить произвольный
// агрегат, — это скрипт, стоимость которого нельзя оценить заранее; а хук
// выполняется на пути обработки события. Новый агрегат — это новая колонка в
// executor_stats и осознанное решение платить за неё.
type Stats struct {
	OrdersCompleted      int
	OrdersCompletedMonth int
	DistinctCustomers    int
	FastestCompletionMin int
	FiveStarStreak       int
	RatingCount          int
	Cancels              int
	EarnedTotal          float64
	PointsToday          int
}

// Granted — уже выданная пользователю ачивка, как её видит скрипт: сколько раз
// и на сколько баллов. Нужна, чтобы одна ачивка могла требовать другую, не
// запрашивая базу.
type Granted struct {
	Count     int
	Points    int
	GrantedAt time.Time
	ExpiresAt time.Time
}

// Facts — всё, что хуку позволено знать.
type Facts struct {
	// Event — имя доменного события, из-за которого хук вызван.
	Event string
	// Config — конфигурация ачивки из базы, наложенная поверх умолчаний скрипта.
	Config map[string]interface{}
	User   *Actor
	// Customer — заказчик заказа, когда событие о заказе. Скрипту он нужен ровно
	// для одного — убедиться, что это не он сам; ту же проверку независимо
	// делает ядро.
	Customer *Actor
	Order    *OrderFacts
	Stats    *Stats
	Granted  map[string]Granted
	Now      time.Time
}

// Manifest — статическая половина ачивки: то, что ядру нужно знать, ничего не
// запуская, потому что это определяет запись в базу и доставку событий.
type Manifest struct {
	Code string `json:"code"`
	// Title и Description видит пользователь на карточке значка.
	Title       string `json:"title"`
	Description string `json:"description"`
	Icon        string `json:"icon"`
	// Audience — кому ачивка адресована: EXECUTOR или CUSTOMER. Определяет, чьим
	// именем ядро подставит User в фактах.
	Audience string `json:"audience"`
	// Events — события, на которые ачивка пересчитывается. Событие вне списка не
	// доставляется, и это единственное, что ограничивает её стоимость.
	Events []string `json:"events"`
	// OncePerUser запрещает вторую выдачу: ядро подставит код ачивки как
	// grant_key, и уникальный индекс отклонит повтор.
	OncePerUser bool `json:"once_per_user"`
	// Weight — вес по умолчанию в баллах. Ноль означает «взять
	// achievement_default_weight».
	Weight int `json:"weight"`
	// LifetimeDays — срок действия баллов по умолчанию. Ноль — вечно.
	LifetimeDays int `json:"lifetime_days"`
	// Defaults — конфигурация, которую ачивка наследует, если не задали своей.
	Defaults map[string]interface{} `json:"defaults"`
	// Hooks перечисляет, какие функции скрипт на самом деле определяет.
	Hooks []string `json:"hooks"`
	// ConstantsSource и Source — собственный текст скрипта, для админ-панели.
	ConstantsSource string `json:"constants_source,omitempty"`
	Source          string `json:"source,omitempty"`
}

// Аудитории. Заказчику ачивки тоже адресуемы, но скидку на комиссию ему давать
// бессмысленно — он её не платит; полезное для него живёт в подарках.
const (
	AudienceExecutor = "EXECUTOR"
	AudienceCustomer = "CUSTOMER"
)

// Handles сообщает, запрашивала ли ачивка это событие.
func (m Manifest) Handles(event string) bool {
	for _, e := range m.Events {
		if e == event {
			return true
		}
	}
	return false
}

// ErrUnknownAchievement сообщает, что строка каталога ссылается на незагруженный
// скрипт — не скомпилировавшийся или оставшийся после отката.
type ErrUnknownAchievement struct{ Code string }

func (e *ErrUnknownAchievement) Error() string {
	return fmt.Sprintf("unknown achievement %q", e.Code)
}
