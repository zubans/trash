// Package passport шифрует паспортные данные и фото документа
// (implementation_plan_delivery_passport.md §3.1).
//
// Шифр — AES-256-GCM, ключ приходит из окружения (PASSPORT_ENC_KEY, 32 байта в
// base64). Каждый шифротекст начинается со случайного nonce, поэтому
// одинаковые паспорта шифруются по-разному. Версия ключа хранится рядом с
// шифротекстом — чтобы ключ можно было сменить, не потеряв старые записи.
package passport

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ErrNoKey — ключ шифрования не задан. Хранить паспорт открытым нельзя даже
// временно, поэтому приём паспортов без ключа выключен.
var ErrNoKey = errors.New("passport encryption key is not configured")

// Cipher шифрует и расшифровывает данные одним ключом.
type Cipher struct {
	aead    cipher.AEAD
	version int
}

// NewCipher разбирает ключ в base64. Пустой ключ — nil без ошибки: сервер
// стартует, а приём паспортов отвечает ErrNoKey.
func NewCipher(keyBase64 string, version int) (*Cipher, error) {
	keyBase64 = strings.TrimSpace(keyBase64)
	if keyBase64 == "" {
		return nil, nil
	}
	key, err := base64.StdEncoding.DecodeString(keyBase64)
	if err != nil {
		return nil, fmt.Errorf("PASSPORT_ENC_KEY is not base64: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("PASSPORT_ENC_KEY must be 32 bytes, got %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if version <= 0 {
		version = 1
	}
	return &Cipher{aead: aead, version: version}, nil
}

// Version — версия ключа, которая пишется рядом с шифротекстом.
func (c *Cipher) Version() int {
	if c == nil {
		return 0
	}
	return c.version
}

// Seal шифрует: nonce, затем шифротекст с меткой подлинности.
func (c *Cipher) Seal(plain []byte) ([]byte, error) {
	if c == nil {
		return nil, ErrNoKey
	}
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return c.aead.Seal(nonce, nonce, plain, nil), nil
}

// Open расшифровывает то, что зашифровал Seal. Чужой ключ или испорченные
// байты — ошибка, а не мусор: GCM проверяет подлинность.
func (c *Cipher) Open(sealed []byte) ([]byte, error) {
	if c == nil {
		return nil, ErrNoKey
	}
	n := c.aead.NonceSize()
	if len(sealed) < n {
		return nil, errors.New("passport ciphertext is too short")
	}
	return c.aead.Open(nil, sealed[:n], sealed[n:], nil)
}

// Photos хранит фото документов зашифрованными файлами в своём каталоге —
// вне uploads, до которого дотягивается маршрут раздачи файлов.
type Photos struct {
	Dir    string
	Cipher *Cipher
}

// Save шифрует и пишет фото, возвращая имя файла. Недописанный файл удаляется.
func (p Photos) Save(name string, plain []byte) error {
	sealed, err := p.Cipher.Seal(plain)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(p.Dir, 0o700); err != nil {
		return err
	}
	path := filepath.Join(p.Dir, filepath.Base(name))
	if err := os.WriteFile(path, sealed, 0o600); err != nil {
		_ = os.Remove(path)
		return err
	}
	return nil
}

// Read читает и расшифровывает фото.
func (p Photos) Read(name string) ([]byte, error) {
	sealed, err := os.ReadFile(filepath.Join(p.Dir, filepath.Base(name)))
	if err != nil {
		return nil, err
	}
	return p.Cipher.Open(sealed)
}

// Remove удаляет фото; отсутствующее — не ошибка.
func (p Photos) Remove(name string) error {
	if name == "" {
		return nil
	}
	err := os.Remove(filepath.Join(p.Dir, filepath.Base(name)))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
