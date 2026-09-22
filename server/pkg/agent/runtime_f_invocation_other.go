//go:build !windows

package agent

import "log/slog"

func platformRuntimeFInvocation(string, []string, *slog.Logger) (string, []string, bool) {
	return "", nil, false
}
