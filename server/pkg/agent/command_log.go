package agent

import (
	"log/slog"
	"os/exec"
	"strings"
)

const redactedAgentCommandArg = "<redacted>"

const maxLoggedAgentCommandFlagLen = 64

type trustedAgentCommandPositional struct {
	index int
	value string
}

func trustAgentCommandPositional(index int, value string) trustedAgentCommandPositional {
	return trustedAgentCommandPositional{index: index, value: value}
}

type agentCommandLogArgs struct {
	invocationArgs     []string
	trustedPositionals []trustedAgentCommandPositional
}

func newAgentCommandLogArgs(invocationArgs []string, trustedPositionals ...trustedAgentCommandPositional) agentCommandLogArgs {
	copied := make([]trustedAgentCommandPositional, len(trustedPositionals))
	copy(copied, trustedPositionals)
	return agentCommandLogArgs{
		invocationArgs:     invocationArgs,
		trustedPositionals: copied,
	}
}

func (c Config) logAgentCommand(cmd *exec.Cmd, source agentCommandLogArgs) {
	c.logAgentCommandFields(cmd, source, 0, false)
}

func (c Config) logAgentCommandWithPrompt(cmd *exec.Cmd, source agentCommandLogArgs, promptBytes int) {
	c.logAgentCommandFields(cmd, source, promptBytes, true)
}

func (c Config) logAgentCommandFields(cmd *exec.Cmd, source agentCommandLogArgs, promptBytes int, includePromptBytes bool) {
	if cmd == nil {
		return
	}
	logger := c.Logger
	if logger == nil {
		logger = slog.Default()
	}

	var args []string
	if len(cmd.Args) > 1 {
		args = cmd.Args[1:]
	} else {
		args = []string{}
	}
	trusted := trustedAgentCommandPositionals(args, source)

	fields := []any{
		"provider", c.provider,
		"exec", cmd.Path,
		"args", redactAgentCommandArgs(args, trusted),
		"arg_count", len(args),
	}
	if includePromptBytes {
		fields = append(fields, "prompt_bytes", promptBytes)
	}
	logger.Info("agent command", fields...)
}

func trustedAgentCommandPositionals(finalArgs []string, source agentCommandLogArgs) map[int]struct{} {
	offset := len(finalArgs) - len(source.invocationArgs)
	if offset < 0 {
		return nil
	}
	for i, arg := range source.invocationArgs {
		if finalArgs[offset+i] != arg {
			return nil
		}
	}

	trusted := make(map[int]struct{}, len(source.trustedPositionals))
	for _, positional := range source.trustedPositionals {
		if positional.index < 0 || positional.index >= len(source.invocationArgs) {
			continue
		}
		if source.invocationArgs[positional.index] != positional.value {
			continue
		}
		trusted[offset+positional.index] = struct{}{}
	}
	return trusted
}

func redactAgentCommandArgs(args []string, trustedPositionals map[int]struct{}) []string {
	redacted := make([]string, len(args))
	for i, arg := range args {
		switch {
		case isTrustedAgentCommandArg(i, trustedPositionals):
			redacted[i] = arg
		default:
			if flag, ok := safeAgentCommandFlagName(arg); ok {
				redacted[i] = flag
			} else {
				redacted[i] = redactedAgentCommandArg
			}
		}
	}
	return redacted
}

func isTrustedAgentCommandArg(index int, trustedPositionals map[int]struct{}) bool {
	_, ok := trustedPositionals[index]
	return ok
}

func safeAgentCommandFlagName(arg string) (string, bool) {
	flag := arg
	if equals := strings.IndexByte(flag, '='); equals > 0 {
		flag = flag[:equals]
	}

	if len(flag) == 2 && flag[0] == '-' && isASCIIAlpha(flag[1]) {
		return flag, true
	}

	if len(flag) < 3 || len(flag) > maxLoggedAgentCommandFlagLen {
		return "", false
	}
	if !strings.HasPrefix(flag, "--") || !isASCIIAlpha(flag[2]) {
		return "", false
	}
	for i := 3; i < len(flag); i++ {
		if !isSafeAgentCommandFlagRune(flag[i]) {
			return "", false
		}
	}
	return flag, true
}

func isSafeAgentCommandFlagRune(ch byte) bool {
	return isASCIIAlpha(ch) || (ch >= '0' && ch <= '9') || ch == '.' || ch == '_' || ch == '-'
}

func isASCIIAlpha(ch byte) bool {
	return ch >= 'A' && ch <= 'Z' || ch >= 'a' && ch <= 'z'
}
