package repository_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"

	"healthlogin/backend/repository"
)

// seedSupportConversation creates a support chat with n messages, one second
// apart, oldest first, and returns the chat id and the messages' timestamps.
//
// The support table is used rather than the order-chat one because it needs no
// order, service variant or customer scaffolding — the paging code under test
// is shared by both tables.
func seedSupportConversation(t *testing.T, db *sql.DB, n int) (uuid.UUID, []time.Time) {
	t.Helper()
	userID := createTestUser(t, db, "CUSTOMER")

	var chatID uuid.UUID
	if err := db.QueryRow(`INSERT INTO support_chats (user_id) VALUES ($1) RETURNING id`, userID).Scan(&chatID); err != nil {
		t.Fatalf("create support chat: %v", err)
	}

	base := time.Now().Add(-time.Duration(n) * time.Second).UTC()
	times := make([]time.Time, 0, n)
	for i := 0; i < n; i++ {
		at := base.Add(time.Duration(i) * time.Second)
		if _, err := db.Exec(
			`INSERT INTO support_messages (chat_id, sender_id, text, created_at) VALUES ($1, $2, $3, $4)`,
			chatID, userID, "msg", at,
		); err != nil {
			t.Fatalf("insert message %d: %v", i, err)
		}
		times = append(times, at)
	}
	return chatID, times
}

func TestMessagePaging(t *testing.T) {
	db := testDB(t)
	defer db.Close()

	repo := repository.NewChatRepository(db)
	ctx := context.Background()
	chatID, times := seedSupportConversation(t, db, 10)

	t.Run("default window returns the newest page, oldest first", func(t *testing.T) {
		msgs, err := repo.GetSupportMessages(ctx, chatID, repository.MessageQuery{Limit: 4})
		if err != nil {
			t.Fatalf("GetSupportMessages: %v", err)
		}
		if len(msgs) != 4 {
			t.Fatalf("got %d messages, want 4", len(msgs))
		}
		// The last four inserted, in ascending order.
		if !msgs[0].CreatedAt.Equal(times[6]) {
			t.Errorf("first message at %v, want %v", msgs[0].CreatedAt, times[6])
		}
		if !msgs[3].CreatedAt.Equal(times[9]) {
			t.Errorf("last message at %v, want %v", msgs[3].CreatedAt, times[9])
		}
		for i := 1; i < len(msgs); i++ {
			if msgs[i].CreatedAt.Before(msgs[i-1].CreatedAt) {
				t.Fatalf("messages are not oldest-first at index %d", i)
			}
		}
	})

	// What a polling client asks for: everything it has not already seen.
	t.Run("after returns only newer messages", func(t *testing.T) {
		cutoff := times[7]
		msgs, err := repo.GetSupportMessages(ctx, chatID, repository.MessageQuery{After: &cutoff})
		if err != nil {
			t.Fatalf("GetSupportMessages: %v", err)
		}
		if len(msgs) != 2 {
			t.Fatalf("got %d messages after index 7, want 2", len(msgs))
		}
		if !msgs[0].CreatedAt.Equal(times[8]) || !msgs[1].CreatedAt.Equal(times[9]) {
			t.Errorf("unexpected window: %v, %v", msgs[0].CreatedAt, msgs[1].CreatedAt)
		}
	})

	// A poll that is already up to date must come back empty, not with the page
	// it already has.
	t.Run("after the newest message returns nothing", func(t *testing.T) {
		cutoff := times[9]
		msgs, err := repo.GetSupportMessages(ctx, chatID, repository.MessageQuery{After: &cutoff})
		if err != nil {
			t.Fatalf("GetSupportMessages: %v", err)
		}
		if len(msgs) != 0 {
			t.Errorf("got %d messages, want none", len(msgs))
		}
	})

	// What scrolling back asks for: the page just above what is on screen.
	t.Run("before returns the newest older messages", func(t *testing.T) {
		cutoff := times[5]
		msgs, err := repo.GetSupportMessages(ctx, chatID, repository.MessageQuery{Before: &cutoff, Limit: 3})
		if err != nil {
			t.Fatalf("GetSupportMessages: %v", err)
		}
		if len(msgs) != 3 {
			t.Fatalf("got %d messages, want 3", len(msgs))
		}
		if !msgs[0].CreatedAt.Equal(times[2]) || !msgs[2].CreatedAt.Equal(times[4]) {
			t.Errorf("window is %v..%v, want %v..%v",
				msgs[0].CreatedAt, msgs[2].CreatedAt, times[2], times[4])
		}
	})

	t.Run("limit is capped", func(t *testing.T) {
		msgs, err := repo.GetSupportMessages(ctx, chatID, repository.MessageQuery{Limit: 100000})
		if err != nil {
			t.Fatalf("GetSupportMessages: %v", err)
		}
		if len(msgs) != 10 {
			t.Fatalf("got %d messages, want all 10", len(msgs))
		}
	})
}

// Paging must never hand back a message that was deleted — the filter has to
// survive being moved into the shared query builder.
func TestMessagePagingSkipsDeleted(t *testing.T) {
	db := testDB(t)
	defer db.Close()

	repo := repository.NewChatRepository(db)
	ctx := context.Background()
	chatID, _ := seedSupportConversation(t, db, 3)

	if _, err := db.Exec(`UPDATE support_messages SET is_deleted = true WHERE chat_id = $1`, chatID); err != nil {
		t.Fatalf("soft-delete messages: %v", err)
	}

	msgs, err := repo.GetSupportMessages(ctx, chatID, repository.MessageQuery{})
	if err != nil {
		t.Fatalf("GetSupportMessages: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("got %d deleted messages, want none", len(msgs))
	}
}
