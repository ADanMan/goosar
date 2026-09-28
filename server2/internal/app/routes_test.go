package app_test

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/adanman/goosar/server2/internal/app"
	"github.com/adanman/goosar/server2/internal/config"
	"github.com/adanman/goosar/server2/internal/contractspec"
	"github.com/adanman/goosar/server2/internal/store"
)

// buildTestRouter собирает полный роутер сервера (домены + заглушки) поверх
// пула БД, который никогда реально не подключается — регистрация маршрутов
// не делает запросов, поэтому dummy DSN достаточно.
func buildTestRouter(t *testing.T) *app.Deps {
	t.Helper()
	db, err := store.Open(context.Background(), "postgres://postgres@localhost:5432/nonexistent-for-route-test")
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(db.Close)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return app.New(config.Config{JWTSecret: "test-secret"}, db, logger)
}

// TestRouteCoverageMatchesContract — acceptance criterion T-026 #3: набор
// зарегистрированных маршрутов (домены + 501-заглушки) должен совпадать
// один-в-один с множеством операций docs/50-api-contract.yaml — не только по
// количеству, но и по конкретным method+path.
func TestRouteCoverageMatchesContract(t *testing.T) {
	ops, err := contractspec.Load("../../../docs/50-api-contract.yaml")
	if err != nil {
		t.Fatalf("contractspec.Load: %v", err)
	}
	if len(ops) == 0 {
		t.Fatal("contractspec.Load вернул 0 операций — путь к спецификации неверен?")
	}

	router := app.NewRouter(buildTestRouter(t))
	registered := router.Registered()

	fromSpec := make(map[string]bool, len(ops))
	for _, op := range ops {
		fromSpec[op.Method+" "+op.Path] = true
	}
	fromRouter := make(map[string]bool, len(registered))
	for _, r := range registered {
		fromRouter[r] = true
	}

	var missing, extra []string
	for k := range fromSpec {
		if !fromRouter[k] {
			missing = append(missing, k)
		}
	}
	for k := range fromRouter {
		if !fromSpec[k] {
			extra = append(extra, k)
		}
	}
	if len(missing) > 0 || len(extra) > 0 {
		t.Fatalf("расхождение маршрутов: %d в контракте, но не зарегистрировано (%v); %d зарегистрировано, но не в контракте (%v)",
			len(missing), missing, len(extra), extra)
	}
}
