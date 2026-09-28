package authn

import (
	"log/slog"
	"time"

	"github.com/adanman/goosar/server2/internal/config"
	"github.com/adanman/goosar/server2/internal/httpapi"
	"github.com/adanman/goosar/server2/internal/mail"
	"github.com/adanman/goosar/server2/internal/store"
)

// Deps — всё, что нужно домену authn. Не переиспользует app.Deps напрямую
// (см. server2/docs/adr/0001-stack.md, раздел "Раскладка пакетов"): это
// избавляет от цикла импорта app<->домен и позволяет параллельным
// реализаторам добавлять домены, зная только свой собственный Deps.
type Deps struct {
	Store  *Store
	Signer *Signer
	Config config.Config
	Mailer mail.Sender
	Logger *slog.Logger

	SendCodeIPLimiter    *httpapi.Limiter
	SendCodeEmailLimiter *httpapi.Limiter
	VerifyIPLimiter      *httpapi.Limiter
}

// New собирает Deps по общей инфраструктуре.
func New(db *store.Store, cfg config.Config, mailer mail.Sender, logger *slog.Logger) *Deps {
	return &Deps{
		Store:                NewStore(db),
		Signer:               NewSigner(cfg.JWTSecret),
		Config:               cfg,
		Mailer:               mailer,
		Logger:               logger,
		SendCodeIPLimiter:    httpapi.NewLimiter(5, time.Minute),
		SendCodeEmailLimiter: httpapi.NewLimiter(10, time.Minute),
		VerifyIPLimiter:      httpapi.NewLimiter(20, time.Minute),
	}
}
