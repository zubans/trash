package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"healthlogin/backend/money"
	"healthlogin/backend/repository"
)

// stockGifts — склад в памяти, считающий обращения к пулу кодов.
type stockGifts struct {
	repository.GiftRepository
	gifts      map[string]*repository.Gift
	free       map[string]int
	countCalls int
}

func (g *stockGifts) List(ctx context.Context, activeOnly bool) ([]*repository.Gift, error) {
	out := make([]*repository.Gift, 0, len(g.gifts))
	for _, gift := range g.gifts {
		out = append(out, gift)
	}
	return out, nil
}

func (g *stockGifts) Get(ctx context.Context, code string) (*repository.Gift, error) {
	if gift, ok := g.gifts[code]; ok {
		return gift, nil
	}
	return nil, repository.ErrGiftNotFound
}

func (g *stockGifts) Upsert(ctx context.Context, gift *repository.Gift) error {
	stored := *gift
	stored.CreatedAt = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	g.gifts[gift.Code] = &stored
	return nil
}

func (g *stockGifts) CountFreeCodesByGift(ctx context.Context) (map[string]int, error) {
	g.countCalls++
	return g.free, nil
}

func (g *stockGifts) CountFreeCodes(ctx context.Context, code string) (int, error) {
	panic("per-gift counting must not be used by the list")
}

// Форма подарка не несёт серверных полей: код — из пути, время создания — из
// базы. Род и сумма проверяются до записи.
func TestGiftCatalog_SaveMapsFormAndValidates(t *testing.T) {
	repo := &stockGifts{gifts: map[string]*repository.Gift{}, free: map[string]int{}}
	catalog := NewGiftCatalog(repo)
	ctx := context.Background()
	admin := uuid.New()

	var form GiftForm
	body := `{"code":"hacked","kind":"BONUS","amount":15000,"is_active":true,
	          "created_at":"2000-01-01T00:00:00Z","updated_at":"2000-01-01T00:00:00Z"}`
	if err := json.Unmarshal([]byte(body), &form); err != nil {
		t.Fatal(err)
	}
	saved, err := catalog.Save(ctx, admin, "welcome", form)
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if saved.Code != "welcome" || saved.CreatedAt.Year() != 2024 || saved.Amount != money.FromRubles(15000) {
		t.Fatalf("form leaked server fields: %+v", saved)
	}

	if _, err := catalog.Save(ctx, admin, "x", GiftForm{Kind: "CAR"}); !errors.Is(err, ErrGiftKindUnknown) {
		t.Fatalf("unknown kind: %v", err)
	}
	if _, err := catalog.Save(ctx, admin, "x", GiftForm{Kind: repository.GiftKindBonus, Amount: money.Amount(-1)}); !errors.Is(err, ErrGiftAmountNegative) {
		t.Fatalf("negative amount: %v", err)
	}
	if _, err := catalog.Save(ctx, admin, " ", GiftForm{Kind: repository.GiftKindBonus}); !errors.Is(err, ErrValidation) {
		t.Fatalf("empty code: %v", err)
	}
}

// Остатки пулов читаются одним запросом на весь список, а не по сертификату.
func TestGiftCatalog_AdminListCountsCodesOnce(t *testing.T) {
	repo := &stockGifts{
		gifts: map[string]*repository.Gift{
			"cert_a": {Code: "cert_a", Kind: repository.GiftKindCertificate},
			"cert_b": {Code: "cert_b", Kind: repository.GiftKindCertificate},
			"bonus":  {Code: "bonus", Kind: repository.GiftKindBonus},
		},
		free: map[string]int{"cert_a": 7},
	}
	out, err := NewGiftCatalog(repo).AdminList(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if repo.countCalls != 1 {
		t.Fatalf("free codes counted %d times, want 1", repo.countCalls)
	}
	got := map[string]int{}
	for _, item := range out {
		got[item.Code] = item.FreeCodes
	}
	if got["cert_a"] != 7 || got["cert_b"] != 0 || got["bonus"] != 0 {
		t.Fatalf("free codes: %v", got)
	}

	// Без сертификатов пул не читается вовсе.
	repo = &stockGifts{gifts: map[string]*repository.Gift{"bonus": {Code: "bonus", Kind: repository.GiftKindBonus}}}
	if _, err := NewGiftCatalog(repo).AdminList(context.Background()); err != nil || repo.countCalls != 0 {
		t.Fatalf("bonus-only list touched the code pool: calls=%d err=%v", repo.countCalls, err)
	}
}
