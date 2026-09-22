package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/adanman/goosar/server/internal/middleware"
	agentpkg "github.com/adanman/goosar/server/pkg/agent"
)

const DefaultMinDaemonVersion = agentpkg.MinQuickCreateCLIVersion

func MinDaemonVersion() string {
	raw := strings.TrimSpace(os.Getenv("GOOSAR_MIN_DAEMON_VERSION"))
	if raw == "" {
		return DefaultMinDaemonVersion
	}
	switch strings.ToLower(raw) {
	case "none", "off", "0", "false":
		return ""
	}
	return raw
}

func RequireMinDaemonVersion(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		minimum := MinDaemonVersion()
		if minimum == "" {
			next.ServeHTTP(w, r)
			return
		}
		_, version, _ := middleware.ClientMetadataFromContext(r.Context())
		err := agentpkg.CheckMinCLIVersionFor(version, minimum)
		if err == nil {
			next.ServeHTTP(w, r)
			return
		}
		if !errors.Is(err, agentpkg.ErrCLIVersionTooOld) {

			slog.Debug("daemon version gate: version not usable, allowing",
				"version", version, "minimum", minimum, "path", r.URL.Path)
			next.ServeHTTP(w, r)
			return
		}

		slog.Warn("daemon version gate: refusing to hand tasks to an outdated daemon",
			"version", version, "minimum", minimum, "path", r.URL.Path)
		w.Header().Set("X-Min-Daemon-Version", minimum)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUpgradeRequired)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "daemon version " + version + " is older than the minimum " + minimum +
				" this server accepts; upgrade with `goosar update` (or reinstall the CLI) and restart the daemon",
			"code":               "daemon_too_old",
			"min_daemon_version": minimum,
			"daemon_version":     version,
		})
	})
}
