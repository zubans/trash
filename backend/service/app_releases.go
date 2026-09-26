package service

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/google/uuid"

	"healthlogin/backend/repository"
)

// AppReleases — релизы мобильного приложения: проверка версии и публикация
// нового APK. Один активный релиз на платформу — правило этого сервиса:
// запись релиза и снятие прежних идут одной транзакцией, а файл на диске,
// оставшийся без строки, удаляется.
type AppReleases struct {
	repo repository.AppReleaseRepository
	// releasesDir — корень файлов релизов на диске; baseURL — с чего начинается
	// ссылка на скачивание.
	releasesDir string
	baseURL     string
}

// NewAppReleases создаёт AppReleases. releasesDir — каталог релизов
// (RELEASES_DIR), читается один раз в composition root.
func NewAppReleases(repo repository.AppReleaseRepository, releasesDir, baseURL string) *AppReleases {
	if releasesDir == "" {
		releasesDir = "releases"
	}
	return &AppReleases{repo: repo, releasesDir: releasesDir, baseURL: baseURL}
}

// allowedPlatforms ограничивает каталожную часть сохраняемого пути.
var allowedPlatforms = map[string]bool{"android": true}

// versionNamePattern не даёт версии управлять именем файла: без него «../../..»
// в platform или version_name записывал загруженный файл куда угодно в
// файловой системе.
var versionNamePattern = regexp.MustCompile(`^[0-9A-Za-z._\-]{1,64}$`)

// Ошибки релизов.
var (
	ErrReleasePlatformUnsupported = validationError("unsupported platform")
	ErrReleaseVersionNameInvalid  = validationError("invalid version_name")
	ErrReleaseVersionCodeInvalid  = validationError("invalid version_code")
	ErrReleaseNotFound            = notFoundError("no active release found")
)

// VersionInfo — ответ проверки версии.
type VersionInfo struct {
	VersionName  string `json:"version_name"`
	VersionCode  int    `json:"version_code"`
	DownloadURL  string `json:"download_url"`
	ForceUpdate  bool   `json:"force_update"`
	ReleaseNotes string `json:"release_notes"`
}

// Version — активный релиз платформы.
func (s *AppReleases) Version(ctx context.Context, platform string) (*VersionInfo, error) {
	if platform == "" {
		return nil, validationError("platform is required")
	}
	release, err := s.repo.GetActiveRelease(ctx, platform)
	if err != nil {
		return nil, err
	}
	if release == nil {
		return nil, ErrReleaseNotFound
	}
	downloadURL := release.FilePath
	if s.baseURL != "" {
		downloadURL = s.baseURL + release.FilePath
	}
	return &VersionInfo{
		VersionName: release.VersionName, VersionCode: release.VersionCode,
		DownloadURL: downloadURL, ForceUpdate: release.ForceUpdate, ReleaseNotes: release.ReleaseNotes,
	}, nil
}

// ReleaseForm — поля публикации релиза.
type ReleaseForm struct {
	Platform     string
	VersionName  string
	VersionCode  int
	ReleaseNotes string
	ForceUpdate  bool
}

// validate проверяет форму до того, как файл тронет диск.
func (f ReleaseForm) validate() error {
	if !allowedPlatforms[f.Platform] {
		return ErrReleasePlatformUnsupported
	}
	if !versionNamePattern.MatchString(f.VersionName) {
		return ErrReleaseVersionNameInvalid
	}
	if f.VersionCode <= 0 {
		return ErrReleaseVersionCodeInvalid
	}
	return nil
}

// Target — куда положить APK: каталог и имя без расширения. Файл пишет
// вызывающий (upload.Save), а Publish заводит строку под уже лежащий файл.
func (s *AppReleases) Target(form ReleaseForm) (dir, name string, err error) {
	if err := form.validate(); err != nil {
		return "", "", err
	}
	name = fmt.Sprintf("app-release-%s-%d", form.VersionName, form.VersionCode)
	dir = filepath.Join(s.releasesDir, "releases", form.Platform)
	// Эшелонированная защита: разрешённый путь обязан остаться внутри корня релизов.
	base, err := filepath.Abs(s.releasesDir)
	if err != nil {
		return "", "", err
	}
	abs, err := filepath.Abs(filepath.Join(dir, name+".apk"))
	if err != nil {
		return "", "", err
	}
	if rel, err := filepath.Rel(base, abs); err != nil || strings.HasPrefix(rel, "..") {
		return "", "", validationError("invalid path")
	}
	return dir, name, nil
}

// Publish заводит релиз под файл, уже лежащий по пути из Target, и снимает
// активность с прежних релизов платформы в той же транзакции. Если строку
// записать не удалось, файл удаляется: релиз без строки никто не найдёт, а
// место он занимает.
func (s *AppReleases) Publish(ctx context.Context, actorID uuid.UUID, form ReleaseForm, filePath string) (*repository.AppRelease, error) {
	if err := form.validate(); err != nil {
		os.Remove(filePath)
		return nil, err
	}
	fileName := filepath.Base(filePath)
	release := &repository.AppRelease{
		ID:           uuid.New(),
		Platform:     form.Platform,
		VersionName:  form.VersionName,
		VersionCode:  form.VersionCode,
		FileName:     fileName,
		FilePath:     "/" + filepath.ToSlash(filepath.Join("releases", form.Platform, fileName)),
		ReleaseNotes: form.ReleaseNotes,
		ForceUpdate:  form.ForceUpdate,
		IsActive:     true,
	}
	if err := s.repo.Publish(ctx, release); err != nil {
		if rmErr := os.Remove(filePath); rmErr != nil {
			log.Printf("[release] cannot remove orphan file %s: %v", filePath, rmErr)
		}
		return nil, err
	}
	log.Printf("[AUDIT] admin %s published %s release %s (%d)", actorID, form.Platform, form.VersionName, form.VersionCode)
	return release, nil
}
