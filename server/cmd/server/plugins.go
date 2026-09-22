package main

import (
	"context"
	"log/slog"

	"github.com/adanman/goosar/server/internal/integrations/plugin"
)

func startPlugins(ctx context.Context, plugins []plugin.Plugin, host plugin.Host) []plugin.Plugin {
	started := make([]plugin.Plugin, 0, len(plugins))
	for _, p := range plugins {
		if err := p.Start(ctx, host); err != nil {
			slog.Error("plugin failed to start", "plugin", p.Name(), "error", err)
			continue
		}
		slog.Info("plugin started", "plugin", p.Name())
		started = append(started, p)
	}
	return started
}

func stopPlugins(ctx context.Context, started []plugin.Plugin) {
	for i := len(started) - 1; i >= 0; i-- {
		p := started[i]
		if err := p.Stop(ctx); err != nil {
			slog.Warn("plugin failed to stop cleanly", "plugin", p.Name(), "error", err)
			continue
		}
		slog.Info("plugin stopped", "plugin", p.Name())
	}
}
