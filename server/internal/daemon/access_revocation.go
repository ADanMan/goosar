package daemon

import (
	"errors"
	"net/http"
	"strings"
	"time"
)

const accessDeniedConfirmationsRequired = 3

const accessDeniedDefaultMinWindow = 10 * time.Minute

type accessDenialStrike struct {
	count   int
	firstAt time.Time
}

func (d *Daemon) recordWorkspaceAccessDenied(runtimeID string) bool {
	observedAt := time.Now()

	d.accessRevokedMu.Lock()
	defer d.accessRevokedMu.Unlock()

	if d.accessRevokedStrikes == nil {
		d.accessRevokedStrikes = make(map[string]accessDenialStrike)
	}
	run := d.accessRevokedStrikes[runtimeID]
	if run.count == 0 {
		run.firstAt = observedAt
	}
	run.count++
	d.accessRevokedStrikes[runtimeID] = run

	if run.count < accessDeniedConfirmationsRequired {
		return false
	}
	return observedAt.Sub(run.firstAt) >= d.accessDeniedMinWindow
}

func (d *Daemon) resetWorkspaceAccessDenialStrikes(runtimeID string) {
	d.accessRevokedMu.Lock()
	delete(d.accessRevokedStrikes, runtimeID)
	d.accessRevokedMu.Unlock()
}

func isWorkspaceAccessDeniedError(err error) bool {
	var reqErr *requestError
	if !errors.As(err, &reqErr) {
		return false
	}
	if reqErr.StatusCode != http.StatusNotFound && reqErr.StatusCode != http.StatusForbidden {
		return false
	}

	body := strings.ToLower(strings.TrimSpace(reqErr.Body))
	if !strings.HasPrefix(body, "{") || !strings.Contains(body, `"error"`) {
		return false
	}

	if reqErr.StatusCode == http.StatusForbidden {
		return true
	}
	if strings.Contains(body, "runtime not found") || strings.Contains(body, "task not found") {
		return false
	}
	return strings.Contains(body, "not found")
}

func (d *Daemon) handleWorkspaceAccessRevoked(runtimeID string) {
	rt := d.findRuntime(runtimeID)
	if rt == nil {
		return
	}
	if !d.claimAccessRevokedEpisode(runtimeID) {
		return
	}
	d.logger.Info("workspace access revoked", "runtime_id", runtimeID, "provider", rt.Provider)
}

func (d *Daemon) claimAccessRevokedEpisode(runtimeID string) bool {
	d.accessRevokedMu.Lock()
	defer d.accessRevokedMu.Unlock()

	if d.accessRevokedDisarmed == nil {
		d.accessRevokedDisarmed = make(map[string]bool)
	}
	if d.accessRevokedDisarmed[runtimeID] {
		return false
	}
	d.accessRevokedDisarmed[runtimeID] = true
	return true
}

func (d *Daemon) markRuntimeAccessRestored(runtimeID string) {
	d.accessRevokedMu.Lock()
	delete(d.accessRevokedDisarmed, runtimeID)
	delete(d.accessRevokedStrikes, runtimeID)
	d.accessRevokedMu.Unlock()
}
