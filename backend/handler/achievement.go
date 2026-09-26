package handler

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"healthlogin/backend/middleware"
	"healthlogin/backend/repository"
	"healthlogin/backend/service"
)

// AchievementHandler обслуживает геймификацию: значки и уровень исполнителя,
// его подарки и купоны, а на стороне админа — каталог ачивок, склад подарков и
// разбор денежных инцидентов. Почта, в которую всё это приходит письмами,
// живёт в MailHandler. Правила — в сервисах; здесь разбор запроса, вызов и
// ответ по классу ошибки.
type AchievementHandler struct {
	catalog   *service.AchievementCatalog
	gifts     *service.GiftCatalog
	incidents *service.MoneyIncidents
	// dispatcher выдаёт ачивки — и по событию, и по кнопке администратора.
	// Через него идут обе админские кнопки выдачи: пересчёт по истории и
	// выдача вручную. Своей копии этой логики у обработчика нет намеренно —
	// разойдясь, она начала бы платить по другим правилам, чем обычная выдача.
	dispatcher *service.AchievementDispatcher
}

// NewAchievementHandler создаёт AchievementHandler.
func NewAchievementHandler(catalog *service.AchievementCatalog, gifts *service.GiftCatalog,
	incidents *service.MoneyIncidents, dispatcher *service.AchievementDispatcher) *AchievementHandler {
	return &AchievementHandler{catalog: catalog, gifts: gifts, incidents: incidents, dispatcher: dispatcher}
}

// RegisterUserRoutes — купоны: и подарки ачивок, и купленное в магазине. У
// заказчика ачивок нет, но купоны на купленные вещи есть.
func (h *AchievementHandler) RegisterUserRoutes(r chi.Router) {
	r.Get("/user/gifts", h.GetGifts)
	r.Post("/user/gifts/{id}/reveal", h.RevealGift)
}

// RegisterExecutorRoutes — значки и подарки исполнителя. Уровень со ставкой и
// очередью привилегий — GET /me/perks в общей группе.
func (h *AchievementHandler) RegisterExecutorRoutes(r chi.Router) {
	r.Get("/executor/achievements", h.GetAchievements)
	r.Get("/executor/gifts", h.GetGifts)
	r.Post("/executor/gifts/{id}/reveal", h.RevealGift)
}

// RegisterAdminRoutes — каталог ачивок, склад подарков и денежные инциденты.
func (h *AchievementHandler) RegisterAdminRoutes(r chi.Router, can func(string) func(http.Handler) http.Handler) {
	r.With(can("achievements.view")).Get("/admin/achievements", h.AdminListAchievements)
	r.With(can("achievements.create")).Post("/admin/achievements", h.AdminCreateAchievement)
	r.With(can("achievements.edit")).Put("/admin/achievements/{code}", h.AdminUpdateAchievement)
	r.With(can("achievements.delete")).Delete("/admin/achievements/{code}", h.AdminDeleteAchievement)
	r.With(can("achievements.edit")).Post("/admin/achievements/{code}/restore", h.AdminRestoreAchievement)
	r.With(can("achievements.delete")).Post("/admin/achievements/grants/{id}/revoke", h.AdminRevokeAchievement)
	r.With(can("achievements.view")).Get("/admin/users/{id}/achievements", h.AdminUserAchievements)
	r.With(can("achievements.edit")).Post("/admin/users/{id}/stats/recalculate", h.AdminRecalculateStats)
	// Пересчёт — правка: он ничего не придумывает, а доводит выданное до
	// того, что и так следует из правил. Выдача вручную — создание: она
	// правило обходит.
	r.With(can("achievements.edit")).Post("/admin/users/{id}/achievements/recheck", h.AdminRecheckUserAchievements)
	r.With(can("achievements.create")).Post("/admin/users/{id}/achievements/{code}", h.AdminGrantAchievement)
	r.With(can("gifts.view")).Get("/admin/gifts", h.AdminListGifts)
	r.With(can("gifts.edit")).Put("/admin/gifts/{code}", h.AdminSaveGift)
	r.With(can("gifts.create")).Post("/admin/gifts/{code}/codes", h.AdminAddGiftCodes)
	r.With(can("gifts.edit")).Post("/admin/gifts/coupons/{coupon}/redeem", h.AdminRedeemCoupon)
	r.With(can("incidents.view")).Get("/admin/finances/incidents", h.AdminListIncidents)
	r.With(can("incidents.edit")).Post("/admin/finances/incidents/{id}/resolve", h.AdminResolveIncident)
}

// GetAchievements обслуживает GET /executor/achievements.
func (h *AchievementHandler) GetAchievements(w http.ResponseWriter, r *http.Request) {
	user, ok := requireUser(w, r)
	if !ok {
		return
	}
	cards, err := h.catalog.Cards(r.Context(), user)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, cards)
}

// GetGifts обслуживает GET /executor/gifts и GET /user/gifts.
func (h *AchievementHandler) GetGifts(w http.ResponseWriter, r *http.Request) {
	user, ok := requireUser(w, r)
	if !ok {
		return
	}
	gifts, err := h.gifts.ListForUser(r.Context(), user.ID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, gifts)
}

// RevealGift обслуживает POST /executor/gifts/{id}/reveal.
func (h *AchievementHandler) RevealGift(w http.ResponseWriter, r *http.Request) {
	user, ok := requireUser(w, r)
	if !ok {
		return
	}
	id, ok := parseIDParam(w, r, "id", "gift id")
	if !ok {
		return
	}
	gift, err := h.gifts.Reveal(r.Context(), user.ID, id)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, gift)
}

// --- Админ -------------------------------------------------------------------

// AdminListAchievements обслуживает GET /admin/achievements.
func (h *AchievementHandler) AdminListAchievements(w http.ResponseWriter, r *http.Request) {
	out, err := h.catalog.AdminList(r.Context(), r.URL.Query().Get("deleted") == "1")
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// AdminCreateAchievement обслуживает POST /admin/achievements.
func (h *AchievementHandler) AdminCreateAchievement(w http.ResponseWriter, r *http.Request) {
	var form service.AchievementForm
	if !decodeBody(w, r, &form) {
		return
	}
	row, err := h.catalog.Create(r.Context(), adminID(middleware.UserFrom(r)), form)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, row)
}

// AdminUpdateAchievement обслуживает PUT /admin/achievements/{code}.
func (h *AchievementHandler) AdminUpdateAchievement(w http.ResponseWriter, r *http.Request) {
	code := strings.TrimSpace(chi.URLParam(r, "code"))
	if code == "" {
		http.Error(w, "invalid code", http.StatusBadRequest)
		return
	}
	var form service.AchievementForm
	if !decodeBody(w, r, &form) {
		return
	}
	row, err := h.catalog.Update(r.Context(), adminID(middleware.UserFrom(r)), code, form)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, row)
}

// AdminDeleteAchievement обслуживает DELETE /admin/achievements/{code}.
func (h *AchievementHandler) AdminDeleteAchievement(w http.ResponseWriter, r *http.Request) {
	if err := h.catalog.Delete(r.Context(), adminID(middleware.UserFrom(r)), chi.URLParam(r, "code")); err != nil {
		writeDomainError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// AdminRestoreAchievement обслуживает POST /admin/achievements/{code}/restore.
func (h *AchievementHandler) AdminRestoreAchievement(w http.ResponseWriter, r *http.Request) {
	if err := h.catalog.Restore(r.Context(), adminID(middleware.UserFrom(r)), chi.URLParam(r, "code")); err != nil {
		writeDomainError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// AdminRevokeAchievement обслуживает POST /admin/achievements/grants/{id}/revoke.
func (h *AchievementHandler) AdminRevokeAchievement(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(w, r, "id", "grant id")
	if !ok {
		return
	}
	var body struct {
		Reason string `json:"reason"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if err := h.catalog.Revoke(r.Context(), adminID(middleware.UserFrom(r)), id, body.Reason); err != nil {
		writeDomainError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// AdminUserAchievements обслуживает GET /admin/users/{id}/achievements.
func (h *AchievementHandler) AdminUserAchievements(w http.ResponseWriter, r *http.Request) {
	userID, ok := parseIDParam(w, r, "id", "user id")
	if !ok {
		return
	}
	out, err := h.catalog.UserGrants(r.Context(), userID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// AdminRecheckUserAchievements обслуживает
// POST /admin/users/{id}/achievements/recheck.
//
// Кнопка отвечает на вопрос «почему у него нет значка, который он заслужил».
// Причин ровно две, и обе лечатся пересчётом: ачивку включили после того, как
// человек выполнил заказы, или событие в своё время не дошло. Пересчёт
// повторяет его подтверждённые заказы и выдаёт то, что выдало бы правило.
func (h *AchievementHandler) AdminRecheckUserAchievements(w http.ResponseWriter, r *http.Request) {
	userID, ok := parseIDParam(w, r, "id", "user id")
	if !ok {
		return
	}
	if h.dispatcher == nil {
		http.Error(w, "achievement dispatcher is not configured", http.StatusServiceUnavailable)
		return
	}
	result, err := h.dispatcher.RecheckUser(r.Context(), userID)
	if err != nil {
		log.Printf("[achievement] recheck of %s failed: %v", userID, err)
		http.Error(w, "не удалось пересчитать ачивки", http.StatusInternalServerError)
		return
	}
	log.Printf("[AUDIT] admin %v rechecked achievements of %s", adminID(middleware.UserFrom(r)), userID)
	writeJSON(w, http.StatusOK, result)
}

// AdminGrantAchievement обслуживает POST /admin/users/{id}/achievements/{code}:
// выдача вручную, минуя правило ачивки.
func (h *AchievementHandler) AdminGrantAchievement(w http.ResponseWriter, r *http.Request) {
	userID, ok := parseIDParam(w, r, "id", "user id")
	if !ok {
		return
	}
	code := strings.TrimSpace(chi.URLParam(r, "code"))
	if code == "" {
		http.Error(w, "invalid code", http.StatusBadRequest)
		return
	}
	if h.dispatcher == nil {
		http.Error(w, "achievement dispatcher is not configured", http.StatusServiceUnavailable)
		return
	}
	var body struct {
		Reason string `json:"reason"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	row, err := h.dispatcher.GrantManually(r.Context(), userID, code, body.Reason)
	switch {
	case errors.Is(err, service.ErrAchievementNotGrantable):
		http.Error(w, "ачивку нельзя выдать: она выключена, удалена или её скрипт не загружен", http.StatusConflict)
		return
	case errors.Is(err, repository.ErrAchievementAlreadyGranted):
		http.Error(w, "разовая ачивка у этого пользователя уже есть", http.StatusConflict)
		return
	case err != nil:
		log.Printf("[achievement] manual grant of %s to %s failed: %v", code, userID, err)
		http.Error(w, "не удалось выдать ачивку", http.StatusInternalServerError)
		return
	}
	log.Printf("[AUDIT] admin %v granted achievement %s to %s by hand: %s",
		adminID(middleware.UserFrom(r)), code, userID, body.Reason)
	writeJSON(w, http.StatusOK, row)
}

// AdminListGifts обслуживает GET /admin/gifts вместе с остатком пула кодов.
func (h *AchievementHandler) AdminListGifts(w http.ResponseWriter, r *http.Request) {
	out, err := h.gifts.AdminList(r.Context())
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// AdminSaveGift обслуживает PUT /admin/gifts/{code}.
func (h *AchievementHandler) AdminSaveGift(w http.ResponseWriter, r *http.Request) {
	var form service.GiftForm
	if !decodeBody(w, r, &form) {
		return
	}
	gift, err := h.gifts.Save(r.Context(), adminID(middleware.UserFrom(r)), chi.URLParam(r, "code"), form)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, gift)
}

// AdminAddGiftCodes обслуживает POST /admin/gifts/{code}/codes — пополнение
// пула сертификатов кодами от партнёра.
func (h *AchievementHandler) AdminAddGiftCodes(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Codes []string `json:"codes"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	added, err := h.gifts.AddCodes(r.Context(), adminID(middleware.UserFrom(r)), chi.URLParam(r, "code"), body.Codes)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"added": added})
}

// AdminRedeemCoupon обслуживает POST /admin/gifts/coupons/{coupon}/redeem: так
// администратор отмечает, что вещь выдана на руки.
func (h *AchievementHandler) AdminRedeemCoupon(w http.ResponseWriter, r *http.Request) {
	admin, ok := requireUser(w, r)
	if !ok {
		return
	}
	gift, err := h.gifts.RedeemCoupon(r.Context(), admin.ID, chi.URLParam(r, "coupon"))
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, gift)
}

// AdminListIncidents обслуживает GET /admin/finances/incidents?all=1&limit=&offset=.
func (h *AchievementHandler) AdminListIncidents(w http.ResponseWriter, r *http.Request) {
	limit, offset := pageParams(r)
	incidents, err := h.incidents.List(r.Context(), r.URL.Query().Get("all") != "1", limit, offset)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, incidents)
}

// AdminResolveIncident обслуживает POST /admin/finances/incidents/{id}/resolve.
func (h *AchievementHandler) AdminResolveIncident(w http.ResponseWriter, r *http.Request) {
	admin, ok := requireUser(w, r)
	if !ok {
		return
	}
	id, ok := parseIDParam(w, r, "id", "incident id")
	if !ok {
		return
	}
	var body struct {
		Resolution string `json:"resolution"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if err := h.incidents.Resolve(r.Context(), admin.ID, id, body.Resolution); err != nil {
		writeDomainError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// AdminRecalculateStats обслуживает POST /admin/users/{id}/stats/recalculate.
func (h *AchievementHandler) AdminRecalculateStats(w http.ResponseWriter, r *http.Request) {
	userID, ok := parseIDParam(w, r, "id", "user id")
	if !ok {
		return
	}
	stats, err := h.catalog.RecalculateStats(r.Context(), adminID(middleware.UserFrom(r)), userID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, stats)
}
