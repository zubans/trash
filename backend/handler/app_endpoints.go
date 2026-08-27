package handler

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"healthlogin/backend/metrics"
)

// AppEndpointsHandler serves the VLESS fallback endpoint list to the mobile app.
//
// The list carries server addresses and Reality keys, so it must not sit on the
// server in the clear nor be readable by anyone who hits the URL:
//   - access is gated by a shared app key sent in the X-App-Key header
//     (constant-time compared);
//   - the payload is encrypted with AES-256-GCM before it leaves the process, so
//     only a build that embeds the same key can read it.
//
// The plaintext file is mounted read-only and never served directly.
type AppEndpointsHandler struct {
	filePath string
	appKey   string
	encKey   []byte // 32 bytes for AES-256, decoded from hex at construction
}

// NewAppEndpointsHandler builds the handler. appKey gates access; encKeyHex is a
// 64-char hex string (32 bytes). If either secret is missing or malformed the
// handler is created disabled and every request answers 503, so a misconfigured
// deployment fails loudly instead of leaking plaintext.
func NewAppEndpointsHandler(filePath, appKey, encKeyHex string) *AppEndpointsHandler {
	h := &AppEndpointsHandler{filePath: filePath, appKey: appKey}
	if key, err := hex.DecodeString(encKeyHex); err == nil && len(key) == 32 {
		h.encKey = key
	}
	// Publish what the list holds at startup rather than waiting for the first
	// client. The gauge reads zero until something sets it, so reporting it
	// only on a served request made "no app has polled yet" indistinguishable
	// from "the list is empty" — and the second is an outage of the fallback
	// channel. Reading the file once here makes the metric describe the file
	// rather than the traffic.
	h.publishListStats(nil)
	h.logListState()
	return h
}

// logListState says at startup whether the endpoint list can actually be read.
//
// Without this the only symptom of a broken list is a 503 per request, which
// looks like an application fault and says nothing about the cause. The
// directory case gets its own message because it is not a typo but a Docker
// behaviour: a bind mount whose source file does not exist is created as a
// directory, and since the same path is mounted read-only into this container,
// every request then fails for a reason that is invisible from in here.
func (h *AppEndpointsHandler) logListState() {
	if h.appKey == "" || len(h.encKey) != 32 {
		log.Printf("[app-endpoints] WARNING: keys are not configured — /app/endpoints will answer 503 and the mobile fallback channel is unavailable")
		return
	}

	info, err := os.Stat(h.filePath)
	switch {
	case err != nil:
		log.Printf("[app-endpoints] WARNING: %s cannot be read (%v) — /app/endpoints will answer 503 and the mobile fallback channel is unavailable", h.filePath, err)
	case info.IsDir():
		log.Printf("[app-endpoints] WARNING: %s is a directory, not a file. Docker creates one when a bind mount's source file is missing; remove it on the host, put the real list there and recreate the container.", h.filePath)
	default:
		log.Printf("[app-endpoints] endpoint list loaded from %s", h.filePath)
	}
}

// Serve handles GET /app/endpoints.
func (h *AppEndpointsHandler) Serve(w http.ResponseWriter, r *http.Request) {
	if h.appKey == "" || len(h.encKey) != 32 || h.filePath == "" {
		metrics.AppEndpointsRequest("unconfigured")
		http.Error(w, "endpoint list not configured", http.StatusServiceUnavailable)
		return
	}

	// Constant-time auth. Never reveal which half is wrong.
	provided := r.Header.Get("X-App-Key")
	if subtle.ConstantTimeCompare([]byte(provided), []byte(h.appKey)) != 1 {
		metrics.AppEndpointsRequest("forbidden")
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	plaintext, err := os.ReadFile(h.filePath)
	if err != nil {
		metrics.AppEndpointsRequest("unavailable")
		http.Error(w, "endpoint list unavailable", http.StatusServiceUnavailable)
		return
	}
	h.publishListStats(plaintext)

	sealed, err := h.encrypt(plaintext)
	if err != nil {
		metrics.AppEndpointsRequest("encrypt_error")
		http.Error(w, "encryption failed", http.StatusInternalServerError)
		return
	}
	metrics.AppEndpointsRequest("ok")

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(sealed))
}

// encrypt returns base64( nonce(12) || AES-256-GCM ciphertext ). The nonce is
// random per response, so the same file yields a different blob each time.
func (h *AppEndpointsHandler) encrypt(plaintext []byte) (string, error) {
	block, err := aes.NewCipher(h.encKey)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ct := gcm.Seal(nil, nonce, plaintext, nil)
	out := make([]byte, 0, len(nonce)+len(ct))
	out = append(out, nonce...)
	out = append(out, ct...)
	return base64.StdEncoding.EncodeToString(out), nil
}

// publishListStats reports what is actually in the list. A list that parses but
// carries no configs answers 200 and still leaves the app with nowhere to fall
// back to, so the count is worth a gauge of its own; an unparseable or
// unreadable file reports zero for the same reason.
//
// Pass the bytes already in hand when serving a request, or nil to have the
// file read here — which is what startup does.
func (h *AppEndpointsHandler) publishListStats(plaintext []byte) {
	if plaintext == nil {
		var err error
		if plaintext, err = os.ReadFile(h.filePath); err != nil {
			metrics.AppEndpointsFile(0, time.Time{})
			return
		}
	}

	var doc struct {
		Configs []json.RawMessage `json:"configs"`
	}
	count := 0
	if err := json.Unmarshal(plaintext, &doc); err == nil {
		count = len(doc.Configs)
	}
	var mtime time.Time
	if info, err := os.Stat(h.filePath); err == nil {
		mtime = info.ModTime()
	}
	metrics.AppEndpointsFile(count, mtime)
}
