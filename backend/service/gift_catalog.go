package service

import (
	"context"
	"errors"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"

	"healthlogin/backend/money"
	"healthlogin/backend/repository"
)

// GiftCatalog — подарки: купоны пользователя и склад для админ-панели.
// Правила — какие роды подарков бывают, что сумма не отрицательна, что показ
// кода сертификата пишется в аудит — живут здесь.
type GiftCatalog struct {
	repo repository.GiftRepository
	// shop закрывает покупку магазина, когда погашен её последний купон.
	shop *ShopOrders
	now  func() time.Time
}

// NewGiftCatalog создаёт GiftCatalog.
func NewGiftCatalog(repo repository.GiftRepository) *GiftCatalog {
	return &GiftCatalog{repo: repo, now: time.Now}
}

// WithShop подключает магазин: купон вещи из магазина гасится тем же
// действием, что и подарок ачивки.
func (g *GiftCatalog) WithShop(shop *ShopOrders) *GiftCatalog {
	g.shop = shop
	return g
}

// Ошибки подарков, на которые смотрят обработчик и тесты.
var (
	// ErrGiftKindUnknown — род подарка не из списка.
	ErrGiftKindUnknown = validationError("unknown gift kind")
	// ErrGiftAmountNegative — отрицательная сумма.
	ErrGiftAmountNegative = validationError("amount must not be negative")
	// ErrGiftCodeRequired — у подарка нет кода.
	ErrGiftCodeRequired = validationError("invalid code")
	// ErrGiftUnavailable — подарок кончился или выключен.
	ErrGiftUnavailable = conflictError("подарок больше недоступен")
	// ErrGiftNotFound — купона с таким id у этого пользователя нет.
	ErrGiftNotFound = notFoundError("gift not found")
	// ErrCouponInvalid — купон недействителен, уже погашен или просрочен.
	ErrCouponInvalid = conflictError("купон недействителен, уже погашен или просрочен")
)

// ListForUser — купоны пользователя. Секреты сертификатов в список не
// попадают: их отдаёт Reveal, который пишется в аудит. Просроченный купон
// показывается просроченным, даже если ночной проход его ещё не пометил.
func (g *GiftCatalog) ListForUser(ctx context.Context, userID uuid.UUID) ([]*repository.UserGift, error) {
	gifts, err := g.repo.ListForUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	MarkExpiredGifts(gifts, g.now())
	return gifts, nil
}

// Reveal показывает код сертификата владельцу.
//
// Код сертификата — предъявительский документ: кто его прочитал, тот им и
// воспользовался. Поэтому он отдаётся только по явному запросу владельца и
// каждый показ пишется в аудит.
func (g *GiftCatalog) Reveal(ctx context.Context, userID, giftID uuid.UUID) (*repository.UserGift, error) {
	gift, err := g.repo.Reveal(ctx, giftID, userID)
	if err != nil {
		if errors.Is(err, repository.ErrGiftUnavailable) {
			return nil, ErrGiftUnavailable
		}
		return nil, ErrGiftNotFound
	}
	log.Printf("[AUDIT] user %s revealed gift %s (coupon %s)", userID, gift.GiftCode, gift.CouponCode)
	return gift, nil
}

// --- Админ -------------------------------------------------------------------

// GiftWithStock — подарок вместе с остатком пула кодов.
type GiftWithStock struct {
	*repository.Gift
	FreeCodes int `json:"free_codes"`
}

// AdminList — склад подарков с остатками пулов. Остатки читаются одним
// запросом на все сертификаты, а не по одному на каждый.
func (g *GiftCatalog) AdminList(ctx context.Context) ([]GiftWithStock, error) {
	gifts, err := g.repo.List(ctx, false)
	if err != nil {
		return nil, err
	}
	var free map[string]int
	for _, gift := range gifts {
		if gift.Kind == repository.GiftKindCertificate {
			if free, err = g.repo.CountFreeCodesByGift(ctx); err != nil {
				return nil, err
			}
			break
		}
	}
	out := make([]GiftWithStock, 0, len(gifts))
	for _, gift := range gifts {
		item := GiftWithStock{Gift: gift}
		if gift.Kind == repository.GiftKindCertificate {
			item.FreeCodes = free[gift.Code]
		}
		out = append(out, item)
	}
	return out, nil
}

// GiftForm — то, что админ-панель присылает при сохранении подарка. Только
// редактируемые поля: код — в пути запроса, время создания и правки строка
// получает от сервера.
type GiftForm struct {
	Kind        string                 `json:"kind"`
	Title       map[string]interface{} `json:"title"`
	Description map[string]interface{} `json:"description"`
	ImageURL    *string                `json:"image_url"`
	Amount      money.Amount           `json:"amount"`
	Partner     *string                `json:"partner"`
	PromoCode   *string                `json:"promo_code"`
	Stock       *int                   `json:"stock"`
	ValidDays   *int                   `json:"valid_days"`
	IsActive    bool                   `json:"is_active"`
}

// Save создаёт или правит подарок.
func (g *GiftCatalog) Save(ctx context.Context, actorID uuid.UUID, code string, form GiftForm) (*repository.Gift, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return nil, ErrGiftCodeRequired
	}
	switch form.Kind {
	case repository.GiftKindBonus, repository.GiftKindCertificate,
		repository.GiftKindPhysical, repository.GiftKindPromo:
	default:
		return nil, ErrGiftKindUnknown
	}
	if form.Amount.IsNegative() {
		return nil, ErrGiftAmountNegative
	}
	gift := &repository.Gift{
		Code: code, Kind: form.Kind, Title: form.Title, Description: form.Description,
		ImageURL: form.ImageURL, Amount: form.Amount, Partner: form.Partner,
		PromoCode: form.PromoCode, Stock: form.Stock, ValidDays: form.ValidDays, IsActive: form.IsActive,
	}
	if err := g.repo.Upsert(ctx, gift); err != nil {
		return nil, err
	}
	log.Printf("[AUDIT] admin %v saved gift %s (%s)", actorID, code, gift.Kind)
	// Ответ — строка из базы, с временем создания и правки, которых в форме нет.
	if saved, err := g.repo.Get(ctx, code); err == nil {
		return saved, nil
	}
	return gift, nil
}

// AddCodes пополняет пул сертификата кодами от партнёра.
func (g *GiftCatalog) AddCodes(ctx context.Context, actorID uuid.UUID, code string, codes []string) (int, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return 0, ErrGiftCodeRequired
	}
	added, err := g.repo.AddCodes(ctx, code, codes)
	if err != nil {
		return 0, err
	}
	// В лог идёт только число: сами коды — предъявительские документы, и лог не
	// то место, где им стоит лежать.
	log.Printf("[AUDIT] admin %v added %d codes to gift %s", actorID, added, code)
	return added, nil
}

// RedeemCoupon гасит купон — так администратор отмечает, что вещь выдана на руки.
func (g *GiftCatalog) RedeemCoupon(ctx context.Context, adminID uuid.UUID, coupon string) (*repository.UserGift, error) {
	coupon = strings.ToUpper(strings.TrimSpace(coupon))
	gift, err := g.repo.RedeemCoupon(ctx, coupon, adminID)
	if err != nil {
		return nil, ErrCouponInvalid
	}
	log.Printf("[AUDIT] admin %s redeemed coupon %s of user %s", adminID, coupon, gift.UserID)
	if g.shop != nil {
		// Сбой здесь не отменяет погашения: вещь уже выдана, а покупку можно
		// закрыть руками из её карточки.
		if err := g.shop.OnCouponRedeemed(ctx, gift); err != nil {
			log.Printf("[shop] cannot complete order of coupon %s: %v", coupon, err)
		}
	}
	return gift, nil
}
