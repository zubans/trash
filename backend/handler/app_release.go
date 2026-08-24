package handler

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

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

// allowedPlatforms limits the directory component of the stored path.
var allowedPlatforms = map[string]bool{"android": true}

// versionNamePattern keeps the version out of the file name's control: without
// it, "../../.." in platform or version_name wrote the uploaded file anywhere
// on the file system.
var versionNamePattern = regexp.MustCompile(`^[0-9A-Za-z._\-]{1,64}$`)

// maxReleaseBytes caps an uploaded APK.
const maxReleaseBytes = 300 << 20

// UploadReleaseHandler handles POST /admin/app-releases.
func (h *AppReleaseHandler) UploadReleaseHandler(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxReleaseBytes)
	platform := r.FormValue("platform")
	versionName := r.FormValue("version_name")
	versionCode, err := strconv.Atoi(r.FormValue("version_code"))
	if err != nil {
		http.Error(w, "invalid version_code", http.StatusBadRequest)
		return
	}
	releaseNotes := r.FormValue("release_notes")
	forceUpdate := r.FormValue("force_update") == "true"

	if !allowedPlatforms[platform] {
		http.Error(w, "unsupported platform", http.StatusBadRequest)
		return
	}
	if !versionNamePattern.MatchString(versionName) {
		http.Error(w, "invalid version_name", http.StatusBadRequest)
		return
	}
	if versionCode <= 0 {
		http.Error(w, "invalid version_code", http.StatusBadRequest)
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

	// Defence in depth: the resolved path must stay under the releases root.
	base, err := filepath.Abs(h.releasesDir)
	if err != nil {
		http.Error(w, "invalid releases directory", http.StatusInternalServerError)
		return
	}
	abs, err := filepath.Abs(fullPath)
	if err != nil {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}
	if rel, err := filepath.Rel(base, abs); err != nil || strings.HasPrefix(rel, "..") {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}

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
