package service

import (
	"net/http"
	"strings"
)

// AllowedOrigins — браузерные источники, которым позволено звать API и
// открывать WebSocket. Собирается в composition root из CORS_ORIGIN: сервис
// окружение не читает, и один и тот же набор получают CORS и проверка Origin у
// сокета чата — им нельзя разойтись.
type AllowedOrigins struct {
	set map[string]bool
}

// NewAllowedOrigins собирает набор: встроенные источники локального клиента
// плюс переданные списки через запятую (пустые элементы пропускаются).
func NewAllowedOrigins(lists ...string) *AllowedOrigins {
	set := map[string]bool{
		"https://localhost":      true,
		"https://localhost:443":  true,
		"https://localhost:8443": true,
		"http://localhost":       true,
		"capacitor://localhost":  true,
		"ionic://localhost":      true,
	}
	for _, list := range lists {
		for _, o := range strings.Split(list, ",") {
			if o = strings.TrimSpace(o); o != "" {
				set[o] = true
			}
		}
	}
	return &AllowedOrigins{set: set}
}

// Allows сообщает, доверенный ли источник. nil-набор доверяет только
// встроенным.
func (o *AllowedOrigins) Allows(origin string) bool {
	if o == nil {
		return NewAllowedOrigins().set[origin]
	}
	return o.set[origin]
}

// AllowsRequest сообщает, доверенный ли заголовок Origin у запроса.
// Отсутствующий Origin принимается, потому что нативные мобильные клиенты его
// не шлют; браузеры шлют всегда — на это и опирается межсайтовая защита.
func (o *AllowedOrigins) AllowsRequest(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	return o.Allows(origin)
}
