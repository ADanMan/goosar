//go:build windows

package agent

import "log/slog"

func platformRuntimeOInvocation(lookedUp string, args []string, logger *slog.Logger) (string, []string, bool) {
	return rewriteCmdToPS1("pi", lookedUp, args, logger)
}
