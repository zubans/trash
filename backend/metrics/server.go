package metrics

import (
	"log"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Serve запускает слушатель метрик в фоне.
//
// Это намеренно отдельный сервер на отдельном порту. /metrics перечисляет
// устройство системы и не должен быть достижим из интернета: порт публикуется
// только в сеть compose, где его собирает Prometheus, а nginx его не
// проксирует. Вынос его с роутера API означает также, что сбор нельзя
// ограничить по частоте, отвергнуть по CORS или замедлить цепочкой
// прикладных middleware.
//
// Пустой addr выключает слушатель.
func Serve(addr string, ops OpsHandlers) {
	if addr == "" {
		return
	}

	mux := http.NewServeMux()
	ops.register(mux)
	mux.Handle("/metrics", promhttp.HandlerFor(Registry, promhttp.HandlerOpts{
		// Сломанный коллектор не должен утаскивать за собой эндпоинт сбора:
		// отдаём, что можно отдать, остальное пишем в лог.
		ErrorHandling: promhttp.ContinueOnError,
		ErrorLog:      log.Default(),
	}))
	// Живость самого сборщика, чтобы у healthcheck контейнера было что-то дешёвое,
	// что не трогает базу.
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		log.Printf("[metrics] serving /metrics on %s", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			// Не фатально: потеря наблюдаемости не должна утаскивать API.
			log.Printf("[metrics] listener stopped: %v", err)
		}
	}()
}
