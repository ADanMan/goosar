package agent

import "log/slog"

func chooseRuntimeGInvocation(execName, lookedUp string, args []string, logger *slog.Logger) (string, []string) {
	argv0, rewritten, ok := platformRuntimeGInvocation(lookedUp, args, logger)
	if !ok {
		return execName, args
	}
	return argv0, rewritten
}
