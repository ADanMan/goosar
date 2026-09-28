package app

import (
	"net/http"
	"strings"

	"github.com/adanman/goosar/server2/internal/httpapi"
	"github.com/adanman/goosar/server2/internal/migrate"
)

// RegisterHealth регистрирует /health, /readyz, /healthz (алиас /readyz) и
// /health/realtime.
func RegisterHealth(router *httpapi.Router, d *Deps) {
	router.Handle(http.MethodGet, "/health", handleLiveness)
	router.Handle(http.MethodGet, "/readyz", d.handleReadiness)
	router.Handle(http.MethodGet, "/healthz", d.handleReadiness)
	router.Handle(http.MethodGet, "/health/realtime", d.handleRealtimeMetrics)
}

func handleLiveness(w http.ResponseWriter, r *http.Request) {
	httpapi.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (d *Deps) handleReadiness(w http.ResponseWriter, r *http.Request) {
	dbStatus := "ok"
	overall := "ok"
	if err := d.Store.Ping(r.Context()); err != nil {
		dbStatus = "error"
		overall = "not_ready"
	}

	migrationsStatus := "ok"
	files, err := migrate.Load(d.Config.MigrationsDir)
	if err != nil {
		migrationsStatus = "unknown"
	} else if dbStatus == "ok" {
		applied, err := migrate.Applied(r.Context(), d.Store.Pool)
		if err != nil {
			migrationsStatus = "error"
		} else {
			for _, f := range files {
				if !applied[f.Version] {
					migrationsStatus = "out_of_date"
					break
				}
			}
		}
	} else {
		migrationsStatus = "unknown"
	}
	if migrationsStatus != "ok" {
		overall = "not_ready"
	}

	status := http.StatusOK
	if overall != "ok" {
		status = http.StatusServiceUnavailable
	}
	httpapi.WriteJSON(w, status, map[string]any{
		"status": overall,
		"checks": map[string]string{
			"db":         dbStatus,
			"migrations": migrationsStatus,
		},
	})
}

// handleRealtimeMetrics — см. описание в контракте: без REALTIME_METRICS_TOKEN
// доступ только с loopback (маршрут в остальном "скрыт", отдаёт голый 404);
// с токеном — обязателен Bearer.
func (d *Deps) handleRealtimeMetrics(w http.ResponseWriter, r *http.Request) {
	if d.Config.RealtimeMetricsToken != "" {
		auth := r.Header.Get("Authorization")
		token, ok := strings.CutPrefix(auth, "Bearer ")
		if !ok || token != d.Config.RealtimeMetricsToken {
			httpapi.Unauthorized(w, "bearer token required")
			return
		}
	} else if !isLoopback(r) {
		http.NotFound(w, r)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, d.Hub.Stats())
}

func isLoopback(r *http.Request) bool {
	if r.Header.Get("X-Forwarded-For") != "" || r.Header.Get("Forwarded") != "" {
		return false
	}
	host := httpapi.ClientIP(r)
	return host == "127.0.0.1" || host == "::1" || host == "localhost"
}
