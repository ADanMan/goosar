// Пакет plugin — точка расширения для интеграций, живущих вне основного
// сервера: контракт подключения и жизненный цикл.
package plugin

import (
	"context"
	"log/slog"
	"sync"

	db "github.com/adanman/goosar/server/pkg/db/generated"
)

type Host struct {
	Logger *slog.Logger

	DB db.DBTX
}

type Plugin interface {
	Name() string

	Start(ctx context.Context, host Host) error

	Stop(ctx context.Context) error
}

var (
	mu       sync.RWMutex
	registry []Plugin
)

func Register(p Plugin) {
	if p == nil {
		return
	}
	mu.Lock()
	defer mu.Unlock()
	registry = append(registry, p)
}

func Registered() []Plugin {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]Plugin, len(registry))
	copy(out, registry)
	return out
}
