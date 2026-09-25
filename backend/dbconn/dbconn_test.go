package dbconn

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestFromEnvDefaults(t *testing.T) {
	for _, key := range []string{"DB_HOST", "DB_PORT", "DB_USER", "DB_PASSWORD", "DB_NAME", "DB_SSLMODE"} {
		t.Setenv(key, "")
	}
	got := FromEnv().DSN()
	want := "host=localhost port=5432 user=healthlogin password=healthlogin dbname=healthlogin sslmode=disable"
	if got != want {
		t.Errorf("DSN with empty environment:\n got %q\nwant %q", got, want)
	}
}

// Каждая переменная доходит до строки подключения — включая DB_SSLMODE,
// которую раньше читали только два бинарника из шести.
func TestFromEnvReadsEveryVariable(t *testing.T) {
	t.Setenv("DB_HOST", "db.internal")
	t.Setenv("DB_PORT", "6432")
	t.Setenv("DB_USER", "app")
	t.Setenv("DB_PASSWORD", "s3cret")
	t.Setenv("DB_NAME", "prod")
	t.Setenv("DB_SSLMODE", "require")
	got := FromEnv().DSN()
	want := "host=db.internal port=6432 user=app password=s3cret dbname=prod sslmode=require"
	if got != want {
		t.Errorf("DSN:\n got %q\nwant %q", got, want)
	}
}

// Недоступная база: попытки кончаются, ошибка возвращается, а не зависает.
func TestOpenGivesUpAfterAttempts(t *testing.T) {
	cfg := Config{Host: "127.0.0.1", Port: "1", User: "x", Password: "x", Name: "x", SSLMode: "disable",
		Attempts: 3, RetryDelay: time.Millisecond}
	started := time.Now()
	db, err := Open(context.Background(), cfg)
	if err == nil {
		db.Close()
		t.Fatal("open succeeded against a closed port")
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Errorf("three attempts with a millisecond delay took %v", elapsed)
	}
}

// Отменённый контекст прекращает ожидание немедленно, не дожидаясь оставшихся попыток.
func TestOpenStopsOnCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cfg := Config{Host: "127.0.0.1", Port: "1", User: "x", Password: "x", Name: "x", SSLMode: "disable",
		Attempts: 100, RetryDelay: time.Hour}
	db, err := Open(ctx, cfg)
	if err == nil {
		db.Close()
		t.Fatal("open succeeded with a cancelled context")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

func TestEnv(t *testing.T) {
	t.Setenv("DBCONN_TEST_KEY", "")
	if got := Env("DBCONN_TEST_KEY", "fallback"); got != "fallback" {
		t.Errorf("empty variable: got %q", got)
	}
	t.Setenv("DBCONN_TEST_KEY", "value")
	if got := Env("DBCONN_TEST_KEY", "fallback"); got != "value" {
		t.Errorf("set variable: got %q", got)
	}
}
