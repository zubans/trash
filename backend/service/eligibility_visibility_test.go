package service

import (
	"context"
	"testing"

	"healthlogin/backend/repository"
)

// TestCanViewOrTakeOrder прогоняет единый предикат видимости/принятия, который
// делят список заказов, карта и путь принятия.
func TestCanViewOrTakeOrder(t *testing.T) {
	verifiedExec := &repository.User{Role: repository.RoleExecutor, Verified: true, Status: "ACTIVE"}
	unverifiedExec := &repository.User{Role: repository.RoleExecutor, Verified: false, Status: "ACTIVE"}
	moderator := &repository.User{Role: repository.RoleModerator, Roles: []string{repository.RoleModerator}, Status: "ACTIVE"}
	execAndMod := &repository.User{Role: repository.RoleExecutor, Roles: []string{repository.RoleExecutor, repository.RoleModerator}, Status: "ACTIVE"}
	bannedExec := &repository.User{Role: repository.RoleExecutor, Verified: true, Status: "BANNED"}

	verifiedCust := &repository.User{Verified: true, Status: "ACTIVE"}
	unverifiedCust := &repository.User{Verified: false, Status: "ACTIVE"}

	normal := &repository.ServiceNode{}
	modOnly := &repository.ServiceNode{ModeratorOnly: true}
	reqVerif := &repository.ServiceNode{RequiresVerification: true}

	cases := []struct {
		name     string
		viewer   *repository.User
		customer *repository.User
		variant  *repository.ServiceNode
		wantOK   bool
	}{
		// Заказы только для модераторов: только модераторы.
		{"mod-only visible to moderator", moderator, unverifiedCust, modOnly, true},
		{"mod-only visible to exec+mod", execAndMod, verifiedCust, modOnly, true},
		{"mod-only hidden from verified exec", verifiedExec, unverifiedCust, modOnly, false},
		{"mod-only hidden from unverified exec", unverifiedExec, unverifiedCust, modOnly, false},

		// Обычный заказ от НЕВЕРИФИЦИРОВАННОГО заказчика: виден всем.
		{"unverified customer -> verified exec", verifiedExec, unverifiedCust, normal, true},
		{"unverified customer -> unverified exec", unverifiedExec, unverifiedCust, normal, true},

		// Обычный заказ от ВЕРИФИЦИРОВАННОГО заказчика: только верифицированным исполнителям / модераторам.
		{"verified customer -> verified exec", verifiedExec, verifiedCust, normal, true},
		{"verified customer -> unverified exec hidden", unverifiedExec, verifiedCust, normal, false},
		{"verified customer -> moderator", moderator, verifiedCust, normal, true},

		// Стандартные проверки исполнителя по обычным заказам всё равно действуют.
		{"requires_verification blocks unverified exec", unverifiedExec, unverifiedCust, reqVerif, false},
		{"requires_verification allows verified exec", verifiedExec, unverifiedCust, reqVerif, true},

		// Бан блокирует всегда.
		{"banned exec blocked", bannedExec, unverifiedCust, normal, false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := canViewOrTakeOrder(context.Background(), nil, c.viewer, c.customer, c.variant)
			if c.wantOK && err != nil {
				t.Errorf("expected visible, got error: %v", err)
			}
			if !c.wantOK && err == nil {
				t.Error("expected hidden, got visible")
			}
		})
	}
}
