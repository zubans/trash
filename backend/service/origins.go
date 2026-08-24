package service

import (
	"net/http"
	"os"
	"strings"
	"sync"
)

var (
	originsOnce sync.Once
	origins     map[string]bool
)

// AllowedOrigins returns the set of browser origins permitted to call the API
// and to open a WebSocket. CORS_ORIGIN may hold a comma separated list.
func AllowedOrigins() map[string]bool {
	originsOnce.Do(func() {
		origins = map[string]bool{
			"https://localhost":      true,
			"https://localhost:443":  true,
			"https://localhost:8443": true,
			"http://localhost":       true,
			"capacitor://localhost":  true,
			"ionic://localhost":      true,
		}
		for _, o := range strings.Split(os.Getenv("CORS_ORIGIN"), ",") {
			if o = strings.TrimSpace(o); o != "" {
				origins[o] = true
			}
		}
	})
	return origins
}

// IsAllowedOrigin reports whether a request's Origin header is trusted. A
// missing Origin is accepted because native mobile clients do not send one;
// browsers always do, which is what the cross-site protection relies on.
func IsAllowedOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	return AllowedOrigins()[origin]
}
