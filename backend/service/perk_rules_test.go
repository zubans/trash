package service

import (
	"context"
	"errors"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"

	"healthlogin/backend/perks"
	"healthlogin/backend/repository"
)

// Коды поставляемых правил — только для тестов: код ядра их не знает.
const (
	ruleMultiplier = "commission_multiplier"
	ruleDiscountPP = "commission_discount_pp"
	ruleFree       = "commission_free"
)

// perkValue — константы товара с одной VALUE; nil — без констант.
func perkValue(v *float64) map[string]interface{} {
	if v == nil {
		return map[string]interface{}{}
	}
	return map[string]interface{}{"VALUE": *v}
}

// fakePerkRules — справочник правил в памяти: три поставляемых, как после
// миграции 062, и версии собственных.
type fakePerkRules struct {
	rules    map[string]*repository.PerkRule
	versions []*repository.PerkRuleVersion
}

func newFakePerkRules() *fakePerkRules {
	f := &fakePerkRules{rules: map[string]*repository.PerkRule{}}
	for _, code := range []string{ruleMultiplier, ruleDiscountPP, ruleFree} {
		f.rules[code] = &repository.PerkRule{Code: code, Title: code, Origin: repository.PerkRuleShipped, IsActive: true}
	}
	return f
}

func (f *fakePerkRules) List(ctx context.Context) ([]*repository.PerkRule, error) {
	out := make([]*repository.PerkRule, 0, len(f.rules))
	for _, r := range f.rules {
		cp := *r
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Code < out[j].Code })
	return out, nil
}

func (f *fakePerkRules) Get(ctx context.Context, code string) (*repository.PerkRule, error) {
	r, ok := f.rules[code]
	if !ok {
		return nil, repository.ErrPerkRuleNotFound
	}
	cp := *r
	return &cp, nil
}

func (f *fakePerkRules) Save(ctx context.Context, rule *repository.PerkRule) error {
	if existing, ok := f.rules[rule.Code]; ok {
		rule.Origin = existing.Origin
	}
	cp := *rule
	f.rules[rule.Code] = &cp
	return nil
}

func (f *fakePerkRules) AddVersion(ctx context.Context, v *repository.PerkRuleVersion) (*repository.PerkRuleVersion, error) {
	cp := *v
	cp.ID = uuid.New()
	// Время строго растёт, как у последовательных сохранений в базе.
	cp.CreatedAt = time.Now().Add(time.Duration(len(f.versions)) * time.Millisecond)
	f.versions = append(f.versions, &cp)
	return &cp, nil
}

func (f *fakePerkRules) LatestVersion(ctx context.Context, code string) (*repository.PerkRuleVersion, error) {
	var latest *repository.PerkRuleVersion
	for _, v := range f.versions {
		if v.RuleCode == code && (latest == nil || v.CreatedAt.After(latest.CreatedAt)) {
			latest = v
		}
	}
	if latest == nil {
		return nil, repository.ErrPerkRuleNotFound
	}
	return latest, nil
}

func (f *fakePerkRules) GetVersion(ctx context.Context, id uuid.UUID) (*repository.PerkRuleVersion, error) {
	for _, v := range f.versions {
		if v.ID == id {
			return v, nil
		}
	}
	return nil, repository.ErrPerkRuleNotFound
}

func newTestPerkRules(t *testing.T, repo repository.PerkRuleRepository, settings repository.SettingsRepository) *PerkRules {
	t.Helper()
	rules, err := NewPerkRules(repo, settings, perks.FS)
	if err != nil {
		t.Fatalf("perk rules: %v", err)
	}
	return rules
}

const ownRule = `MANIFEST = {"title": "Треть комиссии", "defaults": {"SHARE": 0.33}}

def rate(f):
    return f.level_percent * f.config["SHARE"]
`

// Правка собственного правила — новая версия; купленная привилегия
// считается той версией, которую продали.
func TestEditingAnOwnRuleDoesNotChangeSoldPerks(t *testing.T) {
	ctx := context.Background()
	repo := newFakePerkRules()
	rules := newTestPerkRules(t, repo, levelSettings("10", "500", "1"))
	admin := uuid.New()

	if _, _, err := rules.Save(ctx, admin, SavePerkRuleRequest{Code: "third", Title: "Треть", Source: ownRule, IsActive: true}, true); err != nil {
		t.Fatalf("create: %v", err)
	}
	sold, err := rules.Sellable(ctx, "third", nil)
	if err != nil {
		t.Fatalf("sellable: %v", err)
	}
	perk := &repository.UserPerk{RuleCode: "third", RuleVersionID: sold.VersionID, Config: sold.Config}

	edited := `MANIFEST = {"defaults": {"SHARE": 0.33}}

def rate(f):
    return 0
`
	if _, _, err := rules.Save(ctx, admin, SavePerkRuleRequest{Code: "third", Title: "Треть", Source: edited, IsActive: true}, false); err != nil {
		t.Fatalf("edit: %v", err)
	}
	got, err := rules.Rate(ctx, perk, 10, 9, 1)
	if err != nil {
		t.Fatalf("rate: %v", err)
	}
	if want := 9 * 0.33; got < want-1e-9 || got > want+1e-9 {
		t.Errorf("sold perk rate = %v, want %v from the version it was sold with", got, want)
	}
	now, err := rules.Sellable(ctx, "third", nil)
	if err != nil || now.VersionID == nil || *now.VersionID == *sold.VersionID {
		t.Fatalf("a new version was not made current: %+v, %v", now, err)
	}

	// Возврат к прежнему тексту — снова новая версия, и она текущая.
	if _, _, err := rules.Save(ctx, admin, SavePerkRuleRequest{Code: "third", Title: "Треть", Source: ownRule, IsActive: true}, false); err != nil {
		t.Fatalf("revert: %v", err)
	}
	back, _ := rules.Sellable(ctx, "third", nil)
	if back.VersionID == nil || *back.VersionID == *now.VersionID {
		t.Errorf("reverting the text left the edited version current")
	}
	// Сохранение того же текста новой версии не делает.
	before := len(repo.versions)
	if _, _, err := rules.Save(ctx, admin, SavePerkRuleRequest{Code: "third", Title: "Треть", Source: ownRule, IsActive: true}, false); err != nil {
		t.Fatalf("resave: %v", err)
	}
	if len(repo.versions) != before {
		t.Errorf("saving the same text made a version")
	}
}

// Создание не перезаписывает правило, правка не заводит новое, а
// поставляемому можно поменять только активность.
func TestPerkRuleCreateAndUpdateAreSeparate(t *testing.T) {
	ctx := context.Background()
	rules := newTestPerkRules(t, newFakePerkRules(), levelSettings("10", "500", "1"))
	admin := uuid.New()

	if _, _, err := rules.Save(ctx, admin, SavePerkRuleRequest{Code: ruleFree, Title: "x", Source: ownRule}, true); shopCode(err) != ShopErrValidation {
		t.Errorf("creating over a shipped rule: %v", err)
	}
	if _, _, err := rules.Save(ctx, admin, SavePerkRuleRequest{Code: "missing", Title: "x", Source: ownRule}, false); shopCode(err) != ShopErrNotFound {
		t.Errorf("updating a missing rule: %v", err)
	}
	view, _, err := rules.Save(ctx, admin, SavePerkRuleRequest{Code: ruleFree, Source: "garbage", IsActive: false}, false)
	if err != nil || view.IsActive {
		t.Fatalf("switching a shipped rule off: %+v, %v", view, err)
	}
	if _, err := rules.Sellable(ctx, ruleFree, nil); !errors.Is(err, ErrInvalidPerk) {
		t.Errorf("a switched-off rule is still sellable: %v", err)
	}
}

// Правило, которое не компилируется или не проходит сетку, не сохраняется.
func TestBadOwnRulesAreNotSaved(t *testing.T) {
	ctx := context.Background()
	repo := newFakePerkRules()
	rules := newTestPerkRules(t, repo, levelSettings("10", "500", "1"))
	for name, src := range map[string]string{
		"syntax":   "def rate(f)\n",
		"no rate":  `MANIFEST = {}`,
		"negative": "MANIFEST = {\"defaults\": {}}\ndef rate(f):\n    return -1\n",
	} {
		if _, _, err := rules.Save(ctx, uuid.New(), SavePerkRuleRequest{Code: "bad_rule", Title: "x", Source: src}, true); !errors.Is(err, ErrInvalidPerk) {
			t.Errorf("%s: expected ErrInvalidPerk, got %v", name, err)
		}
	}
	if _, ok := repo.rules["bad_rule"]; ok {
		t.Error("a rule that failed the check was saved")
	}
}
