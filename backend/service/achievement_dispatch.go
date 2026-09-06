package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"

	"healthlogin/backend/achievement"
	"healthlogin/backend/metrics"
	"healthlogin/backend/money"
	"healthlogin/backend/repository"
)

// AchievementDispatcher доставляет доменные события скриптам ачивок и применяет
// то, что они просят.
//
// Разделение то же, что у диспетчера поведений, и по той же причине. Скрипт
// решает, заслужена ли ачивка, и говорит это выдачей; этот тип решает, был ли
// просящий вправе просить, и выполняет через обычные сервисы — реестр баллов,
// каталог подарков, Ledger. Поэтому скрипт может ошибиться в решении, но не
// может назначить комиссию, начислить постороннему или выдать подарка больше,
// чем есть на складе.
//
// Проверки, которых нет в скриптах и которые поэтому живут здесь:
//
//   - заказчик и исполнитель — разные люди. Скрипт может её забыть, ядро нет;
//   - заказ подтверждён и оплачен, и оплачен не символически;
//   - суточный потолок баллов: он и есть цена накрутки;
//   - потолок одного денежного подарка.
type AchievementDispatcher struct {
	events       repository.EventRepository
	orders       repository.OrderRepository
	users        repository.UserRepository
	achievements repository.AchievementRepository
	stats        repository.ExecutorStatsRepository
	gifts        repository.GiftRepository
	mail         repository.MailRepository
	incidents    repository.MoneyIncidentRepository
	ledger       *Ledger
	levels       *Levels
	engine       *achievement.Engine

	batchSize   int
	maxAttempts int
}

// NewAchievementDispatcher собирает диспетчер.
func NewAchievementDispatcher(
	events repository.EventRepository,
	orders repository.OrderRepository,
	users repository.UserRepository,
	achievements repository.AchievementRepository,
	stats repository.ExecutorStatsRepository,
	gifts repository.GiftRepository,
	mail repository.MailRepository,
	incidents repository.MoneyIncidentRepository,
	ledger *Ledger,
	levels *Levels,
	engine *achievement.Engine,
) *AchievementDispatcher {
	return &AchievementDispatcher{
		events: events, orders: orders, users: users, achievements: achievements,
		stats: stats, gifts: gifts, mail: mail, incidents: incidents,
		ledger: ledger, levels: levels, engine: engine,
		batchSize: 50, maxAttempts: 10,
	}
}

// Tick обрабатывает одну пачку событий. Вызывается по таймеру воркером под
// защитой лидера.
func (d *AchievementDispatcher) Tick(ctx context.Context) error {
	if d == nil || d.events == nil || d.engine == nil {
		return nil
	}
	events, err := d.events.ClaimPending(ctx, repository.ConsumerAchievements, d.batchSize, d.maxAttempts)
	if err != nil {
		return err
	}
	for _, event := range events {
		if err := d.dispatch(ctx, event); err != nil {
			metrics.AchievementEvent(event.Type, "failed")
			log.Printf("[achievement] event %s (%s) failed: %v", event.ID, event.Type, err)
			// Намеренно оставлено необработанным: следующий тик повторит, вплоть
			// до maxAttempts. Выдачи, успевшие пройти, защищены своими ключами и
			// вторично не начислятся.
			_ = d.events.MarkFailed(ctx, repository.ConsumerAchievements, event.ID, err.Error())
			continue
		}
		metrics.AchievementEvent(event.Type, "processed")
		if err := d.events.MarkProcessed(ctx, repository.ConsumerAchievements, event.ID); err != nil {
			log.Printf("[achievement] event %s applied but not marked processed: %v", event.ID, err)
		}
	}
	if pending, err := d.events.CountPending(ctx, repository.ConsumerAchievements); err == nil {
		metrics.SetAchievementBacklog(pending)
	}
	return nil
}

// subject — человек, о котором событие, и заказ, если он есть.
type subject struct {
	user  *repository.User
	order *repository.Order
	// counterparty — вторая сторона заказа. Нужна ровно для одной проверки:
	// заказчик и исполнитель обязаны быть разными людьми.
	counterparty *repository.User
	audience     string
}

func (d *AchievementDispatcher) dispatch(ctx context.Context, event *repository.DomainEvent) error {
	// Отменённый заказ — не повод выдавать: наоборот, повод отобрать. Выдачи по
	// нему отзываются вместе с баллами, иначе накрутка сводится к «создать,
	// закрыть, отменить».
	if event.Type == repository.EventOrderCanceled {
		return d.revokeForOrder(ctx, event.SubjectID)
	}

	subjects, err := d.subjects(ctx, event)
	if err != nil || len(subjects) == 0 {
		return err
	}

	rows, err := d.achievements.ListActive(ctx)
	if err != nil {
		return err
	}
	now := time.Now()

	for _, s := range subjects {
		facts, err := d.facts(ctx, event, s, now)
		if err != nil {
			return err
		}
		for _, row := range rows {
			manifest, ok := d.engine.Manifest(row.Code)
			if !ok || !manifest.Handles(event.Type) || manifest.Audience != s.audience {
				continue
			}
			// Окно акции проверяет ядро, а не скрипт: «когда ачивку можно
			// заслужить» — свойство строки каталога, и скрипт не должен иметь
			// возможности его обойти.
			if !row.AvailableAt(now) {
				continue
			}
			facts.Config = row.Config
			grant, err := d.engine.Check(row.Code, facts)
			if err != nil {
				metrics.AchievementGrant(row.Code, "refused")
				// Один сломанный скрипт не отменяет остальные ачивки этого
				// события: он логируется и пропускается.
				log.Printf("[achievement] %s: check failed: %v", row.Code, err)
				continue
			}
			if grant == nil {
				continue
			}
			if err := d.apply(ctx, event, s, row, manifest, grant, now); err != nil {
				return err
			}
		}
	}
	return nil
}

// subjects отвечает на вопрос «кого это событие может наградить».
//
// У поведения услуги субъект — заказ; здесь субъект человек, и заказ лишь повод.
// Событие заказа касается обеих сторон, но каждой в своей аудитории: одна и та
// же строка порождает факты исполнителя для ачивок EXECUTOR и факты заказчика
// для ачивок CUSTOMER.
func (d *AchievementDispatcher) subjects(ctx context.Context, event *repository.DomainEvent) ([]subject, error) {
	switch event.SubjectType {
	case repository.EventSubjectOrder:
		order, err := d.orders.GetOrderByID(ctx, event.SubjectID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, nil
			}
			return nil, err
		}
		if err := d.eligible(ctx, order); err != nil {
			log.Printf("[achievement] order %s is not eligible: %v", order.ID, err)
			return nil, nil
		}
		customer, err := d.users.FindByID(ctx, order.CustomerID)
		if err != nil {
			return nil, err
		}
		executor, err := d.users.FindByID(ctx, *order.ExecutorID)
		if err != nil {
			return nil, err
		}
		return []subject{
			{user: executor, order: order, counterparty: customer, audience: achievement.AudienceExecutor},
			{user: customer, order: order, counterparty: executor, audience: achievement.AudienceCustomer},
		}, nil

	case repository.EventSubjectUser:
		user, err := d.users.FindByID(ctx, event.SubjectID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, nil
			}
			return nil, err
		}
		// Событие о человеке приходит обеим аудиториям: кем он был в этом
		// событии, знает сам скрипт.
		return []subject{
			{user: user, audience: achievement.AudienceExecutor},
			{user: user, audience: achievement.AudienceCustomer},
		}, nil
	}
	return nil, fmt.Errorf("unknown event subject %q", event.SubjectType)
}

// eligible — те проверки заказа, которые не доверены скрипту.
//
// Каждая из них закрывает свой способ нарисовать ачивку: заказ сам себе, заказ
// в рубль, заказ, за который никто не платил. Скрипт вправе быть строже, но не
// вправе быть мягче.
func (d *AchievementDispatcher) eligible(ctx context.Context, order *repository.Order) error {
	if order.ExecutorID == nil {
		return errors.New("order has no executor")
	}
	if *order.ExecutorID == order.CustomerID {
		return errors.New("customer and executor are the same person")
	}
	if order.Status != repository.OrderStatusCompleted {
		return fmt.Errorf("order is %s, not completed", order.Status)
	}
	if !order.FinalAmount.IsPositive() {
		return errors.New("order was not paid")
	}
	// Порог суммы проверяется и здесь, и в скриптах. Дублирование намеренное:
	// скрипт вправе быть строже, но заказ в рубль не должен приносить баллы
	// из-за того, что в одном из скриптов о пороге забыли.
	if min := money.FromRubles(d.levels.MinOrderAmount(ctx)); order.FinalAmount < min {
		return fmt.Errorf("order paid %s, below the %s floor", order.FinalAmount, min)
	}
	return nil
}

func (d *AchievementDispatcher) facts(ctx context.Context, event *repository.DomainEvent, s subject, now time.Time) (achievement.Facts, error) {
	facts := achievement.Facts{Event: event.Type, Now: now, User: actorOf(s.user)}
	if s.audience == achievement.AudienceExecutor && s.counterparty != nil {
		facts.Customer = actorOf(s.counterparty)
	}
	if s.order != nil {
		facts.Order = orderFactsFor(s.order)
	}

	if d.levels != nil {
		level := d.levels.For(ctx, nil, s.user.ID)
		facts.User.Points = level.Points
		facts.User.Level = level.Level
	}

	stats := &achievement.Stats{}
	if d.stats != nil {
		row, err := d.stats.Get(ctx, nil, s.user.ID)
		if err != nil {
			return facts, err
		}
		stats = &achievement.Stats{
			OrdersCompleted:      row.OrdersCompleted,
			OrdersCompletedMonth: row.OrdersCompletedMonth,
			DistinctCustomers:    row.DistinctCustomers,
			FastestCompletionMin: row.FastestCompletionMin,
			FiveStarStreak:       row.FiveStarStreak,
			RatingCount:          row.RatingCount,
			Cancels:              row.Cancels,
			EarnedTotal:          row.EarnedTotal.Rubles(),
		}
	}
	if d.achievements != nil {
		if today, err := d.achievements.PointsToday(ctx, nil, s.user.ID); err == nil {
			stats.PointsToday = today
		}
		summary, err := d.achievements.SummaryForUser(ctx, s.user.ID)
		if err != nil {
			return facts, err
		}
		facts.Granted = make(map[string]achievement.Granted, len(summary))
		for code, g := range summary {
			granted := achievement.Granted{Count: g.Count, Points: g.Points, GrantedAt: g.GrantedAt}
			if g.ExpiresAt != nil {
				granted.ExpiresAt = *g.ExpiresAt
			}
			facts.Granted[code] = granted
		}
	}
	facts.Stats = stats
	return facts, nil
}

func actorOf(u *repository.User) *achievement.Actor {
	if u == nil {
		return nil
	}
	return &achievement.Actor{
		ID: u.ID.String(), Role: u.Role, Roles: u.Roles,
		IsVerified: u.IsVerified(), Status: u.Status, RegisteredAt: u.CreatedAt,
	}
}

func orderFactsFor(o *repository.Order) *achievement.OrderFacts {
	facts := &achievement.OrderFacts{
		ID: o.ID.String(), Status: string(o.Status), CustomerID: o.CustomerID.String(),
		Amount: o.FinalAmount.Rubles(), IsUrgent: o.IsUrgent, IsAsap: o.IsAsap,
		CreatedAt: o.CreatedAt,
	}
	if o.ExecutorID != nil {
		facts.ExecutorID = o.ExecutorID.String()
	}
	if o.AssignedAt != nil {
		facts.AssignedAt = *o.AssignedAt
	}
	if o.CompletedAt != nil {
		// Подтверждение и есть завершение: заказ переходит в COMPLETED в той же
		// транзакции, где заказчик его принял.
		facts.CompletedAt = *o.CompletedAt
		facts.ConfirmedAt = *o.CompletedAt
	}
	return facts
}

// apply записывает выдачу и всё, что с ней связано, в одной транзакции: либо
// ачивка выдана, баллы начислены и подарок занят, либо ничего этого не
// произошло и событие будет повторено.
func (d *AchievementDispatcher) apply(
	ctx context.Context, event *repository.DomainEvent, s subject,
	row *repository.Achievement, manifest achievement.Manifest,
	grant *achievement.Grant, now time.Time,
) error {
	key := grant.Key
	if manifest.OncePerUser || key == "" {
		// Разовая ачивка ключа не называет: им становится её код, и уникальный
		// индекс делает вторую выдачу невозможной.
		key = row.Code
	}
	if len(key) > 120 {
		key = key[:120]
	}

	points := d.levels.Weight(ctx, row, manifest, grant.Points)
	lifetime := grant.LifetimeDays
	if lifetime == 0 {
		lifetime = manifest.LifetimeDays
	}

	var mails []repository.Mail

	err := d.ledger.RunInTx(ctx, func(tx *sql.Tx) error {
		// Ключ идемпотентности занимается первым — в той же таблице, что и
		// эффекты поведений: у платформы одно пространство ключей, и выдача,
		// пришедшая дважды, спотыкается о него так же, как дважды пришедшая
		// выплата.
		effectKey := fmt.Sprintf("achievement:%s:%s:%s", row.Code, s.user.ID, key)
		err := d.events.RecordEffect(ctx, tx, effectKey, event.ID, row.Code, "grant", map[string]interface{}{
			"user_id": s.user.ID.String(), "points": points, "key": key,
		})
		if errors.Is(err, repository.ErrEffectAlreadyApplied) {
			metrics.AchievementGrant(row.Code, "duplicate")
			return nil
		}
		if err != nil {
			return err
		}

		granted := &repository.UserAchievement{
			UserID: s.user.ID, Code: row.Code, GrantKey: key,
			Points: points, ExpiresAt: Expiry(now, lifetime),
		}
		if s.order != nil {
			granted.OrderID = &s.order.ID
		}
		if err := d.achievements.Grant(ctx, tx, granted); err != nil {
			if errors.Is(err, repository.ErrAchievementAlreadyGranted) {
				metrics.AchievementGrant(row.Code, "duplicate")
				return nil
			}
			return err
		}

		points = d.capPoints(ctx, tx, s.user.ID, points, row.Code)
		if points > 0 {
			if err := d.achievements.AddPoints(ctx, tx, s.user.ID, points,
				repository.PointSourceAchievement, row.Code, &granted.ID, grant.Reason, granted.ExpiresAt); err != nil {
				return err
			}
		}

		for _, effect := range grant.Effects {
			switch effect.Kind {
			case achievement.EffectGift:
				issued, err := d.applyGift(ctx, tx, s.user.ID, granted, effect.GiftCode)
				if err != nil {
					return err
				}
				if issued != nil {
					mails = append(mails, giftMail(s.user.ID, issued))
				}
			case achievement.EffectNotify:
				mails = append(mails, repository.Mail{
					UserID: s.user.ID, Kind: repository.MailKindAchievement,
					Subject: firstNonEmpty(effect.Subject, manifest.Title),
					Body:    effect.Text, RefType: "achievement", RefID: row.Code,
				})
			default:
				return fmt.Errorf("unknown achievement effect %q", effect.Kind)
			}
		}
		metrics.AchievementGrant(row.Code, "granted")
		log.Printf("[AUDIT] achievement %s granted to %s (%d points, key %s)", row.Code, s.user.ID, points, key)
		return nil
	})
	if err != nil {
		return err
	}

	// Почта — после коммита, как и любое другое уведомление: неотправленное
	// письмо не должно откатывать выдачу, а откаченная выдача не должна была о
	// себе объявлять.
	if d.mail == nil {
		return nil
	}
	for i := range mails {
		if err := d.mail.Send(ctx, nil, &mails[i]); err != nil {
			log.Printf("[achievement] cannot post mail to %s: %v", s.user.ID, err)
		}
	}
	return nil
}

// capPoints ужимает начисление суточным потолком. Потолок — это цена накрутки:
// сколько её ни устраивай, за сутки больше этого не заработать.
//
// Срабатывание записывается инцидентом: сам по себе упёршийся потолок не
// авария, но исполнитель, упирающийся в него каждый день, — это то, на что
// стоит посмотреть глазами.
func (d *AchievementDispatcher) capPoints(ctx context.Context, tx *sql.Tx, userID uuid.UUID, points int, code string) int {
	if points <= 0 || d.levels == nil {
		return points
	}
	limit := d.levels.MaxPointsPerDay(ctx)
	if limit <= 0 {
		return points
	}
	total, err := d.achievements.BumpPointsToday(ctx, tx, userID, points)
	if err != nil {
		log.Printf("[achievement] cannot read the daily cap of %s: %v", userID, err)
		return points
	}
	if total <= limit {
		return points
	}
	allowed := points - (total - limit)
	if allowed < 0 {
		allowed = 0
	}
	if d.incidents != nil {
		_ = d.incidents.Record(ctx, tx, &repository.MoneyIncident{
			Kind: repository.IncidentPointsCapHit, Severity: repository.IncidentSeverityWarning,
			UserID: &userID,
			Details: map[string]interface{}{
				"achievement": code, "requested": points, "allowed": allowed, "limit": limit,
			},
		})
	}
	return allowed
}

// applyGift выдаёт подарок. Денежный проходит через тот же Ledger.Bonus, что и
// вознаграждения поведений, — со счёта платформы и под потолком; остальные
// занимают код или единицу склада и превращаются в купон.
//
// Пустой склад — не ошибка события. Отказ оставил бы его в очереди на повтор до
// исчерпания попыток, задерживая ачивки всех остальных; поэтому ачивка
// выдаётся, баллы начисляются, подарок — нет, а расхождение записывается
// инцидентом. Пустой склад это операционная проблема, а не повод останавливать
// очередь.
func (d *AchievementDispatcher) applyGift(ctx context.Context, tx *sql.Tx, userID uuid.UUID, granted *repository.UserAchievement, code string) (*repository.UserGift, error) {
	if d.gifts == nil {
		return nil, nil
	}
	gift, err := d.gifts.Get(ctx, code)
	if err != nil || gift == nil {
		d.giftIncident(ctx, tx, userID, code, "подарок не найден")
		return nil, nil
	}
	if !gift.IsActive {
		d.giftIncident(ctx, tx, userID, code, "подарок выключен")
		return nil, nil
	}

	if gift.Kind == repository.GiftKindBonus {
		amount := gift.Amount
		max := money.FromRubles(d.levels.MaxBonus(ctx))
		if amount > max {
			// Потолок — то, насколько сильно позволено ошибиться каталогу
			// подарков. Выдаётся зажатая сумма, а расхождение записывается.
			d.giftIncidentAmount(ctx, tx, userID, code, amount, max)
			amount = max
		}
		if !amount.IsPositive() {
			return nil, nil
		}
		if err := d.ledger.Bonus(ctx, tx, userID, amount, money.Zero, nil); err != nil {
			return nil, err
		}
	}

	issued, err := d.gifts.Issue(ctx, tx, gift, userID, &granted.ID)
	if errors.Is(err, repository.ErrGiftUnavailable) {
		d.giftIncident(ctx, tx, userID, code, "подарки кончились")
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	log.Printf("[AUDIT] gift %s issued to %s as coupon %s", code, userID, issued.CouponCode)
	return issued, nil
}

func (d *AchievementDispatcher) giftIncident(ctx context.Context, tx *sql.Tx, userID uuid.UUID, code, reason string) {
	log.Printf("[achievement] gift %s not issued to %s: %s", code, userID, reason)
	metrics.MoneyIncident(repository.IncidentGiftOutOfStock)
	if d.incidents == nil {
		return
	}
	_ = d.incidents.Record(ctx, tx, &repository.MoneyIncident{
		Kind: repository.IncidentGiftOutOfStock, Severity: repository.IncidentSeverityWarning,
		UserID: &userID, Details: map[string]interface{}{"gift": code, "reason": reason},
	})
}

func (d *AchievementDispatcher) giftIncidentAmount(ctx context.Context, tx *sql.Tx, userID uuid.UUID, code string, wanted, applied money.Amount) {
	log.Printf("[AUDIT] gift %s for %s clamped from %s to %s", code, userID, wanted, applied)
	metrics.MoneyIncident(repository.IncidentCommissionOutOfRange)
	if d.incidents == nil {
		return
	}
	_ = d.incidents.Record(ctx, tx, &repository.MoneyIncident{
		Kind: repository.IncidentCommissionOutOfRange, UserID: &userID,
		Expected: &applied, Actual: &wanted, Applied: &applied,
		Details: map[string]interface{}{"gift": code, "reason": "денежный подарок больше потолка"},
	})
}

// revokeForOrder отбирает то, что было выдано за отменённый заказ. Без этого
// накрутка сводится к «создать, закрыть, отменить»: заказ возвращается
// заказчику, а баллы остаются исполнителю.
func (d *AchievementDispatcher) revokeForOrder(ctx context.Context, orderID uuid.UUID) error {
	if d.achievements == nil {
		return nil
	}
	revoked, err := d.achievements.RevokeByOrder(ctx, nil, orderID, "order_canceled")
	if err != nil {
		return err
	}
	if revoked > 0 {
		log.Printf("[AUDIT] %d achievement grants revoked with order %s", revoked, orderID)
	}
	return nil
}

func giftMail(userID uuid.UUID, issued *repository.UserGift) repository.Mail {
	title := "Подарок"
	if issued.Gift != nil {
		if ru, ok := issued.Gift.Title["ru"].(string); ok && ru != "" {
			title = ru
		}
	}
	body := fmt.Sprintf("Ваш купон: %s.", issued.CouponCode)
	if issued.Gift != nil {
		switch issued.Gift.Kind {
		case repository.GiftKindBonus:
			body = fmt.Sprintf("Бонус %s зачислен на ваш счёт.", issued.Gift.Amount)
		case repository.GiftKindCertificate:
			body = "Подарочный сертификат ждёт вас в разделе «Подарки» — откройте карточку, чтобы увидеть код."
		case repository.GiftKindPhysical:
			body = fmt.Sprintf("Купон %s. Покажите его на пункте выдачи, чтобы получить подарок.", issued.CouponCode)
		}
	}
	return repository.Mail{
		UserID: userID, Kind: repository.MailKindGift, Subject: title, Body: body,
		RefType: "gift", RefID: issued.ID.String(),
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return "Новая ачивка"
}
