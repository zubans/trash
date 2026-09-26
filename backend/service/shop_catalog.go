package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"regexp"
	"strings"

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

// ShopCatalog — товары и пункты выдачи глазами администратора (право shop).
// Здесь ничего не продаётся и не двигаются деньги: только справочник.
type ShopCatalog struct {
	shop  repository.ShopRepository
	gifts repository.GiftRepository
	rules *PerkRules
	roles repository.RoleRepository
}

// NewShopCatalog собирает каталог. gifts нужен, чтобы товар ссылался на
// существующий подарок того же рода; rules — чтобы привилегия продавалась.
func NewShopCatalog(shop repository.ShopRepository, gifts repository.GiftRepository, rules *PerkRules) *ShopCatalog {
	return &ShopCatalog{shop: shop, gifts: gifts, rules: rules}
}

// WithRoles подключает справочник ролей: товар нельзя открыть роли, которой
// нет.
func (s *ShopCatalog) WithRoles(roles repository.RoleRepository) *ShopCatalog {
	s.roles = roles
	return s
}

// AdminProducts — все товары, включая выключенные, с точным остатком.
func (s *ShopCatalog) AdminProducts(ctx context.Context, kind, category string) ([]*repository.ShopProduct, error) {
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
func (s *ShopCatalog) AdminProduct(ctx context.Context, id uuid.UUID) (*repository.ShopProduct, error) {
	p, err := s.shop.GetProduct(ctx, id)
	if errors.Is(err, repository.ErrShopProductNotFound) {
		return nil, shopNotFound()
	}
	return p, err
}

// SaveProduct проверяет и сохраняет товар. Новый — при нулевом id. Удаления
// нет: товар с продажами только выключается, на него ссылаются покупки.
// ShopProductForm — то, что админ-панель присылает при создании и правке
// товара. Только редактируемые поля: id — в пути запроса, время создания и
// правки, остаток и признак «в наличии» строка получает от сервера. Имена JSON
// совпадают с полями товара, поэтому форма админ-панели не меняется.
type ShopProductForm struct {
	Kind               string                          `json:"kind"`
	Category           string                          `json:"category"`
	Title              map[string]interface{}          `json:"title"`
	Description        map[string]interface{}          `json:"description"`
	Images             []string                        `json:"images"`
	Price              money.Amount                    `json:"price"`
	CompareAtPrice     *money.Amount                   `json:"compare_at_price"`
	Roles              []string                        `json:"roles"`
	RequiresVerified   bool                            `json:"requires_verified"`
	PerUserLimit       *int                            `json:"per_user_limit"`
	MaxQtyPerOrder     int                             `json:"max_qty_per_order"`
	GiftCode           *string                         `json:"gift_code"`
	Variants           []repository.ShopProductVariant `json:"variants"`
	FulfillmentMethods []string                        `json:"fulfillment_methods"`
	PerkRule           *string                         `json:"perk_rule"`
	PerkConfig         map[string]interface{}          `json:"perk_config"`
	PerkDays           *int                            `json:"perk_days"`
	MaxActivePerUser   *int                            `json:"max_active_per_user"`
	SortOrder          int                             `json:"sort_order"`
	IsActive           bool                            `json:"is_active"`
}

// Product собирает строку товара из формы. id — uuid.Nil для нового товара.
func (f ShopProductForm) Product(id uuid.UUID) *repository.ShopProduct {
	return &repository.ShopProduct{
		ID: id, Kind: f.Kind, Category: f.Category, Title: f.Title, Description: f.Description,
		Images: f.Images, Price: f.Price, CompareAtPrice: f.CompareAtPrice, Roles: f.Roles,
		RequiresVerified: f.RequiresVerified, PerUserLimit: f.PerUserLimit, MaxQtyPerOrder: f.MaxQtyPerOrder,
		GiftCode: f.GiftCode, Variants: f.Variants, FulfillmentMethods: f.FulfillmentMethods,
		PerkRule: f.PerkRule, PerkConfig: f.PerkConfig, PerkDays: f.PerkDays, MaxActivePerUser: f.MaxActivePerUser,
		SortOrder: f.SortOrder, IsActive: f.IsActive,
	}
}

// ShopPickupPointForm — пункт выдачи из формы админ-панели.
type ShopPickupPointForm struct {
	Title    map[string]interface{} `json:"title"`
	Address  string                 `json:"address"`
	Hours    *string                `json:"hours"`
	IsActive bool                   `json:"is_active"`
}

// Point собирает строку пункта выдачи из формы. id — uuid.Nil для нового.
func (f ShopPickupPointForm) Point(id uuid.UUID) *repository.ShopPickupPoint {
	return &repository.ShopPickupPoint{ID: id, Title: f.Title, Address: f.Address, Hours: f.Hours, IsActive: f.IsActive}
}

func (s *ShopCatalog) SaveProduct(ctx context.Context, adminID uuid.UUID, p *repository.ShopProduct) (*repository.ShopProduct, error) {
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

// validateProduct проверяет товар: общие поля, затем поля его рода. Ошибки
// возвращаются по полям, чтобы форма показала их у поля, а не одной строкой.
func (s *ShopCatalog) validateProduct(ctx context.Context, p *repository.ShopProduct) error {
	fields := map[string]string{}
	s.validateCommon(ctx, p, fields)
	var err error
	switch p.Kind {
	case repository.ShopKindPerk:
		err = s.validatePerk(ctx, p, fields)
	case repository.ShopKindPhysical:
		err = s.validatePhysical(ctx, p, fields)
	case repository.ShopKindCertificate:
		err = s.validateCertificate(ctx, p, fields)
	default:
		fields["kind"] = "Неизвестный род товара"
	}
	if err != nil {
		return err
	}
	if len(fields) > 0 {
		return shopValidation(fields)
	}
	return nil
}

// validateCommon — поля, общие для любого рода: категория, название, цена,
// изображения, лимит, роли.
func (s *ShopCatalog) validateCommon(ctx context.Context, p *repository.ShopProduct, fields map[string]string) {
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
}

// validatePerk — привилегия: правило и константы проверяются прогоном по
// сетке — тем же, что и при покупке: товар, который потом не продастся, не
// сохраняется.
func (s *ShopCatalog) validatePerk(ctx context.Context, p *repository.ShopProduct, fields map[string]string) error {
	if perkRule(p) == "" {
		fields["perk_rule"] = "Выберите правило привилегии"
	} else if _, err := s.rules.Sellable(ctx, perkRule(p), p.PerkConfig); errors.Is(err, ErrInvalidPerk) {
		fields["perk_config"] = strings.TrimPrefix(err.Error(), ErrInvalidPerk.Error()+": ")
	} else if err != nil {
		return err
	}
	if perkDays(p) <= 0 {
		fields["perk_days"] = "Срок обязателен и больше нуля"
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
	return nil
}

// validateGiftBacked — общее для вещи и сертификата: полей привилегии нет, а
// подарок есть и того же рода.
func (s *ShopCatalog) validateGiftBacked(ctx context.Context, p *repository.ShopProduct, fields map[string]string) error {
	if p.PerkRule != nil || len(p.PerkConfig) > 0 || p.PerkDays != nil || p.MaxActivePerUser != nil {
		fields["perk_rule"] = "Поля привилегии есть только у привилегии"
	}
	if p.GiftCode == nil || *p.GiftCode == "" {
		fields["gift_code"] = "Выберите подарок, которым выдаётся товар"
		return nil
	}
	gift, err := s.gifts.Get(ctx, *p.GiftCode)
	switch {
	case errors.Is(err, repository.ErrNotFound):
		fields["gift_code"] = "Такого подарка нет"
	case err != nil:
		return err
	case gift.Kind != p.Kind:
		// Склад и пул кодов общие с ачивками: подарок другого рода
		// сломал бы выдачу на первой же покупке.
		fields["gift_code"] = "Подарок другого рода"
	}
	return nil
}

// validateCertificate — сертификат: код показывается в приложении, получать
// его негде, и «количества» у кода нет.
func (s *ShopCatalog) validateCertificate(ctx context.Context, p *repository.ShopProduct, fields map[string]string) error {
	if err := s.validateGiftBacked(ctx, p, fields); err != nil {
		return err
	}
	if len(p.Variants) > 0 || len(p.FulfillmentMethods) > 0 {
		fields["fulfillment_methods"] = "У сертификата нет вариантов и способов получения"
	}
	p.MaxQtyPerOrder = 1
	return nil
}

// validatePhysical — вещь: способы получения и уникальные коды вариантов.
func (s *ShopCatalog) validatePhysical(ctx context.Context, p *repository.ShopProduct, fields map[string]string) error {
	if err := s.validateGiftBacked(ctx, p, fields); err != nil {
		return err
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
	return nil
}

// guardSalesFreeze запрещает менять род и подарок у товара с продажами:
// снимок в покупке говорит, что человек купил, а купоны уже лежат на складе
// этого подарка. Цену, название и активность менять можно.
func (s *ShopCatalog) guardSalesFreeze(ctx context.Context, previous, next *repository.ShopProduct) error {
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

func (s *ShopCatalog) validateRoles(ctx context.Context, roles []string) string {
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
func (s *ShopCatalog) AdminPickupPoints(ctx context.Context) ([]*repository.ShopPickupPoint, error) {
	return s.shop.ListPickupPoints(ctx, false)
}

// SavePickupPoint заводит или правит пункт выдачи. Удаления нет: пункт
// выключается и перестаёт предлагаться при оформлении, а покупки, которые на
// него ссылаются, остаются читаемыми.
func (s *ShopCatalog) SavePickupPoint(ctx context.Context, adminID uuid.UUID, p *repository.ShopPickupPoint) (*repository.ShopPickupPoint, error) {
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
