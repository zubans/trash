package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"

	"healthlogin/backend/dbconn"
	"healthlogin/backend/repository"
)

func main() {
	platform := os.Getenv("PLATFORM")
	if platform == "" {
		platform = "android"
	}

	db, err := openDB(context.Background())
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}
	defer db.Close()

	repo := repository.NewAppReleaseRepository(db)
	nextCode, err := repo.GetNextVersionCode(context.Background(), platform)
	if err != nil {
		log.Fatalf("failed to get next version code: %v", err)
	}

	fmt.Print(nextCode)
}

// openDB открывает базу по окружению без ожидания: это инструмент разработчика
// и конвейера сборки, и отсутствующую базу он должен показать сразу, а не
// через двадцать секунд ретраев.
func openDB(ctx context.Context) (*sql.DB, error) {
	cfg := dbconn.FromEnv()
	cfg.Attempts = 1
	return dbconn.Open(ctx, cfg)
}
