package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"

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
		log.Fatalf("failed to connect to database: %v", err)
	}
	defer db.Close()

	repo := repository.NewAppReleaseRepository(db)
	nextCode, err := repo.GetNextVersionCode(platform)
	if err != nil {
		log.Fatalf("failed to get next version code: %v", err)
	}

	fmt.Print(nextCode)
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
