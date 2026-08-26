// Package metrics is the single place where the process describes itself to
// Prometheus.
//
// Everything lives on a private registry rather than the default one: the
// default registry is global mutable state that any imported library can write
// to, and /metrics is a public-ish surface we would rather keep predictable.
// The registry is exposed only on the dedicated listener started by Serve, so
// scraping never shares a port, a middleware chain or a rate limiter with the
// API.
//
// The package deliberately takes only primitives (strings, floats, bools) so
// that every layer — handlers, services, workers, repositories — can import it
// without dragging a dependency cycle behind.
package metrics

import (
	"database/sql"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
)

// Namespace prefixes every metric so the series are easy to select apart from
// the exporters that scrape the same Prometheus.
const Namespace = "healthlogin"

// Registry holds every collector this process exports.
var Registry = prometheus.NewRegistry()

// Buckets tuned for an API that answers in milliseconds but occasionally waits
// on DaData or on the database. The default client buckets stop at 10s, which
// hides exactly the tail we care about.
var latencyBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30}

var (
	// ---- HTTP -----------------------------------------------------------

	httpRequests = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: Namespace,
		Name:      "http_requests_total",
		Help:      "HTTP requests served, by route pattern and status class.",
	}, []string{"method", "route", "status"})

	httpDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: Namespace,
		Name:      "http_request_duration_seconds",
		Help:      "Time to serve an HTTP request, by route pattern.",
		Buckets:   latencyBuckets,
	}, []string{"method", "route"})

	httpInFlight = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: Namespace,
		Name:      "http_requests_in_flight",
		Help:      "HTTP requests currently being served.",
	})

	// ---- Authentication --------------------------------------------------

	authEvents = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: Namespace,
		Name:      "auth_events_total",
		Help:      "Authentication events by kind and outcome (login, register, refresh, password reset).",
	}, []string{"event", "result"})

	// ---- Business --------------------------------------------------------

	orderEvents = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: Namespace,
		Name:      "order_events_total",
		Help:      "Order lifecycle transitions.",
	}, []string{"event"})

	bidEvents = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: Namespace,
		Name:      "bid_events_total",
		Help:      "Auction events (bid placed, bid accepted).",
	}, []string{"event"})

	shiftEvents = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: Namespace,
		Name:      "shift_events_total",
		Help:      "Executor shift events (started, ended, ended early, auto-closed, geofence violation).",
	}, []string{"event"})

	matchingAssignments = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: Namespace,
		Name:      "matching_assignments_total",
		Help:      "Assignment attempts by the background matcher, by outcome.",
	}, []string{"result"})

	// Marketplace depth, refreshed by the matcher on every pass. Orders queued
	// with nobody on shift is the failure mode that looks fine in the request
	// counters: everything answers 200 and no work gets done.
	ordersSearching = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: Namespace,
		Name:      "orders_searching",
		Help:      "Orders currently waiting for an executor.",
	})

	shiftsActive = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: Namespace,
		Name:      "shifts_active",
		Help:      "Executors currently on shift.",
	})

	// ---- Money -----------------------------------------------------------
	//
	// Amounts are counted in rubles rather than kopecks so a dashboard can show
	// them without dividing, and they are counters because every ledger entry is
	// an absolute movement: the direction is already encoded in the type.

	ledgerEntries = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: Namespace,
		Name:      "ledger_entries_total",
		Help:      "Ledger entries written, by transaction type and system account.",
	}, []string{"type", "account"})

	ledgerAmount = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: Namespace,
		Name:      "ledger_amount_rubles_total",
		Help:      "Absolute amount moved through the ledger, in rubles.",
	}, []string{"type", "account"})

	ledgerErrors = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: Namespace,
		Name:      "ledger_errors_total",
		Help:      "Ledger operations that failed, by operation.",
	}, []string{"op"})

	// ---- Reconciliation --------------------------------------------------
	//
	// The nightly books check already logs loudly; these gauges make the same
	// facts alertable instead of grep-able.

	reconcileOK = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: Namespace,
		Name:      "reconcile_ok",
		Help:      "1 when the last reconciliation pass found no drift, 0 otherwise.",
	})

	reconcileLastRun = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: Namespace,
		Name:      "reconcile_last_run_timestamp_seconds",
		Help:      "Unix time of the last completed reconciliation pass.",
	})

	reconcileFindings = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: Namespace,
		Name:      "reconcile_findings",
		Help:      "Number of findings in the last reconciliation pass, by kind.",
	}, []string{"kind"})

	reconcileDrift = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: Namespace,
		Name:      "reconcile_drift_rubles",
		Help:      "Signed drift found by the last reconciliation pass, in rubles.",
	}, []string{"kind"})

	reconcileFailures = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: Namespace,
		Name:      "reconcile_failures_total",
		Help:      "Reconciliation passes that could not complete.",
	})

	// ---- Background workers ----------------------------------------------

	workerRuns = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: Namespace,
		Name:      "worker_runs_total",
		Help:      "Background worker passes, by worker and outcome.",
	}, []string{"worker", "result"})

	workerDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: Namespace,
		Name:      "worker_run_duration_seconds",
		Help:      "Duration of one background worker pass.",
		Buckets:   latencyBuckets,
	}, []string{"worker"})

	workerLastSuccess = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: Namespace,
		Name:      "worker_last_success_timestamp_seconds",
		Help:      "Unix time of the last successful pass of a background worker.",
	}, []string{"worker"})

	// ---- Outbound dependencies -------------------------------------------

	upstreamRequests = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: Namespace,
		Name:      "upstream_requests_total",
		Help:      "Calls to an external dependency (DaData, geocoder), by outcome.",
	}, []string{"upstream", "operation", "result"})

	upstreamDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: Namespace,
		Name:      "upstream_request_duration_seconds",
		Help:      "Latency of a call to an external dependency.",
		Buckets:   latencyBuckets,
	}, []string{"upstream", "operation"})

	mailSends = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: Namespace,
		Name:      "mail_sends_total",
		Help:      "System mail handed to the SMTP submission server, by kind and outcome.",
	}, []string{"kind", "result"})

	mailDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: Namespace,
		Name:      "mail_send_duration_seconds",
		Help:      "Time spent submitting one message to the mail server.",
		Buckets:   latencyBuckets,
	}, []string{"kind"})

	// ---- Chat -------------------------------------------------------------

	chatConnections = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: Namespace,
		Name:      "chat_websocket_connections",
		Help:      "Open chat WebSocket connections.",
	}, []string{"kind"})

	chatMessages = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: Namespace,
		Name:      "chat_messages_total",
		Help:      "Chat messages accepted by the server, by conversation kind.",
	}, []string{"kind"})

	// ---- VLESS fallback channel (control plane) --------------------------
	//
	// This is the server half of the proxy system: the app asks for the
	// endpoint list only when it is about to use, or is already using, the
	// tunnel. A rising rate here means clients are losing the direct path —
	// which is exactly the signal that the fallback channel is earning its
	// keep, or that something upstream is blocking us.

	appEndpointRequests = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: Namespace,
		Name:      "app_endpoints_requests_total",
		Help:      "Requests for the encrypted VLESS endpoint list, by outcome.",
	}, []string{"result"})

	appEndpointServed = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: Namespace,
		Name:      "app_endpoints_last_served_timestamp_seconds",
		Help:      "Unix time the endpoint list was last served successfully.",
	})

	appEndpointConfigs = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: Namespace,
		Name:      "app_endpoints_configs",
		Help:      "Number of VLESS configs in the endpoint list currently on disk.",
	})

	appEndpointFileAge = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: Namespace,
		Name:      "app_endpoints_file_mtime_seconds",
		Help:      "Modification time of the endpoint list file, as unix time.",
	})
)

func init() {
	Registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		httpRequests, httpDuration, httpInFlight,
		authEvents,
		orderEvents, bidEvents, shiftEvents, matchingAssignments,
		ordersSearching, shiftsActive,
		ledgerEntries, ledgerAmount, ledgerErrors,
		reconcileOK, reconcileLastRun, reconcileFindings, reconcileDrift, reconcileFailures,
		workerRuns, workerDuration, workerLastSuccess,
		upstreamRequests, upstreamDuration,
		mailSends, mailDuration,
		chatConnections, chatMessages,
		appEndpointRequests, appEndpointServed, appEndpointConfigs, appEndpointFileAge,
	)
}

// SetBuildInfo publishes the running version as a constant 1-valued gauge, the
// usual way to make "which build is this?" answerable from a dashboard.
func SetBuildInfo(version, commit string) {
	g := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: Namespace,
		Name:      "build_info",
		Help:      "Build metadata of the running backend; always 1.",
	}, []string{"version", "commit"})
	if err := Registry.Register(g); err != nil {
		return
	}
	g.WithLabelValues(version, commit).Set(1)
}

// RegisterDB exports the connection pool counters. A pool that is permanently
// at MaxOpenConnections with requests queueing behind it looks, from the
// outside, exactly like a slow database.
func RegisterDB(db *sql.DB, name string) {
	_ = Registry.Register(collectors.NewDBStatsCollector(db, name))
}

// ---- Recording helpers -------------------------------------------------
//
// Each helper is a no-op-safe call site: instrumentation must never be the
// reason a request fails, so nothing here returns an error.

// ObserveHTTP records one served request. route is the chi route pattern, not
// the raw path, so per-user URLs cannot explode the label cardinality.
func ObserveHTTP(method, route string, status int, d time.Duration) {
	code := strconv.Itoa(status)
	httpRequests.WithLabelValues(method, route, code).Inc()
	httpDuration.WithLabelValues(method, route).Observe(d.Seconds())
}

// IncInFlight and DecInFlight bracket a request.
func IncInFlight() { httpInFlight.Inc() }
func DecInFlight() { httpInFlight.Dec() }

// AuthEvent records an authentication attempt. result is "ok" or "denied".
func AuthEvent(event, result string) { authEvents.WithLabelValues(event, result).Inc() }

// OrderEvent records an order lifecycle transition.
func OrderEvent(event string) { orderEvents.WithLabelValues(event).Inc() }

// BidEvent records an auction event.
func BidEvent(event string) { bidEvents.WithLabelValues(event).Inc() }

// ShiftEvent records a shift event.
func ShiftEvent(event string) { shiftEvents.WithLabelValues(event).Inc() }

// MatchingAssignment records one assignment attempt by the matcher.
// result is "assigned" or "error".
func MatchingAssignment(result string) { matchingAssignments.WithLabelValues(result).Inc() }

// SetMarketplaceDepth publishes the queue and supply side as seen by the last
// matcher pass.
func SetMarketplaceDepth(searchingOrders, activeShifts int) {
	ordersSearching.Set(float64(searchingOrders))
	shiftsActive.Set(float64(activeShifts))
}

// LedgerEntry records one written ledger entry. rubles is the absolute amount.
func LedgerEntry(kind, account string, rubles float64) {
	ledgerEntries.WithLabelValues(kind, account).Inc()
	if rubles < 0 {
		rubles = -rubles
	}
	ledgerAmount.WithLabelValues(kind, account).Add(rubles)
}

// LedgerError records a failed ledger operation.
func LedgerError(op string) { ledgerErrors.WithLabelValues(op).Inc() }

// ReconcileReport publishes the outcome of a reconciliation pass.
func ReconcileReport(ok bool, discrepancies, holdAnomalies, unknownTypes int, booksDiffRubles, escrowDriftRubles float64) {
	if ok {
		reconcileOK.Set(1)
	} else {
		reconcileOK.Set(0)
	}
	reconcileFindings.WithLabelValues("balance_discrepancies").Set(float64(discrepancies))
	reconcileFindings.WithLabelValues("hold_anomalies").Set(float64(holdAnomalies))
	reconcileFindings.WithLabelValues("unknown_transaction_types").Set(float64(unknownTypes))
	reconcileDrift.WithLabelValues("books").Set(booksDiffRubles)
	reconcileDrift.WithLabelValues("escrow").Set(escrowDriftRubles)
	reconcileLastRun.SetToCurrentTime()
}

// ReconcileFailed records a pass that could not complete. The gauges keep their
// previous values on purpose: a failed pass is not evidence that the books are
// fine, and zeroing them would read as exactly that.
func ReconcileFailed() { reconcileFailures.Inc() }

// WorkerRun records one background pass. Call it deferred, with the error the
// pass returned.
func WorkerRun(worker string, d time.Duration, err error) {
	result := "ok"
	if err != nil {
		result = "error"
	} else {
		workerLastSuccess.WithLabelValues(worker).SetToCurrentTime()
	}
	workerRuns.WithLabelValues(worker, result).Inc()
	workerDuration.WithLabelValues(worker).Observe(d.Seconds())
}

// UpstreamCall records a call to an external dependency.
func UpstreamCall(upstream, operation string, d time.Duration, err error) {
	result := "ok"
	if err != nil {
		result = "error"
	}
	upstreamRequests.WithLabelValues(upstream, operation, result).Inc()
	upstreamDuration.WithLabelValues(upstream, operation).Observe(d.Seconds())
}

// UpstreamResult records a call whose outcome is finer grained than ok/error,
// such as a cache hit or a quota rejection.
func UpstreamResult(upstream, operation, result string, d time.Duration) {
	upstreamRequests.WithLabelValues(upstream, operation, result).Inc()
	upstreamDuration.WithLabelValues(upstream, operation).Observe(d.Seconds())
}

// MailSend records one submission attempt.
func MailSend(kind string, d time.Duration, err error) {
	result := "ok"
	if err != nil {
		result = "error"
	}
	mailSends.WithLabelValues(kind, result).Inc()
	mailDuration.WithLabelValues(kind).Observe(d.Seconds())
}

// ChatConnected and ChatDisconnected bracket a WebSocket session.
func ChatConnected(kind string)    { chatConnections.WithLabelValues(kind).Inc() }
func ChatDisconnected(kind string) { chatConnections.WithLabelValues(kind).Dec() }

// ChatMessage records an accepted message.
func ChatMessage(kind string) { chatMessages.WithLabelValues(kind).Inc() }

// AppEndpointsRequest records one request for the VLESS endpoint list.
func AppEndpointsRequest(result string) {
	appEndpointRequests.WithLabelValues(result).Inc()
	if result == "ok" {
		appEndpointServed.SetToCurrentTime()
	}
}

// AppEndpointsFile publishes what the served list currently contains. An empty
// list served successfully is the worst case for the mobile app — it looks
// healthy from the outside and leaves the client with nowhere to fall back to.
func AppEndpointsFile(configs int, mtime time.Time) {
	appEndpointConfigs.Set(float64(configs))
	if !mtime.IsZero() {
		appEndpointFileAge.Set(float64(mtime.Unix()))
	}
}

// TrackWorker runs one worker pass and records its duration and outcome. It
// returns the pass's own error untouched, so a loop reads the same as before:
//
//	if err := metrics.TrackWorker("auction", w.CheckExpiredAuctions); err != nil {
func TrackWorker(worker string, fn func() error) error {
	started := time.Now()
	err := fn()
	WorkerRun(worker, time.Since(started), err)
	return err
}
