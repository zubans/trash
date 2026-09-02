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

// telegram — намеренно маленький клиент для двух вызовов, нужных этому боту.
//
// Длинный опрос вместо вебхука: вебхук означал бы входящий порт,
// публичный URL и маршрут через nginx для сервиса, вся работа которого —
// выполнять привилегированные команды. Опрос оставляет бота доступным лишь исходящим.
type telegram struct {
	token  string
	client *http.Client
}

func newTelegram(token string) *telegram {
	return &telegram{
		token: token,
		// Больше, чем таймаут опроса ниже, иначе каждый длинный опрос обрывал бы
		// сам клиент.
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
		// Токен не должен попадать в лог; описание безопасно.
		return fmt.Errorf("%s: %s", method, envelope.Description)
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(envelope.Result, out)
}

// getUpdates длинным опросом получает следующую пачку, подтверждая всё до
// offset. Telegram выбрасывает подтверждённые апдейты — это и не даёт команде
// повториться после перезапуска.
func (t *telegram) getUpdates(offset int, timeout time.Duration) ([]update, error) {
	params := url.Values{}
	params.Set("offset", fmt.Sprint(offset))
	params.Set("timeout", fmt.Sprint(int(timeout.Seconds())))
	// Только сообщения: этому боту не нужны правки, реакции и посты каналов, а
	// отредактированное сообщение не должно повторно запускать команду.
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
	// Telegram отклоняет всё длиннее 4096 символов, а отклонённый ответ хуже
	// урезанного, когда это ответ на операционную команду.
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

// escape делает текст безопасным для HTML-режима разбора Telegram. Вывод сверки
// несёт номера телефонов и произвольные причины; неэкранированная угловая
// скобка молча съела бы остаток сообщения.
func escape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	return r.Replace(s)
}
