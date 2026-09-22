package service

import (
	"context"
	"log"
	"time"

	"github.com/google/uuid"

	"healthlogin/backend/achievement"
	"healthlogin/backend/repository"
)

// Ключи настроек уровней. Все правятся на экране настроек, все проверены
// валидатором диапазонов в admin.go: ставка, выведенная из настройки без
// границ, — это способ раздать нулевую комиссию опечаткой.
const (
	// SettingAchievementLevelPoints — баллов на один уровень.
	SettingAchievementLevelPoints = "achievement_level_points"
	// SettingAchievementLevelDiscountPP — процентных пунктов комиссии за уровень.
	SettingAchievementLevelDiscountPP = "achievement_level_discount_pp"
	// SettingAchievementDefaultWeight — вес ачивки, не назначившей свой.
	SettingAchievementDefaultWeight = "achievement_default_weight"
	// SettingAchievementMaxPointsPerDay — суточный потолок начисления баллов.
	SettingAchievementMaxPointsPerDay = "achievement_max_points_per_day"
	// SettingAchievementMaxBonus — потолок одного денежного подарка, в рублях.
	SettingAchievementMaxBonus = "achievement_max_bonus"
	// SettingAchievementMinOrderAmount — заказ дешевле не засчитывается ачивкам.
	SettingAchievementMinOrderAmount = "achievement_min_order_amount"
)

const (
	defaultLevelPoints       = 500.0
	defaultLevelDiscountPP   = 1.0
	defaultAchievementWeight = 5.0
	defaultMaxPointsPerDay   = 50.0
	defaultAchievementBonus  = 5000.0
	defaultMinOrderAmount    = 300.0
)

// Level — уровень исполнителя и всё, что из него следует.
type Level struct {
	Points int `json:"points"`
	Level  int `json:"level"`
	// NextLevelPoints — сколько баллов всего нужно до следующего уровня. Ноль,
	// когда следующий уровень уже ничего не даст.
	NextLevelPoints int `json:"next_level_points"`
	// BasePercent — общая ставка платформы, DiscountPP — сколько с неё снято,
	// Percent — то, по чему заказ действительно закроют.
	BasePercent float64 `json:"base_percent"`
	DiscountPP  float64 `json:"discount_pp"`
	Percent     float64 `json:"percent"`
	// MaxUsefulLevel — уровень, на котором комиссия достигает нуля. Дальше
	// ачивки продолжают начисляться, но на деньги уже не влияют.
	MaxUsefulLevel int `json:"max_useful_level"`

	// LevelPercent — ставка по уровню, до привилегии магазина. Percent
	// отличается от неё, только пока действует привилегия. Витрина проверяет
	// именно её: во время беспроцентной недели Percent тоже 0, но купить
	// следующую привилегию в очередь это мешать не должно.
	LevelPercent float64 `json:"level_percent"`
	// Привилегия, снизившая ставку, — её id уходит в заказ рядом со ставкой.
	PerkID        *uuid.UUID `json:"perk_id,omitempty"`
	PerkKind      string     `json:"perk_kind,omitempty"`
	PerkValue     *float64   `json:"perk_value,omitempty"`
	PerkExpiresAt *time.Time `json:"perk_expires_at,omitempty"`
}

// Levels выводит уровень из баллов, а ставку комиссии — из уровня.
//
// Это единственное место, где баллы превращаются в деньги, и потому
// единственное, где живут границы. Скрипт ачивки не может назначить комиссию —
// такого эффекта у него нет; он приносит баллы, а сколько стоит балл, решает
// администратор настройкой, и решение это зажато здесь.
type Levels struct {
	achievements repository.AchievementRepository
	settings     repository.SettingsRepository
	// perks — привилегии магазина. Без них ставка — это ставка по уровню.
	perks repository.PerkRepository
	// incidents принимает привилегию, которую нельзя применить.
	incidents repository.MoneyIncidentRepository
	// now подменяется тестами очереди.
	now func() time.Time
}

// NewLevels собирает вычислитель уровней. Безопасен к nil: установка без ачивок
// возвращает нулевой уровень, то есть базовую ставку для всех.
func NewLevels(achievements repository.AchievementRepository, settings repository.SettingsRepository) *Levels {
	return &Levels{achievements: achievements, settings: settings, now: time.Now}
}

// WithPerks подключает привилегии магазина: они применяются после уровня
// (implementation_plan_shop.md §3.3). incidents необязателен.
func (l *Levels) WithPerks(perks repository.PerkRepository, incidents repository.MoneyIncidentRepository) *Levels {
	l.perks = perks
	l.incidents = incidents
	return l
}

func (l *Levels) clock() time.Time {
	if l == nil || l.now == nil {
		return time.Now()
	}
	return l.now()
}

// Points возвращает сумму действующих баллов пользователя. Читает через
// переданный Querier, чтобы вызов внутри транзакции подтверждения заказа видел
// то же, что и она.
func (l *Levels) Points(ctx context.Context, q repository.Querier, userID uuid.UUID) int {
	if l == nil || l.achievements == nil {
		return 0
	}
	points, err := l.achievements.ActivePoints(ctx, q, userID)
	if err != nil {
		// Ошибка чтения баллов не должна ломать подтверждение заказа: она делает
		// исполнителя нулевого уровня, то есть берёт базовую комиссию. Ошибиться
		// в свою пользу платформе здесь можно, в пользу скидки — нет.
		log.Printf("[levels] cannot read points of %s, assuming zero: %v", userID, err)
		return 0
	}
	return points
}

// For описывает уровень пользователя целиком — для экрана и для расчёта.
//
// Это единственная точка, где определяется ставка исполнителя: через неё
// ходят подтверждение заказа и решение спора «неизвестно», поэтому привилегия
// магазина применяется здесь, а не в каждом месте вызова.
func (l *Levels) For(ctx context.Context, q repository.Querier, userID uuid.UUID) Level {
	return l.withPerk(ctx, q, userID, l.fromPoints(ctx, l.Points(ctx, q, userID)))
}

// withPerk применяет действующую привилегию после уровня.
//
// Привилегии не складываются: если по ошибке админа действуют несколько
// сразу, берётся самая выгодная покупателю — дающая наименьшую ставку, — а не
// их композиция (implementation_plan_shop.md §3.4). Сравнивать их можно только
// формулой: «минус 5 пунктов» и «×0.5» дают разную ставку при разном уровне.
func (l *Levels) withPerk(ctx context.Context, q repository.Querier, userID uuid.UUID, level Level) Level {
	if l == nil || l.perks == nil {
		return level
	}
	active, err := l.perks.Active(ctx, q, userID, CommissionPerkKinds, l.clock())
	if err != nil {
		// Как и с баллами: ошибка чтения не ломает подтверждение заказа, а
		// берёт ставку без привилегии. Ошибиться в свою пользу платформе здесь
		// можно, в пользу скидки — нет.
		log.Printf("[levels] cannot read perks of %s, assuming none: %v", userID, err)
		return level
	}
	var best *repository.UserPerk
	bestPercent := level.LevelPercent
	for _, perk := range active {
		percent, ok := ApplyPerk(level.LevelPercent, level.BasePercent, perk.Kind, perk.Value)
		if !ok {
			l.recordInvalidPerk(ctx, q, perk)
			continue
		}
		if best == nil || percent < bestPercent {
			best, bestPercent = perk, percent
		}
	}
	if best == nil {
		return level
	}
	level.Percent = bestPercent
	id, expires := best.ID, best.ExpiresAt
	level.PerkID = &id
	level.PerkKind = best.Kind
	level.PerkValue = best.Value
	level.PerkExpiresAt = &expires
	return level
}

// recordInvalidPerk пишет денежный инцидент о привилегии, которую нельзя
// применить. База такую не пропускает (CHECK в миграции 059), поэтому здесь
// это либо новый вид, о котором формула ещё не знает, либо правка мимо
// приложения — в обоих случаях молча оставить ставку нельзя, но и отменить
// подтверждение заказа из-за чужой опечатки нельзя.
func (l *Levels) recordInvalidPerk(ctx context.Context, q repository.Querier, perk *repository.UserPerk) {
	log.Printf("[levels] perk %s of %s (%s) cannot be applied, ignoring it", perk.ID, perk.UserID, perk.Kind)
	if l.incidents == nil {
		return
	}
	userID := perk.UserID
	details := map[string]interface{}{"perk_id": perk.ID.String(), "kind": perk.Kind}
	if perk.Value != nil {
		details["value"] = *perk.Value
	}
	if err := l.incidents.Record(ctx, q, &repository.MoneyIncident{
		Kind: repository.IncidentPerkInvalid, Severity: repository.IncidentSeverityWarning,
		UserID: &userID, Details: details,
	}); err != nil {
		log.Printf("[levels] cannot record perk incident: %v", err)
	}
}

// Queue возвращает действующую привилегию и ждущие своей очереди — для
// плашки на экране уровня. Без подключённых привилегий очередь пуста.
func (l *Levels) Queue(ctx context.Context, userID uuid.UUID) ([]*repository.UserPerk, error) {
	if l == nil || l.perks == nil {
		return []*repository.UserPerk{}, nil
	}
	return l.perks.ListQueue(ctx, userID, CommissionPerkKinds, l.clock())
}

// fromPoints — сама формула, отделённая от чтения, чтобы её можно было
// проверить без базы.
func (l *Levels) fromPoints(ctx context.Context, points int) Level {
	if l == nil {
		return Level{}
	}
	perLevel := settingFloat(ctx, l.settings, SettingAchievementLevelPoints, defaultLevelPoints)
	discountPP := settingFloat(ctx, l.settings, SettingAchievementLevelDiscountPP, defaultLevelDiscountPP)
	base := settingFloat(ctx, l.settings, SettingOrderCommissionPercent, 0)

	if base < 0 {
		base = 0
	}
	if base > 100 {
		base = 100
	}
	result := Level{Points: points, BasePercent: base, Percent: base, LevelPercent: base}
	if perLevel <= 0 || discountPP <= 0 {
		// Уровни выключены настройкой: все на базовой ставке.
		return result
	}

	result.Level = int(float64(points) / perLevel)
	result.DiscountPP = float64(result.Level) * discountPP
	// Зажим — и есть вся защита денег на этом пути. Комиссия не может уйти ниже
	// нуля: отрицательная означала бы, что исполнителю платят больше, чем
	// заплатил заказчик, а на это эскроу по заказу денег не держит.
	if result.DiscountPP > base {
		result.DiscountPP = base
	}
	result.Percent = base - result.DiscountPP
	if result.Percent < 0 {
		result.Percent = 0
	}
	result.LevelPercent = result.Percent

	result.MaxUsefulLevel = int(base / discountPP)
	if result.Level < result.MaxUsefulLevel {
		// Сколько всего баллов нужно набрать до следующего уровня — так это и
		// показывается на полосе прогресса.
		result.NextLevelPoints = int(float64(result.Level+1) * perLevel)
	}
	return result
}

// Weight — вес одной выдачи в баллах: то, что назначила строка каталога, иначе
// то, что объявил скрипт, иначе общая настройка.
//
// Значение снимается в момент выдачи и хранится в ней. Иначе правка веса в
// админке молча пересчитала бы уровни, а с ними и комиссию, всем, кто эту
// ачивку когда-либо получал.
func (l *Levels) Weight(ctx context.Context, row *repository.Achievement, manifest achievement.Manifest, requested int) int {
	if l == nil {
		return requested
	}
	if requested > 0 {
		return requested
	}
	if row != nil && row.Weight != nil && *row.Weight > 0 {
		return *row.Weight
	}
	if manifest.Weight > 0 {
		return manifest.Weight
	}
	return int(settingFloat(ctx, l.settings, SettingAchievementDefaultWeight, defaultAchievementWeight))
}

// MaxPointsPerDay — суточный потолок начисления. Он и есть цена накрутки: чем
// он ниже, тем меньше можно заработать сговором до того, как это заметят.
func (l *Levels) MaxPointsPerDay(ctx context.Context) int {
	if l == nil {
		return 0
	}
	return int(settingFloat(ctx, l.settings, SettingAchievementMaxPointsPerDay, defaultMaxPointsPerDay))
}

// MinOrderAmount — сумма, ниже которой заказ ачивкам не засчитывается. Читается
// ядром, а не скриптом: скрипт может её забыть, ядро — нет.
func (l *Levels) MinOrderAmount(ctx context.Context) float64 {
	if l == nil {
		return 0
	}
	return settingFloat(ctx, l.settings, SettingAchievementMinOrderAmount, defaultMinOrderAmount)
}

// MaxBonus — потолок одного денежного подарка, в рублях.
func (l *Levels) MaxBonus(ctx context.Context) float64 {
	if l == nil {
		return 0
	}
	return settingFloat(ctx, l.settings, SettingAchievementMaxBonus, defaultAchievementBonus)
}

// Expiry считает, когда истекают баллы выдачи. nil — никогда.
func Expiry(now time.Time, lifetimeDays int) *time.Time {
	if lifetimeDays <= 0 {
		return nil
	}
	deadline := now.AddDate(0, 0, lifetimeDays)
	return &deadline
}
