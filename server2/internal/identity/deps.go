// Package identity реализует тег Me контракта: собственный профиль
// пользователя, онбординг, CLI-токен, personal access tokens (/api/tokens) и
// статичный список шаблонов пространств. Persistence аккаунта переиспользует
// authn.Store (см. server2/internal/authn/userview.go) — таблица accounts
// принадлежит authn (сессии/коды тоже читают/пишут её), identity добавляет
// только HTTP-слой профиля.
package identity

import (
	"log/slog"

	"github.com/adanman/goosar/server2/internal/authn"
)

// Deps — зависимости домена identity.
type Deps struct {
	Authn  *authn.Deps
	Logger *slog.Logger
}

func New(authnDeps *authn.Deps, logger *slog.Logger) *Deps {
	return &Deps{Authn: authnDeps, Logger: logger}
}
