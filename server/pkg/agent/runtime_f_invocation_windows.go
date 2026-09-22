//go:build windows

package agent

import "log/slog"

func platformRuntimeFInvocation(lookedUp string, args []string, logger *slog.Logger) (string, []string, bool) {
	return rewriteCmdToPS1("copilot", lookedUp, args, logger)
}
