package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"healthlogin/backend/money"
	"healthlogin/backend/repository"
)

// ShopOrders — обработка покупок (право shop_orders) и выручка магазина
// (shop_revenue): статусы, отмена с возвратом, история пользователя, вывод
// денег, ссылки на покупки в чате поддержки.
type ShopOrders struct {
	orders  repository.ShopOrderRepository
	gifts   repository.GiftRepository
	perks   repository.PerkRepository
	ledger  *Ledger
	mail    repository.MailRepository
	details shopOrderDetails
	now     func() time.Time
}

// NewShopOrders собирает обработку покупок.
func NewShopOrders(orders repository.ShopOrderRepository, gifts repository.GiftRepository,
	perks repository.PerkRepository, ledger *Ledger) *ShopOrders {
	return &ShopOrders{orders: orders, gifts: gifts, perks: perks, ledger: ledger, now: time.Now,
		details: shopOrderDetails{orders: orders, gifts: gifts, perks: perks, now: time.Now}}
}

// WithMail подключает внутреннюю почту: письма о смене статуса и отмене.
func (s *ShopOrders) WithMail(mail repository.MailRepository) *ShopOrders {
	s.mail = mail
	return s
}

// AdminOrders — список покупок для админки.
func (s *ShopOrders) AdminOrders(ctx context.Context, filter repository.ShopOrderFilter) ([]*repository.ShopOrder, int, error) {
	return s.orders.List(ctx, filter)
}

// AdminOrder — карточка покупки: снимок товара, купоны, привилегии,
// проводки, чат поддержки покупателя.
func (s *ShopOrders) AdminOrder(ctx context.Context, id uuid.UUID) (*repository.ShopOrder, error) {
	order, err := s.orders.Get(ctx, nil, id)
	if errors.Is(err, repository.ErrShopOrderNotFound) {
		return nil, shopNotFound()
	}
	if err != nil {
		return nil, err
	}
	return s.details.attachAdmin(ctx, order)
}

// CountPaid — покупки, ждущие обработки: бейдж на пункте меню.
func (s *ShopOrders) CountPaid(ctx context.Context) (int, error) {
	return s.orders.CountByStatus(ctx, repository.ShopOrderPaid)
}

// shopTransitions — переходы, которые администратор делает руками
// (implementation_plan_shop.md §4.3). Отмена сюда не входит: у неё свой путь
// с возвратом денег.
var shopTransitions = map[string][]string{
	repository.ShopOrderPaid:       {repository.ShopOrderProcessing},
	repository.ShopOrderProcessing: {repository.ShopOrderShipped},
	repository.ShopOrderShipped:    {repository.ShopOrderCompleted},
}

var shopStatusTitles = map[string]string{
	repository.ShopOrderProcessing: "взят в работу",
	repository.ShopOrderShipped:    "отправлен",
	repository.ShopOrderCompleted:  "выполнен",
	repository.ShopOrderCanceled:   "отменён",
}

// SetStatus переводит покупку вещи в следующий статус. SHIPPED для доставки
// требует трек-номер; для самовывоза это «готово к выдаче».
func (s *ShopOrders) SetStatus(ctx context.Context, adminID, id uuid.UUID, status, track string) (*repository.ShopOrder, error) {
	track = strings.TrimSpace(track)
	var order *repository.ShopOrder
	err := s.ledger.RunInTx(ctx, func(tx *sql.Tx) error {
		var err error
		order, err = s.orders.Lock(ctx, tx, id)
		if errors.Is(err, repository.ErrShopOrderNotFound) {
			return shopNotFound()
		}
		if err != nil {
			return err
		}
		if kind, _ := order.ProductSnapshot["kind"].(string); kind != repository.ShopKindPhysical {
			return shopErr(http.StatusConflict, ShopErrInvalidTransition, "Статус меняется только у вещей")
		}
		allowed := false
		for _, next := range shopTransitions[order.Status] {
			if next == status {
				allowed = true
			}
		}
		if !allowed {
			return shopErr(http.StatusConflict, ShopErrInvalidTransition,
				fmt.Sprintf("Из статуса %s в %s перейти нельзя", order.Status, status))
		}
		if status == repository.ShopOrderShipped {
			method, _ := order.Fulfillment["method"].(string)
			if method == repository.ShopFulfillmentDelivery && track == "" {
				return shopValidation(map[string]string{"track": "Для доставки нужен трек-номер"})
			}
			if track != "" {
				order.Fulfillment["track"] = track
			}
		}
		if err := s.orders.SetStatus(ctx, tx, id, status, order.Fulfillment); err != nil {
			return err
		}
		order.Status = status
		shopNotify(ctx, s.mail, tx, order.UserID, order, statusMailBody(order))
		return nil
	})
	if err != nil {
		return nil, err
	}
	log.Printf("[AUDIT] admin %s moved shop order №%d to %s", adminID, order.Number, status)
	return s.AdminOrder(ctx, id)
}

func statusMailBody(order *repository.ShopOrder) string {
	body := fmt.Sprintf("Заказ №%d (%s) %s.", order.Number, productTitle(order), shopStatusTitles[order.Status])
	if order.Status == repository.ShopOrderShipped {
		if track, _ := order.Fulfillment["track"].(string); track != "" {
			body += " Трек-номер: " + track + "."
		} else if method, _ := order.Fulfillment["method"].(string); method == repository.ShopFulfillmentPickup {
			body = fmt.Sprintf("Заказ №%d (%s) готов к выдаче. Покажите купон в пункте выдачи.", order.Number, productTitle(order))
		}
	}
	return body
}

// OnCouponRedeemed закрывает покупку, когда погашен последний её купон: вещь
// выдана на руки (implementation_plan_shop.md §7, погашение купона).
func (s *ShopOrders) OnCouponRedeemed(ctx context.Context, coupon *repository.UserGift) error {
	if coupon == nil || coupon.ShopOrderID == nil {
		return nil
	}
	return s.ledger.RunInTx(ctx, func(tx *sql.Tx) error {
		order, err := s.orders.Lock(ctx, tx, *coupon.ShopOrderID)
		if err != nil {
			return err
		}
		if order.Status == repository.ShopOrderCompleted || order.Status == repository.ShopOrderCanceled {
			return nil
		}
		coupons, err := s.gifts.ListByShopOrder(ctx, tx, order.ID)
		if err != nil {
			return err
		}
		for _, c := range coupons {
			if c.Status != repository.GiftStatusRedeemed && c.Status != repository.GiftStatusCanceled {
				return nil
			}
		}
		if err := s.orders.SetStatus(ctx, tx, order.ID, repository.ShopOrderCompleted, order.Fulfillment); err != nil {
			return err
		}
		order.Status = repository.ShopOrderCompleted
		shopNotify(ctx, s.mail, tx, order.UserID, order, statusMailBody(order))
		return nil
	})
}

// RefundQuote — сумма, которую сервер предлагает вернуть при отмене, и то,
// что отмена сделает с выдачей.
type RefundQuote struct {
	Suggested money.Amount `json:"suggested"`
	// Max — сколько вообще можно вернуть: уплачено минус уже возвращено.
	Max money.Amount `json:"max"`
	// CertificateRevealed — код сертификата уже показан: возврат только с
	// подтверждением партнёра, что код не использован, и код в пул не вернётся.
	CertificateRevealed bool `json:"certificate_revealed"`
	// Redeemed — сколько купонов уже погашено: вещь на руках.
	Redeemed int `json:"redeemed"`
}

// RefundQuote считает предложение возврата (implementation_plan_shop.md §4.3).
func (s *ShopOrders) RefundQuote(ctx context.Context, id uuid.UUID) (*RefundQuote, error) {
	order, err := s.AdminOrder(ctx, id)
	if err != nil {
		return nil, err
	}
	return s.refundQuote(order), nil
}

func (s *ShopOrders) refundQuote(order *repository.ShopOrder) *RefundQuote {
	quote := &RefundQuote{Max: order.Total.Sub(order.RefundedAmount)}
	if quote.Max < 0 {
		quote.Max = 0
	}
	kind, _ := order.ProductSnapshot["kind"].(string)
	switch kind {
	case repository.ShopKindPerk:
		now := s.now()
		for _, perk := range order.Perks {
			quote.Suggested += proportionalRefund(order.UnitPrice, perk, now)
		}
	default:
		for _, c := range order.Coupons {
			if c.RevealedAt != nil || c.Status == repository.GiftStatusRevealed {
				quote.CertificateRevealed = kind == repository.ShopKindCertificate
			}
			if c.Status == repository.GiftStatusRedeemed {
				quote.Redeemed++
			}
		}
		quote.Suggested = quote.Max
	}
	if quote.Suggested > quote.Max {
		quote.Suggested = quote.Max
	}
	return quote
}

// proportionalRefund считает возврат за идущую привилегию — пропорционально
// неиспользованным дням (оферта, п. 8.6). Начатый день считается
// использованным: в первый день возвращается (дни − 1)/дни, в последний —
// ничего. Не начавшаяся возвращается полностью, истёкшая — нет.
func proportionalRefund(price money.Amount, perk *repository.UserPerk, now time.Time) money.Amount {
	if perk.RevokedAt != nil {
		return 0
	}
	if !now.After(perk.StartsAt) {
		return price
	}
	if !now.Before(perk.ExpiresAt) {
		return 0
	}
	total := perk.ExpiresAt.Sub(perk.StartsAt).Hours() / 24
	days := int(math.Round(total))
	if days <= 0 {
		return 0
	}
	used := int(math.Ceil(now.Sub(perk.StartsAt).Hours() / 24))
	if used >= days {
		return 0
	}
	return money.Amount(int64(price) * int64(days-used) / int64(days))
}

// CancelRequest — отмена покупки администратором.
type CancelRequest struct {
	Reason string `json:"reason"`
	// Amount — сумма возврата; nil — предложенная сервером.
	Amount *money.Amount `json:"amount"`
	// Restock возвращает вещь на склад.
	Restock bool `json:"restock"`
	// PartnerConfirmed — партнёр подтвердил, что показанный код не
	// использован. Без этого показанный сертификат не отменяется.
	PartnerConfirmed bool `json:"partner_confirmed"`
}

// Cancel отменяет покупку с возвратом (implementation_plan_shop.md §4.3).
// Возврат и снятие выдачи — одна транзакция; отменить может только
// администратор, по обращению в чат поддержки.
func (s *ShopOrders) Cancel(ctx context.Context, adminID, id uuid.UUID, req CancelRequest) (*repository.ShopOrder, error) {
	req.Reason = strings.TrimSpace(req.Reason)
	if req.Reason == "" {
		return nil, shopValidation(map[string]string{"reason": "Укажите причину отмены"})
	}
	var refund money.Amount
	var order *repository.ShopOrder
	err := s.ledger.RunInTx(ctx, func(tx *sql.Tx) error {
		var err error
		order, err = s.orders.Lock(ctx, tx, id)
		if errors.Is(err, repository.ErrShopOrderNotFound) {
			return shopNotFound()
		}
		if err != nil {
			return err
		}
		if order.Status == repository.ShopOrderCanceled {
			return shopErr(http.StatusConflict, ShopErrAlreadyCanceled, "Покупка уже отменена")
		}
		if order.Coupons, err = s.gifts.ListByShopOrder(ctx, tx, order.ID); err != nil {
			return err
		}
		MarkExpiredGifts(order.Coupons, s.now())
		if order.Perks, err = s.perks.ListByShopOrder(ctx, tx, order.ID); err != nil {
			return err
		}
		quote := s.refundQuote(order)
		if quote.CertificateRevealed && !req.PartnerConfirmed {
			return shopErr(http.StatusConflict, ShopErrCertificateRevealed,
				"Код уже показан: отмена только с подтверждением партнёра, что он не использован")
		}
		refund = quote.Suggested
		if req.Amount != nil {
			refund = *req.Amount
		}
		if refund < 0 || refund > quote.Max {
			return shopValidation(map[string]string{"amount": fmt.Sprintf("Сумма от 0 до %s", quote.Max)})
		}

		kind, _ := order.ProductSnapshot["kind"].(string)
		switch kind {
		case repository.ShopKindPerk:
			// Отзываются все привилегии покупки, и будущие тоже; следующие в
			// очереди не сдвигаются.
			for _, perk := range order.Perks {
				if perk.RevokedAt != nil {
					continue
				}
				if _, err := s.perks.Revoke(ctx, tx, perk.ID, adminID); err != nil && !errors.Is(err, repository.ErrPerkNotFound) {
					return err
				}
			}
		default:
			if _, err := s.gifts.CancelShopCoupons(ctx, tx, order.ID); err != nil {
				return err
			}
			if req.Restock && kind == repository.ShopKindPhysical {
				if code, _ := order.ProductSnapshot["gift_code"].(string); code != "" {
					if err := s.gifts.RestoreStock(ctx, tx, code, order.Quantity); err != nil {
						return err
					}
				}
			}
		}

		if err := s.ledger.ShopRefund(ctx, tx, order.UserID, refund, order.ID, adminID); err != nil {
			return err
		}
		if err := s.orders.Cancel(ctx, tx, order.ID, req.Reason, adminID, refund); err != nil {
			return err
		}
		order.Status = repository.ShopOrderCanceled
		body := fmt.Sprintf("Заказ №%d (%s) отменён.", order.Number, productTitle(order))
		if refund.IsPositive() {
			body += fmt.Sprintf(" На баланс возвращено %s.", refund)
		}
		shopNotify(ctx, s.mail, tx, order.UserID, order, body)
		return nil
	})
	if err != nil {
		return nil, err
	}
	log.Printf("[AUDIT] admin %s canceled shop order №%d, refunded %s, restock=%v: %s",
		adminID, order.Number, refund, req.Restock, req.Reason)
	return s.AdminOrder(ctx, id)
}

// UserShopHistory — покупки и привилегии пользователя для его карточки.
type UserShopHistory struct {
	Orders []*repository.ShopOrder `json:"orders"`
	Perks  []*repository.UserPerk  `json:"perks"`
}

// UserHistory возвращает покупки и привилегии пользователя. Покупки — в том
// же виде, что и у самого покупателя (MyOrders): с купонами и привилегиями,
// чтобы карточка показывала, что именно выдано.
func (s *ShopOrders) UserHistory(ctx context.Context, userID uuid.UUID) (*UserShopHistory, error) {
	orders, err := s.orders.ListForUser(ctx, userID, 100)
	if err != nil {
		return nil, err
	}
	if err := s.details.attachAll(ctx, orders); err != nil {
		return nil, err
	}
	perks, err := s.perks.ListForUser(ctx, userID, 100)
	if err != nil {
		return nil, err
	}
	return &UserShopHistory{Orders: orders, Perks: perks}, nil
}

// ShopRevenue — выручка магазина.
type ShopRevenue struct {
	Balance  money.Amount               `json:"balance"`
	From     time.Time                  `json:"from"`
	To       time.Time                  `json:"to"`
	Sales    []*repository.ShopSalesRow `json:"sales"`
	Total    money.Amount               `json:"total"`
	Refunded money.Amount               `json:"refunded"`
}

// Revenue — остаток счёта SHOP и продажи по товарам за период [from, to).
func (s *ShopOrders) Revenue(ctx context.Context, from, to time.Time) (*ShopRevenue, error) {
	account, err := s.ledger.AccountBalance(ctx, repository.AccountShop)
	if err != nil {
		return nil, err
	}
	sales, err := s.orders.Sales(ctx, from, to)
	if err != nil {
		return nil, err
	}
	out := &ShopRevenue{Balance: account.Balance, From: from, To: to, Sales: sales}
	for _, row := range sales {
		out.Total += row.Total
		out.Refunded += row.Refunded
	}
	return out, nil
}

// Payout выводит выручку из системы. Списание охраняется остатком счёта:
// два одновременных вывода не заберут больше, чем собрано.
func (s *ShopOrders) Payout(ctx context.Context, adminID uuid.UUID, amount money.Amount) (money.Amount, error) {
	if !amount.IsPositive() {
		return 0, shopValidation(map[string]string{"amount": "Сумма больше нуля"})
	}
	err := s.ledger.RunInTx(ctx, func(tx *sql.Tx) error {
		return s.ledger.ShopPayout(ctx, tx, adminID, amount)
	})
	if errors.Is(err, repository.ErrInsufficientFunds) {
		return 0, shopErr(http.StatusConflict, ShopErrInsufficientRevenue, "На счёте магазина меньше запрошенной суммы")
	}
	if err != nil {
		return 0, err
	}
	log.Printf("[AUDIT] admin %s withdrew %s from the shop account", adminID, amount)
	account, err := s.ledger.AccountBalance(ctx, repository.AccountShop)
	if err != nil {
		return 0, err
	}
	return account.Balance, nil
}

// shopNumberInText находит номера покупок в сообщении: «№1042», «№ 1042».
var shopNumberInText = regexp.MustCompile(`№\s*(\d+)`)

// ShopOrderLinks переводит номера покупок, упомянутые в сообщениях
// покупателя, в ссылки на их карточки (implementation_plan_shop.md §4.4).
// Ищутся только покупки владельца чата: чужой номер ссылкой не станет. На
// деньги разметка не влияет — отменяет всегда человек.
func (s *ShopOrders) ShopOrderLinks(ctx context.Context, ownerID uuid.UUID, messages []*repository.Message) error {
	var numbers []int64
	seen := map[int64]bool{}
	for _, m := range messages {
		if m.SenderID != ownerID {
			continue
		}
		for _, match := range shopNumberInText.FindAllStringSubmatch(m.Text, -1) {
			if n, ok := parseShopNumber(match[1]); ok && !seen[n] {
				seen[n] = true
				numbers = append(numbers, n)
			}
		}
	}
	if len(numbers) == 0 {
		return nil
	}
	ids, err := s.orders.Numbers(ctx, ownerID, numbers)
	if err != nil {
		return err
	}
	for _, m := range messages {
		if m.SenderID != ownerID {
			continue
		}
		for _, match := range shopNumberInText.FindAllStringSubmatch(m.Text, -1) {
			n, _ := parseShopNumber(match[1])
			if id, ok := ids[n]; ok {
				m.ShopOrders = append(m.ShopOrders, repository.ShopOrderRef{Number: n, ID: id})
			}
		}
	}
	return nil
}
