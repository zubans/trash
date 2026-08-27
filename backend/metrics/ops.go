package metrics

import (
	"crypto/subtle"
	"encoding/json"
	"log"
	"net/http"
)

// OpsHandlers are the privileged actions the metrics listener will expose when
// they are supplied. They live here rather than on the API router for the same
// reason /metrics does: this listener is reachable only from the compose
// network, nginx never proxies it and its port is not published, so nothing on
// it is one misconfigured route away from the internet.
type OpsHandlers struct {
	// Secret gates every ops route with a constant-time comparison. Empty
	// disables the routes entirely — a shared secret that was never configured
	// must not become an open door.
	Secret string

	// Reconcile runs the books check on demand and returns a JSON-serialisable
	// summary. It is expected to publish the reconciliation gauges itself.
	Reconcile func() (any, error)
}

func (o OpsHandlers) enabled() bool { return o.Secret != "" && o.Reconcile != nil }

// authorize compares the shared secret in constant time. The header is checked
// before anything else runs, and the answer never says which half was wrong.
func (o OpsHandlers) authorize(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return false
	}
	provided := r.Header.Get("X-Ops-Key")
	if subtle.ConstantTimeCompare([]byte(provided), []byte(o.Secret)) != 1 {
		http.Error(w, "forbidden", http.StatusForbidden)
		return false
	}
	return true
}

func (o OpsHandlers) register(mux *http.ServeMux) {
	if !o.enabled() {
		log.Printf("[metrics] ops routes disabled: OPS_KEY is not set")
		return
	}

	mux.HandleFunc("/internal/reconcile", func(w http.ResponseWriter, r *http.Request) {
		if !o.authorize(w, r) {
			return
		}
		log.Printf("[ops] reconciliation requested")
		report, err := o.Reconcile()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(report)
	})

	log.Printf("[metrics] ops routes enabled at /internal/*")
}
