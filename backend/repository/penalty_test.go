package repository_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"healthlogin/backend/repository"
)

func userStatus(t *testing.T, db *sql.DB, id interface{}) string {
	t.Helper()
	var status string
	if err := db.QueryRow(`SELECT status::text FROM users WHERE id = $1`, id).Scan(&status); err != nil {
		t.Fatalf("read status: %v", err)
	}
	return status
}

// Мягкий бан — два шага в одной транзакции вызывающего: статус ставит
// UserRepository.UpdateStatus, причину пишет ApplySoftBan. Снятие симметрично;
// флаг прошлой тихой блокировки оно не трогает.
func TestPenaltyRepository_SoftBan(t *testing.T) {
	db := testDB(t)
	defer db.Close()
	repo := repository.NewPenaltyRepository(db)
	users := repository.New(db)
	ctx := context.Background()

	userID := createTestUser(t, db, "EXECUTOR")
	adminID := createTestUser(t, db, "ADMIN")

	if _, err := db.Exec(`INSERT INTO user_penalty_flags (user_id, had_silent_block_at) VALUES ($1, now())`, userID); err != nil {
		t.Fatalf("seed flags: %v", err)
	}

	if err := users.UpdateStatus(ctx, nil, userID, repository.UserStatusSoftBanned); err != nil {
		t.Fatalf("status: %v", err)
	}
	if err := repo.ApplySoftBan(ctx, nil, userID, &adminID, "обход правил"); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if got := userStatus(t, db, userID); got != repository.UserStatusSoftBanned {
		t.Fatalf("status %s, want SOFT_BANNED", got)
	}
	flags, err := repo.GetFlags(ctx, nil, userID)
	if err != nil {
		t.Fatalf("flags: %v", err)
	}
	if flags.SoftBannedAt == nil || flags.SoftBannedBy == nil || *flags.SoftBannedBy != adminID || flags.SoftBanReason != "обход правил" {
		t.Fatalf("soft ban not recorded: %+v", flags)
	}

	if err := users.UpdateStatus(ctx, nil, userID, repository.UserStatusActive); err != nil {
		t.Fatalf("status: %v", err)
	}
	if err := repo.LiftSoftBan(ctx, nil, userID); err != nil {
		t.Fatalf("lift: %v", err)
	}
	if got := userStatus(t, db, userID); got != repository.UserStatusActive {
		t.Fatalf("status %s, want ACTIVE", got)
	}
	flags, _ = repo.GetFlags(ctx, nil, userID)
	if flags.SoftBannedAt != nil || flags.SoftBannedBy != nil || flags.SoftBanReason != "" {
		t.Fatalf("soft ban reason left after lift: %+v", flags)
	}
	if flags.HadSilentBlockAt == nil {
		t.Fatal("lift cleared the past silent block flag")
	}
}

// Системный бан (by == nil) пишется и без строки флагов; статус несуществующего
// пользователя не ставится — UpdateStatus отвечает ErrNotFound.
func TestPenaltyRepository_SystemSoftBanWithoutFlagsRow(t *testing.T) {
	db := testDB(t)
	defer db.Close()
	repo := repository.NewPenaltyRepository(db)
	users := repository.New(db)
	ctx := context.Background()

	userID := createTestUser(t, db, "CUSTOMER")
	if err := repo.ApplySoftBan(ctx, nil, userID, nil, "рецидив"); err != nil {
		t.Fatalf("apply: %v", err)
	}
	flags, err := repo.GetFlags(ctx, nil, userID)
	if err != nil {
		t.Fatalf("flags: %v", err)
	}
	if flags.SoftBannedBy != nil || flags.SoftBanReason != "рецидив" {
		t.Fatalf("system soft ban: %+v", flags)
	}

	missing := createTestUser(t, db, "CUSTOMER")
	if _, err := db.Exec(`DELETE FROM users WHERE id = $1`, missing); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := users.UpdateStatus(ctx, nil, missing, repository.UserStatusSoftBanned); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("status of a missing user: %v, want ErrNotFound", err)
	}
}
