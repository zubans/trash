package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"io"
	"testing"
)

// sealForTest mirrors AppEndpointsHandler.encrypt: base64( nonce || ct+tag ).
func sealForTest(t *testing.T, key, plaintext []byte) string {
	t.Helper()
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatalf("aes: %v", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatalf("gcm: %v", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		t.Fatalf("nonce: %v", err)
	}
	return base64.StdEncoding.EncodeToString(append(nonce, gcm.Seal(nil, nonce, plaintext, nil)...))
}
