package middleware

import "testing"

// Мягкий бан оставляет доступ к уже купленному, но не к тратам: витрина и
// покупка закрыты, свои покупки и купоны — открыты.
func TestSoftBanClosesTheShopButNotPurchases(t *testing.T) {
	for route, want := range map[string]bool{
		"POST /shop/orders":            false,
		"GET /shop/products":           false,
		"GET /shop/products/{id}":      false,
		"GET /shop/orders":             true,
		"GET /shop/orders/{id}":        true,
		"GET /user/gifts":              true,
		"POST /user/gifts/{id}/reveal": true,
	} {
		if _, got := softBanAllowedRoutes[route]; got != want {
			t.Errorf("%s allowed = %v, want %v", route, got, want)
		}
	}
}
