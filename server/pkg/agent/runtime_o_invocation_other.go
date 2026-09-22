//go:build !windows

package agent

import "log/slog"

func platformRuntimeOInvocation(string, []string, *slog.Logger) (execArgv0 string, fullArgs []string, rewritten bool) {
	return "", nil, false
}
