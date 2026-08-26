package metrics

import (
	"log"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Serve starts the metrics listener in the background.
//
// It is a separate server on a separate port on purpose. /metrics enumerates
// the shape of the system and must not be reachable from the internet: the
// port is only published to the compose network, where Prometheus scrapes it,
// and nginx never proxies it. Keeping it off the API router also means a
// scrape cannot be rate limited, CORS-rejected or slowed down by the
// application middleware chain.
//
// Passing an empty addr disables the listener.
func Serve(addr string) {
	if addr == "" {
		return
	}

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(Registry, promhttp.HandlerOpts{
		// A broken collector must not take the scrape endpoint down with it:
		// report what can be reported and log the rest.
		ErrorHandling: promhttp.ContinueOnError,
		ErrorLog:      log.Default(),
	}))
	// Liveness for the scraper itself, so a container healthcheck has something
	// cheap to hit that does not touch the database.
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
			// Not fatal: losing observability must not take the API with it.
			log.Printf("[metrics] listener stopped: %v", err)
		}
	}()
}
