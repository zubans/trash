package main

import (
	"database/sql"
	"fmt"
	"os"
	"strconv"
	"strings"

	_ "github.com/lib/pq"

	"healthlogin/backend/repository"
)

func main() {
	platform := os.Getenv("PLATFORM")
	if platform == "" {
		platform = "android"
	}

	db, err := openDB()
	if err != nil {
		fmt.Print("1.0.0")
		return
	}
	defer db.Close()

	repo := repository.NewAppReleaseRepository(db)
	active, err := repo.GetActiveRelease(platform)
	if err != nil || active == nil || active.VersionName == "" {
		fmt.Print("1.0.0")
		return
	}

	parts := strings.Split(active.VersionName, ".")
	if len(parts) == 3 {
		if patch, err := strconv.Atoi(parts[2]); err == nil {
			parts[2] = strconv.Itoa(patch + 1)
			fmt.Print(strings.Join(parts, "."))
			return
		}
	}

	fmt.Print(active.VersionName + ".1")
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
