package repository

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"math/big"
	"time"

	"github.com/google/uuid"

	"healthlogin/backend/money"
)

// Роды подарков. Различие между ними — не косметика: у денег есть проводка, у
// сертификата есть пул кодов, у вещи есть склад и человек, который её выдаёт.
const (
	// GiftKindBonus — деньги на счёт, со счёта платформы BONUSES.
	GiftKindBonus = "BONUS"
	// GiftKindCertificate — код интернет-магазина, выдаваемый из пула.
	GiftKindCertificate = "CERTIFICATE"
	// GiftKindPhysical — вещь: купон в профиле, который гасит администратор при выдаче.
	GiftKindPhysical = "PHYSICAL"
	// GiftKindPromo — общий промокод партнёра, один на всех.
	GiftKindPromo = "PROMO"
)

// Состояния выданного подарка.
const (
	GiftStatusIssued   = "ISSUED"
	GiftStatusRevealed = "REVEALED"
	GiftStatusRedeemed = "REDEEMED"
	GiftStatusExpired  = "EXPIRED"
	GiftStatusCanceled = "CANCELED"
)

// ErrGiftUnavailable сообщает, что подарок кончился или выключен. Это не сбой:
// пустой склад — операционная проблема, и ачивка при нём выдаётся без подарка,
// а не остаётся невыданной (см. service/achievement_dispatch.go).
var ErrGiftUnavailable = errors.New("gift is not available")

// Gift — строка каталога подарков.
type Gift struct {
	Code        string                 `json:"code"`
	Kind        string                 `json:"kind"`
	Title       map[string]interface{} `json:"title"`
	Description map[string]interface{} `json:"description"`
	ImageURL    *string                `json:"image_url,omitempty"`
	// Amount для BONUS — сумма начисления; для остальных — номинал, справочно.
	Amount  money.Amount `json:"amount"`
	Partner *string      `json:"partner,omitempty"`
	// PromoCode — общий код для PROMO. Пусто у остальных родов.
	PromoCode *string `json:"promo_code,omitempty"`
	// Stock — остаток. nil означает «не ограничен».
	Stock *int `json:"stock,omitempty"`
	// ValidDays — сколько живёт купон с момента выдачи. nil — бессрочно.
	ValidDays *int      `json:"valid_days,omitempty"`
	IsActive  bool      `json:"is_active"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// UserGift — то, что человек получил, и в каком состоянии оно находится.
type UserGift struct {
	ID            uuid.UUID              `json:"id"`
	UserID        uuid.UUID              `json:"user_id"`
	GiftCode      string                 `json:"gift_code"`
	GiftCodeID    *uuid.UUID             `json:"-"`
	AchievementID *uuid.UUID             `json:"achievement_id,omitempty"`
	CouponCode    string                 `json:"coupon_code"`
	Status        string                 `json:"status"`
	Fulfillment   map[string]interface{} `json:"fulfillment,omitempty"`
	GrantedAt     time.Time              `json:"granted_at"`
	ExpiresAt     *time.Time             `json:"expires_at,omitempty"`
	RevealedAt    *time.Time             `json:"revealed_at,omitempty"`
	RedeemedAt    *time.Time             `json:"redeemed_at,omitempty"`
	// Gift — сам подарок, когда список отдают пользователю.
	Gift *Gift `json:"gift,omitempty"`
	// Secret — код сертификата. Заполняется только на явный запрос показать его
	// и никогда — в списке: показ пишется в аудит, а список читают походя.
	Secret string `json:"secret,omitempty"`
}

// GiftRepository хранит каталог подарков, пул кодов и выданное.
type GiftRepository interface {
	List(ctx context.Context, activeOnly bool) ([]*Gift, error)
	Get(ctx context.Context, code string) (*Gift, error)
	Upsert(ctx context.Context, gift *Gift) error
	// AddCodes пополняет пул кодов сертификата. Повторная загрузка того же кода
	// молча пропускается: файл выгрузки от партнёра нередко присылают дважды.
	AddCodes(ctx context.Context, giftCode string, secrets []string) (int, error)
	// CountFreeCodes сообщает остаток пула — для админского экрана.
	CountFreeCodes(ctx context.Context, giftCode string) (int, error)

	// Issue выдаёт подарок в транзакции вызывающего: занимает код или единицу
	// склада и создаёт купон. Возвращает ErrGiftUnavailable, когда брать нечего.
	Issue(ctx context.Context, q Querier, gift *Gift, userID uuid.UUID, achievementID *uuid.UUID) (*UserGift, error)
	// ListForUser возвращает подарки пользователя вместе с их описанием, но
	// никогда — с секретом.
	ListForUser(ctx context.Context, userID uuid.UUID) ([]*UserGift, error)
	// Reveal показывает код сертификата и помечает купон показанным.
	Reveal(ctx context.Context, id, userID uuid.UUID) (*UserGift, error)
	// RedeemCoupon гасит купон по его коду — так администратор отмечает, что
	// вещь выдана на руки.
	RedeemCoupon(ctx context.Context, coupon string, adminID uuid.UUID) (*UserGift, error)
}

type giftRepo struct {
	db *sql.DB
}

// NewGiftRepository создаёт GiftRepository.
func NewGiftRepository(db *sql.DB) GiftRepository {
	return &giftRepo{db: db}
}

func (r *giftRepo) exec(q Querier) Querier {
	if q == nil {
		return r.db
	}
	return q
}

const giftColumns = `code, kind, title, description, image_url, amount, partner, promo_code, stock, valid_days, is_active, created_at, updated_at`

func (r *giftRepo) List(ctx context.Context, activeOnly bool) ([]*Gift, error) {
	query := `SELECT ` + giftColumns + ` FROM gifts`
	if activeOnly {
		query += ` WHERE is_active`
	}
	query += ` ORDER BY code`
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]*Gift, 0)
	for rows.Next() {
		gift, err := scanGift(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, gift)
	}
	return out, rows.Err()
}

func (r *giftRepo) Get(ctx context.Context, code string) (*Gift, error) {
	return scanGift(r.db.QueryRowContext(ctx, `SELECT `+giftColumns+` FROM gifts WHERE code = $1`, code))
}

func scanGift(row rowScanner) (*Gift, error) {
	var g Gift
	var title, description []byte
	var stock, validDays sql.NullInt64
	if err := row.Scan(&g.Code, &g.Kind, &title, &description, &g.ImageURL, &g.Amount,
		&g.Partner, &g.PromoCode, &stock, &validDays, &g.IsActive, &g.CreatedAt, &g.UpdatedAt); err != nil {
		return nil, err
	}
	if len(title) > 0 {
		_ = json.Unmarshal(title, &g.Title)
	}
	if len(description) > 0 {
		_ = json.Unmarshal(description, &g.Description)
	}
	if stock.Valid {
		value := int(stock.Int64)
		g.Stock = &value
	}
	if validDays.Valid {
		value := int(validDays.Int64)
		g.ValidDays = &value
	}
	return &g, nil
}

func (r *giftRepo) Upsert(ctx context.Context, gift *Gift) error {
	title, err := json.Marshal(gift.Title)
	if err != nil {
		return err
	}
	description, err := json.Marshal(gift.Description)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `
        INSERT INTO gifts (code, kind, title, description, image_url, amount, partner, promo_code, stock, valid_days, is_active)
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
        ON CONFLICT (code) DO UPDATE SET
            kind = EXCLUDED.kind, title = EXCLUDED.title, description = EXCLUDED.description,
            image_url = EXCLUDED.image_url, amount = EXCLUDED.amount, partner = EXCLUDED.partner,
            promo_code = EXCLUDED.promo_code, stock = EXCLUDED.stock, valid_days = EXCLUDED.valid_days,
            is_active = EXCLUDED.is_active, updated_at = now()
    `, gift.Code, gift.Kind, title, description, gift.ImageURL, int64(gift.Amount),
		gift.Partner, gift.PromoCode, gift.Stock, gift.ValidDays, gift.IsActive)
	return err
}

func (r *giftRepo) AddCodes(ctx context.Context, giftCode string, secrets []string) (int, error) {
	added := 0
	for _, secret := range secrets {
		if secret == "" {
			continue
		}
		result, err := r.db.ExecContext(ctx, `
            INSERT INTO gift_codes (gift_code, secret) VALUES ($1, $2)
            ON CONFLICT (gift_code, secret) DO NOTHING
        `, giftCode, secret)
		if err != nil {
			return added, err
		}
		if affected, _ := result.RowsAffected(); affected > 0 {
			added++
		}
	}
	return added, nil
}

func (r *giftRepo) CountFreeCodes(ctx context.Context, giftCode string) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM gift_codes WHERE gift_code = $1 AND issued_to IS NULL`, giftCode).Scan(&count)
	return count, err
}

func (r *giftRepo) Issue(ctx context.Context, q Querier, gift *Gift, userID uuid.UUID, achievementID *uuid.UUID) (*UserGift, error) {
	if gift == nil || !gift.IsActive {
		return nil, ErrGiftUnavailable
	}
	exec := r.exec(q)

	var codeID *uuid.UUID
	if gift.Kind == GiftKindCertificate {
		// Захват кода: строка достаётся ровно одному, потому что UPDATE ...
		// RETURNING берёт её под блокировку. Проверка «есть ли свободные» и
		// выдача здесь — один оператор, поэтому два одновременных события не
		// заберут один код.
		var id uuid.UUID
		err := exec.QueryRowContext(ctx, `
            UPDATE gift_codes SET issued_to = $2, issued_at = now()
            WHERE id = (SELECT id FROM gift_codes
                        WHERE gift_code = $1 AND issued_to IS NULL
                        ORDER BY id LIMIT 1 FOR UPDATE SKIP LOCKED)
            RETURNING id
        `, gift.Code, userID).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrGiftUnavailable
		}
		if err != nil {
			return nil, err
		}
		codeID = &id
	} else if gift.Stock != nil {
		// Склад снимается тем же приёмом: условие и списание в одном операторе.
		var left int
		err := exec.QueryRowContext(ctx, `
            UPDATE gifts SET stock = stock - 1, updated_at = now()
            WHERE code = $1 AND stock IS NOT NULL AND stock > 0
            RETURNING stock
        `, gift.Code).Scan(&left)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrGiftUnavailable
		}
		if err != nil {
			return nil, err
		}
	}

	coupon, err := newCouponCode()
	if err != nil {
		return nil, err
	}
	var expiresAt *time.Time
	if gift.ValidDays != nil && *gift.ValidDays > 0 {
		deadline := time.Now().AddDate(0, 0, *gift.ValidDays)
		expiresAt = &deadline
	}

	issued := &UserGift{
		ID: uuid.New(), UserID: userID, GiftCode: gift.Code, GiftCodeID: codeID,
		AchievementID: achievementID, CouponCode: coupon, Status: GiftStatusIssued,
		ExpiresAt: expiresAt, Gift: gift,
	}
	err = exec.QueryRowContext(ctx, `
        INSERT INTO user_gifts (id, user_id, gift_code, gift_code_id, achievement_id, coupon_code, status, expires_at)
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
        RETURNING granted_at
    `, issued.ID, issued.UserID, issued.GiftCode, issued.GiftCodeID, issued.AchievementID,
		issued.CouponCode, issued.Status, issued.ExpiresAt).Scan(&issued.GrantedAt)
	if err != nil {
		return nil, err
	}
	return issued, nil
}

// couponAlphabet без похожих друг на друга символов: купон диктуют голосом и
// вводят руками, и «0 или O» здесь дороже четырёх лишних знаков энтропии.
const couponAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

func newCouponCode() (string, error) {
	buf := make([]byte, 12)
	for i := range buf {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(couponAlphabet))))
		if err != nil {
			return "", err
		}
		buf[i] = couponAlphabet[n.Int64()]
	}
	// Читается группами: XXXX-XXXX-XXXX.
	return string(buf[0:4]) + "-" + string(buf[4:8]) + "-" + string(buf[8:12]), nil
}

func (r *giftRepo) ListForUser(ctx context.Context, userID uuid.UUID) ([]*UserGift, error) {
	rows, err := r.db.QueryContext(ctx, `
        SELECT ug.id, ug.user_id, ug.gift_code, ug.gift_code_id, ug.achievement_id, ug.coupon_code,
               ug.status, ug.fulfillment, ug.granted_at, ug.expires_at, ug.revealed_at, ug.redeemed_at,
               `+giftColumnsPrefixed+`
        FROM user_gifts ug
        JOIN gifts g ON g.code = ug.gift_code
        WHERE ug.user_id = $1
        ORDER BY ug.granted_at DESC
    `, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]*UserGift, 0)
	for rows.Next() {
		var ug UserGift
		var fulfillment []byte
		var g Gift
		var title, description []byte
		var stock, validDays sql.NullInt64
		if err := rows.Scan(&ug.ID, &ug.UserID, &ug.GiftCode, &ug.GiftCodeID, &ug.AchievementID,
			&ug.CouponCode, &ug.Status, &fulfillment, &ug.GrantedAt, &ug.ExpiresAt, &ug.RevealedAt, &ug.RedeemedAt,
			&g.Code, &g.Kind, &title, &description, &g.ImageURL, &g.Amount, &g.Partner, &g.PromoCode,
			&stock, &validDays, &g.IsActive, &g.CreatedAt, &g.UpdatedAt); err != nil {
			return nil, err
		}
		if len(fulfillment) > 0 {
			_ = json.Unmarshal(fulfillment, &ug.Fulfillment)
		}
		if len(title) > 0 {
			_ = json.Unmarshal(title, &g.Title)
		}
		if len(description) > 0 {
			_ = json.Unmarshal(description, &g.Description)
		}
		if stock.Valid {
			value := int(stock.Int64)
			g.Stock = &value
		}
		if validDays.Valid {
			value := int(validDays.Int64)
			g.ValidDays = &value
		}
		// Промокод — общий на всех и секретом не является; код сертификата
		// секрет и в список не попадает никогда.
		ug.Gift = &g
		if g.Kind == GiftKindPromo && g.PromoCode != nil {
			ug.Secret = *g.PromoCode
		}
		// Просроченный купон показывается просроченным, даже если ночной проход
		// не успел его пометить: состояние не должно зависеть от того, работал
		// ли фоновый воркер.
		if ug.Status == GiftStatusIssued && ug.ExpiresAt != nil && ug.ExpiresAt.Before(time.Now()) {
			ug.Status = GiftStatusExpired
		}
		out = append(out, &ug)
	}
	return out, rows.Err()
}

const giftColumnsPrefixed = `g.code, g.kind, g.title, g.description, g.image_url, g.amount, g.partner, g.promo_code, g.stock, g.valid_days, g.is_active, g.created_at, g.updated_at`

func (r *giftRepo) Reveal(ctx context.Context, id, userID uuid.UUID) (*UserGift, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var ug UserGift
	var kind string
	var secret sql.NullString
	var promo sql.NullString
	err = tx.QueryRowContext(ctx, `
        SELECT ug.id, ug.user_id, ug.gift_code, ug.coupon_code, ug.status, ug.granted_at, ug.expires_at,
               g.kind, gc.secret, g.promo_code
        FROM user_gifts ug
        JOIN gifts g ON g.code = ug.gift_code
        LEFT JOIN gift_codes gc ON gc.id = ug.gift_code_id
        WHERE ug.id = $1 AND ug.user_id = $2
        FOR UPDATE OF ug
    `, id, userID).Scan(&ug.ID, &ug.UserID, &ug.GiftCode, &ug.CouponCode, &ug.Status,
		&ug.GrantedAt, &ug.ExpiresAt, &kind, &secret, &promo)
	if err != nil {
		return nil, err
	}
	if ug.ExpiresAt != nil && ug.ExpiresAt.Before(time.Now()) {
		return nil, ErrGiftUnavailable
	}
	switch {
	case secret.Valid:
		ug.Secret = secret.String
	case promo.Valid:
		ug.Secret = promo.String
	}
	if _, err := tx.ExecContext(ctx, `
        UPDATE user_gifts SET status = $2, revealed_at = COALESCE(revealed_at, now())
        WHERE id = $1 AND status = $3
    `, id, GiftStatusRevealed, GiftStatusIssued); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	ug.Status = GiftStatusRevealed
	return &ug, nil
}

func (r *giftRepo) RedeemCoupon(ctx context.Context, coupon string, adminID uuid.UUID) (*UserGift, error) {
	var ug UserGift
	err := r.db.QueryRowContext(ctx, `
        UPDATE user_gifts
           SET status = $3, redeemed_at = now(), redeemed_by = $2
         WHERE coupon_code = $1
           AND status IN ($4, $5)
           AND (expires_at IS NULL OR expires_at > now())
        RETURNING id, user_id, gift_code, coupon_code, status, granted_at, expires_at, redeemed_at
    `, coupon, adminID, GiftStatusRedeemed, GiftStatusIssued, GiftStatusRevealed).
		Scan(&ug.ID, &ug.UserID, &ug.GiftCode, &ug.CouponCode, &ug.Status, &ug.GrantedAt, &ug.ExpiresAt, &ug.RedeemedAt)
	if errors.Is(err, sql.ErrNoRows) {
		// Купона нет, он уже погашен или просрочен. Различать эти случаи для
		// того, кто вводит код на пункте выдачи, незачем: во всех трёх вещь не
		// выдаётся.
		return nil, ErrGiftUnavailable
	}
	return &ug, err
}
