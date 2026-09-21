package service

import (
	"errors"
	"fmt"
)

// Виды привилегий магазина. Все три меняют одно и то же — ставку комиссии
// исполнителя, — поэтому они не складываются, а встают в общую очередь
// (implementation_plan_shop.md §3.4). Новые виды, не связанные с комиссией,
// получат свою точку применения и в эту очередь не встанут.
const (
	// PerkKindCommissionMultiplier умножает ставку: 7 % → 3.5 %. Держит обещание
	// «вдвое» при любой базовой ставке.
	PerkKindCommissionMultiplier = "COMMISSION_MULTIPLIER"
	// PerkKindCommissionDiscountPP вычитает пункты: 7 % → 2 %. Та же арифметика,
	// что у скидки за уровень, поэтому правило объяснять не нужно.
	PerkKindCommissionDiscountPP = "COMMISSION_DISCOUNT_PP"
	// PerkKindCommissionFree обнуляет ставку на весь срок. Значения у этого вида
	// нет вовсе: не «ноль», а отсутствие значения.
	PerkKindCommissionFree = "COMMISSION_FREE"
)

// CommissionPerkKinds перечисляет виды, меняющие ставку комиссии, — одним
// списком, чтобы SQL-запросы очереди и формула не разошлись, когда видов
// станет больше.
var CommissionPerkKinds = []string{
	PerkKindCommissionMultiplier,
	PerkKindCommissionDiscountPP,
	PerkKindCommissionFree,
}

// ErrInvalidPerk — товар или выдача с параметрами привилегии, не подходящими
// её виду. Такой товар не должен молча продаваться.
var ErrInvalidPerk = errors.New("invalid perk")

// ApplyPerk применяет привилегию к ставке, уже сниженной за уровень
// (implementation_plan_shop.md §3.3):
//
//	COMMISSION_MULTIPLIER   → levelPercent × value
//	COMMISSION_DISCOUNT_PP  → clamp(levelPercent − value, 0, base)
//	COMMISSION_FREE         → 0
//
// Применение после уровня — часть обещания витрины: «вдвое меньше» остаётся
// вдвое меньше той комиссии, которую исполнитель платил бы сейчас.
//
// ok равен false, когда вид неизвестен или значение не подходит виду: такая
// привилегия не применяется, а вызывающий пишет денежный инцидент — молча
// оставить ставку как есть нельзя, но и отменить подтверждение заказа из-за
// чужой опечатки нельзя.
func ApplyPerk(levelPercent, base float64, kind string, value *float64) (percent float64, ok bool) {
	if err := validatePerkValue(kind, value); err != nil {
		return levelPercent, false
	}
	switch kind {
	case PerkKindCommissionMultiplier:
		percent = levelPercent * *value
	case PerkKindCommissionDiscountPP:
		percent = levelPercent - *value
	case PerkKindCommissionFree:
		percent = 0
	}
	// Тот же зажим, что у скидки за уровень: комиссия не может уйти ниже нуля
	// или выше базовой ставки.
	if percent < 0 {
		percent = 0
	}
	if percent > base {
		percent = base
	}
	return percent, true
}

// ValidatePerk проверяет сочетание вида, значения и срока. Он один на
// админ-API товара, ручную выдачу привилегии и покупку: правило, написанное
// дважды, разойдётся на третьей правке. Бессрочной привилегии нет — срок
// обязателен (implementation_plan_shop.md §3.2).
func ValidatePerk(kind string, value *float64, days int) error {
	if err := validatePerkValue(kind, value); err != nil {
		return err
	}
	if days <= 0 {
		return fmt.Errorf("%w: срок привилегии обязателен и больше нуля", ErrInvalidPerk)
	}
	return nil
}

// validatePerkValue — часть правил про вид и значение, без срока: ApplyPerk
// проверяет то же самое перед применением.
func validatePerkValue(kind string, value *float64) error {
	switch kind {
	case PerkKindCommissionMultiplier:
		if value == nil || *value <= 0 || *value > 1 {
			return fmt.Errorf("%w: множитель обязателен и лежит в (0, 1]", ErrInvalidPerk)
		}
	case PerkKindCommissionDiscountPP:
		if value == nil || *value <= 0 {
			return fmt.Errorf("%w: пункты скидки обязательны и больше нуля", ErrInvalidPerk)
		}
	case PerkKindCommissionFree:
		if value != nil {
			return fmt.Errorf("%w: у беспроцентного периода значения нет", ErrInvalidPerk)
		}
	default:
		// Неизвестный вид — ошибка, а не «оставить ставку как есть»: товар с
		// опечаткой в виде не должен молча продаваться.
		return fmt.Errorf("%w: неизвестный вид %q", ErrInvalidPerk, kind)
	}
	return nil
}
