package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
	"time"

	"github.com/google/uuid"

	"healthlogin/backend/achievement"
	"healthlogin/backend/repository"
)

// Здесь живут две операции, которые просит администратор, а не событие:
// пересчёт условий по истории и выдача вручную. Обе кончаются тем же apply, что
// и обычная выдача, — значит и потолок баллов, и подарки, и почта, и запись в
// аудит у них общие с ней. Своей копии этой логики у админских кнопок нет
// намеренно: разойдясь, копия начала бы платить по другим правилам.

// ErrAchievementNotGrantable сообщает, что ачивку нельзя выдать вручную: её
// нет, она выключена или её скрипт не загружен.
var ErrAchievementNotGrantable = errors.New("achievement cannot be granted")

// recheckOrderLimit ограничивает глубину пересчёта. Пересчёт — это вызов хука
// на каждый заказ и чтение свода после каждой выдачи, то есть работа, линейная
// по истории; у исполнителя с тысячей заказов она успеет надоесть админу,
// который нажал кнопку. Свежих заказов хватает: ачивка, не выданная год назад,
// почти всегда не выдана из-за правила, а не из-за сбоя.
const recheckOrderLimit = 200

// RecheckResult — что сделал пересчёт. Он возвращается администратору целиком:
// «ничего не выдано» — такой же осмысленный ответ, как список кодов, и по числу
// прогнанных заказов видно, что кнопка вообще что-то сделала.
type RecheckResult struct {
	OrdersReplayed int      `json:"orders_replayed"`
	Granted        []string `json:"granted"`
}

// RecheckUser прогоняет условия действующих ачивок по истории пользователя и
// выдаёт заслуженное.
//
// Пересчёт повторяет подтверждённые заказы — те же факты, что видел бы
// диспетчер, — от старых к новым, потому что порядок выдач бывает виден в
// правилах: ачивка вправе требовать другую. Агрегаты при этом берутся
// сегодняшние, а не восстановленные на момент заказа: восстановить их
// невозможно, а притворяться, что можно, — худший из вариантов.
//
// Отсюда граница применимости, которую стоит знать заранее: пересчёт видит
// только то, что следует из заказов. Ачивка, срабатывающая на другом событии,
// им не покрывается — такую выдают вручную.
func (d *AchievementDispatcher) RecheckUser(ctx context.Context, userID uuid.UUID) (RecheckResult, error) {
	result := RecheckResult{Granted: []string{}}
	if d == nil || d.engine == nil {
		return result, nil
	}

	user, err := d.users.FindByID(ctx, userID)
	if err != nil {
		return result, err
	}
	rows, err := d.achievements.ListActive(ctx)
	if err != nil {
		return result, err
	}
	orders, err := d.orders.FindAllByExecutor(ctx, userID, recheckOrderLimit)
	if err != nil {
		return result, err
	}

	// От старых к новым: FindAllByExecutor отдаёт свежие первыми.
	sort.SliceStable(orders, func(i, j int) bool {
		return orderTime(&orders[i]).Before(orderTime(&orders[j]))
	})

	now := time.Now()
	for i := range orders {
		order := &orders[i]
		if err := d.eligible(ctx, order); err != nil {
			continue
		}
		customer, err := d.users.FindByID(ctx, order.CustomerID)
		if err != nil {
			return result, err
		}
		s := subject{user: user, order: order, counterparty: customer, audience: achievement.AudienceExecutor}
		result.OrdersReplayed++

		// Факты собираются заново на каждый заказ: предыдущая итерация могла
		// что-то выдать, а следующая обязана это видеть — и чтобы не предложить
		// разовую ачивку второй раз, и потому что правило вправе смотреть на
		// уже выданное.
		facts, err := d.facts(ctx, &repository.DomainEvent{Type: repository.EventOrderConfirmed}, s, now)
		if err != nil {
			return result, err
		}
		for _, row := range rows {
			manifest, ok := d.engine.Manifest(row.Code)
			if !ok || !manifest.Handles(repository.EventOrderConfirmed) || manifest.Audience != s.audience {
				continue
			}
			if !row.AvailableAt(now) {
				continue
			}
			if manifest.OncePerUser {
				if _, has := facts.Granted[row.Code]; has {
					continue
				}
			}
			facts.Config = row.Config
			grant, err := d.engine.Check(row.Code, facts)
			if err != nil {
				log.Printf("[achievement] recheck %s: check failed: %v", row.Code, err)
				continue
			}
			if grant == nil {
				continue
			}
			issued, err := d.apply(ctx, nil, s, row, manifest, grant, now)
			if err != nil {
				return result, err
			}
			if issued {
				result.Granted = append(result.Granted, row.Code)
			}
		}
	}

	log.Printf("[AUDIT] achievements rechecked for %s: %d orders replayed, granted %v",
		userID, result.OrdersReplayed, result.Granted)
	return result, nil
}

// orderTime — момент, по которому заказ занимает место в истории.
func orderTime(o *repository.Order) time.Time {
	if o.CompletedAt != nil {
		return *o.CompletedAt
	}
	return o.CreatedAt
}

// GrantManually выдаёт ачивку по решению администратора, минуя её правило.
//
// Правило обходится намеренно и только здесь: причины выдать значок вручную —
// разобранная жалоба, компенсация за сбой, акция, придуманная после того, как
// скрипт написан, — все лежат вне того, что скрипт может увидеть в фактах.
// Обходится при этом только условие: вес, срок жизни баллов, суточный потолок,
// почта и запись в аудит остаются те же, что у обычной выдачи, потому что это
// та же выдача.
//
// Подарки вручную не выдаются: подарок — это деньги или код со склада, и его
// выдача проходит своей дверью, где есть остаток, потолок суммы и аудит.
func (d *AchievementDispatcher) GrantManually(ctx context.Context, userID uuid.UUID, code, reason string) (*repository.Achievement, error) {
	if d == nil || d.engine == nil {
		return nil, ErrAchievementNotGrantable
	}
	row, err := d.achievements.Get(ctx, code)
	if err != nil {
		return nil, ErrAchievementNotGrantable
	}
	// Выключенную выдать нельзя. Выключенная ачивка — это правило, которое
	// администратор счёл неготовым; ручная выдача по нему обошла бы это решение
	// молча, а её баллы всё так же снижали бы комиссию.
	if row.DeletedAt != nil || !row.IsActive {
		return nil, ErrAchievementNotGrantable
	}
	manifest, ok := d.engine.Manifest(code)
	if !ok {
		return nil, ErrAchievementNotGrantable
	}
	user, err := d.users.FindByID(ctx, userID)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	grant := &achievement.Grant{
		Reason: firstNonEmpty(reason, "выдана администратором"),
		Effects: []achievement.Effect{{
			Kind:    achievement.EffectNotify,
			Subject: manifest.Title,
			Text:    firstNonEmpty(manifest.Description, manifest.Title),
		}},
	}
	if !manifest.OncePerUser {
		// У повторяемой ачивки ключ выдачи называет скрипт; здесь скрипта нет,
		// поэтому ключом становится момент — две ручные выдачи подряд это две
		// разные выдачи, и слипаться в одну они не должны.
		grant.Key = fmt.Sprintf("manual:%d", now.UnixNano())
	}

	s := subject{user: user, audience: manifest.Audience}
	issued, err := d.apply(ctx, nil, s, row, manifest, grant, now)
	if err != nil {
		return nil, err
	}
	if !issued {
		// Разовая ачивка, которая у человека уже есть. Это не ошибка сервера, а
		// ответ на вопрос «выдать ещё раз»: нет, она уже выдана.
		return nil, repository.ErrAchievementAlreadyGranted
	}
	return row, nil
}
