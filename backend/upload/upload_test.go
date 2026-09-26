package upload

import (
	"bytes"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// pngHeader — начало настоящего PNG: по нему сервер и узнаёт изображение.
var pngHeader = []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, 13, 'I', 'H', 'D', 'R'}

func request(t *testing.T, field, name string, body []byte) (*httptest.ResponseRecorder, *http.Request) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile(field, name)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write(body)
	_ = w.WriteField("text", "подпись")
	_ = w.Close()
	req := httptest.NewRequest(http.MethodPost, "/upload", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	return httptest.NewRecorder(), req
}

func imageOpts(dir string) Options {
	return Options{Field: "file", MaxBytes: 1 << 20, Dir: dir,
		Accept: ByContent(map[string]string{"image/png": ".png", "image/jpeg": ".jpg"}, "только изображения")}
}

// Тип берётся из содержимого, имя даёт сервер, а поля формы остаются
// доступны вызывающему.
func TestSaveNamesByContent(t *testing.T) {
	dir := t.TempDir()
	rec, req := request(t, "file", "anything.bin", append(pngHeader, make([]byte, 64)...))
	saved, err := Save(rec, req, imageOpts(dir))
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if !strings.HasSuffix(saved.Name, ".png") || filepath.Dir(saved.Path) != dir {
		t.Fatalf("saved as %q in %q", saved.Name, saved.Path)
	}
	if saved.Size != int64(len(pngHeader)+64) || saved.ClientName != "anything.bin" {
		t.Fatalf("size %d, client name %q", saved.Size, saved.ClientName)
	}
	if info, err := os.Stat(saved.Path); err != nil || info.Size() != saved.Size {
		t.Fatalf("file on disk: %v", err)
	}
	if req.FormValue("text") != "подпись" {
		t.Fatal("form fields are not readable after Save")
	}
}

// HTML под именем картинки отвергается по содержимому, и на диске ничего не остаётся.
func TestSaveRejectsWrongContent(t *testing.T) {
	dir := t.TempDir()
	rec, req := request(t, "file", "photo.png", []byte("<html><script>alert(1)</script></html>"))
	if _, err := Save(rec, req, imageOpts(dir)); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("html as png: %v, want ErrUnsupported", err)
	}
	assertEmpty(t, dir)

	// По расширению: картинка обязана быть картинкой, документ — не HTML.
	byExt := ByExtension(map[string][]string{".jpg": {"image/"}, ".txt": nil})
	rec, req = request(t, "file", "photo.jpg", []byte("plain text, not a picture"))
	if _, err := Save(rec, req, Options{Field: "file", Dir: dir, Accept: byExt}); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("text as jpg: %v", err)
	}
	rec, req = request(t, "file", "notes.txt", []byte("<html><body>x</body></html>"))
	if _, err := Save(rec, req, Options{Field: "file", Dir: dir, Accept: byExt}); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("html as txt: %v", err)
	}
	rec, req = request(t, "file", "notes.exe", []byte("MZ"))
	if _, err := Save(rec, req, Options{Field: "file", Dir: dir, Accept: byExt}); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("exe: %v", err)
	}
	rec, req = request(t, "file", "notes.txt", []byte("just notes"))
	if saved, err := Save(rec, req, Options{Field: "file", Dir: dir, Accept: byExt}); err != nil || !strings.HasSuffix(saved.Name, ".txt") {
		t.Fatalf("txt: %v %+v", err, saved)
	}
}

// Тело больше потолка — ErrTooLarge, а не половина файла на диске.
func TestSaveLimitsSize(t *testing.T) {
	dir := t.TempDir()
	rec, req := request(t, "file", "big.png", append(pngHeader, make([]byte, 4096)...))
	opts := imageOpts(dir)
	opts.MaxBytes = 1024
	if _, err := Save(rec, req, opts); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("oversized: %v, want ErrTooLarge", err)
	}
	assertEmpty(t, dir)
}

// Нет поля с файлом — ErrNoFile.
func TestSaveRequiresFile(t *testing.T) {
	dir := t.TempDir()
	rec, req := request(t, "other", "x.png", pngHeader)
	if _, err := Save(rec, req, imageOpts(dir)); !errors.Is(err, ErrNoFile) {
		t.Fatalf("missing field: %v", err)
	}
	assertEmpty(t, dir)
}

// Сбой записи не оставляет частичного файла: каталог назначения — файл, и
// временный файл в нём создать нельзя.
func TestSaveCleansUpOnFailure(t *testing.T) {
	root := t.TempDir()
	blocked := filepath.Join(root, "dir")
	if err := os.WriteFile(blocked, []byte("not a dir"), 0o644); err != nil {
		t.Fatal(err)
	}
	rec, req := request(t, "file", "a.png", pngHeader)
	if _, err := Save(rec, req, imageOpts(blocked)); err == nil {
		t.Fatal("expected an error writing into a file used as a directory")
	}
	entries, _ := os.ReadDir(root)
	if len(entries) != 1 {
		t.Fatalf("partial files left behind: %v", entries)
	}
}

func assertEmpty(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("directory not empty after a rejected upload: %v", entries)
	}
}
