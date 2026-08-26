package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"golang.org/x/net/proxy"
)

// checkReachable does the cheap half of the probe: open a TCP connection and,
// where the endpoint speaks plain TLS, complete a handshake and read the
// certificate expiry.
//
// It answers a different question from the tunnel probe below. A server can
// accept connections while the proxy behind it is dead, and it can also be
// perfectly healthy while some middlebox on our side blocks the port — the two
// signals together say which.
func checkReachable(ctx context.Context, t target, timeout time.Duration) {
	l := labelsFor(t)

	dialer := &net.Dialer{Timeout: timeout}
	started := time.Now()
	conn, err := dialer.DialContext(ctx, "tcp", t.hostPort())
	if err != nil {
		endpointUp.WithLabelValues(l...).Set(0)
		return
	}
	endpointConnect.WithLabelValues(l...).Set(time.Since(started).Seconds())
	endpointUp.WithLabelValues(l...).Set(1)
	defer conn.Close()

	// Reality's handshake is with a decoy host, so neither its timing nor its
	// certificate belongs to us; measuring them would produce a confident,
	// meaningless number.
	if t.Security != "tls" {
		return
	}

	_ = conn.SetDeadline(time.Now().Add(timeout))
	tlsConn := tls.Client(conn, &tls.Config{ServerName: t.SNI})
	handshakeStart := time.Now()
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		// The TCP layer is up; only the TLS layer failed, and endpoint_up
		// already says so. The handshake gauge is left stale-free by clearing.
		endpointHandshake.DeleteLabelValues(l...)
		endpointCertExpiry.DeleteLabelValues(l...)
		return
	}
	endpointHandshake.WithLabelValues(l...).Set(time.Since(handshakeStart).Seconds())

	if certs := tlsConn.ConnectionState().PeerCertificates; len(certs) > 0 {
		endpointCertExpiry.WithLabelValues(l...).Set(float64(certs[0].NotAfter.Unix()))
	}
}

// checkTunnel is the probe that answers the question the fallback channel
// exists for: with this config, can a client actually reach the API?
//
// It runs the config through the same core the app runs — xray — with the
// inbound rewritten to a local SOCKS port, then fetches the health URL through
// that port. Anything short of this (a TCP connect, a TLS handshake) can pass
// on an endpoint whose proxy no longer forwards a single byte.
func checkTunnel(ctx context.Context, cfg json.RawMessage, t target, opts probeOptions) {
	l := labelsFor(t)
	fail := func(stage string) {
		tunnelUp.WithLabelValues(l...).Set(0)
		tunnelStatus.WithLabelValues(l...).Set(0)
		tunnelFailures.WithLabelValues(t.Remarks, stage).Inc()
	}

	started := time.Now()

	rewritten, err := withSocksInbound(cfg, opts.SocksPort)
	if err != nil {
		fail("config")
		return
	}

	dir, err := os.MkdirTemp("", "vlessprobe")
	if err != nil {
		fail("config")
		return
	}
	defer os.RemoveAll(dir)

	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, rewritten, 0o600); err != nil {
		fail("config")
		return
	}

	runCtx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, opts.XrayBin, "run", "-c", path)
	// xray's own log would drown the prober's; only failures matter here and
	// they show up as a failed fetch.
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		fail("xray_start")
		return
	}
	defer func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
	}()

	socksAddr := fmt.Sprintf("127.0.0.1:%d", opts.SocksPort)
	if err := waitForListener(runCtx, socksAddr, 5*time.Second); err != nil {
		fail("xray_start")
		return
	}

	status, err := fetchThroughSocks(runCtx, socksAddr, opts.TargetURL, opts.Timeout)
	tunnelDuration.WithLabelValues(l...).Set(time.Since(started).Seconds())
	if err != nil {
		fail("request")
		return
	}

	tunnelStatus.WithLabelValues(l...).Set(float64(status))
	if status >= 200 && status < 400 {
		tunnelUp.WithLabelValues(l...).Set(1)
		return
	}
	// The tunnel carried the request but the far end answered badly — that is a
	// backend problem surfacing through the proxy, not a dead proxy.
	tunnelUp.WithLabelValues(l...).Set(0)
	tunnelFailures.WithLabelValues(t.Remarks, "http_status").Inc()
}

// withSocksInbound replaces whatever inbounds a config carries with a single
// local SOCKS listener. The lists are written for the phone, where the inbound
// is supplied by the app; here the prober needs a port it can dial.
func withSocksInbound(cfg json.RawMessage, port int) ([]byte, error) {
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(cfg, &doc); err != nil {
		return nil, err
	}

	inbound := map[string]any{
		"tag":      "socks-in",
		"listen":   "127.0.0.1",
		"port":     port,
		"protocol": "socks",
		"settings": map[string]any{"auth": "noauth", "udp": false},
	}
	encoded, err := json.Marshal([]any{inbound})
	if err != nil {
		return nil, err
	}
	doc["inbounds"] = encoded
	// "remarks" is ours, not xray's; harmless, but dropped to keep the config
	// as close as possible to what the core expects.
	delete(doc, "remarks")
	return json.Marshal(doc)
}

// waitForListener polls until the SOCKS port accepts a connection. xray takes
// a moment to bind, and racing it produces a "connection refused" that looks
// exactly like a dead endpoint.
func waitForListener(ctx context.Context, addr string, limit time.Duration) error {
	deadline := time.Now().Add(limit)
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		conn, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
		if err == nil {
			conn.Close()
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("socks listener %s did not come up: %w", addr, err)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func fetchThroughSocks(ctx context.Context, socksAddr, url string, timeout time.Duration) (int, error) {
	dialer, err := proxy.SOCKS5("tcp", socksAddr, nil, proxy.Direct)
	if err != nil {
		return 0, err
	}
	contextDialer, ok := dialer.(proxy.ContextDialer)
	if !ok {
		return 0, fmt.Errorf("socks dialer does not support contexts")
	}

	client := &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			DialContext:         contextDialer.DialContext,
			TLSHandshakeTimeout: timeout,
			DisableKeepAlives:   true,
		},
		// Follow nothing: a redirect is an answer, and chasing it would measure
		// somewhere else.
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	return resp.StatusCode, nil
}
