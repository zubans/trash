package handler

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"healthlogin/backend/middleware"
	"healthlogin/backend/service"
	"healthlogin/backend/upload"
)

// AppReleaseHandler обслуживает HTTP-эндпоинты релизов мобильного приложения.
type AppReleaseHandler struct {
	releases *service.AppReleases
}

// NewAppReleaseHandler создаёт AppReleaseHandler.
func NewAppReleaseHandler(releases *service.AppReleases) *AppReleaseHandler {
	return &AppReleaseHandler{releases: releases}
}

// RegisterPublicRoutes — проверка версии приложением.
func (h *AppReleaseHandler) RegisterPublicRoutes(r chi.Router) {
	r.Get("/app/version", h.GetVersionHandler)
}

// RegisterAdminRoutes — публикация релиза.
func (h *AppReleaseHandler) RegisterAdminRoutes(r chi.Router, can func(string) func(http.Handler) http.Handler) {
	r.With(can("releases.create")).Post("/admin/app-releases", h.UploadReleaseHandler)
}

// GetVersionHandler обслуживает GET /app/version?platform=.
func (h *AppReleaseHandler) GetVersionHandler(w http.ResponseWriter, r *http.Request) {
	info, err := h.releases.Version(r.Context(), r.URL.Query().Get("platform"))
	if err != nil {
		// Нет платформы — 400 с текстом, как и раньше; нет релиза — 404.
		writeCatalogError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, info)
}

// maxReleaseBytes ограничивает размер загружаемого APK.
const maxReleaseBytes = 300 << 20

// apkTypes — APK есть zip; ничего другого под именем релиза не сохраняется.
var apkTypes = map[string]string{"application/zip": ".apk"}

// UploadReleaseHandler обслуживает POST /admin/app-releases (multipart:
// platform, version_name, version_code, release_notes, force_update, apk).
func (h *AppReleaseHandler) UploadReleaseHandler(w http.ResponseWriter, r *http.Request) {
	// Потолок ставится до первого чтения формы: имя файла зависит от версии,
	// поэтому поля читаются раньше файла, а форма разбирается один раз.
	r.Body = http.MaxBytesReader(w, r.Body, maxReleaseBytes)
	versionCode, err := strconv.Atoi(r.FormValue("version_code"))
	if err != nil {
		http.Error(w, "invalid version_code", http.StatusBadRequest)
		return
	}
	form := service.ReleaseForm{
		Platform:     r.FormValue("platform"),
		VersionName:  r.FormValue("version_name"),
		VersionCode:  versionCode,
		ReleaseNotes: r.FormValue("release_notes"),
		ForceUpdate:  r.FormValue("force_update") == "true",
	}
	dir, name, err := h.releases.Target(form)
	if err != nil {
		writeCatalogError(w, err)
		return
	}
	saved, err := upload.Save(w, r, upload.Options{
		Field: "apk", Dir: dir, Name: name,
		Accept: upload.ByContent(apkTypes, "apk file required"),
	})
	if err != nil {
		writeUploadError(w, err, "apk file required")
		return
	}
	release, err := h.releases.Publish(r.Context(), adminID(middleware.UserFrom(r)), form, saved.Path)
	if err != nil {
		writeCatalogError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, release)
}
