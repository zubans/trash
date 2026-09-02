// Command opsbot открывает несколько операционных действий через Telegram.
//
// В плохой день платформой управляют три вещи: принудительная сверка книг,
// взгляд на ключевые метрики, перезапуск сервисов. Все три раньше требовали
// SSH-сессии, что плохо сочетается с моментом, когда они действительно нужны.
//
// Бот — привилегированный инструмент и устроен соответственно:
//
//   - он опрашивает Telegram исходящими запросами и не слушает ни одного порта,
//     поэтому входящей поверхности атаки у него нет вовсе;
//   - каждое сообщение сверяется со списком разрешённых чатов и пользователей
//     ещё до того, как будет разобрано как команда;
//   - перезапуск получает фиксированный список аргументов без шелла, поэтому
//     сообщение решает лишь, запускать ли, но никогда — что запускается;
//   - перезапуску нужно ещё и второе явное подтверждение, потому что случайный
//     тап не должен ронять прод;
//   - сообщения, появившиеся раньше процесса, подтверждаются и отбрасываются,
//     чтобы команда, отправленная при лежащем боте, не сработала при старте —
//     для команды перезапуска это иначе был бы цикл.
//
// Чтобы перезапускать стек, ему нужен сокет Docker, а это root на хосте.
// Поэтому он — отдельный сервис, а не горутина в бэкенде: бэкенд смотрит в
// интернет, а этот смотреть не должен.
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

	// По одной команде за раз. Два одновременных перезапуска или сверка,
	// гоняющаяся с перезапуском, — никогда не то, что имелось в виду.
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
			// Подтверждаем в любом случае: апдейт, который возвращается снова и снова
			// из-за сбоя обработки, повторял бы привилегированную команду.
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

	// Список разрешённых проверяется до того, как сообщение вообще будет
	// истолковано. Неизвестный чат не получает ответа: ответ подтвердил бы, что
	// бот есть, и подсказал незваному гостю, какие команды пробовать.
	if !b.cfg.allowedChats[chatID] || (len(b.cfg.allowedUsers) > 0 && !b.cfg.allowedUsers[userID]) {
		log.Printf("[opsbot] ignoring a message from chat=%d user=%d", chatID, userID)
		return
	}

	command, _, _ := strings.Cut(text, " ")
	// Групповые чаты доставляют команды в виде /restart@thebot.
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
		// Отказаться стартовать лучше, чем стартовать с пустым списком разрешённых —
		// тогда команды принимались бы от любого, кто нашёл бота.
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
