package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"regexp"
	"strings"

	"github.com/google/uuid"

	"healthlogin/backend/perk"
	"healthlogin/backend/repository"
)

// ErrInvalidPerk — правило или его константы не годятся: не компилируется,
// не прошло проверку по сетке, неизвестная константа, нет срока. Такая
// привилегия не должна ни продаваться, ни выдаваться.
var ErrInvalidPerk = errors.New("invalid perk")

// PerkRules — сторона ядра в правилах привилегий
// (implementation_plan_delivery_passport.md §1): держит движок, компилирует
// версии по требованию и считает ставку.
//
// Поставляемые правила компилируются при старте из бинарника под ключом
// «shipped:<код>». Версия собственного правила — под ключом «v:<id>» и лениво,
// при первом обращении: строка версии неизменяема, поэтому скомпилированное
// никогда не устаревает и синхронизация по таймеру, как у ачивок, не нужна.
type PerkRules struct {
	engine   *perk.Engine
	repo     repository.PerkRuleRepository
	settings repository.SettingsRepository
	shipped  map[string]bool
}

// NewPerkRules компилирует поставляемые правила из fsys. Правило, которое не
// компилируется, — ошибка сборки, а не повод стартовать без него: на нём
// могут стоять купленные привилегии.
func NewPerkRules(repo repository.PerkRuleRepository, settings repository.SettingsRepository, fsys fs.FS) (*PerkRules, error) {
	r := &PerkRules{engine: perk.New(perk.DefaultLimits), repo: repo, settings: settings, shipped: map[string]bool{}}
	codes, err := perk.ShippedCodes(fsys)
	if err != nil {
		return nil, err
	}
	for _, code := range codes {
		files, err := perk.ReadShipped(fsys, code)
		if err != nil {
			return nil, err
		}
		if err := r.engine.CompileFiles(shippedKey(code), files); err != nil {
			return nil, fmt.Errorf("perk rule %s: %w", code, err)
		}
		r.shipped[code] = true
	}
	return r, nil
}

func shippedKey(code string) string  { return "shipped:" + code }
func versionKey(id uuid.UUID) string { return "v:" + id.String() }
func sourceHash(source string) string {
	h := sha256.Sum256([]byte(source))
	return hex.EncodeToString(h[:])
}
func ruleFiles(source string) []perk.SourceFile {
	return []perk.SourceFile{{Name: "rule.star", Src: []byte(source)}}
}

// keyFor находит скомпилированный текст привилегии: поставляемое правило —
// по коду, собственное — по проданной версии.
func (r *PerkRules) keyFor(ctx context.Context, ruleCode string, versionID *uuid.UUID) (string, error) {
	if versionID == nil {
		if !r.shipped[ruleCode] {
			return "", fmt.Errorf("perk rule %s is not shipped and has no version", ruleCode)
		}
		return shippedKey(ruleCode), nil
	}
	key := versionKey(*versionID)
	if r.engine.Has(key) {
		return key, nil
	}
	v, err := r.repo.GetVersion(ctx, *versionID)
	if err != nil {
		return "", err
	}
	if err := r.engine.CompileFiles(key, ruleFiles(v.Source)); err != nil {
		return "", err
	}
	return key, nil
}

// Rate считает ставку по привилегии и зажимает её в [0, base]. Ошибку
// возвращает как есть: что делать с упавшим правилом, решает вызывающий.
func (r *PerkRules) Rate(ctx context.Context, p *repository.UserPerk, base, levelPercent float64, level int) (float64, error) {
	key, err := r.keyFor(ctx, p.RuleCode, p.RuleVersionID)
	if err != nil {
		return 0, err
	}
	// Снимок привилегии полный, а умолчания подставляются только для
	// констант, которых в нём нет. Без проверки на лишние ключи: константу,
	// которую правило давно перестало читать, проданная привилегия вправе
	// хранить.
	m, _ := r.engine.Manifest(key)
	config := make(map[string]interface{}, len(m.Defaults)+len(p.Config))
	for k, v := range m.Defaults {
		config[k] = v
	}
	for k, v := range p.Config {
		config[k] = v
	}
	percent, err := r.engine.Rate(key, perk.Facts{Base: base, LevelPercent: levelPercent, Level: level, Config: config})
	if err != nil {
		return 0, err
	}
	return clampPercent(percent, base), nil
}

func clampPercent(percent, base float64) float64 {
	if percent < 0 {
		return 0
	}
	if percent > base {
		return base
	}
	return percent
}

// Sellable — текущий текст правила и константы товара, проверенные сеткой:
// то, что уйдёт в привилегию при покупке.
type Sellable struct {
	RuleCode  string
	VersionID *uuid.UUID
	Config    map[string]interface{}
	Grid      []perk.GridRow
}

// Sellable проверяет, что правило существует, включено и с этими
// константами отвечает на сетке числом в [0, base]. Им пользуются и
// сохранение товара, и покупка, и ручная выдача: правило, проверенное в одном
// месте и не проверенное в другом, разойдётся на третьей правке.
func (r *PerkRules) Sellable(ctx context.Context, ruleCode string, override map[string]interface{}) (*Sellable, error) {
	key, sellable, config, err := r.resolve(ctx, ruleCode, override)
	if err != nil {
		return nil, err
	}
	if sellable.Grid, err = r.engine.Check(key, config, r.discountPP(ctx)); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidPerk, err)
	}
	return sellable, nil
}

// Preview считает ставку, которую дало бы правило с константами товара, — для
// карточки «сейчас / с привилегией». Без прогона по сетке: его проходит
// сохранение товара и покупка, а карточку смотрят часто.
func (r *PerkRules) Preview(ctx context.Context, ruleCode string, override map[string]interface{}, base, levelPercent float64, level int) (float64, error) {
	key, _, config, err := r.resolve(ctx, ruleCode, override)
	if err != nil {
		return 0, err
	}
	percent, err := r.engine.Rate(key, perk.Facts{Base: base, LevelPercent: levelPercent, Level: level, Config: config})
	if err != nil {
		return 0, err
	}
	return clampPercent(percent, base), nil
}

// resolve находит текущий текст правила и сливает константы товара с его
// умолчаниями. Возвращает ключ в движке, будущий снимок привилегии и слитую
// конфигурацию.
func (r *PerkRules) resolve(ctx context.Context, ruleCode string, override map[string]interface{}) (string, *Sellable, map[string]interface{}, error) {
	fail := func(err error) (string, *Sellable, map[string]interface{}, error) { return "", nil, nil, err }
	rule, err := r.repo.Get(ctx, ruleCode)
	if errors.Is(err, repository.ErrPerkRuleNotFound) {
		return fail(fmt.Errorf("%w: правила %q нет", ErrInvalidPerk, ruleCode))
	}
	if err != nil {
		return fail(err)
	}
	if !rule.IsActive {
		return fail(fmt.Errorf("%w: правило %q выключено", ErrInvalidPerk, ruleCode))
	}
	var versionID *uuid.UUID
	if rule.Origin == repository.PerkRuleOwn {
		v, err := r.repo.LatestVersion(ctx, ruleCode)
		if errors.Is(err, repository.ErrPerkRuleNotFound) {
			return fail(fmt.Errorf("%w: у правила %q нет текста", ErrInvalidPerk, ruleCode))
		}
		if err != nil {
			return fail(err)
		}
		versionID = &v.ID
	}
	key, err := r.keyFor(ctx, ruleCode, versionID)
	if err != nil {
		return fail(fmt.Errorf("%w: %v", ErrInvalidPerk, err))
	}
	m, _ := r.engine.Manifest(key)
	config, err := perk.Config(m.Defaults, override)
	if err != nil {
		return fail(fmt.Errorf("%w: %v", ErrInvalidPerk, err))
	}
	// В привилегию уходят все константы, вместе с умолчаниями правила: релиз,
	// поменявший умолчание поставляемого правила, не должен менять уже
	// проданные привилегии.
	return key, &Sellable{RuleCode: ruleCode, VersionID: versionID, Config: config}, config, nil
}

func (r *PerkRules) discountPP(ctx context.Context) float64 {
	return settingFloat(ctx, r.settings, SettingAchievementLevelDiscountPP, defaultLevelDiscountPP)
}

// PerkRuleView — правило для админки: строка справочника, текст и объявление.
type PerkRuleView struct {
	*repository.PerkRule
	Description string                 `json:"description"`
	Defaults    map[string]interface{} `json:"defaults"`
	Source      string                 `json:"source"`
	VersionID   *uuid.UUID             `json:"version_id,omitempty"`
}

// List — все правила с их текущим текстом.
func (r *PerkRules) List(ctx context.Context) ([]*PerkRuleView, error) {
	rules, err := r.repo.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*PerkRuleView, 0, len(rules))
	for _, rule := range rules {
		view, err := r.view(ctx, rule)
		if err != nil {
			// Правило без текста или с поломанной версией показывается, но
			// без объявления: админ должен его увидеть, чтобы починить.
			log.Printf("[perk] rule %s: %v", rule.Code, err)
			view = &PerkRuleView{PerkRule: rule, Defaults: map[string]interface{}{}}
		}
		out = append(out, view)
	}
	return out, nil
}

func (r *PerkRules) view(ctx context.Context, rule *repository.PerkRule) (*PerkRuleView, error) {
	view := &PerkRuleView{PerkRule: rule}
	var key string
	if rule.Origin == repository.PerkRuleShipped {
		key = shippedKey(rule.Code)
	} else {
		v, err := r.repo.LatestVersion(ctx, rule.Code)
		if err != nil {
			return nil, err
		}
		view.VersionID = &v.ID
		if key, err = r.keyFor(ctx, rule.Code, &v.ID); err != nil {
			return nil, err
		}
	}
	m, ok := r.engine.Manifest(key)
	if !ok {
		return nil, fmt.Errorf("rule %s is not compiled", rule.Code)
	}
	view.Description, view.Defaults, view.Source = m.Description, m.Defaults, m.Source
	return view, nil
}

// SavePerkRuleRequest — собственное правило из формы админки.
type SavePerkRuleRequest struct {
	Code     string `json:"code"`
	Title    string `json:"title"`
	Source   string `json:"source"`
	IsActive bool   `json:"is_active"`
}

var perkRuleCode = regexp.MustCompile(`^[a-z][a-z0-9_]{2,63}$`)

// Check компилирует текст и прогоняет его по сетке с константами по
// умолчанию, ничего не сохраняя: форма показывает таблицу до сохранения.
func (r *PerkRules) Check(ctx context.Context, source string) ([]perk.GridRow, map[string]interface{}, error) {
	probe := perk.New(perk.DefaultLimits)
	if err := probe.CompileFiles("candidate", ruleFiles(source)); err != nil {
		return nil, nil, fmt.Errorf("%w: %v", ErrInvalidPerk, err)
	}
	m, _ := probe.Manifest("candidate")
	grid, err := probe.Check("candidate", m.Defaults, r.discountPP(ctx))
	if err != nil {
		return grid, m.Defaults, fmt.Errorf("%w: %v", ErrInvalidPerk, err)
	}
	return grid, m.Defaults, nil
}

// Save сохраняет правило. create отличает заведение нового от правки: новое
// не перезапишет существующее, а правка не заведёт отсутствующее — у них
// разные права. Текст собственного правила проверяется до записи; новый текст
// — новая версия, а купленные привилегии остаются на своей. Поставляемому
// правилу можно поменять только активность.
func (r *PerkRules) Save(ctx context.Context, adminID uuid.UUID, req SavePerkRuleRequest, create bool) (*PerkRuleView, []perk.GridRow, error) {
	req.Title = strings.TrimSpace(req.Title)
	existing, err := r.repo.Get(ctx, req.Code)
	if err != nil && !errors.Is(err, repository.ErrPerkRuleNotFound) {
		return nil, nil, err
	}
	if create && existing != nil {
		return nil, nil, shopErr(http.StatusConflict, ShopErrValidation, "Правило с таким кодом уже есть")
	}
	if !create && existing == nil {
		return nil, nil, shopNotFound()
	}
	if existing != nil && existing.Origin == repository.PerkRuleShipped {
		existing.IsActive = req.IsActive
		if err := r.repo.Save(ctx, existing); err != nil {
			return nil, nil, err
		}
		log.Printf("[AUDIT] admin %s set shipped perk rule %s active=%v", adminID, req.Code, req.IsActive)
		view, err := r.view(ctx, existing)
		return view, nil, err
	}
	if !perkRuleCode.MatchString(req.Code) {
		return nil, nil, fmt.Errorf("%w: код — латиница, цифры и подчёркивание, от 3 символов", ErrInvalidPerk)
	}
	if req.Title == "" {
		return nil, nil, fmt.Errorf("%w: название обязательно", ErrInvalidPerk)
	}
	grid, _, err := r.Check(ctx, req.Source)
	if err != nil {
		return nil, grid, err
	}

	rule := &repository.PerkRule{Code: req.Code, Title: req.Title, Origin: repository.PerkRuleOwn, IsActive: req.IsActive}
	if err := r.repo.Save(ctx, rule); err != nil {
		return nil, nil, err
	}
	hash := sourceHash(req.Source)
	latest, err := r.repo.LatestVersion(ctx, req.Code)
	if err != nil && !errors.Is(err, repository.ErrPerkRuleNotFound) {
		return nil, nil, err
	}
	if latest == nil || latest.Hash != hash {
		v, err := r.repo.AddVersion(ctx, &repository.PerkRuleVersion{
			RuleCode: req.Code, Hash: hash, Source: req.Source, CreatedBy: &adminID,
		})
		if err != nil {
			return nil, nil, err
		}
		log.Printf("[AUDIT] admin %s saved perk rule %s, version %s", adminID, req.Code, v.ID)
	}
	view, err := r.view(ctx, rule)
	return view, grid, err
}
