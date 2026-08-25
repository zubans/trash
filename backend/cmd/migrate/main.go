// Command migrate applies pending database migrations and exits. The server
// applies them on start as well; this binary exists for deploy pipelines that
// prefer migrating as a separate, explicit step.
package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"time"

	_ "github.com/lib/pq"

	"healthlogin/backend/repository"
)

func main() {
	dsn := fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		env("DB_HOST", "localhost"),
		env("DB_PORT", "5432"),
		env("DB_USER", "healthlogin"),
		env("DB_PASSWORD", "healthlogin"),
		env("DB_NAME", "healthlogin"),
		env("DB_SSLMODE", "disable"),
	)

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer db.Close()

	for i := 0; i < 10; i++ {
		if err = db.Ping(); err == nil {
			break
		}
		log.Printf("database not ready, retrying... (%d/10)", i+1)
		time.Sleep(2 * time.Second)
	}
	if err != nil {
		log.Fatalf("connect to database: %v", err)
	}

	if err := repository.Migrate(db, env("MIGRATIONS_DIR", "migrations")); err != nil {
		log.Fatalf("migrate: %v", err)
	}
	log.Println("migrations up to date")
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
