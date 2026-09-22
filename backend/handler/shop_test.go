package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// pngHeader — начало настоящего PNG: по нему сервер и узнаёт изображение.
var pngHeader = []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, 13, 'I', 'H', 'D', 'R'}

func uploadRequest(t *testing.T, name string, body []byte) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile("file", name)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write(body)
	_ = w.Close()
	req := httptest.NewRequest(http.MethodPost, "/admin/shop/images", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	return req
}

// Тип изображения определяется по содержимому: «картинка.jpg» с HTML внутри
// рядом с изображениями не ляжет. Настоящее изображение получает имя от
// сервера и отдаётся своим маршрутом как картинка.
func TestShopImageUploadAcceptsOnlyImages(t *testing.T) {
	t.Setenv("UPLOADS_DIR", t.TempDir())
	h := NewShopHandler(nil, nil)

	rec := httptest.NewRecorder()
	h.AdminUploadImage(rec, uploadRequest(t, "photo.jpg", []byte("<html><script>alert(1)</script></html>")))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("html disguised as jpg: status %d, want 400", rec.Code)
	}

	rec = httptest.NewRecorder()
	h.AdminUploadImage(rec, uploadRequest(t, "anything.bin", append(pngHeader, make([]byte, 64)...)))
	if rec.Code != http.StatusOK {
		t.Fatalf("png upload: status %d: %s", rec.Code, rec.Body.String())
	}
	var out struct{ URL string }
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if !strings.HasPrefix(out.URL, "/uploads/shop/") || !strings.HasSuffix(out.URL, ".png") {
		t.Fatalf("url = %q, want /uploads/shop/<uuid>.png", out.URL)
	}

	serve := func(name string) *httptest.ResponseRecorder {
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("name", name)
		req := httptest.NewRequest(http.MethodGet, "/uploads/shop/"+name, nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		rec := httptest.NewRecorder()
		h.ServeImage(rec, req)
		return rec
	}
	name := strings.TrimPrefix(out.URL, "/uploads/shop/")
	if rec := serve(name); rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/png" {
		t.Errorf("serve uploaded image: %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	// Имя, которого сервер не выдавал, не отдаётся — даже если файл есть.
	for _, bad := range []string{"../chat/secret.png", "photo.png", name + ".html"} {
		if rec := serve(bad); rec.Code != http.StatusNotFound {
			t.Errorf("serve %q: status %d, want 404", bad, rec.Code)
		}
	}
}
