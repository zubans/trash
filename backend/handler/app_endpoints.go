package handler

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"io"
	"net/http"
	"os"
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
	return h
}

// Serve handles GET /app/endpoints.
func (h *AppEndpointsHandler) Serve(w http.ResponseWriter, r *http.Request) {
	if h.appKey == "" || len(h.encKey) != 32 || h.filePath == "" {
		http.Error(w, "endpoint list not configured", http.StatusServiceUnavailable)
		return
	}

	// Constant-time auth. Never reveal which half is wrong.
	provided := r.Header.Get("X-App-Key")
	if subtle.ConstantTimeCompare([]byte(provided), []byte(h.appKey)) != 1 {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	plaintext, err := os.ReadFile(h.filePath)
	if err != nil {
		http.Error(w, "endpoint list unavailable", http.StatusServiceUnavailable)
		return
	}

	sealed, err := h.encrypt(plaintext)
	if err != nil {
		http.Error(w, "encryption failed", http.StatusInternalServerError)
		return
	}

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
