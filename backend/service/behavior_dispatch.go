package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"healthlogin/backend/behavior"
	"healthlogin/backend/metrics"
	"healthlogin/backend/money"
	"healthlogin/backend/repository"
)

// SettingBehaviorMaxBonus ограничивает одну скриптовую выплату, в рублях. Сумму
// решает скрипт; это решает, насколько сильно скрипту позволено ошибиться.
const SettingBehaviorMaxBonus = "behavior_max_bonus"

const defaultBehaviorMaxBonus = 5000.0

// ConfigVerifierRole — ключ конфигурации, называющий роль, которая может
// выполнять услугу. Ядро читает его для проверки verify_user; поведение,
// использующее verify_user, обязано его объявить (см. behaviors/verification/config.star).
const ConfigVerifierRole = "verifier_role"

// BehaviorDispatcher доставляет доменные события скриптам поведений и применяет
// эффекты, которые они просят.
//
// Разделение, которое он обеспечивает, — смысл всего замысла. Скрипт решает,
// *что* должно произойти, и говорит это эффектами; этот тип решает, был ли
// просящий на это вправе, и выполняет через обычные сервисы — реестр,
// жизненный цикл заказа, флаг верификации. Поэтому скрипт может ошибиться в
// решении, но не может заплатить постороннему, верифицировать произвольного
// пользователя или записать несбалансированную пару проводок.
type BehaviorDispatcher struct {
	events      repository.EventRepository
	orders      repository.OrderRepository
	users       repository.UserRepository
	catalog     repository.ServiceCatalogRepository
	claims      repository.ServiceClaimRepository
	chat        repository.ChatRepository
	settings    repository.SettingsRepository
	submissions repository.SubmissionRepository
	ledger      *Ledger
	behaviors   *Behaviors
	orderSvc    *OrderService

	// batchSize ограничивает один тик; maxAttempts ограничивает жизнь одного
	// события, чтобы постоянно падающее событие перестало занимать пачку, а не
	// блокировало навсегда все события за собой.
	batchSize   int
	maxAttempts int

	// Обработанные события хранятся как история столько времени и подметаются не
	// чаще, чем purgeEvery. Окно намного длиннее любой переотправки, поэтому ключ
	// идемпотентности не исчезает, пока его событие ещё может вернуться.
	retention  time.Duration
	purgeEvery time.Duration
	mu         sync.Mutex
	lastPurge  time.Time
}

// NewBehaviorDispatcher собирает диспетчер. Ему нужен сервис заказов, потому что
// завершение и отмена заказа обязаны идти ровно тем же кодом, каким идёт
// собственное подтверждение заказчика.
func NewBehaviorDispatcher(
	events repository.EventRepository,
	orders repository.OrderRepository,
	users repository.UserRepository,
	catalog repository.ServiceCatalogRepository,
	claims repository.ServiceClaimRepository,
	chat repository.ChatRepository,
	settings repository.SettingsRepository,
	ledger *Ledger,
	behaviors *Behaviors,
	orderSvc *OrderService,
) *BehaviorDispatcher {
	return &BehaviorDispatcher{
		events: events, orders: orders, users: users, catalog: catalog,
		claims: claims, chat: chat, settings: settings, ledger: ledger,
		behaviors: behaviors, orderSvc: orderSvc,
		batchSize: 50, maxAttempts: 10,
		retention: 30 * 24 * time.Hour, purgeEvery: time.Hour,
	}
}

// WithSubmissions подключает хранилище за проверками данных и эскалациями. Без
// него поведение, объявившее check_fields, просто не принимает отправок.
func (d *BehaviorDispatcher) WithSubmissions(submissions repository.SubmissionRepository) *BehaviorDispatcher {
	d.submissions = submissions
	return d
}

// Tick обрабатывает одну пачку ожидающих событий. Вызывается по таймеру воркером
// поведений, под защитой лидера.
func (d *BehaviorDispatcher) Tick(ctx context.Context) error {
	if d == nil || d.events == nil || d.behaviors == nil {
		return nil
	}
	events, err := d.events.ClaimPending(ctx, repository.ConsumerBehaviors, d.batchSize, d.maxAttempts)
	if err != nil {
		return err
	}
	for _, event := range events {
		if _, err := d.dispatch(ctx, event); err != nil {
			metrics.BehaviorEvent(event.Type, "failed")
			log.Printf("[behavior] event %s (%s) failed: %v", event.ID, event.Type, err)
			// Намеренно оставлено необработанным: следующий тик повторит, вплоть до
			// maxAttempts. Причина сохраняется, чтобы её можно было прочитать, не
			// копаясь в логах.
			_ = d.events.MarkFailed(ctx, repository.ConsumerBehaviors, event.ID, err.Error())
			continue
		}
		metrics.BehaviorEvent(event.Type, "processed")
		if err := d.events.MarkProcessed(ctx, repository.ConsumerBehaviors, event.ID); err != nil {
			log.Printf("[behavior] event %s applied but not marked processed: %v", event.ID, err)
		}
	}
	if pending, err := d.events.CountPending(ctx, repository.ConsumerBehaviors); err == nil {
		metrics.SetBehaviorBacklog(pending)
	}
	d.purge(ctx)
	return nil
}

// purge подрезает обработанную историю, не чаще раза в purgeEvery. Сбой
// логируется, и больше ничего: медленно растущая таблица — не повод прекращать
// диспетчеризацию.
func (d *BehaviorDispatcher) purge(ctx context.Context) {
	d.mu.Lock()
	due := time.Since(d.lastPurge) >= d.purgeEvery
	if due {
		d.lastPurge = time.Now()
	}
	d.mu.Unlock()
	if !due {
		return
	}
	if removed, err := d.events.PurgeProcessed(ctx, d.retention); err != nil {
		log.Printf("[behavior] cannot trim processed events: %v", err)
	} else if removed > 0 {
		log.Printf("[behavior] trimmed %d processed events older than %s", removed, d.retention)
	}
}

// target — один заказ, на который поведение может подействовать в ответ на событие.
type target struct {
	order   *repository.Order
	variant *repository.ServiceNode
}

// dispatch определяет, кого касается событие, и запускает их поведения. Он
// возвращает сообщения, опубликованные поведениями, — для вызывающего, который
// ждёт исхода: исполнителя, только что отправившего данные на проверку.
func (d *BehaviorDispatcher) dispatch(ctx context.Context, event *repository.DomainEvent) ([]string, error) {
	targets, err := d.targets(ctx, event)
	if err != nil {
		return nil, err
	}
	var messages []string
	for _, t := range targets {
		manifest, ok := d.behaviors.Manifest(t.variant)
		if !ok || !manifest.Handles(event.Type) {
			continue
		}
		facts, err := d.facts(ctx, event, t)
		if err != nil {
			return messages, err
		}
		effects, err := d.behaviors.Engine().OnEvent(d.behaviors.Code(t.variant), facts)
		if err != nil {
			metrics.BehaviorHookError(d.behaviors.Code(t.variant), behavior.HookOnEvent)
			return messages, err
		}
		if len(effects) == 0 {
			continue
		}
		posted, err := d.apply(ctx, event, t, effects)
		messages = append(messages, posted...)
		if err != nil {
			return messages, err
		}
	}
	return messages, nil
}

// targets отвечает на вопрос «какие выполняющиеся заказы это событие может изменить».
//
//   - Событие заказа касается своего заказа и ничего больше.
//   - Событие пользователя касается каждого его ещё выполняющегося заказа.
//     Именно это позволяет «этот заказчик теперь верифицирован» закрыть открытый
//     у него заказ верификации, притом что действие админа ничего не знает об услугах.
func (d *BehaviorDispatcher) targets(ctx context.Context, event *repository.DomainEvent) ([]target, error) {
	var orders []*repository.Order
	switch event.SubjectType {
	case repository.EventSubjectOrder:
		order, err := d.orders.GetOrderByID(ctx, event.SubjectID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, nil
			}
			return nil, err
		}
		orders = []*repository.Order{order}
	case repository.EventSubjectUser:
		open, err := d.orders.FindOpenByCustomer(ctx, event.SubjectID)
		if err != nil {
			return nil, err
		}
		orders = open
	default:
		return nil, fmt.Errorf("unknown event subject %q", event.SubjectType)
	}

	targets := make([]target, 0, len(orders))
	for _, order := range orders {
		if order == nil {
			continue
		}
		variant, err := d.catalog.GetNodeByID(ctx, order.ServiceVariantID)
		if err != nil || variant == nil || !variant.HasBehavior() {
			continue
		}
		targets = append(targets, target{order: order, variant: variant})
	}
	return targets, nil
}

func (d *BehaviorDispatcher) facts(ctx context.Context, event *repository.DomainEvent, t target) (behavior.Facts, error) {
	facts := behavior.Facts{
		Event:   event.Type,
		Order:   orderFacts(t.order),
		Variant: variantFacts(t.variant),
		Config:  t.variant.BehaviorConfig,
	}
	if d.users != nil {
		if customer, err := d.users.FindByID(ctx, t.order.CustomerID); err == nil {
			facts.Customer = actorFacts(customer)
			facts.User = facts.Customer
		}
		if t.order.ExecutorID != nil {
			if executor, err := d.users.FindByID(ctx, *t.order.ExecutorID); err == nil {
				facts.Viewer = actorFacts(executor)
			}
		}
	}
	if d.claims != nil {
		if count, err := d.claims.CountForVariant(ctx, t.order.CustomerID, t.variant.ID); err == nil {
			facts.Claims = count
		}
	}
	if event.Type == repository.EventOrderSubmission {
		escalated := false
		if d.submissions != nil {
			if open, err := d.submissions.HasOpenEscalation(ctx, t.order.ID); err == nil {
				escalated = open
			}
		}
		facts.Submission = submissionFacts(event, escalated)
	}
	return facts, nil
}

// apply выполняет эффекты одного поведения в одной транзакции: либо заказчик
// верифицирован, заказ закрыт и вознаграждение выплачено, либо ничего этого не
// произошло и событие будет повторено.
func (d *BehaviorDispatcher) apply(ctx context.Context, event *repository.DomainEvent, t target, effects []behavior.Effect) ([]string, error) {
	maxBonus := money.FromRubles(settingFloat(ctx, d.settings, SettingBehaviorMaxBonus, defaultBehaviorMaxBonus))

	var messages []behavior.Effect
	err := d.ledger.RunInTx(ctx, func(tx *sql.Tx) error {
		for i, effect := range effects {
			if effect.Kind == behavior.EffectSystemMessage {
				// Чат не входит в денежную транзакцию: неудавшееся сообщение не
				// должно откатывать платёж, а откаченный платёж не должен был
				// о себе объявлять.
				messages = append(messages, effect)
				continue
			}
			key := effect.Key
			if key == "" {
				key = fmt.Sprintf("%s:%d:%s", event.ID, i, effect.Kind)
			}
			// Занять ключ первым — вот что делает переотправку безопасной: вторая
			// попытка выплатить то же вознаграждение находит строку занятой и останавливается.
			err := d.events.RecordEffect(ctx, tx, key, event.ID, d.behaviors.Code(t.variant), string(effect.Kind), map[string]interface{}{
				"order_id": effect.OrderID,
				"user_id":  effect.UserID,
				"amount":   effect.Amount,
				"reason":   effect.Reason,
			})
			if errors.Is(err, repository.ErrEffectAlreadyApplied) {
				metrics.BehaviorEffect(string(effect.Kind), "duplicate")
				continue
			}
			if err != nil {
				return err
			}
			if err := d.applyOne(ctx, tx, t, effect, maxBonus); err != nil {
				metrics.BehaviorEffect(string(effect.Kind), "refused")
				return err
			}
			metrics.BehaviorEffect(string(effect.Kind), "applied")
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	posted := make([]string, 0, len(messages))
	for _, message := range messages {
		d.postMessage(ctx, t, message)
		posted = append(posted, message.Text)
	}
	return posted, nil
}

// applyOne выполняет один эффект после проверки того, что поведение было вправе
// его просить. Каждая проверка здесь отвечает на один и тот же вопрос: может ли
// этот эффект дотянуться до кого-то или чего-то вне заказа, о котором было событие?
func (d *BehaviorDispatcher) applyOne(ctx context.Context, tx *sql.Tx, t target, effect behavior.Effect, maxBonus money.Amount) error {
	switch effect.Kind {
	case behavior.EffectCompleteOrder:
		if err := d.requireOwnOrder(t, effect.OrderID); err != nil {
			return err
		}
		if err := d.orderSvc.confirmTx(ctx, tx, t.order.ID); err != nil {
			return err
		}
		// У закрытого заказа администратору решать уже нечего.
		if d.submissions != nil {
			return d.submissions.ResolveByOrder(ctx, tx, t.order.ID, nil)
		}
		return nil

	case behavior.EffectCancelOrder:
		if err := d.requireOwnOrder(t, effect.OrderID); err != nil {
			return err
		}
		return d.orderSvc.cancelTx(ctx, tx, t.order.ID,
			repository.OrderStatusSearching, repository.OrderStatusAssigned, repository.OrderStatusExecuted)

	case behavior.EffectPayBonus:
		recipient, err := uuid.Parse(effect.UserID)
		if err != nil {
			return fmt.Errorf("pay_bonus: invalid recipient %q", effect.UserID)
		}
		// Оплатить по заказу можно только тому, кто в нём участвует. Без этого скрипт
		// мог бы назвать любой идентификатор пользователя в системе.
		if recipient != t.order.CustomerID && (t.order.ExecutorID == nil || recipient != *t.order.ExecutorID) {
			return fmt.Errorf("pay_bonus: %s is not a party to order %s", recipient, t.order.ID)
		}
		amount := money.FromRubles(effect.Amount)
		if !amount.IsPositive() {
			return fmt.Errorf("pay_bonus: amount %s is not positive", amount)
		}
		if amount > maxBonus {
			return fmt.Errorf("pay_bonus: %s exceeds the %s ceiling (%s)", amount, maxBonus, SettingBehaviorMaxBonus)
		}
		return d.ledger.Bonus(ctx, tx, recipient, amount, d.commissionOnBonus(ctx, effect, amount), &t.order.ID)

	case behavior.EffectVerifyUser:
		subject, err := uuid.Parse(effect.UserID)
		if err != nil {
			return fmt.Errorf("verify_user: invalid user %q", effect.UserID)
		}
		// Скрипт может попросить верифицировать только заказчика того заказа, на
		// который он реагирует, и только когда этот заказ действительно выполнял
		// модератор. Именно эта проверка делает скриптовую верификацию столь же
		// достоверной, как заменяемый ею админский чекбокс.
		if subject != t.order.CustomerID {
			return fmt.Errorf("verify_user: %s is not the customer of order %s", subject, t.order.ID)
		}
		if err := d.requireModeratorExecutor(ctx, t); err != nil {
			return err
		}
		if err := d.users.UpdateVerifiedTx(ctx, tx, subject, true); err != nil {
			return err
		}
		log.Printf("[AUDIT] behavior %s verified user %s through order %s", d.behaviors.Code(t.variant), subject, t.order.ID)
		// Публикуется, как любая другая верификация, чтобы всё остальное, что реагирует
		// на верификацию пользователя, увидело и эту.
		return d.events.Publish(ctx, tx, &repository.DomainEvent{
			Type:        repository.EventUserVerified,
			SubjectType: repository.EventSubjectUser,
			SubjectID:   subject,
			ActorID:     t.order.ExecutorID,
		})

	case behavior.EffectEscalate:
		if err := d.requireOwnOrder(t, effect.OrderID); err != nil {
			return err
		}
		if d.submissions == nil {
			return errors.New("escalations are not available on this server")
		}
		reason := strings.TrimSpace(effect.Reason)
		if reason == "" {
			reason = "передано администратору поведением услуги"
		}
		log.Printf("[AUDIT] behavior %s escalated order %s: %s", d.behaviors.Code(t.variant), t.order.ID, reason)
		return d.submissions.Escalate(ctx, tx, &repository.BehaviorEscalation{
			OrderID:      t.order.ID,
			BehaviorCode: d.behaviors.Code(t.variant),
			Reason:       reason,
		})

	default:
		return fmt.Errorf("unknown effect %q", effect.Kind)
	}
}

// commissionOnBonus вычисляет долю платформы с вознаграждения. Ноль, пока
// поведение об этом не попросит: вознаграждение — это деньги, которые платит
// платформа, а не деньги заказчика, поэтому комиссия — доля от уплаченного
// заказчиком — к нему по умолчанию неприменима. Когда поведение соглашается,
// ставка — обычный order_commission_percent, ужатый тем же commissionOn, что и
// на пути заказа, поэтому определение ставки в сервисе одно.
func (d *BehaviorDispatcher) commissionOnBonus(ctx context.Context, effect behavior.Effect, amount money.Amount) money.Amount {
	if !effect.Commission {
		return money.Zero
	}
	percent := settingFloat(ctx, d.settings, SettingOrderCommissionPercent, 0)
	return commissionOn(amount, map[string]float64{SettingOrderCommissionPercent: percent})
}

// requireOwnOrder отклоняет эффект, направленный на любой заказ, кроме того, о
// котором было событие.
func (d *BehaviorDispatcher) requireOwnOrder(t target, orderID string) error {
	if orderID != "" && !strings.EqualFold(orderID, t.order.ID.String()) {
		return fmt.Errorf("effect targets order %s, but the event was about %s", orderID, t.order.ID)
	}
	return nil
}

// requireModeratorExecutor проверяет, что заказ выполнил кто-то, кому
// конфигурация поведения доверяет его выполнять.
func (d *BehaviorDispatcher) requireModeratorExecutor(ctx context.Context, t target) error {
	if t.order.ExecutorID == nil {
		return fmt.Errorf("order %s has no executor to vouch for it", t.order.ID)
	}
	if *t.order.ExecutorID == t.order.CustomerID {
		return fmt.Errorf("order %s was taken by its own customer", t.order.ID)
	}
	executor, err := d.users.FindByID(ctx, *t.order.ExecutorID)
	if err != nil {
		return err
	}
	// Роль берётся из собственных констант поведения, переопределённых узлом:
	// ровно то, что читает can_view_or_take скрипта, поэтому требования ядра к
	// проверяющему не могут разойтись с тем, кому скрипт позволил взять заказ.
	required := d.behaviors.ConfigString(t.variant, ConfigVerifierRole, repository.RoleModerator)
	if !executor.HasRole(required) {
		return fmt.Errorf("executor %s does not hold %s", executor.ID, required)
	}
	return nil
}

func (d *BehaviorDispatcher) postMessage(ctx context.Context, t target, effect behavior.Effect) {
	if d.chat == nil || strings.TrimSpace(effect.Text) == "" {
		return
	}
	if err := d.requireOwnOrder(t, effect.OrderID); err != nil {
		log.Printf("[behavior] refusing system_message: %v", err)
		return
	}
	chat, err := d.chat.GetChatByOrderID(ctx, t.order.ID)
	if err != nil || chat == nil {
		return
	}
	sender := t.order.CustomerID
	if t.order.ExecutorID != nil {
		sender = *t.order.ExecutorID
	}
	if _, err := d.chat.SaveMessage(ctx, chat.ID, sender, effect.Text); err != nil {
		log.Printf("[behavior] cannot post system message to order %s: %v", t.order.ID, err)
	}
}
