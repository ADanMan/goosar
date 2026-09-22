package agent

import "log/slog"

func chooseRuntimeOInvocation(execName, lookedUp string, args []string, logger *slog.Logger) (string, []string) {
	rewrittenExec, rewrittenArgs, rewritten := platformRuntimeOInvocation(lookedUp, args, logger)
	if !rewritten {
		return execName, args
	}
	return rewrittenExec, rewrittenArgs
}
