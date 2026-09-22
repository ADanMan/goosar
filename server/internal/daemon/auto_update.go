package daemon

import (
	"context"
	"time"

	"github.com/adanman/goosar/server/internal/cli"
	"github.com/adanman/goosar/server/internal/deliveryprofile"
)

var (
	fetchLatestRelease = cli.FetchLatestRelease
	isReleaseVersion   = cli.IsReleaseVersion
	isNewerVersion     = cli.IsNewerVersion
)

const serverDeliveryProfileTimeout = 10 * time.Second

var fetchServerDeliveryProfile = func(ctx context.Context, c *Client) (string, error) {
	if c == nil {
		return "", nil
	}
	ctx, cancel := context.WithTimeout(ctx, serverDeliveryProfileTimeout)
	defer cancel()
	return c.ServerDeliveryProfile(ctx)
}

var autoUpdateInitialDelay = 2 * time.Minute

func (d *Daemon) autoUpdateLoop(ctx context.Context) {
	if !d.cfg.AutoUpdateEnabled {
		d.logger.Info("auto-update: disabled")
		return
	}
	if d.cfg.LaunchedBy == "desktop" {

		d.logger.Info("auto-update: skipped (managed by Desktop)")
		return
	}
	if !isReleaseVersion(d.cfg.CLIVersion) {

		d.logger.Info("auto-update: skipped (not a release build)", "version", d.cfg.CLIVersion)
		return
	}

	interval := d.cfg.AutoUpdateCheckInterval
	if interval <= 0 {
		interval = DefaultAutoUpdateCheckInterval
	}
	d.logger.Info("auto-update: started", "interval", interval, "current", d.cfg.CLIVersion)

	if err := sleepWithContext(ctx, autoUpdateInitialDelay); err != nil {
		return
	}
	d.tryAutoUpdate(ctx)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.tryAutoUpdate(ctx)
		}
	}
}

func (d *Daemon) tryAutoUpdate(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}

	if d.updating.Load() {
		d.logger.Debug("auto-update: skip — update already in progress")
		return
	}

	if running := d.activeTasks.Load(); running > 0 {
		d.logger.Debug("auto-update: skip — tasks running", "active", running)
		return
	}

	profile, err := fetchServerDeliveryProfile(ctx, d.client)
	if err != nil {
		d.logger.Debug("auto-update: server delivery profile unknown — proceeding with local default", "error", err)
	} else if deliveryprofile.IsPerimeterAdvertised(profile) {
		d.logger.Info("auto-update: skip — server runs the perimeter delivery profile; updates are operator-delivered")
		return
	}

	release, err := fetchLatestRelease()
	if err != nil {
		d.logger.Warn("auto-update: fetch latest release failed — will retry", "error", err)
		return
	}
	if release == nil || release.TagName == "" {
		return
	}
	if !isNewerVersion(release.TagName, d.cfg.CLIVersion) {
		return
	}

	if !d.updating.CompareAndSwap(false, true) {
		d.logger.Debug("auto-update: skip — update already in progress (raced)")
		return
	}
	released := false
	defer func() {
		if !released {
			d.updating.Store(false)
		}
	}()

	if !d.trySetClaimBarrier() {
		d.logger.Info("auto-update: deferring — task or claim in flight at barrier check")
		return
	}
	barrierReleased := false
	defer func() {
		if !barrierReleased {
			d.releaseClaimBarrier()
		}
	}()

	d.logger.Info("auto-update: newer release available, upgrading",
		"current", d.cfg.CLIVersion, "target", release.TagName)

	output, err := d.runUpdateFn(release.TagName)
	if err != nil {
		d.logger.Warn("auto-update: upgrade failed — will retry", "error", err, "output", output)
		return
	}

	d.logger.Info("auto-update: upgrade completed, restarting", "target", release.TagName, "output", output)

	released = true
	barrierReleased = true
	d.triggerRestart()
}
