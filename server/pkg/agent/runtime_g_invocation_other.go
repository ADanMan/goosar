//go:build !windows

package agent

import "log/slog"

func platformRuntimeGInvocation(string, []string, *slog.Logger) (string, []string, bool) {
	return "", nil, false
}
