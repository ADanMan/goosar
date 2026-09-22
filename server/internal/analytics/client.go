// Пакет analytics отправляет продуктовые события телеметрии во внешний backend
// (PostHog). Захват неблокирующий: события складываются в ограниченный канал,
// фоновый воркер сбрасывает их пачками.
package analytics

import (
	"log/slog"
	"os"
	"strings"
	"time"
)

type Event struct {
	Name string

	DistinctID string

	WorkspaceID string

	Properties map[string]any

	SetOnce map[string]any

	Set map[string]any

	Timestamp time.Time
}

type Client interface {
	Capture(e Event)

	Close()
}

func NewFromEnv() Client {
	if isDisabled() {
		slog.Info("analytics disabled via ANALYTICS_DISABLED")
		return NoopClient{}
	}
	key := os.Getenv("POSTHOG_API_KEY")
	if key == "" {
		slog.Info("analytics: POSTHOG_API_KEY not set, using noop client")
		return NoopClient{}
	}
	host := os.Getenv("POSTHOG_HOST")
	if host == "" {
		host = "https://us.i.posthog.com"
	}
	slog.Info("analytics: posthog client enabled", "host", host)
	return NewPostHogClient(PostHogConfig{
		APIKey:      key,
		Host:        host,
		Environment: EnvironmentFromEnv(),
	})
}

func isDisabled() bool {
	v := os.Getenv("ANALYTICS_DISABLED")
	return v == "true" || v == "1"
}

func EnvironmentFromEnv() string {
	if v := normalizeEnvironment(os.Getenv("ANALYTICS_ENVIRONMENT")); v != "" {
		return v
	}
	if v := normalizeEnvironment(os.Getenv("APP_ENV")); v != "" {
		return v
	}
	return "dev"
}

func normalizeEnvironment(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "production", "prod":
		return "production"
	case "staging", "stage":
		return "staging"
	case "development", "dev", "test", "local":
		return "dev"
	default:
		return ""
	}
}

type NoopClient struct{}

func (NoopClient) Capture(Event) {}
func (NoopClient) Close()        {}
