// Command vlessprobe watches the VLESS fallback channel from the outside.
//
// The mobile app falls back to a VLESS tunnel when it cannot reach the API
// directly. That path is, by design, the one nobody exercises on a good day —
// which is exactly why it rots unnoticed: a server gets renumbered, a
// certificate expires, a provider blocks a port, and the first person to find
// out is a user who was already having a bad day.
//
// This prober exercises the whole chain on a timer:
//
//  1. the control plane — fetch the encrypted endpoint list through the public
//     API with the same key and the same decryption the app uses;
//  2. reachability — TCP connect and, for plain TLS endpoints, a handshake and
//     the certificate's expiry;
//  3. the tunnel itself — start xray with the config, point a SOCKS client at
//     it and fetch the API health URL through it.
//
// Step 3 is the one that matters: an endpoint can pass steps 1 and 2 while
// forwarding nothing. It needs an xray binary; without one the prober keeps
// running with the cheaper checks and reports vless_probe_e2e_enabled 0.
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type probeOptions struct {
	XrayBin   string
	TargetURL string
	SocksPort int
	Timeout   time.Duration
}

type config struct {
	listURL     string
	appKey      string
	encKey      string
	listFile    string
	targetURL   string
	xrayBin     string
	socksPort   int
	interval    time.Duration
	timeout     time.Duration
	metricsAddr string
	e2eEnabled  bool
}

func main() {
	cfg := loadConfig()

	reg := prometheus.NewRegistry()
	registerMetrics(reg)

	if cfg.e2eEnabled {
		e2eEnabled.Set(1)
	} else {
		e2eEnabled.Set(0)
		log.Printf("[vlessprobe] xray binary %q not found: tunnels will not be probed end to end", cfg.xrayBin)
	}

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	srv := &http.Server{
		Addr:              cfg.metricsAddr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		log.Printf("[vlessprobe] serving /metrics on %s", cfg.metricsAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("[vlessprobe] metrics listener failed: %v", err)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Probed once at startup so a freshly deployed prober does not report
	// nothing at all for a whole interval.
	known := runPass(ctx, cfg, nil)

	ticker := time.NewTicker(cfg.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			log.Printf("[vlessprobe] shutting down")
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = srv.Shutdown(shutdownCtx)
			return
		case <-ticker.C:
			known = runPass(ctx, cfg, known)
		}
	}
}

// runPass probes every endpoint once and returns the set it saw, so the next
// pass can drop the series of endpoints that have left the list.
func runPass(ctx context.Context, cfg config, previous map[string]target) map[string]target {
	passStart := time.Now()
	defer func() {
		passDuration.Set(time.Since(passStart).Seconds())
		passTimestamp.SetToCurrentTime()
	}()

	list, err := loadList(cfg)
	if err != nil {
		listFetchUp.Set(0)
		log.Printf("[vlessprobe] could not load the endpoint list: %v", err)
		// The previous pass's per-endpoint gauges are left alone: a control
		// plane failure says nothing about whether the servers are up, and
		// zeroing them here would fire every endpoint alert at once.
		return previous
	}
	listFetchUp.Set(1)
	listConfigs.Set(float64(len(list.Configs)))

	seen := make(map[string]target, len(list.Configs))
	usable := 0
	port := cfg.socksPort

	for _, raw := range list.Configs {
		t, err := describe(raw)
		if err != nil {
			listParseErrors.Inc()
			log.Printf("[vlessprobe] skipping config %q: %v", t.Remarks, err)
			continue
		}
		seen[t.hostPort()] = t

		checkReachable(ctx, t, cfg.timeout)

		if cfg.e2eEnabled {
			// One port per endpoint within a pass, so a listener that lingers
			// after a killed xray cannot be mistaken for the next endpoint's.
			checkTunnel(ctx, raw, t, probeOptions{
				XrayBin:   cfg.xrayBin,
				TargetURL: cfg.targetURL,
				SocksPort: port,
				Timeout:   cfg.timeout,
			})
			port++
		}

		if isUp(t) {
			usable++
		}
	}

	endpointsTotal.Set(float64(len(seen)))
	endpointsUsable.Set(float64(usable))

	for key, t := range previous {
		if _, still := seen[key]; !still {
			forget(t)
		}
	}
	return seen
}

// isUp reports whether an endpoint counts towards the usable fleet. With
// end-to-end probing on, only a working tunnel counts; without it, the best
// available evidence is that the port answers.
func isUp(t target) bool {
	l := labelsFor(t)
	if e2eOn() {
		return gaugeValue(tunnelUp, l) == 1
	}
	return gaugeValue(endpointUp, l) == 1
}

func e2eOn() bool { return gaugeScalar(e2eEnabled) == 1 }

func loadList(cfg config) (*endpointList, error) {
	if cfg.listURL != "" {
		started := time.Now()
		list, err := fetchList(cfg.listURL, cfg.appKey, cfg.encKey, cfg.timeout)
		listFetchDuration.Set(time.Since(started).Seconds())
		return list, err
	}
	started := time.Now()
	list, err := readListFile(cfg.listFile)
	listFetchDuration.Set(time.Since(started).Seconds())
	return list, err
}

func loadConfig() config {
	cfg := config{
		listURL:     os.Getenv("PROBE_ENDPOINTS_URL"),
		appKey:      os.Getenv("PROBE_APP_KEY"),
		encKey:      os.Getenv("PROBE_ENC_KEY"),
		listFile:    getEnv("PROBE_ENDPOINTS_FILE", "/app/vless-endpoints.json"),
		targetURL:   getEnv("PROBE_TARGET_URL", "https://moya-usluga.ru/health"),
		xrayBin:     getEnv("PROBE_XRAY_BIN", "xray"),
		socksPort:   getEnvInt("PROBE_SOCKS_PORT", 11080),
		interval:    getEnvDuration("PROBE_INTERVAL", time.Minute),
		timeout:     getEnvDuration("PROBE_TIMEOUT", 20*time.Second),
		metricsAddr: getEnv("PROBE_METRICS_ADDR", ":9102"),
	}

	// Fetching through the public API is the preferred source: it probes the
	// delivery path the app depends on, not just the servers behind it. The
	// mounted file is the fallback when the keys are not configured.
	if cfg.listURL != "" && (cfg.appKey == "" || cfg.encKey == "") {
		log.Printf("[vlessprobe] PROBE_ENDPOINTS_URL is set without PROBE_APP_KEY/PROBE_ENC_KEY; falling back to %s", cfg.listFile)
		cfg.listURL = ""
	}

	if _, err := exec.LookPath(cfg.xrayBin); err == nil {
		cfg.e2eEnabled = true
	}
	return cfg
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v, err := strconv.Atoi(os.Getenv(key)); err == nil && v > 0 {
		return v
	}
	return fallback
}

func getEnvDuration(key string, fallback time.Duration) time.Duration {
	if v, err := time.ParseDuration(os.Getenv(key)); err == nil && v > 0 {
		return v
	}
	return fallback
}
