// Command migrate применяет непринятые миграции базы и завершается. Сервер
// применяет их и при старте; этот бинарник существует для деплой-конвейеров,
// которым удобнее миграция отдельным явным шагом.
package main

import (
	"context"
	"log"

	"healthlogin/backend/dbconn"
	"healthlogin/backend/repository"
)

func main() {
	db, err := dbconn.OpenFromEnv(context.Background())
	if err != nil {
		log.Fatalf("connect to database: %v", err)
	}
	defer db.Close()

	if err := repository.Migrate(db, dbconn.Env("MIGRATIONS_DIR", "migrations")); err != nil {
		log.Fatalf("migrate: %v", err)
	}
	log.Println("migrations up to date")
}
