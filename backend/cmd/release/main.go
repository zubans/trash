package main

import (
	"database/sql"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"

	"github.com/google/uuid"
	_ "github.com/lib/pq"

	"healthlogin/backend/repository"
)

func main() {
	var (
		apkPath      = flag.String("apk", "", "Path to APK file (required)")
		platform     = flag.String("platform", "android", "Platform name")
		versionName  = flag.String("version-name", "", "Major/minor version, e.g. 1.0.1 (required, overrides build.gradle)")
		versionCode  = flag.Int("version-code", 0, "Version code (required, overrides auto-detection)")
		versionFile  = flag.String("version-file", "../frontend/android/app/build.gradle", "Path to build.gradle fallback")
		releasesDir  = flag.String("releases-dir", "releases", "Directory where release files are stored")
		releaseNotes = flag.String("release-notes", "", "Release notes")
		forceUpdate  = flag.Bool("force-update", false, "Mark release as forced update")
	)
	flag.Parse()

	if *apkPath == "" {
		flag.Usage()
		log.Fatal("-apk is required")
	}

	if _, err := os.Stat(*apkPath); err != nil {
		log.Fatalf("apk file not found: %v", err)
	}

	db, err := openDB()
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}
	defer db.Close()

	releaseRepo := repository.NewAppReleaseRepository(db)

	resolvedVersionName := *versionName
	if resolvedVersionName == "" {
		parsedName, _, err := parseVersionFromGradle(*versionFile)
		if err != nil {
			log.Fatalf("failed to parse version from %s: %v", *versionFile, err)
		}
		resolvedVersionName = parsedName
	}

	resolvedVersionCode := *versionCode
	if resolvedVersionCode <= 0 {
		_, parsedCode, err := parseVersionFromGradle(*versionFile)
		if err != nil {
			log.Fatalf("failed to parse version code from %s: %v", *versionFile, err)
		}
		resolvedVersionCode = parsedCode
	}

	fileName := fmt.Sprintf("app-release-%s-%d.apk", resolvedVersionName, resolvedVersionCode)
	relFilePath := filepath.Join("releases", *platform, fileName)
	fullDestPath := filepath.Join(*releasesDir, *platform, fileName)

	if err := os.MkdirAll(filepath.Dir(fullDestPath), 0755); err != nil {
		log.Fatalf("failed to create release directory: %v", err)
	}

	src, err := os.Open(*apkPath)
	if err != nil {
		log.Fatalf("failed to open apk: %v", err)
	}
	defer src.Close()

	dst, err := os.Create(fullDestPath)
	if err != nil {
		log.Fatalf("failed to create destination file: %v", err)
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		log.Fatalf("failed to copy apk: %v", err)
	}

	release := &repository.AppRelease{
		ID:           uuid.New(),
		Platform:     *platform,
		VersionName:  resolvedVersionName,
		VersionCode:  resolvedVersionCode,
		FileName:     fileName,
		FilePath:     "/" + filepath.ToSlash(relFilePath),
		ReleaseNotes: *releaseNotes,
		ForceUpdate:  *forceUpdate,
		IsActive:     true,
	}

	if err := releaseRepo.CreateRelease(release); err != nil {
		log.Fatalf("failed to create release record: %v", err)
	}

	if err := releaseRepo.DeactivateOldReleases(*platform, release.ID); err != nil {
		log.Printf("warning: failed to deactivate old releases: %v", err)
	}

	fmt.Printf("Registered release: platform=%s version=%s code=%d file=%s url=%s\n",
		release.Platform, release.VersionName, release.VersionCode, fullDestPath, release.FilePath)
}

func parseVersionFromGradle(path string) (string, int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", 0, err
	}
	content := string(data)

	nameRe := regexp.MustCompile(`versionName\s+"([^"]+)"`)
	codeRe := regexp.MustCompile(`versionCode\s+(\d+)`)

	nameMatch := nameRe.FindStringSubmatch(content)
	codeMatch := codeRe.FindStringSubmatch(content)

	if len(nameMatch) < 2 {
		return "", 0, fmt.Errorf("versionName not found in %s", path)
	}
	if len(codeMatch) < 2 {
		return "", 0, fmt.Errorf("versionCode not found in %s", path)
	}

	code, err := strconv.Atoi(codeMatch[1])
	if err != nil {
		return "", 0, fmt.Errorf("invalid versionCode: %w", err)
	}

	return nameMatch[1], code, nil
}

func openDB() (*sql.DB, error) {
	host := getEnv("DB_HOST", "localhost")
	port := getEnv("DB_PORT", "5432")
	user := getEnv("DB_USER", "healthlogin")
	password := getEnv("DB_PASSWORD", "healthlogin")
	dbname := getEnv("DB_NAME", "healthlogin")

	dsn := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		host, port, user, password, dbname)

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		return nil, err
	}
	return db, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
