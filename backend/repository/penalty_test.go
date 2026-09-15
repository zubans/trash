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

// Мягкий бан ставится вместе с причиной и снимается вместе с ней; флаг прошлой
// тихой блокировки снятие не трогает.
func TestPenaltyRepository_SoftBan(t *testing.T) {
	db := testDB(t)
	defer db.Close()
	repo := repository.NewPenaltyRepository(db)
	ctx := context.Background()

	userID := createTestUser(t, db, "EXECUTOR")
	adminID := createTestUser(t, db, "ADMIN")

	if _, err := db.Exec(`INSERT INTO user_penalty_flags (user_id, had_silent_block_at) VALUES ($1, now())`, userID); err != nil {
		t.Fatalf("seed flags: %v", err)
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

	// Второе снятие — не в том состоянии.
	if err := repo.LiftSoftBan(ctx, nil, userID); !errors.Is(err, repository.ErrConflict) {
		t.Fatalf("lift of an active user: %v, want ErrConflict", err)
	}
}

// Бан, поставленный системой, — без автора; строка флагов заводится сама.
func TestPenaltyRepository_SystemSoftBanWithoutFlagsRow(t *testing.T) {
	db := testDB(t)
	defer db.Close()
	repo := repository.NewPenaltyRepository(db)
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
	if err := repo.ApplySoftBan(ctx, nil, missing, nil, "x"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("apply to a missing user: %v, want sql.ErrNoRows", err)
	}
}
