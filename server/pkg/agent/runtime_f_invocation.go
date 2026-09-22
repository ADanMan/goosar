package agent

import "log/slog"

func chooseRuntimeFInvocation(execName, lookedUp string, args []string, logger *slog.Logger) (string, []string) {
	argv0, rewritten, ok := platformRuntimeFInvocation(lookedUp, args, logger)
	if !ok {
		return execName, args
	}
	return argv0, rewritten
}
