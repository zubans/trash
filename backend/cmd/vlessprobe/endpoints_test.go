package main

import (
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

const testEncKeyHex = "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"

func TestDescribeReadsXhttpTLSConfig(t *testing.T) {
	raw := json.RawMessage(`{
		"remarks": "Helsinki",
		"outbounds": [
			{"tag":"proxy","protocol":"vless",
			 "settings":{"address":"helsinki.example.com","port":443,"id":"u"},
			 "streamSettings":{"network":"xhttp","security":"tls",
			   "tlsSettings":{"serverName":"helsinki.example.com"}}},
			{"tag":"direct","protocol":"freedom","settings":{}}
		]}`)

	got, err := describe(raw)
	if err != nil {
		t.Fatalf("describe: %v", err)
	}
	if got.hostPort() != "helsinki.example.com:443" || got.Security != "tls" || got.Network != "xhttp" {
		t.Fatalf("unexpected target: %+v", got)
	}
}

// Under Reality the SNI is a decoy host, and the probe uses it to decide that
// the certificate is not ours to check.
func TestDescribeReadsRealitySNI(t *testing.T) {
	raw := json.RawMessage(`{
		"remarks": "Amsterdam",
		"outbounds": [
			{"tag":"proxy","protocol":"vless",
			 "settings":{"address":"203.0.113.10","port":443,"id":"u"},
			 "streamSettings":{"network":"tcp","security":"reality",
			   "realitySettings":{"serverName":"yahoo.com"}}}
		]}`)

	got, err := describe(raw)
	if err != nil {
		t.Fatalf("describe: %v", err)
	}
	if got.SNI != "yahoo.com" || got.Security != "reality" {
		t.Fatalf("unexpected target: %+v", got)
	}
}

func TestDescribeRejectsConfigWithoutProxyOutbound(t *testing.T) {
	raw := json.RawMessage(`{"remarks":"broken","outbounds":[{"tag":"direct","protocol":"freedom"}]}`)
	if _, err := describe(raw); err == nil {
		t.Fatal("a config with no proxy outbound must be reported, not silently probed")
	}
}

// The prober is only useful if it reads the list the way the app does, so the
// wire format is asserted end to end against the server's own encryption.
func TestFetchListDecryptsServerPayload(t *testing.T) {
	key, _ := hex.DecodeString(testEncKeyHex)
	plaintext := []byte(`{"version":1,"configs":[{"remarks":"one"},{"remarks":"two"}]}`)
	sealed := sealForTest(t, key, plaintext)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-App-Key") != "secret" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		_, _ = w.Write([]byte(sealed))
	}))
	defer srv.Close()

	list, err := fetchList(srv.URL, "secret", testEncKeyHex, 5*time.Second)
	if err != nil {
		t.Fatalf("fetchList: %v", err)
	}
	if len(list.Configs) != 2 {
		t.Fatalf("configs = %d, want 2", len(list.Configs))
	}

	if _, err := fetchList(srv.URL, "wrong", testEncKeyHex, 5*time.Second); err == nil {
		t.Fatal("a rejected app key must surface as an error, not an empty list")
	}
}

func TestWithSocksInboundReplacesInbounds(t *testing.T) {
	raw := json.RawMessage(`{"remarks":"x","inbounds":[{"port":1080}],"outbounds":[{"tag":"proxy"}]}`)

	out, err := withSocksInbound(raw, 11080)
	if err != nil {
		t.Fatalf("withSocksInbound: %v", err)
	}

	var doc struct {
		Remarks  string `json:"remarks"`
		Inbounds []struct {
			Port     int    `json:"port"`
			Protocol string `json:"protocol"`
			Listen   string `json:"listen"`
		} `json:"inbounds"`
		Outbounds []json.RawMessage `json:"outbounds"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("result is not valid JSON: %v", err)
	}
	if len(doc.Inbounds) != 1 || doc.Inbounds[0].Port != 11080 || doc.Inbounds[0].Protocol != "socks" {
		t.Fatalf("inbounds not rewritten: %+v", doc.Inbounds)
	}
	// Binding anywhere but loopback would expose an open proxy on the host.
	if doc.Inbounds[0].Listen != "127.0.0.1" {
		t.Fatalf("socks inbound listens on %q, want 127.0.0.1", doc.Inbounds[0].Listen)
	}
	if len(doc.Outbounds) != 1 {
		t.Fatal("outbounds must survive the rewrite untouched")
	}
	if doc.Remarks != "" {
		t.Fatal("remarks is ours, not xray's, and must not reach the core")
	}
}
