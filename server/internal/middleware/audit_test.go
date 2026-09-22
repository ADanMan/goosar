package middleware

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/adanman/goosar/server/internal/audit"
)

func TestRefusalRowsAreBudgetedPerClientIP(t *testing.T) {
	refusalRowLimiter = NewMemoryRateLimitStore()
	ctx := context.Background()

	for i := 0; i < refusalRowBudget; i++ {
		if !refusalRowAllowed(ctx, "198.51.100.9") {
			t.Fatalf("row %d of the budget was dropped", i+1)
		}
	}
	if refusalRowAllowed(ctx, "198.51.100.9") {
		t.Fatal("budget exceeded but the row was still written")
	}

	if !refusalRowAllowed(ctx, "203.0.113.7") {
		t.Fatal("a different client IP was starved by the first one's flood")
	}
}

func TestAuditEventUsesTheUnforgeableClientIP(t *testing.T) {
	t.Setenv("GOOSAR_TRUSTED_PROXIES", "")
	t.Setenv("RATE_LIMIT_TRUSTED_PROXIES", "")

	req := httptest.NewRequest("GET", "/api/issues", nil)
	req.RemoteAddr = "198.51.100.9:41234"
	req.Header.Set("X-Forwarded-For", "10.0.0.1")

	if got := auditEventFor(req, audit.Event{}).ClientIP; got != "198.51.100.9" {
		t.Fatalf("client_ip = %q, want the socket peer", got)
	}
}
