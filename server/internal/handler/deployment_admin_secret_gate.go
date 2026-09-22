// Стартовая проверка ключа шифрования для админ-поверхности конфигурации.
// Если она включена без настроенного ключа, сервер откажется стартовать с
// понятной ошибкой — иначе администраторы получили бы либо сплошные 503,
// либо откат к хранению секретов в открытом виде.
package handler

import (
	"context"
	"fmt"

	db "github.com/adanman/goosar/server/pkg/db/generated"
)

func RequireSecretKeyForAdminSurface(ctx context.Context, queries *db.Queries, rawEnvEmails string, keyConfigured bool) error {
	if keyConfigured {
		return nil
	}
	enabled := len(parseDeploymentAdminEmails(rawEnvEmails)) > 0
	if !enabled {
		count, err := queries.CountDeploymentAdmins(ctx)
		if err != nil {
			return fmt.Errorf("admin secret-key gate: count deployment admins: %w", err)
		}
		enabled = count > 0
	}
	if !enabled {
		return nil
	}
	return fmt.Errorf("the deployment admin surface is enabled (%s is set or deployment_admin is non-empty) but GOOSAR_MCP_SECRET_KEY is not — admins could not store any gateway key or MCP credential (writes fail closed with 503). Generate a key with `openssl rand -base64 32`, set GOOSAR_MCP_SECRET_KEY, and back it up alongside the database; losing it loses every stored secret", DeploymentAdminEmailsEnvVar)
}
