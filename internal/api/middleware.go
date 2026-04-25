package api

import (
	"net/http"
	"net/url"
	"os"
	"strings"
)

// LocalCORSMiddleware allows the standalone local UI dev server to call the API.
// It intentionally only accepts loopback browser origins by default.
func LocalCORSMiddleware(next http.Handler) http.Handler {
	allowedFromEnv := parseOriginList(os.Getenv("AGENT_CORS_ALLOWED_ORIGINS"))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && isAllowedLocalOrigin(origin, allowedFromEnv) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PATCH,DELETE,OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type,Accept")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func parseOriginList(raw string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, item := range strings.Split(raw, ",") {
		item = strings.TrimSpace(item)
		if item != "" {
			out[item] = struct{}{}
		}
	}
	return out
}

func isAllowedLocalOrigin(origin string, explicit map[string]struct{}) bool {
	if _, ok := explicit[origin]; ok {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	host := u.Hostname()
	return host == "127.0.0.1" || host == "localhost" || host == "::1"
}
