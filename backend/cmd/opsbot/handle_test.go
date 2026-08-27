package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// newTestBot wires a bot whose Telegram calls land on a local stub, so the
// dispatch logic can be exercised without touching the network.
func newTestBot(t *testing.T, cfg config) (*bot, *[]string) {
	t.Helper()
	var sent []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		sent = append(sent, r.FormValue("text"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{}}`))
	}))
	t.Cleanup(srv.Close)

	tg := newTelegram("test-token")
	tg.client = srv.Client()
	// Point every API call at the stub.
	tg.client.Transport = rewriteHost{srv.URL, http.DefaultTransport}

	return &bot{cfg: cfg, tg: tg, http: srv.Client(), pendingRestart: map[int64]time.Time{}}, &sent
}

type rewriteHost struct {
	base string
	next http.RoundTripper
}

func (r rewriteHost) RoundTrip(req *http.Request) (*http.Response, error) {
	stub, err := http.NewRequest(req.Method, r.base+req.URL.Path, req.Body)
	if err != nil {
		return nil, err
	}
	stub.Header = req.Header
	return r.next.RoundTrip(stub)
}

func message(chatID, userID int64, text string) update {
	u := update{UpdateID: 1}
	u.Message = &struct {
		MessageID int    `json:"message_id"`
		Text      string `json:"text"`
		Date      int64  `json:"date"`
		From      *struct {
			ID       int64  `json:"id"`
			Username string `json:"username"`
		} `json:"from"`
		Chat *struct {
			ID int64 `json:"id"`
		} `json:"chat"`
	}{Text: text, Date: time.Now().Unix()}
	u.Message.Chat = &struct {
		ID int64 `json:"id"`
	}{ID: chatID}
	u.Message.From = &struct {
		ID       int64  `json:"id"`
		Username string `json:"username"`
	}{ID: userID}
	return u
}

func baseConfig() config {
	return config{
		allowedChats:  map[int64]bool{100: true},
		confirmWindow: time.Minute,
	}
}

// A bot that can restart production must not answer strangers at all. Replying
// would confirm it exists and advertise which commands to try.
func TestUnknownChatGetsNoReply(t *testing.T) {
	b, sent := newTestBot(t, baseConfig())

	b.handle(context.Background(), message(999, 1, "/help"))

	if len(*sent) != 0 {
		t.Fatalf("replied to an unknown chat: %q", *sent)
	}
}

// An allowed chat is not enough when a user allowlist is also configured.
func TestUnknownUserInAnAllowedChatIsIgnored(t *testing.T) {
	cfg := baseConfig()
	cfg.allowedUsers = map[int64]bool{7: true}
	b, sent := newTestBot(t, cfg)

	b.handle(context.Background(), message(100, 8, "/help"))

	if len(*sent) != 0 {
		t.Fatalf("replied to an unknown user: %q", *sent)
	}
}

// The restart must never happen on a single message. This asserts the first
// /restart only arms it — the command itself is not reached.
func TestRestartRequiresConfirmation(t *testing.T) {
	b, sent := newTestBot(t, baseConfig())

	b.handle(context.Background(), message(100, 1, "/restart"))

	if len(*sent) != 1 || !strings.Contains((*sent)[0], "restart_confirm") {
		t.Fatalf("first /restart should only ask for confirmation, got %q", *sent)
	}
	if _, armed := b.pendingRestart[100]; !armed {
		t.Fatal("the restart was not armed")
	}
}

func TestConfirmationWithoutARequestIsRefused(t *testing.T) {
	b, sent := newTestBot(t, baseConfig())

	b.handle(context.Background(), message(100, 1, "/restart_confirm"))

	if len(*sent) != 1 || !strings.Contains((*sent)[0], "истёк") {
		t.Fatalf("a bare confirmation must be refused, got %q", *sent)
	}
}

// An expired arming must not be usable: the window exists so that a
// confirmation typed much later cannot revive a forgotten intent.
func TestExpiredConfirmationIsRefused(t *testing.T) {
	cfg := baseConfig()
	cfg.confirmWindow = time.Millisecond
	b, sent := newTestBot(t, cfg)

	b.handle(context.Background(), message(100, 1, "/restart"))
	time.Sleep(5 * time.Millisecond)
	b.handle(context.Background(), message(100, 1, "/restart_confirm"))

	last := (*sent)[len(*sent)-1]
	if !strings.Contains(last, "истёк") {
		t.Fatalf("an expired confirmation must be refused, got %q", last)
	}
	if _, armed := b.pendingRestart[100]; armed {
		t.Fatal("an expired request must not stay armed")
	}
}

// One chat arming a restart must not let another chat confirm it.
func TestConfirmationIsPerChat(t *testing.T) {
	cfg := baseConfig()
	cfg.allowedChats[200] = true
	b, sent := newTestBot(t, cfg)

	b.handle(context.Background(), message(100, 1, "/restart"))
	b.handle(context.Background(), message(200, 1, "/restart_confirm"))

	last := (*sent)[len(*sent)-1]
	if !strings.Contains(last, "истёк") {
		t.Fatalf("another chat confirmed a restart it did not request: %q", last)
	}
}

// Group chats deliver commands as /restart@thebot.
func TestCommandWithBotSuffixIsRecognised(t *testing.T) {
	b, sent := newTestBot(t, baseConfig())

	b.handle(context.Background(), message(100, 1, "/restart@moya_usluga_bot"))

	if _, armed := b.pendingRestart[100]; !armed {
		t.Fatalf("a suffixed command was not recognised, replies: %q", *sent)
	}
}

// Ordinary chatter must not be answered; the bot lives in a chat people use.
func TestPlainTextIsIgnored(t *testing.T) {
	b, sent := newTestBot(t, baseConfig())

	b.handle(context.Background(), message(100, 1, "перезапусти пожалуйста"))

	if len(*sent) != 0 {
		t.Fatalf("answered ordinary chatter: %q", *sent)
	}
}
