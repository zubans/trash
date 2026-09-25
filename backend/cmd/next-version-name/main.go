package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strconv"
	"strings"

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
		fmt.Print("1.0.0")
		return
	}
	defer db.Close()

	repo := repository.NewAppReleaseRepository(db)
	active, err := repo.GetActiveRelease(context.Background(), platform)
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

// openDB открывает базу по окружению без ожидания: это инструмент разработчика
// и конвейера сборки, и отсутствующую базу он должен показать сразу, а не
// через двадцать секунд ретраев.
func openDB(ctx context.Context) (*sql.DB, error) {
	cfg := dbconn.FromEnv()
	cfg.Attempts = 1
	return dbconn.Open(ctx, cfg)
}
