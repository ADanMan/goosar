package daemon

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"testing"

	"github.com/adanman/goosar/server/internal/cli"
)

func newAutoUpdateTestDaemon(t *testing.T, currentVersion string) (*Daemon, *atomic.Int32) {
	t.Helper()
	var restartCalls atomic.Int32
	d := &Daemon{
		cfg:    Config{CLIVersion: currentVersion, AutoUpdateEnabled: true},
		logger: slog.Default(),
		cancelFunc: func() {
			restartCalls.Add(1)
		},
	}
	d.runUpdateFn = func(string) (string, error) {
		t.Fatalf("runUpdateFn called unexpectedly")
		return "", nil
	}
	return d, &restartCalls
}

func withStubRelease(t *testing.T, release *cli.GitHubRelease, err error) {
	t.Helper()
	prev := fetchLatestRelease
	fetchLatestRelease = func() (*cli.GitHubRelease, error) { return release, err }
	t.Cleanup(func() { fetchLatestRelease = prev })
}

func TestTryAutoUpdate_SkipsWhenUpdating(t *testing.T) {
	d, restartCalls := newAutoUpdateTestDaemon(t, "v0.1.13")
	d.updating.Store(true)
	withStubRelease(t, &cli.GitHubRelease{TagName: "v0.1.14"}, nil)

	d.tryAutoUpdate(context.Background())

	if restartCalls.Load() != 0 {
		t.Fatalf("triggerRestart called while another update was in progress")
	}
}

func TestTryAutoUpdate_SkipsWhenTasksRunning(t *testing.T) {
	d, restartCalls := newAutoUpdateTestDaemon(t, "v0.1.13")
	d.activeTasks.Store(1)
	withStubRelease(t, &cli.GitHubRelease{TagName: "v0.1.14"}, nil)

	d.tryAutoUpdate(context.Background())

	if restartCalls.Load() != 0 {
		t.Fatalf("triggerRestart fired with active tasks; auto-update must defer")
	}
	if d.updating.Load() {
		t.Fatalf("updating flag should not have been claimed while tasks were running")
	}
}

func TestTryAutoUpdate_DefersWhenClaimInFlightAtBarrier(t *testing.T) {
	d, restartCalls := newAutoUpdateTestDaemon(t, "v0.1.13")
	withStubRelease(t, &cli.GitHubRelease{TagName: "v0.1.14"}, nil)

	d.claimsInFlight = 1

	d.tryAutoUpdate(context.Background())

	if restartCalls.Load() != 0 {
		t.Fatalf("triggerRestart fired despite a claim being in flight at the barrier")
	}
	if d.updating.Load() {
		t.Fatalf("updating flag must be released after a deferred upgrade so the next tick can retry")
	}
	if d.pauseClaims {
		t.Fatalf("pauseClaims must be cleared after a deferred upgrade")
	}
}

func TestTryAutoUpdate_HoldsBarrierAcrossRestart(t *testing.T) {
	d, restartCalls := newAutoUpdateTestDaemon(t, "v0.1.13")
	withStubRelease(t, &cli.GitHubRelease{TagName: "v0.1.14"}, nil)
	d.runUpdateFn = func(string) (string, error) { return "upgraded", nil }

	d.tryAutoUpdate(context.Background())

	if restartCalls.Load() != 1 {
		t.Fatalf("triggerRestart fired %d times, want 1", restartCalls.Load())
	}
	if !d.pauseClaims {
		t.Fatalf("pauseClaims must remain set across the restart kick; got cleared")
	}
}

func TestTryAutoUpdate_ReleasesBarrierOnUpgradeFailure(t *testing.T) {
	d, restartCalls := newAutoUpdateTestDaemon(t, "v0.1.13")
	withStubRelease(t, &cli.GitHubRelease{TagName: "v0.1.14"}, nil)
	d.runUpdateFn = func(string) (string, error) {
		return "brew network error", errors.New("brew upgrade failed")
	}

	d.tryAutoUpdate(context.Background())

	if restartCalls.Load() != 0 {
		t.Fatalf("triggerRestart fired despite upgrade failure")
	}
	if d.pauseClaims {
		t.Fatalf("pauseClaims must be cleared after a failed upgrade so pollers resume claiming")
	}
}

func TestTryEnterClaim_RespectsBarrier(t *testing.T) {
	d := &Daemon{}

	if !d.tryEnterClaim() {
		t.Fatal("tryEnterClaim should succeed when barrier is unset")
	}
	d.exitClaim()
	if d.claimsInFlight != 0 {
		t.Fatalf("claimsInFlight not balanced: %d", d.claimsInFlight)
	}

	if !d.trySetClaimBarrier() {
		t.Fatal("trySetClaimBarrier should succeed when idle")
	}
	if d.tryEnterClaim() {
		t.Fatal("tryEnterClaim must refuse while barrier is held")
	}
	d.releaseClaimBarrier()
	if !d.tryEnterClaim() {
		t.Fatal("tryEnterClaim should succeed after barrier release")
	}
	d.exitClaim()
}

func TestTryAutoUpdate_SkipsWhenFetchFails(t *testing.T) {
	d, restartCalls := newAutoUpdateTestDaemon(t, "v0.1.13")
	withStubRelease(t, nil, errors.New("network down"))

	d.tryAutoUpdate(context.Background())

	if restartCalls.Load() != 0 {
		t.Fatalf("triggerRestart fired despite fetch failure")
	}
}

func TestTryAutoUpdate_SkipsWhenNotNewer(t *testing.T) {
	d, restartCalls := newAutoUpdateTestDaemon(t, "v0.1.13")
	withStubRelease(t, &cli.GitHubRelease{TagName: "v0.1.13"}, nil)

	d.tryAutoUpdate(context.Background())

	if restartCalls.Load() != 0 {
		t.Fatalf("triggerRestart fired even though latest == current")
	}
}

func TestTryAutoUpdate_RunsUpgradeAndRestartsOnNewer(t *testing.T) {
	d, restartCalls := newAutoUpdateTestDaemon(t, "v0.1.13")
	withStubRelease(t, &cli.GitHubRelease{TagName: "v0.1.14"}, nil)

	var upgradedTo string
	d.runUpdateFn = func(target string) (string, error) {
		upgradedTo = target
		return "upgraded", nil
	}

	d.tryAutoUpdate(context.Background())

	if upgradedTo != "v0.1.14" {
		t.Fatalf("runUpdateFn called with %q, want v0.1.14", upgradedTo)
	}
	if restartCalls.Load() != 1 {
		t.Fatalf("triggerRestart fired %d times, want 1", restartCalls.Load())
	}
	if !d.updating.Load() {
		t.Fatalf("updating flag should remain set across the restart kick; got cleared")
	}
}

func TestTryAutoUpdate_DoesNotRestartOnUpgradeFailure(t *testing.T) {
	d, restartCalls := newAutoUpdateTestDaemon(t, "v0.1.13")
	withStubRelease(t, &cli.GitHubRelease{TagName: "v0.1.14"}, nil)

	d.runUpdateFn = func(string) (string, error) {
		return "brew: network error", errors.New("brew upgrade failed")
	}

	d.tryAutoUpdate(context.Background())

	if restartCalls.Load() != 0 {
		t.Fatalf("triggerRestart fired despite upgrade failure")
	}
	if d.updating.Load() {
		t.Fatalf("updating flag must be released after a failed upgrade so the next tick can retry")
	}
}

func TestAutoUpdateLoop_EarlyExits(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
	}{
		{
			name: "disabled by config",
			cfg:  Config{AutoUpdateEnabled: false, CLIVersion: "v0.1.13"},
		},
		{
			name: "managed by desktop",
			cfg:  Config{AutoUpdateEnabled: true, CLIVersion: "v0.1.13", LaunchedBy: "desktop"},
		},
		{
			name: "dev build",
			cfg:  Config{AutoUpdateEnabled: true, CLIVersion: "v0.1.13-235-gabcdef0"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &Daemon{cfg: tt.cfg, logger: slog.Default()}
			d.runUpdateFn = func(string) (string, error) {
				t.Fatalf("runUpdateFn called from an early-exit code path")
				return "", nil
			}
			withStubRelease(t, &cli.GitHubRelease{TagName: "v0.1.14"}, nil)

			done := make(chan struct{})
			go func() {
				d.autoUpdateLoop(context.Background())
				close(done)
			}()
			<-done
		})
	}
}

func withStubDeliveryProfile(t *testing.T, profile string, err error) *atomic.Int32 {
	t.Helper()
	var calls atomic.Int32
	prev := fetchServerDeliveryProfile
	fetchServerDeliveryProfile = func(context.Context, *Client) (string, error) {
		calls.Add(1)
		return profile, err
	}
	t.Cleanup(func() { fetchServerDeliveryProfile = prev })
	return &calls
}

func TestTryAutoUpdate_SkipsWhenServerRunsPerimeterProfile(t *testing.T) {
	d, restartCalls := newAutoUpdateTestDaemon(t, "v0.1.13")
	profileCalls := withStubDeliveryProfile(t, "perimeter", nil)
	var releaseFetches atomic.Int32
	prev := fetchLatestRelease
	fetchLatestRelease = func() (*cli.GitHubRelease, error) {
		releaseFetches.Add(1)
		return &cli.GitHubRelease{TagName: "v0.1.14"}, nil
	}
	t.Cleanup(func() { fetchLatestRelease = prev })

	d.tryAutoUpdate(context.Background())

	if profileCalls.Load() == 0 {
		t.Fatalf("server delivery profile was never consulted")
	}
	if releaseFetches.Load() != 0 {
		t.Fatalf("release fetch fired against a perimeter server; auto-update must skip before GitHub")
	}
	if restartCalls.Load() != 0 {
		t.Fatalf("triggerRestart fired against a perimeter server")
	}
	if d.updating.Load() {
		t.Fatalf("updating flag claimed against a perimeter server")
	}
}

func TestTryAutoUpdate_ProceedsWhenProfileUnknown_NoLocalPin(t *testing.T) {
	d, restartCalls := newAutoUpdateTestDaemon(t, "v0.1.13")
	withStubDeliveryProfile(t, "", errors.New("connection refused"))
	withStubRelease(t, &cli.GitHubRelease{TagName: "v0.1.14"}, nil)
	d.runUpdateFn = func(string) (string, error) { return "ok", nil }

	d.tryAutoUpdate(context.Background())

	if restartCalls.Load() != 1 {
		t.Fatalf("triggerRestart calls = %d, want 1 (profile-unknown must not block the upgrade on unpinned machines)", restartCalls.Load())
	}
}

func TestAutoUpdateLoop_PerimeterEnvPinSurvivesProbeFailure(t *testing.T) {
	stageFakeAgent(t)
	t.Setenv("GOOSAR_DELIVERY_PROFILE", "perimeter")
	t.Setenv("GOOSAR_DAEMON_AUTO_UPDATE", "true")

	cfg, err := LoadConfig(Overrides{
		ServerURL:      "http://perimeter.internal:8080",
		WorkspacesRoot: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.AutoUpdateEnabled {
		t.Fatalf("AutoUpdateEnabled = true with GOOSAR_DELIVERY_PROFILE=perimeter pinned; the pin must win over GOOSAR_DAEMON_AUTO_UPDATE=true")
	}

	d, restartCalls := newAutoUpdateTestDaemon(t, "v0.1.13")
	d.cfg.AutoUpdateEnabled = cfg.AutoUpdateEnabled
	profileCalls := withStubDeliveryProfile(t, "", errors.New("probe down"))
	var releaseFetches atomic.Int32
	prev := fetchLatestRelease
	fetchLatestRelease = func() (*cli.GitHubRelease, error) {
		releaseFetches.Add(1)
		return &cli.GitHubRelease{TagName: "v0.1.14"}, nil
	}
	t.Cleanup(func() { fetchLatestRelease = prev })

	d.autoUpdateLoop(context.Background())

	if profileCalls.Load() != 0 {
		t.Fatalf("profile probe fired %d times; a pinned perimeter host must not depend on the probe at all", profileCalls.Load())
	}
	if releaseFetches.Load() != 0 {
		t.Fatalf("release fetch fired on a pinned perimeter host")
	}
	if restartCalls.Load() != 0 {
		t.Fatalf("triggerRestart fired on a pinned perimeter host")
	}
}

func TestTryAutoUpdate_ProceedsOnCloudProfile(t *testing.T) {
	d, restartCalls := newAutoUpdateTestDaemon(t, "v0.1.13")
	withStubDeliveryProfile(t, "cloud", nil)
	withStubRelease(t, &cli.GitHubRelease{TagName: "v0.1.14"}, nil)
	d.runUpdateFn = func(string) (string, error) { return "ok", nil }

	d.tryAutoUpdate(context.Background())

	if restartCalls.Load() != 1 {
		t.Fatalf("triggerRestart calls = %d, want 1 on the cloud profile", restartCalls.Load())
	}
}
