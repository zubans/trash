package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// telegram is a deliberately small client for the two calls this bot needs.
//
// Long polling rather than a webhook: a webhook would mean an inbound port,
// a public URL and a route through nginx for a service whose whole job is to
// run privileged commands. Polling keeps the bot reachable only outbound.
type telegram struct {
	token  string
	client *http.Client
}

func newTelegram(token string) *telegram {
	return &telegram{
		token: token,
		// Longer than the poll timeout below, or every long poll would be cut
		// off by the client itself.
		client: &http.Client{Timeout: 90 * time.Second},
	}
}

type update struct {
	UpdateID int `json:"update_id"`
	Message  *struct {
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
	} `json:"message"`
}

func (t *telegram) call(method string, params url.Values, out any) error {
	endpoint := fmt.Sprintf("https://api.telegram.org/bot%s/%s", t.token, method)
	resp, err := t.client.PostForm(endpoint, params)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}

	var envelope struct {
		OK          bool            `json:"ok"`
		Description string          `json:"description"`
		Result      json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return fmt.Errorf("%s: malformed response", method)
	}
	if !envelope.OK {
		// The token must never reach a log line; the description is safe.
		return fmt.Errorf("%s: %s", method, envelope.Description)
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(envelope.Result, out)
}

// getUpdates long-polls for the next batch, acknowledging everything before
// offset. Telegram drops acknowledged updates, which is what keeps a command
// from being replayed after a restart.
func (t *telegram) getUpdates(offset int, timeout time.Duration) ([]update, error) {
	params := url.Values{}
	params.Set("offset", fmt.Sprint(offset))
	params.Set("timeout", fmt.Sprint(int(timeout.Seconds())))
	// Only messages: this bot has no use for edits, reactions or channel posts,
	// and an edited message must never re-trigger a command.
	params.Set("allowed_updates", `["message"]`)

	var updates []update
	if err := t.call("getUpdates", params, &updates); err != nil {
		return nil, err
	}
	return updates, nil
}

func (t *telegram) send(chatID int64, text string) error {
	params := url.Values{}
	params.Set("chat_id", fmt.Sprint(chatID))
	params.Set("parse_mode", "HTML")
	params.Set("disable_web_page_preview", "true")
	// Telegram rejects anything over 4096 characters, and a rejected reply is
	// worse than a trimmed one when it is the answer to an ops command.
	params.Set("text", truncate(text, 4000))
	return t.call("sendMessage", params, nil)
}

func truncate(s string, limit int) string {
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return string(runes[:limit]) + "\n…"
}

// escape makes text safe for Telegram's HTML parse mode. Reconciliation output
// carries phone numbers and free-form reasons; an unescaped angle bracket would
// silently drop the rest of the message.
func escape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	return r.Replace(s)
}
