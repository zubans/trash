package main

import (
	"github.com/prometheus/client_golang/prometheus"
)

// Labels kept to the handful of endpoints in the list: remarks names the server
// the way the app shows it, address is what actually gets dialled.
var probeLabels = []string{"remarks", "address", "protocol", "network", "security"}

var (
	// ---- Control plane: the list itself -----------------------------------

	listFetchUp = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "vless",
		Name:      "config_fetch_up",
		Help:      "1 when the endpoint list was fetched and decrypted successfully.",
	})

	listFetchDuration = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "vless",
		Name:      "config_fetch_duration_seconds",
		Help:      "Time taken by the last endpoint list fetch.",
	})

	listConfigs = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "vless",
		Name:      "config_count",
		Help:      "Configs in the endpoint list as last retrieved.",
	})

	listParseErrors = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "vless",
		Name:      "config_parse_errors_total",
		Help:      "Configs in the list that could not be understood.",
	})

	// ---- Layer 4/5: is the server there at all? ---------------------------

	endpointUp = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "vless",
		Name:      "endpoint_up",
		Help:      "1 when the endpoint accepted a TCP connection on the last pass.",
	}, probeLabels)

	endpointConnect = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "vless",
		Name:      "endpoint_tcp_connect_seconds",
		Help:      "Time to establish a TCP connection to the endpoint.",
	}, probeLabels)

	endpointHandshake = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "vless",
		Name:      "endpoint_tls_handshake_seconds",
		Help:      "Time to complete the TLS handshake with the endpoint.",
	}, probeLabels)

	// Reality endpoints are absent from this metric on purpose: their handshake
	// borrows a decoy site's certificate, so its expiry says nothing about us.
	endpointCertExpiry = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "vless",
		Name:      "endpoint_cert_expiry_timestamp_seconds",
		Help:      "Not-after time of the endpoint's TLS certificate, as unix time.",
	}, probeLabels)

	// ---- End to end: does the tunnel actually carry traffic? --------------

	tunnelUp = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "vless",
		Name:      "tunnel_up",
		Help:      "1 when the API health check succeeded through this endpoint's tunnel.",
	}, probeLabels)

	tunnelDuration = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "vless",
		Name:      "tunnel_request_duration_seconds",
		Help:      "Time to fetch the health URL through the tunnel, including xray startup.",
	}, probeLabels)

	tunnelStatus = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "vless",
		Name:      "tunnel_http_status",
		Help:      "HTTP status returned through the tunnel; 0 when nothing came back.",
	}, probeLabels)

	tunnelFailures = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "vless",
		Name:      "tunnel_failures_total",
		Help:      "Failed end-to-end tunnel probes, by the stage that failed.",
	}, []string{"remarks", "stage"})

	// ---- Fleet level -------------------------------------------------------
	//
	// The number worth alerting on. One dead endpoint is routine; the app walks
	// the list. Zero reachable endpoints means the fallback channel is gone.

	endpointsTotal = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "vless",
		Name:      "endpoints_total",
		Help:      "Endpoints in the list.",
	})

	endpointsUsable = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "vless",
		Name:      "endpoints_usable",
		Help:      "Endpoints that carried a successful end-to-end probe on the last pass.",
	})

	passDuration = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "vless",
		Name:      "probe_pass_duration_seconds",
		Help:      "Duration of the last full probe pass.",
	})

	passTimestamp = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "vless",
		Name:      "probe_last_pass_timestamp_seconds",
		Help:      "Unix time of the last completed probe pass.",
	})

	e2eEnabled = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "vless",
		Name:      "probe_e2e_enabled",
		Help:      "1 when an xray binary is available and tunnels are probed end to end.",
	})
)

func registerMetrics(reg *prometheus.Registry) {
	reg.MustRegister(
		listFetchUp, listFetchDuration, listConfigs, listParseErrors,
		endpointUp, endpointConnect, endpointHandshake, endpointCertExpiry,
		tunnelUp, tunnelDuration, tunnelStatus, tunnelFailures,
		endpointsTotal, endpointsUsable, passDuration, passTimestamp, e2eEnabled,
	)
}

// labelsFor keeps the label order in one place; getting it wrong silently
// mislabels every series.
func labelsFor(t target) []string {
	return []string{t.Remarks, t.hostPort(), t.Protocol, t.Network, t.Security}
}

// forget drops the series for an endpoint that has left the list. Without this
// a removed server keeps reporting its last value forever and alerts on a
// machine nobody runs any more.
func forget(t target) {
	l := labelsFor(t)
	endpointUp.DeleteLabelValues(l...)
	endpointConnect.DeleteLabelValues(l...)
	endpointHandshake.DeleteLabelValues(l...)
	endpointCertExpiry.DeleteLabelValues(l...)
	tunnelUp.DeleteLabelValues(l...)
	tunnelDuration.DeleteLabelValues(l...)
	tunnelStatus.DeleteLabelValues(l...)
}
