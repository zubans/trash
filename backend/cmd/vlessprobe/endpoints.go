package main

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// endpointList mirrors the file the backend serves: a version and a list of
// complete Xray configs, each one a fallback the mobile app may end up using.
type endpointList struct {
	Version int               `json:"version"`
	Configs []json.RawMessage `json:"configs"`
}

// target is what one config says about the server it dials, pulled out for
// labelling and for the cheap TCP/TLS layer of the probe.
type target struct {
	Remarks  string
	Address  string
	Port     int
	Protocol string
	Network  string
	Security string
	// SNI is the name presented in the TLS handshake. Under Reality it is a
	// decoy host that has nothing to do with us, which is why the certificate
	// checks below skip Reality endpoints entirely.
	SNI string
}

func (t target) hostPort() string { return fmt.Sprintf("%s:%d", t.Address, t.Port) }

// fetchList retrieves the endpoint list the same way the Android client does:
// the shared key in X-App-Key, the response decrypted with AES-256-GCM. Probing
// through the real delivery path means a broken key, an unreadable file or a
// 403 shows up here instead of only in a user's crash report.
func fetchList(url, appKey, encKeyHex string, timeout time.Duration) (*endpointList, error) {
	encKey, err := hex.DecodeString(strings.TrimSpace(encKeyHex))
	if err != nil || len(encKey) != 32 {
		return nil, errors.New("encryption key must be 64 hex characters")
	}

	client := &http.Client{Timeout: timeout}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-App-Key", appKey)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("endpoint list returned status %d", resp.StatusCode)
	}

	// 1 MiB is far above any plausible list and keeps a misrouted response from
	// filling memory.
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}

	plaintext, err := decrypt(encKey, strings.TrimSpace(string(body)))
	if err != nil {
		return nil, err
	}
	return parseList(plaintext)
}

// readListFile is the offline path, used when the prober runs beside the
// backend and can read the same mounted file.
func readListFile(path string) (*endpointList, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parseList(raw)
}

func parseList(raw []byte) (*endpointList, error) {
	var list endpointList
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, fmt.Errorf("endpoint list is not valid JSON: %w", err)
	}
	return &list, nil
}

// decrypt reverses the handler's encrypt: base64( nonce(12) || ciphertext+tag ).
func decrypt(key []byte, sealed string) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(sealed)
	if err != nil {
		return nil, fmt.Errorf("payload is not base64: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(raw) < gcm.NonceSize() {
		return nil, errors.New("payload is shorter than the nonce")
	}
	return gcm.Open(nil, raw[:gcm.NonceSize()], raw[gcm.NonceSize():], nil)
}

// describe extracts the dial target from a config's "proxy" outbound. Anything
// it cannot read is reported rather than guessed: a config the prober does not
// understand is one the app may not understand either.
func describe(raw json.RawMessage) (target, error) {
	var cfg struct {
		Remarks   string `json:"remarks"`
		Outbounds []struct {
			Tag      string `json:"tag"`
			Protocol string `json:"protocol"`
			Settings struct {
				Address string `json:"address"`
				Port    int    `json:"port"`
				// Some generators nest the server under vnext instead of
				// address/port, which is the shape Xray itself documents.
				Vnext []struct {
					Address string `json:"address"`
					Port    int    `json:"port"`
				} `json:"vnext"`
			} `json:"settings"`
			StreamSettings struct {
				Network         string                      `json:"network"`
				Security        string                      `json:"security"`
				TLSSettings     struct{ ServerName string } `json:"tlsSettings"`
				RealitySettings struct{ ServerName string } `json:"realitySettings"`
			} `json:"streamSettings"`
		} `json:"outbounds"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return target{}, err
	}

	for _, out := range cfg.Outbounds {
		if out.Tag != "proxy" {
			continue
		}
		t := target{
			Remarks:  cfg.Remarks,
			Address:  out.Settings.Address,
			Port:     out.Settings.Port,
			Protocol: out.Protocol,
			Network:  out.StreamSettings.Network,
			Security: out.StreamSettings.Security,
		}
		if len(out.Settings.Vnext) > 0 {
			t.Address = out.Settings.Vnext[0].Address
			t.Port = out.Settings.Vnext[0].Port
		}
		t.SNI = out.StreamSettings.TLSSettings.ServerName
		if t.SNI == "" {
			t.SNI = out.StreamSettings.RealitySettings.ServerName
		}
		if t.SNI == "" {
			t.SNI = t.Address
		}
		if t.Remarks == "" {
			t.Remarks = t.hostPort()
		}
		if t.Address == "" || t.Port == 0 {
			return t, errors.New("proxy outbound has no server address")
		}
		return t, nil
	}
	return target{Remarks: cfg.Remarks}, errors.New("config has no outbound tagged \"proxy\"")
}
