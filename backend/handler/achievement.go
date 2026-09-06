package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"healthlogin/backend/achievement"
	"healthlogin/backend/repository"
	"healthlogin/backend/service"
)

// AchievementHandler обслуживает геймификацию: значки и уровень исполнителя,
// его подарки и купоны, внутреннюю почту, а на стороне админа — каталог ачивок,
// склад подарков и разбор денежных инцидентов.
type AchievementHandler struct {
	achievements repository.AchievementRepository
	gifts        repository.GiftRepository
	mail         repository.MailRepository
	stats        repository.ExecutorStatsRepository
	incidents    repository.MoneyIncidentRepository
	levels       *service.Levels
	engine       *achievement.Engine
	// scripts компилирует и проверяет собственные скрипты ачивок, написанные
	// прямо здесь. Без него редактор работает как читалка: сохранить можно
	// только то, что не меняет правил.
	scripts *service.Achievements
}

// WithScripts подключает компиляцию собственных скриптов ачивок.
func (h *AchievementHandler) WithScripts(scripts *service.Achievements) *AchievementHandler {
	h.scripts = scripts
	return h
}

// NewAchievementHandler создаёт AchievementHandler.
func NewAchievementHandler(
	achievements repository.AchievementRepository,
	gifts repository.GiftRepository,
	mail repository.MailRepository,
	stats repository.ExecutorStatsRepository,
	incidents repository.MoneyIncidentRepository,
	levels *service.Levels,
	engine *achievement.Engine,
) *AchievementHandler {
	return &AchievementHandler{
		achievements: achievements, gifts: gifts, mail: mail, stats: stats,
		incidents: incidents, levels: levels, engine: engine,
	}
}

// achievementCard — одна карточка на экране ачивок: что это, получено ли,
// сколько раз, на сколько баллов и что даёт.
type achievementCard struct {
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
}

// GetAchievements обслуживает GET /executor/achievements.
func (h *AchievementHandler) GetAchievements(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r)
	if user == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	ctx := r.Context()

	rows, err := h.achievements.ListActive(ctx)
	if err != nil {
		http.Error(w, "cannot load achievements", http.StatusInternalServerError)
		return
	}
	summary, err := h.achievements.SummaryForUser(ctx, user.ID)
	if err != nil {
		http.Error(w, "cannot load achievements", http.StatusInternalServerError)
		return
	}
	facts := h.factsFor(ctx, user, summary)

	cards := make([]achievementCard, 0, len(rows))
	now := time.Now()
	for _, row := range rows {
		manifest, ok := h.engine.Manifest(row.Code)
		if !ok || manifest.Audience != achievement.AudienceExecutor {
			continue
		}
		granted, has := summary[row.Code]
		// Ачивка вне окна акции показывается только тому, кто её уже получил:
		// закончившаяся акция — не витрина, а полученный значок остаётся.
		if !row.AvailableAt(now) && !has {
			continue
		}
		if visible, err := h.engine.Visible(row.Code, facts); err == nil && !visible && !has {
			continue
		}

		card := achievementCard{
			Code: row.Code, Title: manifest.Title, Description: manifest.Description,
			Icon: manifest.Icon, Repeatable: !manifest.OncePerUser,
			Weight:      h.levels.Weight(ctx, row, manifest, 0),
			AvailableTo: row.AvailableTo,
		}
		if has {
			card.Granted = true
			card.Count = granted.Count
			card.Points = granted.Points
			card.GrantedAt = &granted.GrantedAt
			card.ExpiresAt = granted.ExpiresAt
		} else if value, ok, err := h.engine.Progress(row.Code, facts); err == nil && ok {
			progress := value
			card.Progress = &progress
		}
		cards = append(cards, card)
	}
	writeJSON(w, cards)
}

// GetLevel обслуживает GET /executor/level: баллы, уровень и ставка комиссии,
// которая из него следует.
func (h *AchievementHandler) GetLevel(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r)
	if user == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	writeJSON(w, h.levels.For(r.Context(), nil, user.ID))
}

// factsFor собирает факты для хуков, которые вызываются не диспетчером, а
// экраном: видимость и прогресс. Заказа в них нет — они о человеке целиком.
func (h *AchievementHandler) factsFor(ctx context.Context, user *repository.User, summary map[string]repository.GrantSummary) achievement.Facts {
	facts := achievement.Facts{
		Now: time.Now(),
		User: &achievement.Actor{
			ID: user.ID.String(), Role: user.Role, Roles: user.Roles,
			IsVerified: user.IsVerified(), Status: user.Status, RegisteredAt: user.CreatedAt,
		},
		Stats:   &achievement.Stats{},
		Granted: map[string]achievement.Granted{},
	}
	if h.stats != nil {
		if row, err := h.stats.Get(ctx, nil, user.ID); err == nil {
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
	level := h.levels.For(ctx, nil, user.ID)
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

// GetGifts обслуживает GET /executor/gifts. Секреты сертификатов в список не
// попадают: их отдаёт отдельный запрос, который пишется в аудит.
func (h *AchievementHandler) GetGifts(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r)
	if user == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	gifts, err := h.gifts.ListForUser(r.Context(), user.ID)
	if err != nil {
		http.Error(w, "cannot load gifts", http.StatusInternalServerError)
		return
	}
	writeJSON(w, gifts)
}

// RevealGift обслуживает POST /executor/gifts/{id}/reveal.
//
// Код сертификата — предъявительский документ: кто его прочитал, тот им и
// воспользовался. Поэтому он отдаётся только по явному запросу владельца и
// каждый показ пишется в аудит.
func (h *AchievementHandler) RevealGift(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r)
	if user == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		http.Error(w, "invalid gift id", http.StatusBadRequest)
		return
	}
	gift, err := h.gifts.Reveal(r.Context(), id, user.ID)
	if err != nil {
		if errors.Is(err, repository.ErrGiftUnavailable) {
			http.Error(w, "подарок больше недоступен", http.StatusConflict)
			return
		}
		http.Error(w, "gift not found", http.StatusNotFound)
		return
	}
	log.Printf("[AUDIT] user %s revealed gift %s (coupon %s)", user.ID, gift.GiftCode, gift.CouponCode)
	writeJSON(w, gift)
}

// GetMail обслуживает GET /user/mail.
func (h *AchievementHandler) GetMail(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r)
	if user == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	mail, err := h.mail.ListForUser(r.Context(), user.ID, 100)
	if err != nil {
		http.Error(w, "cannot load mail", http.StatusInternalServerError)
		return
	}
	unread, _ := h.mail.UnreadCount(r.Context(), user.ID)
	writeJSON(w, map[string]interface{}{"messages": mail, "unread": unread})
}

// GetMailUnread обслуживает GET /user/mail/unread — счётчик для значка в меню.
func (h *AchievementHandler) GetMailUnread(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r)
	if user == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	unread, err := h.mail.UnreadCount(r.Context(), user.ID)
	if err != nil {
		http.Error(w, "cannot count mail", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]int{"unread": unread})
}

// MarkMailRead обслуживает POST /user/mail/{id}/read.
func (h *AchievementHandler) MarkMailRead(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r)
	if user == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		http.Error(w, "invalid mail id", http.StatusBadRequest)
		return
	}
	if err := h.mail.MarkRead(r.Context(), id, user.ID); err != nil {
		http.Error(w, "cannot mark as read", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// MarkAllMailRead обслуживает POST /user/mail/read-all.
func (h *AchievementHandler) MarkAllMailRead(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r)
	if user == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if err := h.mail.MarkAllRead(r.Context(), user.ID); err != nil {
		http.Error(w, "cannot mark as read", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// DeleteMail обслуживает DELETE /user/mail/{id}. Удаление мягкое: письмо о
// выданном подарке — след выдачи, и он не должен исчезать из базы оттого, что
// получатель смахнул карточку.
func (h *AchievementHandler) DeleteMail(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r)
	if user == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		http.Error(w, "invalid mail id", http.StatusBadRequest)
		return
	}
	if err := h.mail.Delete(r.Context(), id, user.ID); err != nil {
		http.Error(w, "cannot delete", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Админ -------------------------------------------------------------------

// adminAchievement — строка каталога вместе с тем, что о ней знает скрипт.
type adminAchievement struct {
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

// AdminListAchievements обслуживает GET /admin/achievements.
func (h *AchievementHandler) AdminListAchievements(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rows, err := h.achievements.List(ctx)
	if err != nil {
		http.Error(w, "cannot load achievements", http.StatusInternalServerError)
		return
	}
	if r.URL.Query().Get("deleted") == "1" {
		rows, err = h.achievements.ListDeleted(ctx)
		if err != nil {
			http.Error(w, "cannot load achievements", http.StatusInternalServerError)
			return
		}
	}
	out := make([]adminAchievement, 0, len(rows))
	for _, row := range rows {
		item := adminAchievement{Achievement: row, IsLibrary: h.scripts.IsLibrary(row.Code)}
		if manifest, ok := h.engine.Manifest(row.Code); ok {
			item.ScriptLoaded = true
			item.Title = manifest.Title
			item.Description = manifest.Description
			item.Icon = manifest.Icon
			item.Audience = manifest.Audience
			item.Events = manifest.Events
			item.Repeatable = !manifest.OncePerUser
			item.ScriptWeight = manifest.Weight
			item.EffectiveWeight = h.levels.Weight(ctx, row, manifest, 0)
			item.ConstantsSource = manifest.ConstantsSource
			item.SourceText = manifest.Source
		}
		out = append(out, item)
	}
	writeJSON(w, out)
}

// achievementForm — то, что админ-панель присылает при создании и правке.
type achievementForm struct {
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

// codePattern ограничивает код тем, чем он и является: именем, которое станет
// частью ключа идемпотентности и путём в API.
var codePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{2,63}$`)

// AdminCreateAchievement обслуживает POST /admin/achievements — новая ачивка,
// написанная прямо в админ-панели.
//
// Скрипт обязателен: ачивка без правила — это строка, которая никогда не
// сработает. Он компилируется до сохранения, поэтому сломанный скрипт
// отклоняется, пока на него ещё кто-то смотрит, а не молча перестаёт выдавать
// ачивку.
func (h *AchievementHandler) AdminCreateAchievement(w http.ResponseWriter, r *http.Request) {
	var body achievementForm
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	code := strings.TrimSpace(body.Code)
	if !codePattern.MatchString(code) {
		http.Error(w, "код: строчные латинские буквы, цифры и подчёркивание, от 3 до 64 символов", http.StatusBadRequest)
		return
	}
	if h.scripts.IsLibrary(code) {
		// Иначе строка в базе перехватила бы код поставляемой ачивки, и он
		// означал бы одно, а выполнялся другой.
		http.Error(w, "этот код занят поставляемой ачивкой", http.StatusConflict)
		return
	}
	if strings.TrimSpace(body.Source) == "" {
		http.Error(w, "скрипт обязателен: без него ачивка никогда не сработает", http.StatusBadRequest)
		return
	}
	if err := h.validateForm(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	row := rowFromForm(code, &body)
	if err := h.scripts.Validate(row); err != nil {
		// Ошибка Starlark называет файл, строку и суть — её и показываем.
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := h.achievements.Create(r.Context(), row); err != nil {
		if errors.Is(err, repository.ErrAchievementExists) {
			// В том числе заархивированной: её код остаётся занятым, потому что
			// на него ссылаются выданные экземпляры. Такую восстанавливают.
			http.Error(w, "ачивка с таким кодом уже есть — возможно, в архиве", http.StatusConflict)
			return
		}
		http.Error(w, "cannot create achievement", http.StatusInternalServerError)
		return
	}
	if err := h.scripts.Sync(row); err != nil {
		log.Printf("[achievement] %s saved but not compiled: %v", code, err)
	}
	log.Printf("[AUDIT] admin %v created achievement %s (active=%v)", adminID(userFromContext(r)), code, row.IsActive)
	writeJSON(w, row)
}

// AdminUpdateAchievement обслуживает PUT /admin/achievements/{code}.
//
// У поставляемой ачивки правится всё, кроме скрипта: он приехал со сборкой и
// прошёл ревью. У собственной правится и он.
func (h *AchievementHandler) AdminUpdateAchievement(w http.ResponseWriter, r *http.Request) {
	code := strings.TrimSpace(chi.URLParam(r, "code"))
	if code == "" {
		http.Error(w, "invalid code", http.StatusBadRequest)
		return
	}
	var body achievementForm
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if err := h.validateForm(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	existing, err := h.achievements.Get(r.Context(), code)
	if err != nil {
		http.Error(w, "achievement not found", http.StatusNotFound)
		return
	}
	row := rowFromForm(code, &body)
	if h.scripts.IsLibrary(code) {
		// Скрипт поставляемой ачивки не редактируется отсюда — и не стирается
		// молча тем, что форма прислала пустые поля.
		row.Constants, row.Source = existing.Constants, existing.Source
	} else if !row.HasOwnScript() {
		http.Error(w, "скрипт обязателен: без него ачивка никогда не сработает", http.StatusBadRequest)
		return
	}
	if err := h.scripts.Validate(row); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if body.IsActive {
		if _, ok := h.engine.Manifest(code); !ok && !row.HasOwnScript() {
			// Включить ачивку без скрипта — значит завести строку, которая
			// никогда не сработает и о которой все будут думать, что она работает.
			http.Error(w, "script for this achievement is not loaded", http.StatusBadRequest)
			return
		}
	}

	if err := h.achievements.Upsert(r.Context(), row); err != nil {
		http.Error(w, "cannot save achievement", http.StatusInternalServerError)
		return
	}
	if err := h.scripts.Sync(row); err != nil {
		log.Printf("[achievement] %s saved but not compiled: %v", code, err)
	}
	log.Printf("[AUDIT] admin %v updated achievement %s (active=%v)", adminID(userFromContext(r)), code, body.IsActive)
	writeJSON(w, row)
}

// AdminDeleteAchievement обслуживает DELETE /admin/achievements/{code}.
//
// Удаление мягкое, и это не осторожность ради осторожности: у ачивки есть
// выданные экземпляры и начисленные по ним баллы, то есть чей-то уровень и чья-
// то ставка комиссии. Строка уходит из списка и из движка, история остаётся.
// Чтобы отобрать выданное, есть отдельное действие — отзыв.
func (h *AchievementHandler) AdminDeleteAchievement(w http.ResponseWriter, r *http.Request) {
	code := strings.TrimSpace(chi.URLParam(r, "code"))
	if h.scripts.IsLibrary(code) {
		// Строка исчезла бы, а скрипт в бинарнике остался: код продолжил бы
		// существовать, но уже ничей. Такую ачивку выключают, а не удаляют.
		http.Error(w, "поставляемую ачивку нельзя удалить — её можно выключить", http.StatusConflict)
		return
	}
	row, err := h.achievements.Get(r.Context(), code)
	if err != nil {
		http.Error(w, "achievement not found", http.StatusNotFound)
		return
	}
	if err := h.achievements.Delete(r.Context(), code); err != nil {
		http.Error(w, "cannot delete achievement", http.StatusConflict)
		return
	}
	archived := time.Now()
	row.DeletedAt = &archived
	// Убираем из движка сразу: иначе ачивка продолжила бы срабатывать на этом
	// процессе до ближайшей пересинхронизации.
	if err := h.scripts.Sync(row); err != nil {
		log.Printf("[achievement] %s deleted but still compiled: %v", code, err)
	}
	log.Printf("[AUDIT] admin %v archived achievement %s", adminID(userFromContext(r)), code)
	w.WriteHeader(http.StatusNoContent)
}

// AdminRestoreAchievement обслуживает POST /admin/achievements/{code}/restore.
// Ачивка возвращается выключенной: восстановление — это «верните строку», а не
// «начните снова раздавать баллы».
func (h *AchievementHandler) AdminRestoreAchievement(w http.ResponseWriter, r *http.Request) {
	code := strings.TrimSpace(chi.URLParam(r, "code"))
	if err := h.achievements.Restore(r.Context(), code); err != nil {
		http.Error(w, "cannot restore achievement", http.StatusConflict)
		return
	}
	if row, err := h.achievements.Get(r.Context(), code); err == nil {
		if err := h.scripts.Sync(row); err != nil {
			log.Printf("[achievement] %s restored but not compiled: %v", code, err)
		}
	}
	log.Printf("[AUDIT] admin %v restored achievement %s", adminID(userFromContext(r)), code)
	w.WriteHeader(http.StatusNoContent)
}

// validateForm проверяет то, что нельзя доверить скрипту: границы веса.
func (h *AchievementHandler) validateForm(body *achievementForm) error {
	if body.Weight != nil && (*body.Weight < 0 || *body.Weight > 10000) {
		// Вес превращается в баллы, баллы — в уровень, уровень — в комиссию.
		// Опечатка в этом поле стоит денег, поэтому у неё есть границы.
		return errors.New("вес должен быть от 0 до 10000")
	}
	if body.AvailableFrom != nil && body.AvailableTo != nil && body.AvailableTo.Before(*body.AvailableFrom) {
		return errors.New("окно акции заканчивается раньше, чем начинается")
	}
	return nil
}

func rowFromForm(code string, body *achievementForm) *repository.Achievement {
	return &repository.Achievement{
		Code: code, IsActive: body.IsActive,
		AvailableFrom: body.AvailableFrom, AvailableTo: body.AvailableTo,
		Weight: body.Weight, Config: body.Config, SortOrder: body.SortOrder,
		Constants: body.Constants, Source: body.Source,
	}
}

// AdminRevokeAchievement обслуживает POST /admin/achievements/grants/{id}/revoke.
func (h *AchievementHandler) AdminRevokeAchievement(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		http.Error(w, "invalid grant id", http.StatusBadRequest)
		return
	}
	var body struct {
		Reason string `json:"reason"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if err := h.achievements.Revoke(r.Context(), id, firstNonEmptyString(body.Reason, "revoked by admin")); err != nil {
		http.Error(w, "cannot revoke", http.StatusConflict)
		return
	}
	log.Printf("[AUDIT] admin %v revoked achievement grant %s: %s", adminID(userFromContext(r)), id, body.Reason)
	w.WriteHeader(http.StatusNoContent)
}

// AdminUserAchievements обслуживает GET /admin/users/{id}/achievements.
func (h *AchievementHandler) AdminUserAchievements(w http.ResponseWriter, r *http.Request) {
	userID, err := parseUUIDParam(r, "id")
	if err != nil {
		http.Error(w, "invalid user id", http.StatusBadRequest)
		return
	}
	grants, err := h.achievements.ListForUser(r.Context(), userID)
	if err != nil {
		http.Error(w, "cannot load grants", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{
		"grants": grants,
		"level":  h.levels.For(r.Context(), nil, userID),
	})
}

// AdminListGifts обслуживает GET /admin/gifts вместе с остатком пула кодов.
func (h *AchievementHandler) AdminListGifts(w http.ResponseWriter, r *http.Request) {
	gifts, err := h.gifts.List(r.Context(), false)
	if err != nil {
		http.Error(w, "cannot load gifts", http.StatusInternalServerError)
		return
	}
	type giftWithStock struct {
		*repository.Gift
		FreeCodes int `json:"free_codes"`
	}
	out := make([]giftWithStock, 0, len(gifts))
	for _, gift := range gifts {
		item := giftWithStock{Gift: gift}
		if gift.Kind == repository.GiftKindCertificate {
			item.FreeCodes, _ = h.gifts.CountFreeCodes(r.Context(), gift.Code)
		}
		out = append(out, item)
	}
	writeJSON(w, out)
}

// AdminSaveGift обслуживает PUT /admin/gifts/{code}.
func (h *AchievementHandler) AdminSaveGift(w http.ResponseWriter, r *http.Request) {
	code := strings.TrimSpace(chi.URLParam(r, "code"))
	if code == "" {
		http.Error(w, "invalid code", http.StatusBadRequest)
		return
	}
	var gift repository.Gift
	if err := json.NewDecoder(r.Body).Decode(&gift); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	gift.Code = code
	switch gift.Kind {
	case repository.GiftKindBonus, repository.GiftKindCertificate,
		repository.GiftKindPhysical, repository.GiftKindPromo:
	default:
		http.Error(w, "unknown gift kind", http.StatusBadRequest)
		return
	}
	if gift.Amount.IsNegative() {
		http.Error(w, "amount must not be negative", http.StatusBadRequest)
		return
	}
	if err := h.gifts.Upsert(r.Context(), &gift); err != nil {
		http.Error(w, "cannot save gift", http.StatusInternalServerError)
		return
	}
	log.Printf("[AUDIT] admin %v saved gift %s (%s)", adminID(userFromContext(r)), code, gift.Kind)
	writeJSON(w, gift)
}

// AdminAddGiftCodes обслуживает POST /admin/gifts/{code}/codes — пополнение
// пула сертификатов кодами от партнёра.
func (h *AchievementHandler) AdminAddGiftCodes(w http.ResponseWriter, r *http.Request) {
	code := strings.TrimSpace(chi.URLParam(r, "code"))
	var body struct {
		Codes []string `json:"codes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	added, err := h.gifts.AddCodes(r.Context(), code, body.Codes)
	if err != nil {
		http.Error(w, "cannot add codes", http.StatusInternalServerError)
		return
	}
	// В лог идёт только число: сами коды — предъявительские документы, и лог не
	// то место, где им стоит лежать.
	log.Printf("[AUDIT] admin %v added %d codes to gift %s", adminID(userFromContext(r)), added, code)
	writeJSON(w, map[string]int{"added": added})
}

// AdminRedeemCoupon обслуживает POST /admin/gifts/coupons/{coupon}/redeem: так
// администратор отмечает, что вещь выдана на руки.
func (h *AchievementHandler) AdminRedeemCoupon(w http.ResponseWriter, r *http.Request) {
	admin := userFromContext(r)
	if admin == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	coupon := strings.ToUpper(strings.TrimSpace(chi.URLParam(r, "coupon")))
	gift, err := h.gifts.RedeemCoupon(r.Context(), coupon, admin.ID)
	if err != nil {
		http.Error(w, "купон недействителен, уже погашен или просрочен", http.StatusConflict)
		return
	}
	log.Printf("[AUDIT] admin %s redeemed coupon %s of user %s", admin.ID, coupon, gift.UserID)
	writeJSON(w, gift)
}

// AdminBroadcastMail обслуживает POST /admin/mail/broadcast — новость или акция
// во внутренние ящики.
func (h *AchievementHandler) AdminBroadcastMail(w http.ResponseWriter, r *http.Request) {
	admin := userFromContext(r)
	if admin == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var body struct {
		Kind    string `json:"kind"`
		Role    string `json:"role"`
		Subject string `json:"subject"`
		Body    string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(body.Subject) == "" {
		http.Error(w, "subject is required", http.StatusBadRequest)
		return
	}
	kind := body.Kind
	if kind != repository.MailKindPromo && kind != repository.MailKindNews {
		// Рассылкой можно послать только новость или акцию: письма о выдачах
		// пишет ядро, и подделывать их вручную незачем.
		kind = repository.MailKindNews
	}

	recipients, err := h.mail.RecipientsByRole(r.Context(), body.Role)
	if err != nil {
		http.Error(w, "cannot resolve recipients", http.StatusInternalServerError)
		return
	}
	sent, err := h.mail.Broadcast(r.Context(), &repository.Mail{
		Kind: kind, Subject: body.Subject, Body: body.Body, SenderID: &admin.ID,
	}, recipients)
	if err != nil {
		http.Error(w, "cannot send", http.StatusInternalServerError)
		return
	}
	log.Printf("[AUDIT] admin %s broadcast %s mail to %d users (role %q)", admin.ID, kind, sent, body.Role)
	writeJSON(w, map[string]int{"sent": sent})
}

// AdminListIncidents обслуживает GET /admin/finances/incidents.
func (h *AchievementHandler) AdminListIncidents(w http.ResponseWriter, r *http.Request) {
	openOnly := r.URL.Query().Get("all") != "1"
	var (
		incidents []*repository.MoneyIncident
		err       error
	)
	if openOnly {
		incidents, err = h.incidents.ListOpen(r.Context(), 200)
	} else {
		incidents, err = h.incidents.List(r.Context(), 200)
	}
	if err != nil {
		http.Error(w, "cannot load incidents", http.StatusInternalServerError)
		return
	}
	writeJSON(w, incidents)
}

// AdminResolveIncident обслуживает POST /admin/finances/incidents/{id}/resolve.
func (h *AchievementHandler) AdminResolveIncident(w http.ResponseWriter, r *http.Request) {
	admin := userFromContext(r)
	if admin == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		http.Error(w, "invalid incident id", http.StatusBadRequest)
		return
	}
	var body struct {
		Resolution string `json:"resolution"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if strings.TrimSpace(body.Resolution) == "" {
		// Инцидент закрывается разбором, а не кнопкой: пустое объяснение
		// превращает журнал в список того, что кто-то когда-то смахнул.
		http.Error(w, "resolution is required", http.StatusBadRequest)
		return
	}
	if err := h.incidents.Resolve(r.Context(), id, admin.ID, body.Resolution); err != nil {
		http.Error(w, "cannot resolve", http.StatusConflict)
		return
	}
	log.Printf("[AUDIT] admin %s resolved money incident %s: %s", admin.ID, id, body.Resolution)
	w.WriteHeader(http.StatusNoContent)
}

// AdminRecalculateStats обслуживает POST /admin/users/{id}/stats/recalculate:
// пересчёт агрегатов по журналу заказов. Существует по той же причине, что и
// сверка балансов, — счётчик может разойтись с тем, что он считает.
func (h *AchievementHandler) AdminRecalculateStats(w http.ResponseWriter, r *http.Request) {
	userID, err := parseUUIDParam(r, "id")
	if err != nil {
		http.Error(w, "invalid user id", http.StatusBadRequest)
		return
	}
	if err := h.stats.Recalculate(r.Context(), userID); err != nil {
		http.Error(w, "cannot recalculate", http.StatusInternalServerError)
		return
	}
	stats, err := h.stats.Get(r.Context(), nil, userID)
	if err != nil {
		http.Error(w, "cannot read stats", http.StatusInternalServerError)
		return
	}
	log.Printf("[AUDIT] admin %v recalculated stats of %s", adminID(userFromContext(r)), userID)
	writeJSON(w, stats)
}

func adminID(user *repository.User) uuid.UUID {
	if user == nil {
		return uuid.Nil
	}
	return user.ID
}

func firstNonEmptyString(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
