// commands.go — исполнение команд goosar_admin поверх БД server2. Разбор
// флагов — flags.go (без побочных эффектов, unit-тестируется отдельно).
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/adanman/goosar/server2/internal/authn"
	"github.com/adanman/goosar/server2/internal/config"
	"github.com/adanman/goosar/server2/internal/deployment"
	"github.com/adanman/goosar/server2/internal/seal"
	"github.com/adanman/goosar/server2/internal/store"
	"github.com/adanman/goosar/server2/internal/workspace"
)

// --- list-pending / confirm / reject / grant -----------------------------------

func runListPending(ctx context.Context, out io.Writer, db *store.Store) error {
	pending, err := deployment.ListPendingRequests(ctx, db)
	if err != nil {
		return err
	}
	if len(pending) == 0 {
		fmt.Fprintln(out, "нет заявок, ждущих подтверждения")
		return nil
	}
	for _, p := range pending {
		target := ""
		if p.TargetUserID != nil {
			target = *p.TargetUserID
		} else if p.TargetEmail != nil {
			target = *p.TargetEmail
		}
		fmt.Fprintf(out, "%s\t%s\t%s\tзапрошена=%s\tкем=%s\n", p.RequestID, p.Action, target, p.RequestedAt.Format(time.RFC3339), p.RequestedBy)
	}
	return nil
}

func runConfirm(ctx context.Context, out io.Writer, db *store.Store, id string) error {
	result, err := deployment.ConfirmPendingRequest(ctx, db, id)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "заявка %s подтверждена: %s\n", id, result)
	return nil
}

func runReject(ctx context.Context, out io.Writer, db *store.Store, id string) error {
	if err := deployment.RejectPendingRequest(ctx, db, id); err != nil {
		return err
	}
	fmt.Fprintf(out, "заявка %s отклонена\n", id)
	return nil
}

func runGrant(ctx context.Context, out io.Writer, db *store.Store, email string) error {
	admin, alreadyAdmin, err := deployment.GrantAdminDirect(ctx, db, email, "")
	if err != nil {
		return err
	}
	if alreadyAdmin {
		fmt.Fprintf(out, "%s уже deployment-admin\n", admin.Email)
		return nil
	}
	fmt.Fprintf(out, "%s выдана роль deployment-admin (аварийно, минуя заявку)\n", admin.Email)
	return nil
}

// --- gc-uploads ------------------------------------------------------------------

// orphanedAsset — минимум, нужный gc-uploads: строка assets, не привязанная
// ни к задаче, ни к комментарию, ни к сообщению чата (contract, приложение
// CLI). dispatch_job_id (вложения агентских запусков) в контракте среди трёх
// названных категорий не упомянут — сознательно не считается "осиротевшим"
// здесь (см. server2/docs/decisions.md, раздел T-029).
type orphanedAsset struct {
	ID         string
	StorageURI string
	Filename   string
	CreatedAt  time.Time
}

func findOrphanedAssets(ctx context.Context, db *store.Store, grace time.Duration, limit int) ([]orphanedAsset, error) {
	rows, err := db.Pool.Query(ctx, `
		SELECT id, as_storage_uri, as_filename, created_at FROM assets
		WHERE ticket_id IS NULL AND ticket_note_id IS NULL AND convo_message_id IS NULL
		  AND created_at < now() - $1::interval
		ORDER BY created_at LIMIT $2`, fmt.Sprintf("%d seconds", int(grace.Seconds())), limit)
	if err != nil {
		return nil, fmt.Errorf("admin: поиск осиротевших загрузок: %w", err)
	}
	defer rows.Close()
	out := []orphanedAsset{}
	for rows.Next() {
		var a orphanedAsset
		if err := rows.Scan(&a.ID, &a.StorageURI, &a.Filename, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// storageKeyFromLocalURI — то же соглашение, что internal/asset.LocalStorage.Save
// ("local://<key>"), продублировано здесь узко (не импортируем internal/asset
// целиком ради одной внутренней функции).
func storageKeyFromLocalURI(uri string) (key string, ok bool) {
	const prefix = "local://"
	if len(uri) <= len(prefix) || uri[:len(prefix)] != prefix {
		return "", false
	}
	return uri[len(prefix):], true
}

func runGCUploads(ctx context.Context, out io.Writer, db *store.Store, cfg config.Config, opts gcUploadsOptions) error {
	assets, err := findOrphanedAssets(ctx, db, opts.grace, opts.limit)
	if err != nil {
		return err
	}
	if len(assets) == 0 {
		fmt.Fprintln(out, "осиротевших загрузок не найдено")
		return nil
	}
	for _, a := range assets {
		fmt.Fprintf(out, "%s\t%s\t%s\n", a.ID, a.Filename, a.CreatedAt.Format(time.RFC3339))
	}
	if opts.dryRun {
		fmt.Fprintf(out, "--dry-run: %d загрузок было бы удалено\n", len(assets))
		return nil
	}
	deleted := 0
	for _, a := range assets {
		if _, err := db.Pool.Exec(ctx, `DELETE FROM assets WHERE id = $1`, a.ID); err != nil {
			return fmt.Errorf("admin: удаление вложения %s: %w", a.ID, err)
		}
		if key, ok := storageKeyFromLocalURI(a.StorageURI); ok && cfg.LocalUploadDir != "" {
			_ = removeLocalUpload(cfg.LocalUploadDir, key)
		}
		deleted++
	}
	fmt.Fprintf(out, "удалено загрузок: %d\n", deleted)
	return nil
}

// --- purge -----------------------------------------------------------------------

type purgeWindow struct {
	label     string
	countSQL  string
	deleteSQL string
	window    time.Duration
}

func purgeWindows(opts purgeOptions) []purgeWindow {
	return []purgeWindow{
		{
			label:     "chat",
			countSQL:  `SELECT count(*) FROM convos WHERE created_at < now() - $1::interval`,
			deleteSQL: `DELETE FROM convos WHERE created_at < now() - $1::interval`,
			window:    opts.chat,
		},
		{
			label:     "tasks",
			countSQL:  `SELECT count(*) FROM dispatch_jobs WHERE dj_status IN ('completed','failed','cancelled') AND created_at < now() - $1::interval`,
			deleteSQL: `DELETE FROM dispatch_jobs WHERE dj_status IN ('completed','failed','cancelled') AND created_at < now() - $1::interval`,
			window:    opts.tasks,
		},
		{
			label:     "closed-issues",
			countSQL:  `SELECT count(*) FROM tickets WHERE tk_status IN ('done','cancelled') AND updated_at < now() - $1::interval`,
			deleteSQL: `DELETE FROM tickets WHERE tk_status IN ('done','cancelled') AND updated_at < now() - $1::interval`,
			window:    opts.closedIssues,
		},
		{
			label:     "activity",
			countSQL:  `SELECT count(*) FROM ticket_activity WHERE created_at < now() - $1::interval`,
			deleteSQL: `DELETE FROM ticket_activity WHERE created_at < now() - $1::interval`,
			window:    opts.activity,
		},
		{
			label: "attachments",
			countSQL: `SELECT count(*) FROM assets WHERE ticket_id IS NULL AND ticket_note_id IS NULL
				AND convo_message_id IS NULL AND created_at < now() - $1::interval`,
			deleteSQL: `DELETE FROM assets WHERE ticket_id IS NULL AND ticket_note_id IS NULL
				AND convo_message_id IS NULL AND created_at < now() - $1::interval`,
			window: opts.attachmentGrace,
		},
	}
}

func runPurge(ctx context.Context, out io.Writer, db *store.Store, opts purgeOptions) error {
	for _, w := range purgeWindows(opts) {
		arg := fmt.Sprintf("%d seconds", int(w.window.Seconds()))
		var count int
		if err := db.Pool.QueryRow(ctx, w.countSQL, arg).Scan(&count); err != nil {
			return fmt.Errorf("admin: purge %s: подсчёт: %w", w.label, err)
		}
		if opts.dryRun {
			fmt.Fprintf(out, "%s: %d строк было бы удалено (окно %s)\n", w.label, count, w.window)
			continue
		}
		if count == 0 {
			fmt.Fprintf(out, "%s: нечего удалять\n", w.label)
			continue
		}
		if _, err := db.Pool.Exec(ctx, w.deleteSQL, arg); err != nil {
			return fmt.Errorf("admin: purge %s: удаление: %w", w.label, err)
		}
		fmt.Fprintf(out, "%s: удалено %d строк (окно %s)\n", w.label, count, w.window)
	}
	return nil
}

// --- provision-roles ---------------------------------------------------------------

func runProvisionRoles(ctx context.Context, out io.Writer, db *store.Store, cfg config.Config) error {
	ws := workspace.NewStore(db)
	templates := deployment.EnabledRoleTemplates(cfg.RoleWorkspaces)
	if len(templates) == 0 {
		templates = deployment.EnabledRoleTemplates("auto")
	}
	results, err := deployment.ProvisionRoleWorkspaces(ctx, db, ws, templates)
	if err != nil {
		return err
	}
	for _, r := range results {
		if r.Action == "skipped" {
			fmt.Fprintf(out, "%s: пропущено (%s)\n", r.Key, r.Reason)
			continue
		}
		fmt.Fprintf(out, "%s: создано (workspace_id=%s, slug=%s)\n", r.Key, r.WorkspaceID, r.Slug)
	}
	return nil
}

// --- rotate-secrets --mcp [--mfa] --------------------------------------------------

type sealedColumn struct {
	table  string
	idCol  string
	sealed string
}

// mcpSealedColumns — T-029 доводка: до неё agent (operatives.op_*_sealed) и
// autopilot (sentinel_triggers.strig_signing_secret_sealed) шифровали своими
// независимыми копиями AES-256-GCM, не internal/seal — rotate-secrets не мог
// их коснуться (см. server2/docs/decisions.md, раздел T-029, «rotate-secrets
// --mcp: переносит только таблицы, запечатанные через internal/seal этой
// сессии»). Доводка T-029 перевела оба пакета на internal/seal (тот же
// формат, с чтением их исторических данных), поэтому rotateColumn теперь
// работает и для них — contract §«CLI администратора деплоя»: "перешифровывает
// **все** значения, запечатанные ключом GOOSAR_MCP_SECRET_KEY".
// operatives хранит три *_sealed-колонки, каждая из которых может быть
// незашифрованным plaintext (contract: "без ключа — сохраняется как есть") —
// rotateColumn использует seal.Open (не Optional-вариант), который для
// такого значения корректно возвращает ok=false и считает строку "пропущено"
// (нечего перешифровывать), не ошибку.
var mcpSealedColumns = []sealedColumn{
	{"platform_mcp_servers", "id", "pmcp_config_sealed"},
	{"space_mcp_servers", "id", "wmcp_config_sealed"},
	{"space_mcp_credentials", "id", "wmcpc_value_sealed"},
	{"space_config", "workspace_id", "cfg_llm_api_key_sealed"},
	{"operatives", "id", "op_runtime_config_sealed"},
	{"operatives", "id", "op_mcp_config_sealed"},
	{"operatives", "id", "op_custom_env_sealed"},
	{"sentinel_triggers", "id", "strig_signing_secret_sealed"},
}

func rotateColumn(ctx context.Context, db *store.Store, c sealedColumn, cfg config.Config, dryRun bool) (touched, skipped int, err error) {
	rows, err := db.Pool.Query(ctx, fmt.Sprintf(`SELECT %s, %s FROM %s WHERE %s IS NOT NULL`, c.idCol, c.sealed, c.table, c.sealed))
	if err != nil {
		return 0, 0, err
	}
	type pending struct {
		id    string
		plain []byte
	}
	var toWrite []pending
	for rows.Next() {
		var id string
		var sealedVal []byte
		if err := rows.Scan(&id, &sealedVal); err != nil {
			rows.Close()
			return 0, 0, err
		}
		plain, ok := seal.Open(cfg.McpSecretKey, cfg.McpSecretKeyPrevious, sealedVal)
		if !ok {
			skipped++
			continue
		}
		toWrite = append(toWrite, pending{id, plain})
	}
	if err := rows.Err(); err != nil {
		return 0, 0, err
	}
	rows.Close()
	if dryRun {
		return len(toWrite), skipped, nil
	}
	for _, p := range toWrite {
		newSealed, err := seal.Seal(cfg.McpSecretKey, p.plain)
		if err != nil {
			return touched, skipped, err
		}
		if _, err := db.Pool.Exec(ctx, fmt.Sprintf(`UPDATE %s SET %s = $1 WHERE %s = $2`, c.table, c.sealed, c.idCol), newSealed, p.id); err != nil {
			return touched, skipped, err
		}
		touched++
	}
	return touched, skipped, nil
}

// rotateConfigOverrides — space_config_overrides не имеет однокомпонентного
// PK (см. server2/docs/decisions.md) — отдельная функция вместо
// generic-варианта rotateColumn.
func rotateConfigOverrides(ctx context.Context, db *store.Store, cfg config.Config, dryRun bool) (touched, skipped int, err error) {
	rows, err := db.Pool.Query(ctx, `SELECT workspace_id, account_id, cfgo_llm_api_key_sealed FROM space_config_overrides WHERE cfgo_llm_api_key_sealed IS NOT NULL`)
	if err != nil {
		return 0, 0, err
	}
	type key struct{ workspaceID, accountID string }
	plains := map[key][]byte{}
	for rows.Next() {
		var k key
		var sealedVal []byte
		if err := rows.Scan(&k.workspaceID, &k.accountID, &sealedVal); err != nil {
			rows.Close()
			return 0, 0, err
		}
		plain, ok := seal.Open(cfg.McpSecretKey, cfg.McpSecretKeyPrevious, sealedVal)
		if !ok {
			skipped++
			continue
		}
		plains[k] = plain
	}
	if err := rows.Err(); err != nil {
		return 0, 0, err
	}
	rows.Close()
	if dryRun {
		return len(plains), skipped, nil
	}
	for k, plain := range plains {
		newSealed, err := seal.Seal(cfg.McpSecretKey, plain)
		if err != nil {
			return touched, skipped, err
		}
		if _, err := db.Pool.Exec(ctx, `UPDATE space_config_overrides SET cfgo_llm_api_key_sealed = $1 WHERE workspace_id = $2 AND account_id = $3`,
			newSealed, k.workspaceID, k.accountID); err != nil {
			return touched, skipped, err
		}
		touched++
	}
	return touched, skipped, nil
}

// rotateMFAFactors — T-029 доводка перевела internal/authn (mfa_secret_sealed)
// на internal/seal тоже (тот же формат, что и остальные таблицы этого файла),
// так что это больше не best-effort по формату — остаётся best-effort только
// в обычном смысле rotateColumn: строка, которую не удаётся открыть ни
// текущим, ни предыдущим ключом (например ключ сменился без ротации ещё
// давнее значение), тихо пропускается (skipped), а не считается ошибкой всей
// команды. Отдельный флаг --mfa (не часть основного списка mcpSealedColumns)
// — прямое требование contract §«CLI администратора деплоя»: "--mfa
// дополнительно покрывает TOTP-секреты".
func rotateMFAFactors(ctx context.Context, db *store.Store, cfg config.Config, dryRun bool) (touched, skipped int, err error) {
	return rotateColumn(ctx, db, sealedColumn{"mfa_factors", "id", "mfa_secret_sealed"}, cfg, dryRun)
}

func runRotateSecrets(ctx context.Context, out io.Writer, db *store.Store, cfg config.Config, opts rotateSecretsOptions) error {
	if cfg.McpSecretKey == "" {
		return errors.New("GOOSAR_MCP_SECRET_KEY is not configured")
	}
	total, totalSkipped := 0, 0
	for _, c := range mcpSealedColumns {
		touched, skipped, err := rotateColumn(ctx, db, c, cfg, opts.dryRun)
		if err != nil {
			return fmt.Errorf("admin: rotate-secrets %s.%s: %w", c.table, c.sealed, err)
		}
		fmt.Fprintf(out, "%s.%s: %d перешифровано, %d пропущено (не открылись текущим/предыдущим ключом)\n", c.table, c.sealed, touched, skipped)
		total += touched
		totalSkipped += skipped
	}
	touched, skipped, err := rotateConfigOverrides(ctx, db, cfg, opts.dryRun)
	if err != nil {
		return fmt.Errorf("admin: rotate-secrets space_config_overrides: %w", err)
	}
	fmt.Fprintf(out, "space_config_overrides.cfgo_llm_api_key_sealed: %d перешифровано, %d пропущено\n", touched, skipped)
	total += touched
	totalSkipped += skipped

	if opts.mfa {
		touched, skipped, err := rotateMFAFactors(ctx, db, cfg, opts.dryRun)
		if err != nil {
			return fmt.Errorf("admin: rotate-secrets mfa_factors: %w", err)
		}
		fmt.Fprintf(out, "mfa_factors.mfa_secret_sealed: %d перешифровано, %d пропущено\n", touched, skipped)
		total += touched
		totalSkipped += skipped
	}
	if opts.dryRun {
		fmt.Fprintf(out, "--dry-run: всего было бы перешифровано %d значений, пропущено %d\n", total, totalSkipped)
	} else {
		fmt.Fprintf(out, "готово: перешифровано %d значений, пропущено %d\n", total, totalSkipped)
	}
	return nil
}

// --- mfa-reset -------------------------------------------------------------------

func runMfaReset(ctx context.Context, out io.Writer, db *store.Store, authnStore *authn.Store, email string) error {
	acct, err := authnStore.FindAccountByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, authn.ErrNotFound) {
			return fmt.Errorf("no account with email %s", email)
		}
		return err
	}
	if _, err := db.Pool.Exec(ctx, `DELETE FROM mfa_recovery_codes WHERE account_id = $1`, acct.ID); err != nil {
		return err
	}
	if _, err := db.Pool.Exec(ctx, `DELETE FROM mfa_factors WHERE account_id = $1`, acct.ID); err != nil {
		return err
	}
	if _, err := authnStore.RevokeAllSessions(ctx, acct.ID); err != nil {
		return err
	}
	audit := deployment.AuditWrite{
		Source: "admin", Action: "mfa_reset",
		TargetType: strPtr("account"), TargetID: strPtr(acct.ID),
	}
	_ = deployment.WriteAudit(ctx, db, audit)
	fmt.Fprintf(out, "MFA сброшен и сессии %s завершены\n", email)
	return nil
}

func strPtr(s string) *string { return &s }

// --- mcp-library seed --------------------------------------------------------------

type mcpLibraryEntry struct {
	name string
	url  string
}

func mcpLibraryEntries(cfg config.Config) []mcpLibraryEntry {
	return []mcpLibraryEntry{
		{"jira", cfg.DeploymentJiraURL},
		{"confluence", cfg.DeploymentConfluenceURL},
		{"ews", cfg.DeploymentEWSURL},
		{"bitrix24", cfg.DeploymentBitrix24URL},
		{"mcp-gateway", cfg.DeploymentMcpGatewayURL},
	}
}

func runMcpLibrarySeed(ctx context.Context, out io.Writer, db *store.Store, cfg config.Config, opts mcpLibrarySeedOptions) error {
	if cfg.McpSecretKey == "" {
		return errors.New("GOOSAR_MCP_SECRET_KEY is not configured")
	}
	for _, e := range mcpLibraryEntries(cfg) {
		if e.url == "" {
			fmt.Fprintf(out, "%s: пропущено (адрес не задан)\n", e.name)
			continue
		}
		var id string
		var createdAt, updatedAt time.Time
		err := db.Pool.QueryRow(ctx, `SELECT id, created_at, updated_at FROM platform_mcp_servers WHERE pmcp_name = $1`, e.name).Scan(&id, &createdAt, &updatedAt)
		if err == nil {
			if !updatedAt.Equal(createdAt) {
				fmt.Fprintf(out, "%s: пропущено (изменён вручную после прошлого заполнения)\n", e.name)
				continue
			}
			if opts.dryRun {
				fmt.Fprintf(out, "%s: было бы обновлено\n", e.name)
				continue
			}
			sealed, sealErr := seal.SealJSON(cfg.McpSecretKey, map[string]any{"url": e.url})
			if sealErr != nil {
				return sealErr
			}
			if _, err := db.Pool.Exec(ctx, `UPDATE platform_mcp_servers SET pmcp_config_sealed = $2, updated_at = now() WHERE id = $1`, id, sealed); err != nil {
				return err
			}
			fmt.Fprintf(out, "%s: обновлено\n", e.name)
			continue
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if opts.dryRun {
			fmt.Fprintf(out, "%s: было бы создано\n", e.name)
			continue
		}
		sealed, sealErr := seal.SealJSON(cfg.McpSecretKey, map[string]any{"url": e.url})
		if sealErr != nil {
			return sealErr
		}
		if _, err := db.Pool.Exec(ctx, `
			INSERT INTO platform_mcp_servers (pmcp_name, pmcp_transport, pmcp_config_sealed, pmcp_credential_schema)
			VALUES ($1, 'http', $2, '[]'::jsonb)`, e.name, sealed); err != nil {
			return err
		}
		fmt.Fprintf(out, "%s: создано\n", e.name)
	}
	return nil
}

// removeLocalUpload удаляет байты локально хранимой загрузки (тот же формат
// ключа, что internal/asset.LocalStorage: root/<key>); отсутствие файла — не
// ошибка (могли уже удалить руками).
func removeLocalUpload(root, key string) error {
	path := filepath.Join(root, filepath.FromSlash(key))
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
