package passport_test

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"healthlogin/backend/passport"
)

func newKey(t *testing.T) string {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(key)
}

func TestSealOpenAndForeignKey(t *testing.T) {
	c, err := passport.NewCipher(newKey(t), 1)
	if err != nil {
		t.Fatal(err)
	}
	plain := []byte(`{"series":"4510","number":"123456"}`)
	sealed, err := c.Seal(plain)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, []byte("123456")) {
		t.Fatal("the number is readable in the ciphertext")
	}
	got, err := c.Open(sealed)
	if err != nil || !bytes.Equal(got, plain) {
		t.Fatalf("open: %q, %v", got, err)
	}
	other, _ := passport.NewCipher(newKey(t), 1)
	if _, err := other.Open(sealed); err == nil {
		t.Fatal("a foreign key opened the passport")
	}
}

func TestNoKeyRefusesInsteadOfStoringPlainText(t *testing.T) {
	c, err := passport.NewCipher("", 1)
	if err != nil || c != nil {
		t.Fatalf("empty key: %v, %v", c, err)
	}
	if _, err := c.Seal([]byte("x")); !errors.Is(err, passport.ErrNoKey) {
		t.Fatalf("seal without a key: %v", err)
	}
	if _, err := passport.NewCipher(base64.StdEncoding.EncodeToString([]byte("short")), 1); err == nil {
		t.Fatal("a short key was accepted")
	}
}

func TestPhotosAreEncryptedOnDisk(t *testing.T) {
	c, _ := passport.NewCipher(newKey(t), 1)
	photos := passport.Photos{Dir: t.TempDir(), Cipher: c}
	plain := []byte("\xff\xd8\xff photo of the passport 4510 123456")
	if err := photos.Save("u.jpg", plain); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(photos.Dir, "u.jpg"))
	if bytes.Contains(raw, []byte("123456")) {
		t.Fatal("the photo is stored in the clear")
	}
	got, err := photos.Read("u.jpg")
	if err != nil || !bytes.Equal(got, plain) {
		t.Fatalf("read: %v", err)
	}
	if err := photos.Remove("u.jpg"); err != nil {
		t.Fatal(err)
	}
	if err := photos.Remove("u.jpg"); err != nil {
		t.Fatalf("removing a missing photo: %v", err)
	}
}
