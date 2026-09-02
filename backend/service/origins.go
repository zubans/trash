package service

import (
	"net/http"
	"os"
	"strings"
	"sync"
)

var (
	originsOnce sync.Once
	origins     map[string]bool
)

// AllowedOrigins возвращает набор браузерных источников, которым позволено
// звать API и открывать WebSocket. CORS_ORIGIN может содержать список через запятую.
func AllowedOrigins() map[string]bool {
	originsOnce.Do(func() {
		origins = map[string]bool{
			"https://localhost":      true,
			"https://localhost:443":  true,
			"https://localhost:8443": true,
			"http://localhost":       true,
			"capacitor://localhost":  true,
			"ionic://localhost":      true,
		}
		for _, o := range strings.Split(os.Getenv("CORS_ORIGIN"), ",") {
			if o = strings.TrimSpace(o); o != "" {
				origins[o] = true
			}
		}
	})
	return origins
}

// IsAllowedOrigin сообщает, доверенный ли заголовок Origin у запроса.
// Отсутствующий Origin принимается, потому что нативные мобильные клиенты его
// не шлют; браузеры шлют всегда — на это и опирается межсайтовая защита.
func IsAllowedOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	return AllowedOrigins()[origin]
}
