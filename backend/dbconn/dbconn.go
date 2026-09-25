// Package dbconn — единственное место, где из окружения собирается строка
// подключения к базе и где процесс ждёт её готовности.
//
// Раньше это было переписано в шести бинарниках и уже разошлось: sslmode
// читали только migrate и reconcile, ретраи были не везде. Сервер, миграции,
// сверка и инструменты релиза теперь открывают базу одним и тем же способом.
package dbconn

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"time"

	_ "github.com/lib/pq"
)

// Умолчания подключения. Совпадают с тем, что compose-окружение ставит
// сервису; для чужого хоста всё переопределяется переменными DB_*.
const (
	DefaultHost     = "localhost"
	DefaultPort     = "5432"
	DefaultUser     = "healthlogin"
	DefaultPassword = "healthlogin"
	DefaultName     = "healthlogin"
	DefaultSSLMode  = "disable"

	// DefaultAttempts и DefaultRetryDelay — политика ожидания базы на старте:
	// десять попыток раз в две секунды, как ждал сервер до появления пакета.
	DefaultAttempts   = 10
	DefaultRetryDelay = 2 * time.Second
)

// Config — параметры подключения.
type Config struct {
	Host     string
	Port     string
	User     string
	Password string
	Name     string
	SSLMode  string

	// Attempts — сколько раз пинговать базу, прежде чем сдаться; ноль означает
	// DefaultAttempts. RetryDelay — пауза между попытками; ноль означает
	// DefaultRetryDelay.
	Attempts   int
	RetryDelay time.Duration
}

// FromEnv читает подключение из DB_HOST, DB_PORT, DB_USER, DB_PASSWORD,
// DB_NAME и DB_SSLMODE, подставляя умолчания вместо незаданных.
func FromEnv() Config {
	return Config{
		Host:     Env("DB_HOST", DefaultHost),
		Port:     Env("DB_PORT", DefaultPort),
		User:     Env("DB_USER", DefaultUser),
		Password: Env("DB_PASSWORD", DefaultPassword),
		Name:     Env("DB_NAME", DefaultName),
		SSLMode:  Env("DB_SSLMODE", DefaultSSLMode),
	}
}

// DSN собирает строку подключения в формате lib/pq.
func (c Config) DSN() string {
	return fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		c.Host, c.Port, c.User, c.Password, c.Name, c.SSLMode,
	)
}

// OpenFromEnv открывает базу по окружению и ждёт её готовности.
func OpenFromEnv(ctx context.Context) (*sql.DB, error) {
	return Open(ctx, FromEnv())
}

// Open открывает базу и пингует её, пока она не ответит или не кончатся
// попытки. Отмена ctx прекращает ожидание сразу. Ошибка означает, что база так
// и не ответила; *sql.DB при этом уже закрыт.
func Open(ctx context.Context, cfg Config) (*sql.DB, error) {
	attempts := cfg.Attempts
	if attempts <= 0 {
		attempts = DefaultAttempts
	}
	delay := cfg.RetryDelay
	if delay <= 0 {
		delay = DefaultRetryDelay
	}

	db, err := sql.Open("postgres", cfg.DSN())
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

wait:
	for attempt := 1; ; attempt++ {
		if err = db.PingContext(ctx); err == nil {
			return db, nil
		}
		if attempt >= attempts {
			break
		}
		log.Printf("[db] database not ready, retrying... (%d/%d)", attempt, attempts)
		select {
		case <-ctx.Done():
			err = ctx.Err()
			break wait
		case <-time.After(delay):
		}
	}
	_ = db.Close()
	return nil, fmt.Errorf("connect to database: %w", err)
}

// Env читает переменную окружения, подставляя fallback вместо пустой.
func Env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
