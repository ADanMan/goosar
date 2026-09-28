package authn

import (
	"log/slog"
	"sync"
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

	// Лимитеры этого пакета названы по переменной окружения, которую они
	// читают (docs/50-api-contract.md §1.5), а не по одной ручке — контракт
	// сам группирует несколько операций под одним именем лимита и одним
	// значением по умолчанию, так что несколько ручек намеренно делят один
	// экземпляр *httpapi.Limiter (общий бюджет на этот IP/email в минуту),
	// вместо того чтобы каждая заводила свой независимый бюджет того же
	// размера (см. server2/docs/decisions.md, раздел T-029):
	//   - AuthIPLimiter (RATE_LIMIT_AUTH, по IP): send-code, ldap-login.
	//   - AuthEmailLimiter (RATE_LIMIT_AUTH_EMAIL, по email/username):
	//     send-code, verify-code, ldap-login.
	//   - AuthVerifyIPLimiter (RATE_LIMIT_AUTH_VERIFY, по IP): verify-code,
	//     verify-link, methods, oidc/start, oidc/callback, mfa/verify.
	AuthIPLimiter       *httpapi.Limiter
	AuthEmailLimiter    *httpapi.Limiter
	AuthVerifyIPLimiter *httpapi.Limiter

	// TokenOpsLimiter (RATE_LIMIT_TOKEN, по user, 1 час) — MFA
	// enroll/confirm/disable/recovery-codes. cli-token (домен identity) не
	// делит этот экземпляр (другой пакет) — та же переменная и то же
	// значение конфигурации ему доступны напрямую через config.Config, если
	// решат завести собственный лимитер того же размера.
	TokenOpsLimiter *httpapi.Limiter

	// MfaVerifyTokenLimiter (RATE_LIMIT_MFA_VERIFY, по хэшу mfa_token, 5
	// минут — TTL самого pending-токена) — только POST /api/auth/mfa/verify.
	MfaVerifyTokenLimiter *httpapi.Limiter

	// TaskActors — правка T-028: проверка mat_-токенов агента-исполнителя
	// задачи (contract §1.3), подключается доменом daemon после New() через
	// SetTaskActorLookup (см. agent_actor.go). nil, пока домен daemon не
	// собран — тогда mat_-токены по-прежнему отклоняются, как и раньше.
	TaskActors TaskActorLookup

	// oidcOnce/oidcInstance — ленивая сборка клиента OIDC (см. oidc.go,
	// метод oidc()); не создаётся вовсе, пока OIDC не настроен.
	oidcOnce     sync.Once
	oidcInstance *oidcClient
}

// New собирает Deps по общей инфраструктуре.
func New(db *store.Store, cfg config.Config, mailer mail.Sender, logger *slog.Logger) *Deps {
	return &Deps{
		Store:  NewStore(db),
		Signer: NewSigner(cfg.JWTSecret),
		Config: cfg,
		Mailer: mailer,
		Logger: logger,

		AuthIPLimiter:       httpapi.NewLimiter(cfg.RateLimits.Auth, time.Minute),
		AuthEmailLimiter:    httpapi.NewLimiter(cfg.RateLimits.AuthEmail, time.Minute),
		AuthVerifyIPLimiter: httpapi.NewLimiter(cfg.RateLimits.AuthVerify, time.Minute),

		TokenOpsLimiter:       httpapi.NewLimiter(cfg.RateLimits.Token, time.Hour),
		MfaVerifyTokenLimiter: httpapi.NewLimiter(cfg.RateLimits.MfaVerify, 5*time.Minute),
	}
}
