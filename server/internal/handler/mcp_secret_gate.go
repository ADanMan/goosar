// Стартовая проверка GOOSAR_MCP_SECRET_KEY для периметрового профиля
// поставки. Без ключа MCP-конфигурация хранится в открытом виде — в облаке
// это лишь предупреждение, а внутри периметра клиента это неприемлемо, ведь
// именно ради защиты этих кредов деплой и разворачивается on-prem. Поэтому
// при GOOSAR_DELIVERY_PROFILE=perimeter отсутствие ключа фатально.
package handler

import (
	"fmt"

	"github.com/adanman/goosar/server/internal/deliveryprofile"
)

func RequireSecretKeyForPerimeter(profile deliveryprofile.Profile, keyConfigured bool) error {
	if keyConfigured || !profile.IsPerimeter() {
		return nil
	}
	return fmt.Errorf("%s=%s but GOOSAR_MCP_SECRET_KEY is not set — every stored integration credential (MCP servers, Jira, Exchange) would be written to the database, and therefore to every backup, as plaintext. Generate a key with `openssl rand -base64 32`, set GOOSAR_MCP_SECRET_KEY, and back it up alongside the database; losing it loses every stored secret",
		deliveryprofile.EnvVar, deliveryprofile.Perimeter)
}

func MissingSecretKeyWarning() string {
	return "GOOSAR_MCP_SECRET_KEY is not set — stored integration credentials (MCP servers, Jira, Exchange) are written to the database, and therefore to every backup, as PLAINTEXT, and configuration admin secret writes return 503. Generate a key with `openssl rand -base64 32` and back it up alongside the database; losing it loses every stored secret. Deployments on GOOSAR_DELIVERY_PROFILE=perimeter refuse to start in this state."
}
