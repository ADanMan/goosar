package main

import (
	"crypto/subtle"
	"encoding/json"
	"net"
	"net/http"
	"strings"

	"github.com/adanman/goosar/server/internal/daemonws"
	"github.com/adanman/goosar/server/internal/realtime"
)

func realtimeMetricsHandler(token string) http.HandlerFunc {
	token = strings.TrimSpace(token)
	return func(w http.ResponseWriter, r *http.Request) {
		if token != "" {
			if !hasBearerToken(r, token) {
				w.Header().Set("WWW-Authenticate", `Bearer realm="metrics"`)
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		} else if !isDirectLoopbackRequest(r) {

			http.NotFound(w, r)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		snapshot := realtime.M.Snapshot()
		snapshot["daemonws"] = daemonws.M.Snapshot()
		_ = json.NewEncoder(w).Encode(snapshot)
	}
}

func hasBearerToken(r *http.Request, want string) bool {
	auth := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if len(auth) <= len(prefix) || !strings.EqualFold(auth[:len(prefix)], prefix) {
		return false
	}
	got := strings.TrimSpace(auth[len(prefix):])
	if got == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

func isDirectLoopbackRequest(r *http.Request) bool {

	if hasForwardingHeader(r) {
		return false
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if host == "" {
		return false
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback()
}

func hasForwardingHeader(r *http.Request) bool {
	for _, h := range []string{
		"X-Forwarded-For",
		"X-Forwarded-Host",
		"X-Forwarded-Proto",
		"X-Real-Ip",
		"Forwarded",
	} {
		if strings.TrimSpace(r.Header.Get(h)) != "" {
			return true
		}
	}
	return false
}
