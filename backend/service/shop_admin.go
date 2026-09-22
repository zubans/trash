package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"healthlogin/backend/money"
	"healthlogin/backend/repository"
)

// maxShopImages — сколько изображений у карточки товара.
const maxShopImages = 5

// ShopImagePrefix — где лежат изображения товаров. Другой путь в карточке
// означал бы чужой файл: вложение чата или фото-подтверждение.
const ShopImagePrefix = "/uploads/shop/"

var shopCodePattern = regexp.MustCompile(`^[a-z0-9_-]{1,32}$`)
var variantCodePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,32}$`)

// AdminProducts — все товары, включая выключенные, с точным остатком.
func (s *ShopService) AdminProducts(ctx context.Context, kind, category string) ([]*repository.ShopProduct, error) {
	filter := repository.ShopProductFilter{AllRoles: true}
	if kind != "" {
		filter.Kind = &kind
	}
	if category != "" {
		filter.Category = &category
	}
	return s.shop.ListProducts(ctx, filter)
}

// AdminProduct — один товар для формы.
func (s *ShopService) AdminProduct(ctx context.Context, id uuid.UUID) (*repository.ShopProduct, error) {
	p, err := s.shop.GetProduct(ctx, id)
	if errors.Is(err, repository.ErrShopProductNotFound) {
		return nil, shopNotFound()
	}
	return p, err
}

// SaveProduct проверяет и сохраняет товар. Новый — при нулевом id. Удаления
// нет: товар с продажами только выключается, на него ссылаются покупки.
func (s *ShopService) SaveProduct(ctx context.Context, adminID uuid.UUID, p *repository.ShopProduct) (*repository.ShopProduct, error) {
	var previous *repository.ShopProduct
	if p.ID != uuid.Nil {
		existing, err := s.shop.GetProduct(ctx, p.ID)
		if errors.Is(err, repository.ErrShopProductNotFound) {
			return nil, shopNotFound()
		}
		if err != nil {
			return nil, err
		}
		previous = existing
	}
	if err := s.validateProduct(ctx, p); err != nil {
		return nil, err
	}
	if previous != nil {
		if err := s.guardSalesFreeze(ctx, previous, p); err != nil {
			return nil, err
		}
	}
	if err := s.shop.UpsertProduct(ctx, p); err != nil {
		return nil, err
	}
	switch {
	case previous == nil:
		log.Printf("[AUDIT] admin %s created shop product %s (%s) at %s", adminID, p.ID, p.Kind, p.Price)
	case previous.Price != p.Price:
		// Смена цены — то, о чём потом спрашивают: «я видел другую цену».
		log.Printf("[AUDIT] admin %s changed the price of shop product %s: %s -> %s", adminID, p.ID, previous.Price, p.Price)
	}
	return s.AdminProduct(ctx, p.ID)
}

// validateProduct проверяет товар по его роду. Ошибки возвращаются по полям,
// чтобы форма показала их у поля, а не одной строкой.
func (s *ShopService) validateProduct(ctx context.Context, p *repository.ShopProduct) error {
	fields := map[string]string{}
	p.Category = strings.TrimSpace(p.Category)
	if !shopCodePattern.MatchString(p.Category) {
		fields["category"] = "Код категории: латиница, цифры, «-» и «_», до 32 знаков"
	}
	if title, _ := p.Title["ru"].(string); strings.TrimSpace(title) == "" {
		fields["title"] = "Название на русском обязательно"
	}
	if !p.Price.IsPositive() {
		fields["price"] = "Цена больше нуля"
	}
	if p.CompareAtPrice != nil && *p.CompareAtPrice <= p.Price {
		fields["compare_at_price"] = "Старая цена должна быть больше цены"
	}
	if len(p.Images) > maxShopImages {
		fields["images"] = fmt.Sprintf("Не больше %d изображений", maxShopImages)
	}
	for _, img := range p.Images {
		if !strings.HasPrefix(img, ShopImagePrefix) || strings.Contains(img, "..") {
			fields["images"] = "Изображения загружаются через форму товара"
		}
	}
	if p.PerUserLimit != nil && *p.PerUserLimit <= 0 {
		fields["per_user_limit"] = "Лимит больше нуля или пусто"
	}
	if err := s.validateRoles(ctx, p.Roles); err != "" {
		fields["roles"] = err
	}

	switch p.Kind {
	case repository.ShopKindPerk:
		kind := ""
		if p.PerkKind != nil {
			kind = *p.PerkKind
		}
		if err := ValidatePerk(kind, p.PerkValue, perkDays(p)); err != nil {
			switch {
			case kind == "":
				fields["perk_kind"] = "Выберите вид привилегии"
			case perkDays(p) <= 0 && validatePerkValue(kind, p.PerkValue) == nil:
				fields["perk_days"] = "Срок обязателен и больше нуля"
			default:
				fields["perk_value"] = strings.TrimPrefix(err.Error(), ErrInvalidPerk.Error()+": ")
			}
		}
		if p.MaxActivePerUser != nil && *p.MaxActivePerUser <= 0 {
			fields["max_active_per_user"] = "Больше нуля или пусто"
		}
		// Поля чужого рода — всегда ошибка ввода: у привилегии нет подарка,
		// вариантов и способов получения, она выдаётся строкой user_perks.
		if p.GiftCode != nil {
			fields["gift_code"] = "У привилегии нет подарка"
		}
		if len(p.Variants) > 0 || len(p.FulfillmentMethods) > 0 {
			fields["fulfillment_methods"] = "У привилегии нет вариантов и способов получения"
		}
		// Две привилегии в одной покупке встали бы в очередь, которую никто не
		// выбирал: привилегия продаётся по одной.
		p.MaxQtyPerOrder = 1
	case repository.ShopKindPhysical, repository.ShopKindCertificate:
		if p.PerkKind != nil || p.PerkValue != nil || p.PerkDays != nil || p.MaxActivePerUser != nil {
			fields["perk_kind"] = "Поля привилегии есть только у привилегии"
		}
		if p.GiftCode == nil || *p.GiftCode == "" {
			fields["gift_code"] = "Выберите подарок, которым выдаётся товар"
		} else {
			gift, err := s.gifts.Get(ctx, *p.GiftCode)
			switch {
			case errors.Is(err, sql.ErrNoRows):
				fields["gift_code"] = "Такого подарка нет"
			case err != nil:
				return err
			case gift.Kind != p.Kind:
				// Склад и пул кодов общие с ачивками: подарок другого рода
				// сломал бы выдачу на первой же покупке.
				fields["gift_code"] = "Подарок другого рода"
			}
		}
		if p.Kind == repository.ShopKindCertificate {
			// Код показывается в приложении: получать его негде, и «количества»
			// у кода нет.
			if len(p.Variants) > 0 || len(p.FulfillmentMethods) > 0 {
				fields["fulfillment_methods"] = "У сертификата нет вариантов и способов получения"
			}
			p.MaxQtyPerOrder = 1
			break
		}
		if p.MaxQtyPerOrder <= 0 {
			p.MaxQtyPerOrder = 1
		}
		if len(p.FulfillmentMethods) == 0 {
			fields["fulfillment_methods"] = "Включите хотя бы один способ получения"
		}
		for _, m := range p.FulfillmentMethods {
			if m != repository.ShopFulfillmentPickup && m != repository.ShopFulfillmentDelivery {
				fields["fulfillment_methods"] = "Неизвестный способ получения"
			}
		}
		seen := map[string]bool{}
		for _, v := range p.Variants {
			if !variantCodePattern.MatchString(v.Code) || seen[v.Code] {
				fields["variants"] = "Коды вариантов уникальны: латиница и цифры, до 32 знаков"
			}
			seen[v.Code] = true
		}
	default:
		fields["kind"] = "Неизвестный род товара"
	}
	if len(fields) > 0 {
		return shopValidation(fields)
	}
	return nil
}

// guardSalesFreeze запрещает менять род и подарок у товара с продажами:
// снимок в покупке говорит, что человек купил, а купоны уже лежат на складе
// этого подарка. Цену, название и активность менять можно.
func (s *ShopService) guardSalesFreeze(ctx context.Context, previous, next *repository.ShopProduct) error {
	sameGift := (previous.GiftCode == nil && next.GiftCode == nil) ||
		(previous.GiftCode != nil && next.GiftCode != nil && *previous.GiftCode == *next.GiftCode)
	if previous.Kind == next.Kind && sameGift {
		return nil
	}
	sold, err := s.shop.CountProductOrders(ctx, previous.ID)
	if err != nil {
		return err
	}
	if sold > 0 {
		return shopValidation(map[string]string{
			"kind": "У товара есть продажи: род и подарок менять нельзя — снимите его с витрины и заведите новый",
		})
	}
	return nil
}

func (s *ShopService) validateRoles(ctx context.Context, roles []string) string {
	if len(roles) == 0 || s.roles == nil {
		return ""
	}
	known, err := s.roles.List(ctx)
	if err != nil {
		return "Не удалось прочитать справочник ролей"
	}
	codes := map[string]bool{}
	for _, r := range known {
		codes[r.Code] = true
	}
	for _, r := range roles {
		if !codes[r] {
			return "Роли " + r + " нет в справочнике"
		}
	}
	return ""
}

// AdminPickupPoints — весь справочник пунктов выдачи.
func (s *ShopService) AdminPickupPoints(ctx context.Context) ([]*repository.ShopPickupPoint, error) {
	return s.shop.ListPickupPoints(ctx, false)
}

// SavePickupPoint заводит или правит пункт выдачи. Удаления нет: пункт
// выключается и перестаёт предлагаться при оформлении, а покупки, которые на
// него ссылаются, остаются читаемыми.
func (s *ShopService) SavePickupPoint(ctx context.Context, adminID uuid.UUID, p *repository.ShopPickupPoint) (*repository.ShopPickupPoint, error) {
	fields := map[string]string{}
	if title, _ := p.Title["ru"].(string); strings.TrimSpace(title) == "" {
		fields["title"] = "Название обязательно"
	}
	p.Address = strings.TrimSpace(p.Address)
	if p.Address == "" {
		fields["address"] = "Адрес обязателен"
	}
	if len(fields) > 0 {
		return nil, shopValidation(fields)
	}
	var err error
	if p.ID == uuid.Nil {
		err = s.shop.CreatePickupPoint(ctx, p)
	} else {
		err = s.shop.UpdatePickupPoint(ctx, p)
	}
	if errors.Is(err, repository.ErrShopPickupPointNotFound) {
		return nil, shopNotFound()
	}
	if err != nil {
		return nil, err
	}
	log.Printf("[AUDIT] admin %s saved pickup point %s (active=%v)", adminID, p.ID, p.IsActive)
	return p, nil
}

// AdminOrders — список покупок для админки.
func (s *ShopService) AdminOrders(ctx context.Context, filter repository.ShopOrderFilter) ([]*repository.ShopOrder, int, error) {
	return s.orders.List(ctx, filter)
}

// AdminOrder — карточка покупки: снимок товара, купоны, привилегии,
// проводки, чат поддержки покупателя.
func (s *ShopService) AdminOrder(ctx context.Context, id uuid.UUID) (*repository.ShopOrder, error) {
	order, err := s.orders.Get(ctx, nil, id)
	if errors.Is(err, repository.ErrShopOrderNotFound) {
		return nil, shopNotFound()
	}
	if err != nil {
		return nil, err
	}
	return s.withDetails(ctx, order, true)
}

// CountPaid — покупки, ждущие обработки: бейдж на пункте меню.
func (s *ShopService) CountPaid(ctx context.Context) (int, error) {
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
func (s *ShopService) SetStatus(ctx context.Context, adminID, id uuid.UUID, status, track string) (*repository.ShopOrder, error) {
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
		s.notify(ctx, tx, order.UserID, order, statusMailBody(order))
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
func (s *ShopService) OnCouponRedeemed(ctx context.Context, coupon *repository.UserGift) error {
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
		s.notify(ctx, tx, order.UserID, order, statusMailBody(order))
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
func (s *ShopService) RefundQuote(ctx context.Context, id uuid.UUID) (*RefundQuote, error) {
	order, err := s.AdminOrder(ctx, id)
	if err != nil {
		return nil, err
	}
	return s.refundQuote(order), nil
}

func (s *ShopService) refundQuote(order *repository.ShopOrder) *RefundQuote {
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
func (s *ShopService) Cancel(ctx context.Context, adminID, id uuid.UUID, req CancelRequest) (*repository.ShopOrder, error) {
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
		s.notify(ctx, tx, order.UserID, order, body)
		return nil
	})
	if err != nil {
		return nil, err
	}
	log.Printf("[AUDIT] admin %s canceled shop order №%d, refunded %s, restock=%v: %s",
		adminID, order.Number, refund, req.Restock, req.Reason)
	return s.AdminOrder(ctx, id)
}

// GrantPerkRequest — ручная выдача привилегии: компенсация, акция.
type GrantPerkRequest struct {
	Kind   string   `json:"kind"`
	Value  *float64 `json:"value"`
	Days   int      `json:"days"`
	Reason string   `json:"reason"`
}

// GrantPerk выдаёт привилегию без денег. Она встаёт в ту же общую очередь,
// что и купленные: беспроцентный день, выданный поверх множителя, не съедает
// его оставшиеся дни.
func (s *ShopService) GrantPerk(ctx context.Context, adminID, userID uuid.UUID, req GrantPerkRequest) (*repository.UserPerk, error) {
	fields := map[string]string{}
	if err := ValidatePerk(req.Kind, req.Value, req.Days); err != nil {
		fields["kind"] = strings.TrimPrefix(err.Error(), ErrInvalidPerk.Error()+": ")
	}
	req.Reason = strings.TrimSpace(req.Reason)
	if req.Reason == "" {
		fields["reason"] = "Укажите причину"
	}
	if len(fields) > 0 {
		return nil, shopValidation(fields)
	}
	perk := &repository.UserPerk{UserID: userID, Kind: req.Kind, Value: req.Value, GrantedBy: &adminID, Reason: &req.Reason}
	err := s.ledger.RunInTx(ctx, func(tx *sql.Tx) error {
		if err := s.perks.LockQueue(ctx, tx, userID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return shopNotFound()
			}
			return err
		}
		start, err := s.perks.NextStart(ctx, tx, userID, CommissionPerkKinds, s.now())
		if err != nil {
			return err
		}
		perk.StartsAt, perk.ExpiresAt = start, start.AddDate(0, 0, req.Days)
		if err := s.perks.Create(ctx, tx, perk); err != nil {
			return err
		}
		if s.mail != nil {
			if err := s.mail.Send(ctx, tx, &repository.Mail{
				UserID: userID, Kind: repository.MailKindShop, Subject: "Вам выдана привилегия",
				Body: fmt.Sprintf("Привилегия на комиссию действует с %s по %s.",
					perk.StartsAt.Format("02.01.2006"), perk.ExpiresAt.Format("02.01.2006")),
				RefType: "perk", RefID: perk.ID.String(),
			}); err != nil {
				log.Printf("[shop] cannot mail user %s about a granted perk: %v", userID, err)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	log.Printf("[AUDIT] admin %s granted perk %s (%s, %d days) to user %s: %s", adminID, perk.ID, perk.Kind, req.Days, userID, req.Reason)
	return perk, nil
}

// RevokePerk отзывает привилегию без возврата денег. Купленную с возвратом
// отменяют через покупку.
func (s *ShopService) RevokePerk(ctx context.Context, adminID, perkID uuid.UUID) (*repository.UserPerk, error) {
	perk, err := s.perks.Revoke(ctx, nil, perkID, adminID)
	if errors.Is(err, repository.ErrPerkNotFound) {
		return nil, shopNotFound()
	}
	if err != nil {
		return nil, err
	}
	log.Printf("[AUDIT] admin %s revoked perk %s of user %s without a refund", adminID, perkID, perk.UserID)
	return perk, nil
}

// UserShopHistory — покупки и привилегии пользователя для его карточки.
type UserShopHistory struct {
	Orders []*repository.ShopOrder `json:"orders"`
	Perks  []*repository.UserPerk  `json:"perks"`
}

// UserHistory возвращает покупки и привилегии пользователя.
func (s *ShopService) UserHistory(ctx context.Context, userID uuid.UUID) (*UserShopHistory, error) {
	orders, err := s.orders.ListForUser(ctx, userID, 100)
	if err != nil {
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
func (s *ShopService) Revenue(ctx context.Context, from, to time.Time) (*ShopRevenue, error) {
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
func (s *ShopService) Payout(ctx context.Context, adminID uuid.UUID, amount money.Amount) (money.Amount, error) {
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
func (s *ShopService) ShopOrderLinks(ctx context.Context, ownerID uuid.UUID, messages []*repository.Message) error {
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

// perkReminderLead — за сколько до конца последней привилегии в очереди
// приходит письмо (implementation_plan_shop.md §3.6).
const perkReminderLead = 3 * 24 * time.Hour

// SendPerkReminders пишет владельцам привилегий, которые закончатся в
// ближайшие три дня и за которыми в очереди ничего нет. Отметка ставится тем
// же оператором, что и письмо, в одной транзакции: второй проход, даже на
// другом процессе, второго письма не пошлёт.
func (s *ShopService) SendPerkReminders(ctx context.Context) (int, error) {
	if s.mail == nil {
		return 0, nil
	}
	now := s.now()
	due, err := s.perks.DueReminders(ctx, CommissionPerkKinds, now, now.Add(perkReminderLead), 200)
	if err != nil {
		return 0, err
	}
	sent := 0
	for _, perk := range due {
		err := s.ledger.RunInTx(ctx, func(tx *sql.Tx) error {
			claimed, err := s.perks.MarkReminded(ctx, tx, perk.ID)
			if err != nil || !claimed {
				return err
			}
			sent++
			return s.mail.Send(ctx, tx, &repository.Mail{
				UserID: perk.UserID, Kind: repository.MailKindShop,
				Subject: "Привилегия скоро закончится",
				Body: fmt.Sprintf("Сниженная комиссия действует до %s. Продлить её можно в магазине — новая начнётся сразу после текущей.",
					perk.ExpiresAt.Format("02.01.2006 15:04")),
				RefType: "perk", RefID: perk.ID.String(),
			})
		})
		if err != nil {
			return sent, err
		}
	}
	return sent, nil
}
