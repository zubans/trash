package service

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"healthlogin/backend/repository"
)

// TestHydrateFillsCategoryAtAnyDepth ловит баг, из-за которого клиент показывал
// только название услуги: категорию он искал в /service-categories, а тот отдаёт
// одни корни, и у вложенного каталога (корень → подкатегория → вид) родитель
// варианта туда не попадает. Категория едет вместе с заказом именно поэтому.
func TestHydrateFillsCategoryAtAnyDepth(t *testing.T) {
	rootID := uuid.New()
	subID := uuid.New()
	variantID := uuid.New()

	catalog := &mockCatalogRepo{nodes: map[uuid.UUID]*repository.ServiceNode{
		rootID: {ID: rootID, Code: "TRASH", Name: map[string]string{"ru": "Вывоз мусора"}, NodeType: "CATEGORY"},
		subID:  {ID: subID, ParentID: &rootID, Code: "STD", Name: map[string]string{"ru": "Стандартный"}, NodeType: "CATEGORY"},
		variantID: {
			ID: variantID, ParentID: &subID, Code: "BASEMENT",
			Name: map[string]string{"ru": "Цокольная"}, NodeType: "VARIANT",
		},
	}}

	svc := &OrderService{catalogRepo: catalog}
	orders := []*repository.Order{{ID: uuid.New(), ServiceVariantID: variantID}}
	svc.hydrateServiceVariants(context.Background(), orders)

	if orders[0].ServiceVariant == nil {
		t.Fatal("вариант не заполнен")
	}
	if orders[0].ServiceCategory == nil {
		t.Fatal("категория не заполнена: клиент снова покажет только название услуги")
	}
	if got := orders[0].ServiceCategory.Name["ru"]; got != "Стандартный" {
		t.Errorf("категория = %q, ожидалась %q", got, "Стандартный")
	}
}

// TestHydrateLeavesCategoryEmptyForRootVariant: вид, лежащий прямо в корне,
// родителя не имеет — заголовком становится он сам, и подписи снизу нет.
func TestHydrateLeavesCategoryEmptyForRootVariant(t *testing.T) {
	variantID := uuid.New()
	catalog := &mockCatalogRepo{nodes: map[uuid.UUID]*repository.ServiceNode{
		variantID: {ID: variantID, Code: "SOLO", Name: map[string]string{"ru": "Разовая услуга"}, NodeType: "VARIANT"},
	}}

	svc := &OrderService{catalogRepo: catalog}
	orders := []*repository.Order{{ID: uuid.New(), ServiceVariantID: variantID}}
	svc.hydrateServiceVariants(context.Background(), orders)

	if orders[0].ServiceCategory != nil {
		t.Errorf("категории быть не должно, получено %v", orders[0].ServiceCategory)
	}
}
