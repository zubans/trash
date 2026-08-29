// Command opsbot exposes a few operational actions over Telegram.
//
// Three things run the platform on a bad day: force a books reconciliation,
// look at the headline metrics, restart the services. All three used to need an
// SSH session, which is a poor fit for the moment they are actually wanted.
//
// The bot is a privileged tool and is built like one:
//
//   - it polls Telegram outbound and listens on no port, so it has no inbound
//     attack surface at all;
//   - every message is checked against an allowlist of chat and user ids before
//     it is even parsed as a command;
//   - the restart takes a fixed argument list with no shell, so the only thing
//     a message decides is whether to run it, never what runs;
//   - the restart also needs a second, explicit confirmation, because a stray
//     tap must not bounce production;
//   - messages that predate the process are acknowledged and discarded, so a
//     command sent while the bot was down cannot fire on boot — which for a
//     restart command would otherwise be a loop.
//
// It needs the Docker socket to restart the stack, which is root on the host.
// That is why it is a separate service and not a goroutine in the backend: the
// backend faces the internet, and this must not.
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

type metricTarget struct {
	name  string
	url   string
	watch []string
}

type config struct {
	token          string
	allowedChats   map[int64]bool
	allowedUsers   map[int64]bool
	opsKey         string
	reconcileURL   string
	projectDir     string
	restartTimeout time.Duration
	confirmWindow  time.Duration
	metricTargets  []metricTarget
}

type bot struct {
	cfg  config
	tg   *telegram
	http *http.Client

	// One command at a time. Two concurrent restarts, or a reconciliation
	// racing a restart, is never what anybody meant.
	mu             sync.Mutex
	pendingRestart map[int64]time.Time
}

func main() {
	cfg := loadConfig()
	b := &bot{
		cfg:            cfg,
		tg:             newTelegram(cfg.token),
		http:           &http.Client{Timeout: 60 * time.Second},
		pendingRestart: make(map[int64]time.Time),
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	log.Printf("[opsbot] started; %d chat(s) allowed", len(cfg.allowedChats))
	b.run(ctx)
	log.Printf("[opsbot] stopped")
}

func (b *bot) run(ctx context.Context) {
	offset := 0
	startedAt := time.Now()

	for ctx.Err() == nil {
		updates, err := b.tg.getUpdates(offset, 50*time.Second)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Printf("[opsbot] getUpdates: %v", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(10 * time.Second):
			}
			continue
		}

		for _, u := range updates {
			// Acknowledged whatever happens: an update that keeps coming back
			// because handling it failed would retry a privileged command.
			if u.UpdateID >= offset {
				offset = u.UpdateID + 1
			}
			if u.Message == nil || u.Message.Chat == nil {
				continue
			}
			if time.Unix(u.Message.Date, 0).Before(startedAt) {
				log.Printf("[opsbot] discarding a message that predates this process")
				continue
			}
			b.handle(ctx, u)
		}
	}
}

func (b *bot) handle(ctx context.Context, u update) {
	chatID := u.Message.Chat.ID
	text := strings.TrimSpace(u.Message.Text)

	var userID int64
	var username string
	if u.Message.From != nil {
		userID = u.Message.From.ID
		username = u.Message.From.Username
	}

	// The allowlist is checked before the message is interpreted at all. An
	// unknown chat gets no answer: replying would confirm the bot exists and
	// tell an unwanted visitor which commands to try.
	if !b.cfg.allowedChats[chatID] || (len(b.cfg.allowedUsers) > 0 && !b.cfg.allowedUsers[userID]) {
		log.Printf("[opsbot] ignoring a message from chat=%d user=%d", chatID, userID)
		return
	}

	command, _, _ := strings.Cut(text, " ")
	// Group chats deliver commands as /restart@thebot.
	command, _, _ = strings.Cut(command, "@")
	command = strings.ToLower(command)

	b.mu.Lock()
	defer b.mu.Unlock()

	switch command {
	case "/start", "/help":
		b.reply(chatID, helpText)

	case "/reconcile", "/сверка":
		b.reply(chatID, "⏳ Запускаю сверку…")
		b.reply(chatID, b.runReconcile(ctx))

	case "/metrics", "/метрики":
		b.reply(chatID, "⏳ Собираю метрики…")
		b.reply(chatID, b.checkMetrics(ctx))

	case "/restart":
		b.pendingRestart[chatID] = time.Now()
		b.reply(chatID, "⚠️ <b>Перезапуск сервисов</b>\n\nПриложение будет недоступно примерно минуту. "+
			"Для подтверждения отправьте <code>/restart_confirm</code> в течение "+
			b.cfg.confirmWindow.String()+".")

	case "/restart_confirm":
		requested, ok := b.pendingRestart[chatID]
		delete(b.pendingRestart, chatID)
		if !ok || time.Since(requested) > b.cfg.confirmWindow {
			b.reply(chatID, "Подтверждать нечего — запрос не найден или истёк. Начните с <code>/restart</code>.")
			return
		}
		log.Printf("[opsbot] restart confirmed by user=%d (%s)", userID, username)
		b.reply(chatID, "🔄 Перезапускаю…")
		b.reply(chatID, b.restart(ctx))

	default:
		if strings.HasPrefix(command, "/") {
			b.reply(chatID, "Не знаю такой команды.\n\n"+helpText)
		}
	}
}

const helpText = `<b>Команды</b>

/reconcile — принудительная сверка балансов, с публикацией метрик
/metrics — текущее состояние ключевых метрик
/restart — перезапуск сервисов (нужно подтверждение)
/help — эта справка`

func (b *bot) reply(chatID int64, text string) {
	if err := b.tg.send(chatID, text); err != nil {
		log.Printf("[opsbot] sendMessage: %v", err)
	}
}

func loadConfig() config {
	token := os.Getenv("TELEGRAM_BOT_TOKEN")
	if token == "" {
		log.Fatal("[opsbot] TELEGRAM_BOT_TOKEN is required")
	}

	chats := parseIDs(os.Getenv("TELEGRAM_CHAT_ID") + "," + os.Getenv("OPSBOT_ALLOWED_CHATS"))
	if len(chats) == 0 {
		// Refusing to start beats starting with an empty allowlist, which would
		// take commands from anyone who found the bot.
		log.Fatal("[opsbot] no allowed chats: set TELEGRAM_CHAT_ID (and optionally OPSBOT_ALLOWED_CHATS)")
	}

	opsKey := os.Getenv("OPS_KEY")
	if opsKey == "" {
		log.Fatal("[opsbot] OPS_KEY is required and must match the backend")
	}

	backend := getEnv("OPSBOT_BACKEND_URL", "http://backend:9091")
	return config{
		token:          token,
		allowedChats:   chats,
		allowedUsers:   parseIDs(os.Getenv("OPSBOT_ALLOWED_USERS")),
		opsKey:         opsKey,
		reconcileURL:   strings.TrimRight(backend, "/") + "/internal/reconcile",
		projectDir:     getEnv("OPSBOT_PROJECT_DIR", "/project"),
		restartTimeout: getEnvDuration("OPSBOT_RESTART_TIMEOUT", 10*time.Minute),
		confirmWindow:  getEnvDuration("OPSBOT_CONFIRM_WINDOW", 2*time.Minute),
		metricTargets: []metricTarget{
			{
				name: "Бэкенд",
				url:  strings.TrimRight(backend, "/") + "/metrics",
				watch: []string{
					"healthlogin_reconcile_ok",
					"healthlogin_reconcile_last_run_timestamp_seconds",
					"healthlogin_orders_searching",
					"healthlogin_shifts_active",
					"healthlogin_http_requests_in_flight",
					"healthlogin_chat_websocket_connections",
				},
			},
		},
	}
}

func parseIDs(raw string) map[int64]bool {
	ids := make(map[int64]bool)
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		id, err := strconv.ParseInt(part, 10, 64)
		if err != nil {
			log.Printf("[opsbot] ignoring malformed id %q", part)
			continue
		}
		ids[id] = true
	}
	return ids
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvDuration(key string, fallback time.Duration) time.Duration {
	if v, err := time.ParseDuration(os.Getenv(key)); err == nil && v > 0 {
		return v
	}
	return fallback
}
