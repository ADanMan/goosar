// Пакет agent — единый интерфейс запуска промптов через runtime кодовых агентов.
// Каждый runtime адресуется нейтральным кодом (runtime-a … runtime-r); соответствие
// кода реальному CLI и каталог моделей живут в runtimeregistry.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/adanman/goosar/server/pkg/agent/runtimeregistry"
)

var runtimeRegistry = mustLoadRuntimeRegistry()

func mustLoadRuntimeRegistry() *runtimeregistry.Registry {
	reg, err := runtimeregistry.LoadDefault()
	if err != nil {
		panic(fmt.Sprintf("agent: load runtime registry: %v", err))
	}
	return reg
}

func runtimeCLIName(runtimeCode string) string {
	d, _ := runtimeRegistry.ByCode(runtimeCode)
	return d.CLIName
}

type Backend interface {
	Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error)
}

type ExecOptions struct {
	Cwd   string
	Model string

	SystemPrompt              string
	ThreadName                string
	MaxTurns                  int
	Timeout                   time.Duration
	SemanticInactivityTimeout time.Duration

	IdleWatchdogTimeout time.Duration

	HandshakeTimeout time.Duration
	ResumeSessionID  string

	ResumeExpected bool
	ExtraArgs      []string
	CustomArgs     []string
	McpConfig      json.RawMessage

	ThinkingLevel string

	ServiceTier string

	RuntimeNMode string

	RuntimeCSettingsPath string
}

func runContext(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout > 0 {
		return context.WithTimeout(ctx, timeout)
	}
	return context.WithCancel(ctx)
}

type Session struct {
	Messages <-chan Message

	Result <-chan Result
}

type MessageType string

const (
	MessageText       MessageType = "text"
	MessageThinking   MessageType = "thinking"
	MessageToolUse    MessageType = "tool-use"
	MessageToolResult MessageType = "tool-result"
	MessageStatus     MessageType = "status"
	MessageError      MessageType = "error"
	MessageLog        MessageType = "log"
)

type Message struct {
	Type      MessageType
	Content   string
	Tool      string
	CallID    string
	Input     map[string]any
	Output    string
	Status    string
	Level     string
	SessionID string
}

type TokenUsage struct {
	InputTokens      int64
	OutputTokens     int64
	CacheReadTokens  int64
	CacheWriteTokens int64

	CostUSDTicks int64
}

const CostUSDTicksPerUSD = 10_000_000_000

type Result struct {
	Status     string
	Output     string
	Error      string
	DurationMs int64
	SessionID  string
	Usage      map[string]TokenUsage

	ResumeRejected bool

	runtimeEInitializeRetrySafe bool

	runtimeEStartupRefreshRetrySafe bool
}

type Config struct {
	ExecutablePath string
	CLIVersion     string
	Env            map[string]string
	Logger         *slog.Logger
	TaskID         string

	RuntimeID     string
	DaemonVersion string

	RuntimeEVersion string

	provider string
}

var SupportedTypes = runtimeRegistry.Codes()

func IsSupportedType(runtimeCode string) bool {
	for _, t := range SupportedTypes {
		if t == runtimeCode {
			return true
		}
	}
	return false
}

var resumeRejectionUndetectable = map[string]bool{
	"runtime-a": true,
	"runtime-f": true,
	"runtime-g": true,
	"runtime-h": true,
	"runtime-m": true,
}

func ResumeRejectionUndetectable(runtimeCode string) bool {
	return resumeRejectionUndetectable[runtimeCode]
}

func New(runtimeCode string, cfg Config) (Backend, error) {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.provider == "" {
		cfg.provider = runtimeCode
	}

	switch runtimeCode {
	case "runtime-a":
		return &runtimeABackend{cfg: cfg}, nil
	case "runtime-c":
		return &runtimeCBackend{cfg: cfg}, nil
	case "runtime-d":
		return &runtimeDBackend{cfg: cfg}, nil
	case "runtime-e":
		return &runtimeEBackend{cfg: cfg}, nil
	case "runtime-f":
		return &runtimeFBackend{cfg: cfg}, nil
	case "runtime-g":
		return &runtimeGBackend{cfg: cfg}, nil
	case "runtime-h":
		return &runtimeHBackend{cfg: cfg}, nil
	case "runtime-i":
		return &runtimeIBackend{cfg: cfg}, nil
	case "runtime-j":
		return &runtimeJBackend{cfg: cfg, provider: "runtime-j"}, nil
	case "runtime-k":
		return &runtimeKBackend{cfg: cfg}, nil
	case "runtime-l":
		return &runtimeLBackend{cfg: cfg}, nil
	case "runtime-m":
		return &runtimeMBackend{cfg: cfg}, nil
	case "runtime-n":
		return &runtimeNBackend{cfg: cfg}, nil
	case "runtime-o":
		return &runtimeOBackend{cfg: cfg}, nil
	case "runtime-p":
		return &runtimePBackend{cfg: cfg}, nil
	case "runtime-q":
		return &runtimeQBackend{cfg: cfg}, nil
	case "runtime-r":
		return &runtimeRBackend{cfg: cfg}, nil
	default:
		return nil, fmt.Errorf("unknown runtime code: %q (supported: %s)", runtimeCode, strings.Join(SupportedTypes, ", "))
	}
}

func DetectVersion(ctx context.Context, executablePath string) (string, error) {
	return detectCLIVersion(ctx, executablePath)
}

var launchHeaders = map[string]string{
	"runtime-a": "-p (non-interactive)",
	"runtime-c": "stream-json",
	"runtime-d": "stream-json",
	"runtime-e": "app-server",
	"runtime-f": "json",
	"runtime-g": "stream-json",
	"runtime-h": "run (json)",
	"runtime-i": "agent stdio",
	"runtime-j": "acp",
	"runtime-k": "acp",
	"runtime-l": "acp",
	"runtime-m": "run (json)",
	"runtime-n": "agent (json)",
	"runtime-o": "json mode",
	"runtime-p": "--acp",
	"runtime-q": "-p (stream-json)",
	"runtime-r": "acp serve",
}

func LaunchHeader(runtimeCode string) string {
	return launchHeaders[runtimeCode]
}
