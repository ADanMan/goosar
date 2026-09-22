package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type runtimeABackend struct {
	cfg Config
}

func (b *runtimeABackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		execPath = runtimeCLIName("runtime-a")
	}
	if _, err := exec.LookPath(execPath); err != nil {
		return nil, fmt.Errorf("agy executable not found at %q: %w", execPath, err)
	}

	if opts.Model != "" {
		catalog, _ := ListModels(ctx, "runtime-a", execPath)
		if err := runtimeAModelError(opts.Model, catalog); err != nil {
			return nil, err
		}
	}

	timeout := opts.Timeout
	runCtx, cancel := runContext(ctx, timeout)

	logFile, err := os.CreateTemp("", "goosar-agy-log-*.log")
	if err != nil {
		cancel()
		return nil, fmt.Errorf("create agy log file: %w", err)
	}
	logPath := logFile.Name()
	_ = logFile.Close()

	args := buildRuntimeAArgs(prompt, logPath, timeout, opts, b.cfg.Logger)

	cmd := newRuntimeCmd(exec.CommandContext(runCtx, execPath, args...))
	hideAgentWindow(cmd)
	b.cfg.logAgentCommand(cmd, newAgentCommandLogArgs(args))
	cmd.WaitDelay = 10 * time.Second
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	cmd.Env = buildEnv(b.cfg.Env)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		_ = os.Remove(logPath)
		return nil, fmt.Errorf("agy stdout pipe: %w", err)
	}
	stderrBuf := newStderrTail(newLogWriter(b.cfg.Logger, "[agy:stderr] "), agentStderrTailBytes)
	cmd.Stderr = stderrBuf

	if err := cmd.Start(); err != nil {
		cancel()
		_ = os.Remove(logPath)
		return nil, fmt.Errorf("start agy: %w", err)
	}

	b.cfg.Logger.Info("agy started", "pid", cmd.Process.Pid, "cwd", opts.Cwd, "model", opts.Model)

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)

	go func() {
		<-runCtx.Done()
		_ = stdout.Close()
	}()

	go func() {
		defer cancel()
		defer close(msgCh)
		defer close(resCh)
		defer os.Remove(logPath)

		startTime := time.Now()
		var output strings.Builder
		finalStatus := "completed"
		var finalError string

		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

		trySend(msgCh, Message{Type: MessageStatus, Status: "running"})

		for scanner.Scan() {
			line := scanner.Text()
			if output.Len() > 0 {
				output.WriteByte('\n')
			}
			output.WriteString(line)
			if strings.TrimSpace(line) != "" {
				trySend(msgCh, Message{Type: MessageText, Content: line})
			}
		}
		if err := scanner.Err(); err != nil {
			b.cfg.Logger.Warn("agy stdout scanner error", "err", err)
		}

		waitErr := cmd.Wait()
		duration := time.Since(startTime)

		sessionID := readRuntimeAConversationID(logPath)

		if runCtx.Err() == context.DeadlineExceeded {
			finalStatus = "timeout"
			finalError = fmt.Sprintf("agy timed out after %s", timeout)
		} else if runCtx.Err() == context.Canceled {
			finalStatus = "aborted"
			finalError = "execution cancelled"
		} else if waitErr != nil && finalStatus == "completed" {
			finalStatus = "failed"
			finalError = fmt.Sprintf("agy exited with error: %v", waitErr)
		} else if finalStatus == "completed" && runtimeAPrintTimedOut(logPath) {

			finalStatus = "timeout"
			finalError = fmt.Sprintf(
				"agy --print-timeout elapsed after %s waiting for the agent response; a long-running command likely outlived the print timeout",
				runtimeAPrintTimeout(timeout),
			)
		} else if providerErr := runtimeAProviderError(logPath); finalStatus == "completed" && providerErr != "" {

			finalStatus = "failed"
			finalError = fmt.Sprintf("agy provider error: %s", providerErr)
		}
		if finalError != "" {
			finalError = withAgentStderr(finalError, "agy", stderrBuf.Tail())
		}

		finalOutput := output.String()
		if finalStatus == "completed" && strings.TrimSpace(finalOutput) == "" {

			if recovered := readRuntimeATranscriptOutput(logPath, sessionID); recovered != "" {
				finalOutput = recovered
				trySend(msgCh, Message{Type: MessageText, Content: recovered})
				b.cfg.Logger.Info("agy recovered empty stdout from transcript", "bytes", len(recovered))
			}
		}

		b.cfg.Logger.Info("agy finished", "pid", cmd.Process.Pid, "status", finalStatus, "duration", duration.Round(time.Millisecond).String())

		resCh <- Result{
			Status:     finalStatus,
			Output:     finalOutput,
			Error:      finalError,
			DurationMs: duration.Milliseconds(),
			SessionID:  sessionID,

			Usage: map[string]TokenUsage{},
		}
	}()

	return &Session{Messages: msgCh, Result: resCh}, nil
}

var runtimeAConversationIDRe = regexp.MustCompile(
	`conversation=([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})`,
)

var runtimeAPrintTimeoutRe = regexp.MustCompile(`Print mode: timed out after \d+ polls`)

var runtimeAProviderErrorRe = regexp.MustCompile(`agent executor error:\s*(.+)`)

func runtimeAPrintTimedOut(logPath string) bool {
	if logPath == "" {
		return false
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		return false
	}
	return runtimeAPrintTimeoutRe.Match(data)
}

func runtimeAProviderError(logPath string) string {
	if logPath == "" {
		return ""
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		return ""
	}
	matches := runtimeAProviderErrorRe.FindAllSubmatch(data, -1)
	if len(matches) == 0 {
		return ""
	}
	return strings.TrimSpace(string(matches[len(matches)-1][1]))
}

func readRuntimeAConversationID(logPath string) string {
	if logPath == "" {
		return ""
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		return ""
	}
	matches := runtimeAConversationIDRe.FindAllSubmatch(data, -1)
	if len(matches) == 0 {
		return ""
	}

	return string(matches[len(matches)-1][1])
}

var runtimeAAppDataDirRe = regexp.MustCompile(`CLI app data directory:\s*(.+)`)

type runtimeATranscriptRecord struct {
	Type    string          `json:"type"`
	Source  string          `json:"source"`
	Status  string          `json:"status"`
	Content json.RawMessage `json:"content"`
}

func readRuntimeATranscriptOutput(logPath, conversationID string) string {
	if logPath == "" || conversationID == "" {
		return ""
	}
	appDataDir := readRuntimeAAppDataDir(logPath)
	if appDataDir == "" {
		return ""
	}
	transcriptPath := filepath.Join(
		appDataDir, "brain", conversationID, ".system_generated", "logs", "transcript.jsonl",
	)
	f, err := os.Open(transcriptPath)
	if err != nil {
		return ""
	}
	defer f.Close()

	var parts []string
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var rec runtimeATranscriptRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			continue
		}
		if rec.Type == "USER_INPUT" {

			parts = parts[:0]
			continue
		}
		if rec.Type != "PLANNER_RESPONSE" || rec.Source != "MODEL" || rec.Status != "DONE" {
			continue
		}
		var text string

		if err := json.Unmarshal(rec.Content, &text); err != nil {
			continue
		}
		if strings.TrimSpace(text) != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n\n")
}

func readRuntimeAAppDataDir(logPath string) string {
	if logPath == "" {
		return ""
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		return ""
	}
	m := runtimeAAppDataDirRe.FindSubmatch(data)
	if m == nil {
		return ""
	}
	return strings.TrimSpace(string(m[1]))
}

var runtimeABlockedArgs = map[string]blockedArgMode{
	"-p":                             blockedWithValue,
	"--print":                        blockedWithValue,
	"--prompt":                       blockedWithValue,
	"-i":                             blockedStandalone,
	"--prompt-interactive":           blockedStandalone,
	"-c":                             blockedStandalone,
	"--continue":                     blockedStandalone,
	"--conversation":                 blockedWithValue,
	"--model":                        blockedWithValue,
	"--print-timeout":                blockedWithValue,
	"--dangerously-skip-permissions": blockedStandalone,
	"--log-file":                     blockedWithValue,
	"--settings":                     blockedWithValue,
}

func buildRuntimeAArgs(prompt, logPath string, timeout time.Duration, opts ExecOptions, logger *slog.Logger) []string {
	args := []string{
		"-p", prompt,
		"--dangerously-skip-permissions",
	}
	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
	}

	args = append(args, "--print-timeout", runtimeAFormatTimeout(runtimeAPrintTimeout(timeout)))
	args = append(args, "--log-file", logPath)
	if opts.ResumeSessionID != "" {
		args = append(args, "--conversation", opts.ResumeSessionID)
	}
	if opts.Cwd != "" {
		args = append(args, "--add-dir", filepath.Clean(opts.Cwd))
	}
	args = append(args, filterCustomArgs(opts.ExtraArgs, runtimeABlockedArgs, logger)...)
	args = append(args, filterCustomArgs(opts.CustomArgs, runtimeABlockedArgs, logger)...)
	return args
}

func runtimeAModelError(model string, available []Model) error {
	if model == "" || len(available) == 0 {
		return nil
	}
	ids := make([]string, 0, len(available))
	for _, m := range available {
		if m.ID == model {
			return nil
		}
		ids = append(ids, m.ID)
	}
	return fmt.Errorf(
		"antigravity model %q is not available from `agy models`; pick one of: %s",
		model, strings.Join(ids, ", "),
	)
}

const runtimeANoCapPrintTimeout = 24 * time.Hour

func runtimeAPrintTimeout(timeout time.Duration) time.Duration {
	if timeout > 0 {
		return timeout
	}
	return runtimeANoCapPrintTimeout
}

func runtimeAFormatTimeout(d time.Duration) string {
	if d < time.Second {
		d = time.Second
	}

	return d.String()
}
