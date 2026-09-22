package daemon

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"
)

const tooOldHintInterval = 10 * time.Minute

func daemonTooOldHint(err error) (minimum string, ok bool) {
	var reqErr *requestError
	if !errors.As(err, &reqErr) {
		return "", false
	}
	if reqErr.StatusCode != http.StatusUpgradeRequired {
		return "", false
	}

	var payload struct {
		MinDaemonVersion string `json:"min_daemon_version"`
	}
	if unmarshalErr := json.Unmarshal([]byte(reqErr.Body), &payload); unmarshalErr != nil {
		return "", true
	}
	return payload.MinDaemonVersion, true
}

func (d *Daemon) logTooOldHint(err error) bool {
	minimum, ok := daemonTooOldHint(err)
	if !ok {
		return false
	}

	if last := d.lastTooOldHint.Load(); last != 0 && time.Since(time.Unix(0, last)) < tooOldHintInterval {
		return true
	}

	d.lastTooOldHint.Store(time.Now().UnixNano())
	d.logger.Error("this daemon is too old for the server and will not be given tasks — upgrade the goosar CLI (`goosar update`) and restart the daemon",
		"daemon_version", d.cfg.CLIVersion,
		"min_daemon_version", minimum)
	return true
}
