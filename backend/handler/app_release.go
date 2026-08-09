package handler

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"github.com/google/uuid"

	"healthlogin/backend/repository"
)

// AppReleaseHandler handles mobile app release HTTP endpoints.
type AppReleaseHandler struct {
	releaseRepo repository.AppReleaseRepository
	releasesDir string
	baseURL     string
}

// NewAppReleaseHandler creates an AppReleaseHandler.
func NewAppReleaseHandler(releaseRepo repository.AppReleaseRepository, releasesDir, baseURL string) *AppReleaseHandler {
	if releasesDir == "" {
		releasesDir = "releases"
	}
	return &AppReleaseHandler{releaseRepo: releaseRepo, releasesDir: releasesDir, baseURL: baseURL}
}

// GetVersionHandler handles GET /app/version.
func (h *AppReleaseHandler) GetVersionHandler(w http.ResponseWriter, r *http.Request) {
	platform := r.URL.Query().Get("platform")
	if platform == "" {
		http.Error(w, "platform is required", http.StatusBadRequest)
		return
	}

	release, err := h.releaseRepo.GetActiveRelease(platform)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if release == nil {
		http.Error(w, "no active release found", http.StatusNotFound)
		return
	}

	downloadURL := release.FilePath
	if h.baseURL != "" {
		downloadURL = h.baseURL + release.FilePath
	}

	writeJSON(w, map[string]interface{}{
		"version_name":  release.VersionName,
		"version_code":  release.VersionCode,
		"download_url":  downloadURL,
		"force_update":  release.ForceUpdate,
		"release_notes": release.ReleaseNotes,
	})
}

// UploadReleaseHandler handles POST /admin/app-releases.
func (h *AppReleaseHandler) UploadReleaseHandler(w http.ResponseWriter, r *http.Request) {
	platform := r.FormValue("platform")
	versionName := r.FormValue("version_name")
	versionCode, err := strconv.Atoi(r.FormValue("version_code"))
	if err != nil {
		http.Error(w, "invalid version_code", http.StatusBadRequest)
		return
	}
	releaseNotes := r.FormValue("release_notes")
	forceUpdate := r.FormValue("force_update") == "true"

	if platform == "" || versionName == "" {
		http.Error(w, "platform and version_name are required", http.StatusBadRequest)
		return
	}

	file, _, err := r.FormFile("apk")
	if err != nil {
		http.Error(w, "apk file required", http.StatusBadRequest)
		return
	}
	defer file.Close()

	fileName := fmt.Sprintf("app-release-%s-%d.apk", versionName, versionCode)
	filePath := filepath.Join("releases", platform, fileName)
	fullPath := filepath.Join(h.releasesDir, filePath)

	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	out, err := os.Create(fullPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer out.Close()

	if _, err := io.Copy(out, file); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	release := &repository.AppRelease{
		ID:           uuid.New(),
		Platform:     platform,
		VersionName:  versionName,
		VersionCode:  versionCode,
		FileName:     fileName,
		FilePath:     "/" + filepath.ToSlash(filePath),
		ReleaseNotes: releaseNotes,
		ForceUpdate:  forceUpdate,
		IsActive:     true,
	}

	if err := h.releaseRepo.CreateRelease(release); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Keep only one active release per platform.
	_ = h.releaseRepo.DeactivateOldReleases(platform, release.ID)

	w.WriteHeader(http.StatusCreated)
	writeJSON(w, release)
}
