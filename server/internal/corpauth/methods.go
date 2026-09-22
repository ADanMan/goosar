// Пакет corpauth — корпоративный вход: OIDC как основной путь, LDAP/AD simple
// bind как запасной. Здесь только протокол и конфигурация; сессии, БД и пользователи
// Goosar — зона ответственности internal/handler.
package corpauth

import (
	"os"
	"strings"
)

const (
	MethodEmail = "email"
	MethodOIDC  = "oidc"
	MethodLDAP  = "ldap"
)

const MethodsEnvVar = "GOOSAR_AUTH_METHODS"

type Methods struct {
	Email bool
	OIDC  bool
	LDAP  bool
}

func (m Methods) Any() bool { return m.Email || m.OIDC || m.LDAP }

func ParseMethods(raw string) Methods {
	var m Methods
	for _, part := range strings.Split(raw, ",") {
		switch strings.ToLower(strings.TrimSpace(part)) {
		case MethodEmail:
			m.Email = true
		case MethodOIDC:
			m.OIDC = true
		case MethodLDAP:
			m.LDAP = true
		}
	}
	if !m.Any() {
		return Methods{Email: true}
	}
	return m
}

func MethodsFromEnv() Methods { return ParseMethods(os.Getenv(MethodsEnvVar)) }
