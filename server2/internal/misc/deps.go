// Package misc реализует последние одиночные публичные ручки T-029, у
// которых нет отдельного домена в контракте: `POST /api/feedback`,
// `POST /api/contact-sales`, `POST /api/client-usage`, `GET /api/status`
// (docs/50-api-contract.md §3.5/§3.9, "Рабочие пространства...", §8).
// `/api/workspace-templates` (identity) и `/api/assignee-frequency` (task)
// уже реализованы другими доменами этой сессии — не дублируются здесь (см.
// server2/docs/decisions.md, раздел T-029).
package misc

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/adanman/goosar/server2/internal/httpapi"
	"github.com/adanman/goosar/server2/internal/store"
	"github.com/adanman/goosar/server2/internal/wsctx"
)

type Deps struct {
	Store    *Store
	Resolver *wsctx.Resolver
	Logger   *slog.Logger

	// FeedbackLimiter — contract: "лимит 10/час на пользователя" на
	// POST /api/feedback; имя переменной окружения контракт не называет
	// (в отличие от RATE_LIMIT_CONTACT_SALES/RATE_LIMIT_EXPORT) — решение
	// T-029 в server2/docs/decisions.md: значение зашито константой.
	FeedbackLimiter *httpapi.Limiter

	// ContactSalesIPLimiter — RATE_LIMIT_CONTACT_SALES, по IP.
	ContactSalesIPLimiter *httpapi.Limiter
	// ContactSalesEmailLimiter — contract: "3/hour per business_email
	// server-side cap"; тоже без названной переменной — решение T-029.
	ContactSalesEmailLimiter *httpapi.Limiter
}

const feedbackPerHour = 10
const contactSalesEmailPerHour = 3

func New(db *store.Store, contactSalesIPPerHour int, logger *slog.Logger) *Deps {
	return &Deps{
		Store:                    NewStore(db),
		Resolver:                 wsctx.New(db),
		Logger:                   logger,
		FeedbackLimiter:          httpapi.NewLimiter(feedbackPerHour, time.Hour),
		ContactSalesIPLimiter:    httpapi.NewLimiter(contactSalesIPPerHour, time.Hour),
		ContactSalesEmailLimiter: httpapi.NewLimiter(contactSalesEmailPerHour, time.Hour),
	}
}

// Register занимает POST /api/feedback, POST /api/contact-sales,
// POST /api/client-usage, GET /api/status.
func Register(router *httpapi.Router, deps *Deps) {
	router.Handle(http.MethodPost, "/api/feedback", deps.handleCreateFeedback)
	router.Handle(http.MethodPost, "/api/contact-sales", deps.handleContactSales)
	router.Handle(http.MethodPost, "/api/client-usage", deps.handleClientUsage)
	router.Handle(http.MethodGet, "/api/status", deps.handleWorkspaceStatus)
}
