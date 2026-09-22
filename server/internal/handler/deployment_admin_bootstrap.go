// Бутстрап роли deployment_admin из списка email в переменной окружения.
// Список — это только затравка: он выдаёт роль, пока таблица пуста; как
// только появилась хотя бы одна запись, источником истины становится БД.
// Сид выполняется при каждом старте, пока таблица пуста, а параллельные
// узлы синхронизируются через advisory-лок и ON CONFLICT DO NOTHING.
package handler

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/adanman/goosar/server/pkg/db/generated"
)

const DeploymentAdminEmailsEnvVar = "GOOSAR_DEPLOYMENT_ADMIN_EMAILS"

func parseDeploymentAdminEmails(raw string) []string {
	seen := make(map[string]bool)
	var emails []string
	for _, part := range strings.Split(raw, ",") {
		email := strings.ToLower(strings.TrimSpace(part))
		if email == "" || seen[email] {
			continue
		}
		seen[email] = true
		emails = append(emails, email)
	}
	return emails
}

func SeedDeploymentAdmins(ctx context.Context, txs txStarter, queries *db.Queries, rawEmails string) error {
	emails := parseDeploymentAdminEmails(rawEmails)

	tx, err := txs.Begin(ctx)
	if err != nil {
		return fmt.Errorf("deployment admin seed: begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := queries.WithTx(tx)
	if err := qtx.LockConfigLayerKey(ctx, deploymentAdminsLockKey); err != nil {
		return fmt.Errorf("deployment admin seed: lock: %w", err)
	}

	count, err := qtx.CountDeploymentAdmins(ctx)
	if err != nil {
		return fmt.Errorf("deployment admin seed: count: %w", err)
	}
	if count > 0 {

		if len(emails) > 0 {
			logDeploymentAdminEnvDivergence(ctx, qtx, emails)
		}
		return nil
	}
	if len(emails) == 0 {
		return nil
	}

	seeded := 0
	for _, email := range emails {
		user, err := qtx.GetUserByEmail(ctx, email)
		if isNotFound(err) {

			slog.Warn("deployment admin seed: no user with this email yet — will retry on next start while the table is empty",
				"email_hash", hashEmailForLog(email), "env_var", DeploymentAdminEmailsEnvVar)
			continue
		}
		if err != nil {
			return fmt.Errorf("deployment admin seed: look up %s: %w", email, err)
		}
		inserted, err := qtx.InsertDeploymentAdmin(ctx, db.InsertDeploymentAdminParams{
			UserID: user.ID,

			GrantedBy: pgtype.UUID{},
		})
		if err != nil {
			return fmt.Errorf("deployment admin seed: insert %s: %w", email, err)
		}
		if inserted > 0 {
			if _, err := qtx.InsertAdminAudit(ctx, db.InsertAdminAuditParams{
				ActorUserID: pgtype.UUID{},
				Action:      adminAuditActionBootstrapGrant,
				TargetType:  "user",
				TargetID:    pgtype.Text{String: uuidToString(user.ID), Valid: true},
			}); err != nil {
				return fmt.Errorf("deployment admin seed: audit %s: %w", email, err)
			}
			seeded++
		}
	}
	if seeded == 0 {

		slog.Warn("deployment admin seed: no listed email resolved to a user — the deployment has no administrators yet; the seed retries on the next start",
			"listed", len(emails))
		return nil
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("deployment admin seed: commit: %w", err)
	}
	slog.Info("deployment admin seed: granted the role from the env list (empty-table bootstrap)",
		"seeded", seeded, "listed", len(emails))
	return nil
}

func logDeploymentAdminEnvDivergence(ctx context.Context, qtx *db.Queries, envEmails []string) {
	rows, err := qtx.ListDeploymentAdmins(ctx)
	if err != nil {
		slog.Warn("deployment admin seed: divergence check failed", "error", err)
		return
	}
	inDB := make(map[string]bool, len(rows))
	for _, row := range rows {
		if row.Email.Valid {
			inDB[strings.ToLower(row.Email.String)] = true
		}
	}
	var notGranted []string
	for _, email := range envEmails {
		if !inDB[email] {
			notGranted = append(notGranted, email)
		}
	}
	if len(notGranted) > 0 {
		slog.Warn("deployment admin seed: env list diverges from the database — the table is non-empty, so the list only seeds a FIRST deployment; manage admins through the API",
			"env_var", DeploymentAdminEmailsEnvVar,
			"env_only_count", len(notGranted),
			"db_admins", len(rows))
	}
}
