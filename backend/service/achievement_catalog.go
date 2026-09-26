package service

import (
	"context"
	"errors"
	"log"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"healthlogin/backend/achievement"
	"healthlogin/backend/repository"
)

// AchievementCatalog — каталог ачивок глазами экрана исполнителя и
// админ-панели: витрина и полка значков, создание и правка собственных ачивок,
// отзыв выдачи, пересчёт агрегатов. Правила — код ачивки, конфликт с
// поставляемой, обязательность скрипта, границы веса — живут здесь, а
// обработчик только разбирает запрос и отвечает по классу ошибки.
//
// Выдача — и по событию, и по кнопке — остаётся у AchievementDispatcher:
// разойдясь, вторая копия начала бы платить по другим правилам.
type AchievementCatalog struct {
	repo    repository.AchievementRepository
	stats   repository.ExecutorStatsRepository
	levels  *Levels
	engine  *achievement.Engine
	scripts *Achievements
	now     func() time.Time
}

// NewAchievementCatalog собирает каталог. scripts может быть nil — тогда
// редактор работает как читалка: сохранить можно только то, что не меняет правил.
func NewAchievementCatalog(repo repository.AchievementRepository, stats repository.ExecutorStatsRepository,
	levels *Levels, engine *achievement.Engine, scripts *Achievements) *AchievementCatalog {
	return &AchievementCatalog{repo: repo, stats: stats, levels: levels, engine: engine, scripts: scripts, now: time.Now}
}

// AchievementCard — одна карточка на экране ачивок: что это, получено ли,
// сколько раз, на сколько баллов и что даёт.
type AchievementCard struct {
	Code        string     `json:"code"`
	Title       string     `json:"title"`
	Description string     `json:"description"`
	Icon        string     `json:"icon"`
	Weight      int        `json:"weight"`
	Repeatable  bool       `json:"repeatable"`
	Granted     bool       `json:"granted"`
	Count       int        `json:"count"`
	Points      int        `json:"points"`
	GrantedAt   *time.Time `json:"granted_at,omitempty"`
	// ExpiresAt — когда сгорят баллы этой выдачи. Показывается специально: с
	// уровнем, посчитанным по действующим баллам, истечение снижает уровень, и
	// человек должен увидеть это заранее, а не по факту.
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	// Progress — доля выполнения от 0 до 1, если ачивка её считает.
	Progress *float64 `json:"progress,omitempty"`
	// AvailableTo — конец окна акции.
	AvailableTo *time.Time `json:"available_to,omitempty"`
	// Available — ачивку ещё можно заслужить. У полученной это отдельный от
	// Granted вопрос: значок с закрытой акции остаётся на полке, но повторить
	// его уже нельзя, и повторяемая ачивка не должна обещать обратное.
	Available bool `json:"available"`
}

// Cards — экран ачивок исполнителя: витрина того, что можно заслужить, и полка
// того, что уже заслужено.
func (c *AchievementCatalog) Cards(ctx context.Context, user *repository.User) ([]AchievementCard, error) {
	// Каталог целиком, а не только действующая его часть: полка значков хранит
	// заслуженное, а выданную ачивку админ вправе потом выключить, закрыть
	// акцию или заархивировать. Что показать из невыданного, решает
	// AvailableAt ниже.
	rows, err := c.repo.ListAll(ctx)
	if err != nil {
		return nil, err
	}
	summary, err := c.repo.SummaryForUser(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	facts := c.factsFor(ctx, user, summary)

	cards := make([]AchievementCard, 0, len(rows))
	now := c.now()
	for _, row := range rows {
		granted, has := summary[row.Code]
		manifest, ok := c.engine.Manifest(row.Code)
		if !ok {
			// Скрипта нет: собственную ачивку заархивировали, и компилировать её
			// больше некому. Витрине показывать нечего, а полке есть — значок
			// заслужен, и исчезнуть он не должен оттого, что правило убрали.
			if !has {
				continue
			}
			cards = append(cards, shelvedCard(row, granted))
			continue
		}
		if manifest.Audience != achievement.AudienceExecutor {
			continue
		}
		// Ачивка вне окна акции показывается только тому, кто её уже получил:
		// закончившаяся акция — не витрина, а полученный значок остаётся.
		if !row.AvailableAt(now) && !has {
			continue
		}
		if visible, err := c.engine.Visible(row.Code, facts); err == nil && !visible && !has {
			continue
		}

		card := AchievementCard{
			Code: row.Code, Title: manifest.Title, Description: manifest.Description,
			Icon: manifest.Icon, Repeatable: !manifest.OncePerUser,
			Weight:      c.levels.Weight(ctx, row, manifest, 0),
			AvailableTo: row.AvailableTo,
			Available:   row.AvailableAt(now),
		}
		if has {
			card.Granted = true
			card.Count = granted.Count
			card.Points = granted.Points
			card.GrantedAt = &granted.GrantedAt
			card.ExpiresAt = granted.ExpiresAt
		}
		// Полоса рисуется и у полученной повторяемой ачивки: она считает путь к
		// следующей выдаче, а не к первой, и на полке это единственное, что
		// говорит, сколько осталось. У разовой полученной считать нечего.
		if !has || (card.Repeatable && card.Available) {
			if value, ok, err := c.engine.Progress(row.Code, facts); err == nil && ok {
				progress := value
				card.Progress = &progress
			}
		}
		cards = append(cards, card)
	}
	return cards, nil
}

// shelvedCard рисует значок, скрипта которого больше нет. Заголовком становится
// код: он же стоит в выдаче, по нему ачивку и найдут в админке, если понадобится
// объяснить, за что она была.
func shelvedCard(row *repository.Achievement, granted repository.GrantSummary) AchievementCard {
	return AchievementCard{
		Code: row.Code, Title: row.Code, Granted: true,
		Count: granted.Count, Points: granted.Points,
		GrantedAt: &granted.GrantedAt, ExpiresAt: granted.ExpiresAt,
	}
}

// factsFor собирает факты для хуков, которые вызываются не диспетчером, а
// экраном: видимость и прогресс. Заказа в них нет — они о человеке целиком.
func (c *AchievementCatalog) factsFor(ctx context.Context, user *repository.User, summary map[string]repository.GrantSummary) achievement.Facts {
	facts := achievement.Facts{
		Now: c.now(),
		User: &achievement.Actor{
			ID: user.ID.String(), Role: user.Role, Roles: user.Roles,
			IsVerified: user.IsVerified(), Status: user.Status, RegisteredAt: user.CreatedAt,
		},
		Stats:   &achievement.Stats{},
		Granted: map[string]achievement.Granted{},
	}
	if c.stats != nil {
		if row, err := c.stats.Get(ctx, nil, user.ID); err == nil {
			facts.Stats = &achievement.Stats{
				OrdersCompleted:      row.OrdersCompleted,
				OrdersCompletedMonth: row.OrdersCompletedMonth,
				DistinctCustomers:    row.DistinctCustomers,
				FastestCompletionMin: row.FastestCompletionMin,
				FiveStarStreak:       row.FiveStarStreak,
				RatingCount:          row.RatingCount,
				Cancels:              row.Cancels,
				EarnedTotal:          row.EarnedTotal.Rubles(),
			}
		}
	}
	level := c.levels.For(ctx, nil, user.ID)
	facts.User.Points = level.Points
	facts.User.Level = level.Level
	for code, g := range summary {
		granted := achievement.Granted{Count: g.Count, Points: g.Points, GrantedAt: g.GrantedAt}
		if g.ExpiresAt != nil {
			granted.ExpiresAt = *g.ExpiresAt
		}
		facts.Granted[code] = granted
	}
	return facts
}

// --- Админ -------------------------------------------------------------------

// AdminAchievement — строка каталога вместе с тем, что о ней знает скрипт.
type AdminAchievement struct {
	*repository.Achievement
	Title        string   `json:"title"`
	Description  string   `json:"description"`
	Icon         string   `json:"icon"`
	Audience     string   `json:"audience"`
	Events       []string `json:"events"`
	Repeatable   bool     `json:"repeatable"`
	ScriptWeight int      `json:"script_weight"`
	// EffectiveWeight — вес, который получит следующая выдача.
	EffectiveWeight int `json:"effective_weight"`
	// ScriptLoaded отличает выключенную ачивку от той, чей скрипт не
	// скомпилировался: без него это одно и то же пустое место в списке.
	ScriptLoaded bool `json:"script_loaded"`
	// IsLibrary — ачивка приехала со сборкой. Её скрипт править нельзя, и
	// удалить её тоже нельзя: строка исчезнет, а скрипт в бинарнике останется.
	IsLibrary bool `json:"is_library"`
	// ConstantsSource и SourceText — текст скрипта из движка. У поставляемой
	// это её файлы из бинарника: админ читает их, чтобы разобраться, и копирует
	// как стартовый шаблон для новой ачивки.
	ConstantsSource string `json:"constants_source,omitempty"`
	SourceText      string `json:"source_text,omitempty"`
}

// AdminList — каталог для админ-панели: живые ачивки или, по просьбе, архив.
func (c *AchievementCatalog) AdminList(ctx context.Context, deleted bool) ([]AdminAchievement, error) {
	var (
		rows []*repository.Achievement
		err  error
	)
	if deleted {
		rows, err = c.repo.ListDeleted(ctx)
	} else {
		rows, err = c.repo.List(ctx)
	}
	if err != nil {
		return nil, err
	}
	out := make([]AdminAchievement, 0, len(rows))
	for _, row := range rows {
		item := AdminAchievement{Achievement: row, IsLibrary: c.scripts.IsLibrary(row.Code)}
		if manifest, ok := c.engine.Manifest(row.Code); ok {
			item.ScriptLoaded = true
			item.Title = manifest.Title
			item.Description = manifest.Description
			item.Icon = manifest.Icon
			item.Audience = manifest.Audience
			item.Events = manifest.Events
			item.Repeatable = !manifest.OncePerUser
			item.ScriptWeight = manifest.Weight
			item.EffectiveWeight = c.levels.Weight(ctx, row, manifest, 0)
			item.ConstantsSource = manifest.ConstantsSource
			item.SourceText = manifest.Source
		}
		out = append(out, item)
	}
	return out, nil
}

// AchievementForm — то, что админ-панель присылает при создании и правке.
// Только редактируемые поля: время создания, правки и архивации строка
// получает от сервера.
type AchievementForm struct {
	// Code заполняется только при создании: у правки он в пути запроса.
	Code          string                 `json:"code"`
	IsActive      bool                   `json:"is_active"`
	AvailableFrom *time.Time             `json:"available_from"`
	AvailableTo   *time.Time             `json:"available_to"`
	Weight        *int                   `json:"weight"`
	Config        map[string]interface{} `json:"config"`
	SortOrder     int                    `json:"sort_order"`
	// Constants и Source — собственный скрипт. У поставляемой ачивки они
	// игнорируются: её скрипт живёт в бинарнике.
	Constants string `json:"constants"`
	Source    string `json:"source"`
}

// achievementCodePattern ограничивает код тем, чем он и является: именем,
// которое станет частью ключа идемпотентности и путём в API.
var achievementCodePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{2,63}$`)

// maxAchievementWeight — потолок веса. Вес превращается в баллы, баллы — в
// уровень, уровень — в комиссию: опечатка в этом поле стоит денег.
const maxAchievementWeight = 10000

// Ошибки каталога ачивок, на которые смотрят обработчик и тесты.
var (
	// ErrAchievementCodeInvalid — код не по шаблону.
	ErrAchievementCodeInvalid = validationError("код: строчные латинские буквы, цифры и подчёркивание, от 3 до 64 символов")
	// ErrAchievementCodeLibrary — код занят поставляемой ачивкой.
	ErrAchievementCodeLibrary = conflictError("этот код занят поставляемой ачивкой")
	// ErrAchievementScriptRequired — у собственной ачивки нет скрипта.
	ErrAchievementScriptRequired = validationError("скрипт обязателен: без него ачивка никогда не сработает")
	// ErrAchievementScriptNotLoaded — включают ачивку, чей скрипт не загружен.
	ErrAchievementScriptNotLoaded = validationError("script for this achievement is not loaded")
	// ErrAchievementExists — код уже занят, возможно, заархивированной ачивкой.
	ErrAchievementExists = conflictError("ачивка с таким кодом уже есть — возможно, в архиве")
	// ErrAchievementLibraryDelete — поставляемую ачивку нельзя удалить.
	ErrAchievementLibraryDelete = conflictError("поставляемую ачивку нельзя удалить — её можно выключить")
	// ErrAchievementNotFound — ачивки с таким кодом нет.
	ErrAchievementNotFound = notFoundError("achievement not found")
)

// validateForm проверяет то, что нельзя доверить скрипту: границы веса и окно акции.
func validateAchievementForm(form *AchievementForm) error {
	if form.Weight != nil && (*form.Weight < 0 || *form.Weight > maxAchievementWeight) {
		return validationError("вес должен быть от 0 до 10000")
	}
	if form.AvailableFrom != nil && form.AvailableTo != nil && form.AvailableTo.Before(*form.AvailableFrom) {
		return validationError("окно акции заканчивается раньше, чем начинается")
	}
	return nil
}

func rowFromAchievementForm(code string, form *AchievementForm) *repository.Achievement {
	return &repository.Achievement{
		Code: code, IsActive: form.IsActive,
		AvailableFrom: form.AvailableFrom, AvailableTo: form.AvailableTo,
		Weight: form.Weight, Config: form.Config, SortOrder: form.SortOrder,
		Constants: form.Constants, Source: form.Source,
	}
}

// Create заводит новую ачивку, написанную в админ-панели.
//
// Скрипт обязателен: ачивка без правила — это строка, которая никогда не
// сработает. Он компилируется до сохранения, поэтому сломанный скрипт
// отклоняется, пока на него ещё кто-то смотрит, а не молча перестаёт выдавать
// ачивку.
func (c *AchievementCatalog) Create(ctx context.Context, actorID uuid.UUID, form AchievementForm) (*repository.Achievement, error) {
	code := strings.TrimSpace(form.Code)
	if !achievementCodePattern.MatchString(code) {
		return nil, ErrAchievementCodeInvalid
	}
	if c.scripts.IsLibrary(code) {
		// Иначе строка в базе перехватила бы код поставляемой ачивки, и он
		// означал бы одно, а выполнялся другой.
		return nil, ErrAchievementCodeLibrary
	}
	if strings.TrimSpace(form.Source) == "" {
		return nil, ErrAchievementScriptRequired
	}
	if err := validateAchievementForm(&form); err != nil {
		return nil, err
	}
	row := rowFromAchievementForm(code, &form)
	if err := c.scripts.Validate(row); err != nil {
		// Ошибка Starlark называет файл, строку и суть — её и показываем.
		return nil, validationError(err.Error())
	}
	if err := c.repo.Create(ctx, row); err != nil {
		if errors.Is(err, repository.ErrAchievementExists) {
			// В том числе заархивированной: её код остаётся занятым, потому что
			// на него ссылаются выданные экземпляры. Такую восстанавливают.
			return nil, ErrAchievementExists
		}
		return nil, err
	}
	if err := c.scripts.Sync(row); err != nil {
		log.Printf("[achievement] %s saved but not compiled: %v", code, err)
	}
	log.Printf("[AUDIT] admin %v created achievement %s (active=%v)", actorID, code, row.IsActive)
	return row, nil
}

// Update правит ачивку. У поставляемой правится всё, кроме скрипта: он приехал
// со сборкой и прошёл ревью. У собственной правится и он.
func (c *AchievementCatalog) Update(ctx context.Context, actorID uuid.UUID, code string, form AchievementForm) (*repository.Achievement, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return nil, ErrAchievementCodeInvalid
	}
	if err := validateAchievementForm(&form); err != nil {
		return nil, err
	}
	existing, err := c.repo.Get(ctx, code)
	if err != nil {
		return nil, ErrAchievementNotFound
	}
	row := rowFromAchievementForm(code, &form)
	if c.scripts.IsLibrary(code) {
		// Скрипт поставляемой ачивки не редактируется отсюда — и не стирается
		// молча тем, что форма прислала пустые поля.
		row.Constants, row.Source = existing.Constants, existing.Source
	} else if !row.HasOwnScript() {
		return nil, ErrAchievementScriptRequired
	}
	if err := c.scripts.Validate(row); err != nil {
		return nil, validationError(err.Error())
	}
	if form.IsActive {
		if _, ok := c.engine.Manifest(code); !ok && !row.HasOwnScript() {
			// Включить ачивку без скрипта — значит завести строку, которая
			// никогда не сработает и о которой все будут думать, что она работает.
			return nil, ErrAchievementScriptNotLoaded
		}
	}
	if err := c.repo.Upsert(ctx, row); err != nil {
		return nil, err
	}
	if err := c.scripts.Sync(row); err != nil {
		log.Printf("[achievement] %s saved but not compiled: %v", code, err)
	}
	log.Printf("[AUDIT] admin %v updated achievement %s (active=%v)", actorID, code, form.IsActive)
	return row, nil
}

// Delete архивирует ачивку. Удаление мягкое, и это не осторожность ради
// осторожности: у ачивки есть выданные экземпляры и начисленные по ним баллы,
// то есть чей-то уровень и чья-то ставка комиссии. Строка уходит из списка и
// из движка, история остаётся. Чтобы отобрать выданное, есть отзыв.
func (c *AchievementCatalog) Delete(ctx context.Context, actorID uuid.UUID, code string) error {
	code = strings.TrimSpace(code)
	if c.scripts.IsLibrary(code) {
		// Строка исчезла бы, а скрипт в бинарнике остался: код продолжил бы
		// существовать, но уже ничей. Такую ачивку выключают, а не удаляют.
		return ErrAchievementLibraryDelete
	}
	row, err := c.repo.Get(ctx, code)
	if err != nil {
		return ErrAchievementNotFound
	}
	if err := c.repo.Delete(ctx, code); err != nil {
		return conflictError("cannot delete achievement")
	}
	archived := c.now()
	row.DeletedAt = &archived
	// Убираем из движка сразу: иначе ачивка продолжила бы срабатывать на этом
	// процессе до ближайшей пересинхронизации.
	if err := c.scripts.Sync(row); err != nil {
		log.Printf("[achievement] %s deleted but still compiled: %v", code, err)
	}
	log.Printf("[AUDIT] admin %v archived achievement %s", actorID, code)
	return nil
}

// Restore возвращает ачивку из архива — выключенной: восстановление — это
// «верните строку», а не «начните снова раздавать баллы».
func (c *AchievementCatalog) Restore(ctx context.Context, actorID uuid.UUID, code string) error {
	code = strings.TrimSpace(code)
	if err := c.repo.Restore(ctx, code); err != nil {
		return conflictError("cannot restore achievement")
	}
	if row, err := c.repo.Get(ctx, code); err == nil {
		if err := c.scripts.Sync(row); err != nil {
			log.Printf("[achievement] %s restored but not compiled: %v", code, err)
		}
	}
	log.Printf("[AUDIT] admin %v restored achievement %s", actorID, code)
	return nil
}

// Revoke отзывает одну выдачу решением администратора.
func (c *AchievementCatalog) Revoke(ctx context.Context, actorID, grantID uuid.UUID, reason string) error {
	if strings.TrimSpace(reason) == "" {
		reason = "revoked by admin"
	}
	if err := c.repo.Revoke(ctx, grantID, reason); err != nil {
		return conflictError("cannot revoke")
	}
	log.Printf("[AUDIT] admin %v revoked achievement grant %s: %s", actorID, grantID, reason)
	return nil
}

// UserGrants — выдачи пользователя вместе с его уровнем, для карточки в
// админ-панели.
type UserGrants struct {
	Grants []*repository.UserAchievement `json:"grants"`
	Level  Level                         `json:"level"`
}

// UserGrants — карточка пользователя: что выдано и какой уровень из этого следует.
func (c *AchievementCatalog) UserGrants(ctx context.Context, userID uuid.UUID) (*UserGrants, error) {
	grants, err := c.repo.ListForUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	return &UserGrants{Grants: grants, Level: c.levels.For(ctx, nil, userID)}, nil
}

// RecalculateStats пересчитывает агрегаты по журналу заказов. Существует по
// той же причине, что и сверка балансов, — счётчик может разойтись с тем,
// что он считает.
func (c *AchievementCatalog) RecalculateStats(ctx context.Context, actorID, userID uuid.UUID) (*repository.ExecutorStats, error) {
	if c.stats == nil {
		return nil, ErrNotConfigured
	}
	if err := c.stats.Recalculate(ctx, userID); err != nil {
		return nil, err
	}
	stats, err := c.stats.Get(ctx, nil, userID)
	if err != nil {
		return nil, err
	}
	log.Printf("[AUDIT] admin %v recalculated stats of %s", actorID, userID)
	return stats, nil
}
