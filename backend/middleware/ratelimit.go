package middleware

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// RateLimiter — ограничитель с фиксированным окном по адресу клиента. Он
// локален для процесса: с несколькими репликами каждая держит свою долю, чего
// хватает, чтобы остановить подстановку учётных данных и перебор кодов сброса с
// одного клиента, но при масштабировании его стоит заменить общим хранилищем (Redis).
type RateLimiter struct {
	limit  int
	window time.Duration

	mu      sync.Mutex
	buckets map[string]*bucket
}

type bucket struct {
	count       int
	windowStart time.Time
}

// NewRateLimiter создаёт ограничитель, разрешающий limit запросов за окно на ключ.
func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	rl := &RateLimiter{
		limit:   limit,
		window:  window,
		buckets: make(map[string]*bucket),
	}
	go rl.collect()
	return rl
}

// collect удаляет простаивающие корзины, чтобы карта не росла без границ.
func (rl *RateLimiter) collect() {
	ticker := time.NewTicker(10 * time.Minute)
	for range ticker.C {
		cutoff := time.Now().Add(-2 * rl.window)
		rl.mu.Lock()
		for key, b := range rl.buckets {
			if b.windowStart.Before(cutoff) {
				delete(rl.buckets, key)
			}
		}
		rl.mu.Unlock()
	}
}

// Allow сообщает, помещается ли ещё один запрос с ключа в текущее окно.
func (rl *RateLimiter) Allow(key string) bool {
	now := time.Now()

	rl.mu.Lock()
	defer rl.mu.Unlock()

	b, ok := rl.buckets[key]
	if !ok || now.Sub(b.windowStart) >= rl.window {
		rl.buckets[key] = &bucket{count: 1, windowStart: now}
		return true
	}
	if b.count >= rl.limit {
		return false
	}
	b.count++
	return true
}

// Middleware отвергает запросы сверх лимита с кодом 429.
func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !rl.Allow(clientIP(r)) {
			w.Header().Set("Retry-After", "60")
			http.Error(w, "Слишком много запросов, попробуйте позже", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// clientIP определяет адрес вызывающего, учитывая прокси-заголовок от nginx.
func clientIP(r *http.Request) string {
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		if first := strings.TrimSpace(strings.Split(forwarded, ",")[0]); first != "" {
			return first
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// MaxBodyBytes ограничивает размер тела запроса до того, как его прочтёт обработчик.
// Multipart-загрузки ставят собственный, больший предел.
func MaxBodyBytes(limit int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
				r.Body = http.MaxBytesReader(w, r.Body, limit)
			}
			next.ServeHTTP(w, r)
		})
	}
}

// SecurityHeaders выставляет заголовки ответа, защищающие ответы API в
// браузерном контексте. API возвращает только JSON, поэтому строгий CSP безопасен.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		if r.TLS != nil {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}
