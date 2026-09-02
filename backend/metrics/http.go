package metrics

import (
	"bufio"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

// Middleware пишет по одному временному ряду на шаблон маршрута, а не на URL.
//
// Шаблон читается из chi *после* выполнения обработчика, потому что именно
// тогда маршрутизация разрешена: до этого каждый запрос выглядит как свой сырой
// путь, и /uploads/<uuid>/<filename> порождал бы новый набор лейблов на файл.
// Запросы, не совпавшие ни с чем, сворачиваются в один лейбл "not_found",
// чтобы сканер, обходящий случайные пути, не мог раздуть метрику без границ.
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		IncInFlight()
		defer DecInFlight()

		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		start := time.Now()
		next.ServeHTTP(rec, r)

		ObserveHTTP(r.Method, routePattern(r), rec.status, time.Since(start))
	})
}

func routePattern(r *http.Request) string {
	rctx := chi.RouteContext(r.Context())
	if rctx == nil {
		return "not_found"
	}
	pattern := rctx.RoutePattern()
	// chi оставляет замыкающий шаблон на смонтированных подроутерах, которые не совпали.
	if pattern == "" || pattern == "/*" {
		return "not_found"
	}
	// Легаси-монтирование в корне обслуживает те же обработчики без префикса /api.
	// Сворачивание их в один лейбл сохраняет по ряду на эндпоинт и всё равно
	// оставляет разделение видимым в nginx и в логе доступа.
	return strings.TrimSuffix(pattern, "/*")
}

// statusRecorder перехватывает код статуса и сохраняет необязательные
// интерфейсы, на которые опирается остальной стек: WebSocket чата нужен Hijack,
// а потеря Flush сломала бы любой потоковый ответ.
type statusRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (s *statusRecorder) WriteHeader(code int) {
	if !s.wroteHeader {
		s.status = code
		s.wroteHeader = true
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	s.wroteHeader = true
	return s.ResponseWriter.Write(b)
}

// Unwrap даёт http.ResponseController добраться до нижележащего writer'а, что
// покрывает установщики дедлайнов и всё прочее, что добавят позже.
func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// Hijack обязан быть настоящим методом, а не доступным через Unwrap:
// gorilla/websocket приводит переданный ему ResponseWriter напрямую к
// http.Hijacker, а обёртка логгера самого chi решает, что реализовывать, тоже
// приведением этой обёртки. Без него любой апгрейд WebSocket чата отвечал бы 500.
func (s *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := s.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("metrics: underlying ResponseWriter is not an http.Hijacker")
	}
	// Перехваченное соединение пишет собственную строку статуса, поэтому
	// записанное здесь теряет смысл; 101 — то, что возвращает апгрейд.
	s.status = http.StatusSwitchingProtocols
	s.wroteHeader = true
	return h.Hijack()
}

// Flush оставляет потоковые ответы потоковыми.
func (s *statusRecorder) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
