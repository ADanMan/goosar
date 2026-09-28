package app_test

import (
	"testing"

	"github.com/adanman/goosar/server2/internal/app"
)

// TestNoStubsLeft — доводка T-029: строит полный роутер (все домены +
// заглушки) и печатает/проверяет, какие операции контракта всё ещё отвечает
// голая 501-заглушка (RegisterStubs → HandleStub), потому что ни один домен
// не занял этот путь через Handle. Критерий приёмки — 0 таких маршрутов;
// пока список не пуст, тест печатает его целиком (через t.Log, до Fatal),
// чтобы служить рабочим списком «что ещё реализовать».
func TestNoStubsLeft(t *testing.T) {
	router := app.NewRouter(buildTestRouter(t))
	stubs := router.StubbedRoutes()
	if len(stubs) == 0 {
		return
	}
	t.Logf("операций контракта всё ещё на 501-заглушке: %d", len(stubs))
	for _, s := range stubs {
		t.Logf("  %s", s)
	}
	t.Fatalf("остались нереализованные операции контракта (%d), список выше", len(stubs))
}
