package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/adanman/goosar/server/internal/util/secretbox"
	db "github.com/adanman/goosar/server/pkg/db/generated"
)

const mcpSealedKey = "__goosar_sealed__"

const rotateUsage = `goosar_admin rotate-secrets (--mcp | --mfa | --all) [--dry-run]

Re-encrypts every column sealed with GOOSAR_MCP_SECRET_KEY under the current
key. Requires GOOSAR_MCP_SECRET_KEY to be set; keys still needed to READ old
rows go in GOOSAR_MCP_SECRET_KEY_PREVIOUS (comma-separated).

  --mcp       agent mcp_config documents
  --mfa       TOTP shared secrets (#391)
  --all       both of the above
  --dry-run   report how many rows are still on a retired key, write nothing

Passing no domain flag is an error rather than "everything": a rotation is a
deliberate act on a named estate.

Exits non-zero if any row could not be opened by a key in the ring.`

type rotateOptions struct {
	mcp    bool
	mfa    bool
	dryRun bool
}

func parseRotateArgs(args []string) (rotateOptions, error) {
	var opts rotateOptions
	for _, arg := range args {
		switch arg {
		case "--mcp":
			opts.mcp = true
		case "--mfa":
			opts.mfa = true
		case "--all":
			opts.mcp = true
			opts.mfa = true
		case "--dry-run":
			opts.dryRun = true
		default:
			return opts, fmt.Errorf("unknown rotate-secrets flag %q\n\n%s", arg, rotateUsage)
		}
	}
	if !opts.mcp && !opts.mfa {
		return opts, errors.New("rotate-secrets needs a domain flag\n\n" + rotateUsage)
	}
	return opts, nil
}

const uncoveredMcpEstatesWarning = `WARNING: this pass covers agent mcp_config and TOTP secrets
  ONLY. The same key also seals workspace_mcp_server.config,
  deployment_mcp_server.config, workspace_mcp_user_credential.sealed_values,
  workspace_config.mcp_defaults / user_config_override.mcp_overrides and the
  stored LLM API keys — there is no pass for those yet. Do NOT empty
  GOOSAR_MCP_SECRET_KEY_PREVIOUS while any of them may still be sealed with
  the retired key: they would become permanently undecryptable.
`

func rotateSecrets(ctx context.Context, out io.Writer, args []string) error {
	opts, err := parseRotateArgs(args)
	if err != nil {
		return err
	}

	box, err := secretbox.FromEnv("GOOSAR_MCP_SECRET_KEY")
	if err != nil {
		if errors.Is(err, secretbox.ErrKeyNotSet) {
			return errors.New("GOOSAR_MCP_SECRET_KEY is not set — nothing to rotate to. Set the NEW key there and the retired one in GOOSAR_MCP_SECRET_KEY_PREVIOUS, then re-run")
		}
		return fmt.Errorf("GOOSAR_MCP_SECRET_KEY: %w", err)
	}

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		return errors.New("DATABASE_URL is not set")
	}
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer pool.Close()
	queries := db.New(pool)

	var total rotateStats
	if opts.mcp {
		agents, err := rotateMcpConfigs(ctx, queries, box, opts.dryRun, out)
		if err != nil {
			return err
		}
		total.add(agents)
	}
	if opts.mfa {
		mfa, err := rotateMFASecrets(ctx, queries, box, opts.dryRun, out)
		if err != nil {
			return err
		}
		total.add(mfa)
	}

	if opts.dryRun {
		fmt.Fprintf(out, "dry run: scanned=%d stale=%d unreadable=%d (nothing written)\n",
			total.scanned, total.stale, total.unreadable)
		return unreadableError(total.unreadable)
	}
	fmt.Fprintf(out, "rotation complete: scanned=%d resealed=%d skipped=%d unreadable=%d\n",
		total.scanned, total.resealed, total.skipped, total.unreadable)

	if _, err := queries.InsertAdminAudit(ctx, db.InsertAdminAuditParams{
		Action:     "secret.rotate",
		TargetType: "secret_key",
		TargetID:   pgText(rotateDomainLabel(opts)),
		AfterHash:  pgText(fmt.Sprintf("scanned=%d resealed=%d skipped=%d unreadable=%d", total.scanned, total.resealed, total.skipped, total.unreadable)),
		RequestID:  pgText("goosar_admin rotate-secrets"),
	}); err != nil {
		fmt.Fprintf(out, "warning: rotation succeeded but the audit row could not be written: %v\n", err)
	}
	return unreadableError(total.unreadable)
}

func unreadableError(unreadable int) error {
	if unreadable == 0 {
		return nil
	}
	return fmt.Errorf("%d row(s) could not be opened by any key in the ring and were left untouched — "+
		"add the missing key to GOOSAR_MCP_SECRET_KEY_PREVIOUS and re-run; do NOT destroy the retired key yet", unreadable)
}

type rotateStats struct {
	scanned    int
	stale      int
	resealed   int
	skipped    int
	unreadable int
}

func (s *rotateStats) add(other rotateStats) {
	s.scanned += other.scanned
	s.stale += other.stale
	s.resealed += other.resealed
	s.skipped += other.skipped
	s.unreadable += other.unreadable
}

func rotateMcpConfigs(ctx context.Context, queries *db.Queries, box *secretbox.Box, dryRun bool, out io.Writer) (rotateStats, error) {
	var stats rotateStats
	rows, err := queries.ListAgentMcpConfigsForBackfill(ctx)
	if err != nil {
		return stats, fmt.Errorf("list agent mcp_config: %w", err)
	}
	for _, row := range rows {
		stats.scanned++
		plaintext, sealed, err := openMcpEnvelope(box, row.McpConfig)
		if err != nil {
			stats.unreadable++
			fmt.Fprintf(out, "agent %s: mcp_config unreadable — left untouched: %v\n", uuidText(row.ID), err)
			continue
		}
		if sealed && box.SealedWithCurrentKey(sealedBytes(row.McpConfig)) {
			stats.skipped++
			continue
		}
		stats.stale++
		if dryRun {
			continue
		}
		envelope, err := sealMcpEnvelope(box, plaintext)
		if err != nil {
			return stats, fmt.Errorf("agent %s: reseal: %w", uuidText(row.ID), err)
		}
		affected, err := queries.SealAgentMcpConfigForBackfill(ctx, db.SealAgentMcpConfigForBackfillParams{
			ID:       row.ID,
			Sealed:   envelope,
			Previous: row.McpConfig,
		})
		if err != nil {
			return stats, fmt.Errorf("agent %s: write: %w", uuidText(row.ID), err)
		}
		if affected == 0 {

			stats.skipped++
			stats.stale--
			continue
		}
		stats.resealed++
		fmt.Fprintf(out, "agent %s: mcp_config resealed\n", uuidText(row.ID))
	}
	return stats, nil
}

func openMcpEnvelope(box *secretbox.Box, stored []byte) (plaintext []byte, sealed bool, err error) {
	raw := sealedBytes(stored)
	if raw == nil {
		return stored, false, nil
	}
	opened, err := box.Open(raw)
	if err != nil {
		return nil, true, err
	}
	return opened, true, nil
}

func sealedBytes(stored []byte) []byte {
	if len(stored) == 0 {
		return nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(stored, &obj); err != nil || len(obj) != 1 {
		return nil
	}
	val, ok := obj[mcpSealedKey]
	if !ok {
		return nil
	}
	var s string
	if err := json.Unmarshal(val, &s); err != nil {
		return nil
	}
	raw, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil
	}
	return raw
}

func sealMcpEnvelope(box *secretbox.Box, plaintext []byte) ([]byte, error) {
	sealed, err := box.Seal(plaintext)
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]string{mcpSealedKey: base64.StdEncoding.EncodeToString(sealed)})
}

func pgText(s string) pgtype.Text { return pgtype.Text{String: s, Valid: s != ""} }

func uuidText(id pgtype.UUID) string {
	if !id.Valid {
		return ""
	}
	return uuid.UUID(id.Bytes).String()
}

func rotateDomainLabel(opts rotateOptions) string {
	switch {
	case opts.mcp && opts.mfa:
		return "mcp+mfa"
	case opts.mfa:
		return "mfa"
	default:
		return "mcp"
	}
}

func rotateMFASecrets(ctx context.Context, queries *db.Queries, box *secretbox.Box, dryRun bool, out io.Writer) (rotateStats, error) {
	var stats rotateStats
	rows, err := queries.ListUserMFASecretsForRotation(ctx)
	if err != nil {
		return stats, fmt.Errorf("list TOTP secrets: %w", err)
	}
	for _, row := range rows {
		stats.scanned++
		if box.SealedWithCurrentKey(row.TotpSecretSealed) {
			stats.skipped++
			continue
		}
		plaintext, err := box.Open(row.TotpSecretSealed)
		if err != nil {
			stats.unreadable++
			fmt.Fprintf(out, "user %s: TOTP secret unreadable — left untouched: %v\n", uuidText(row.UserID), err)
			continue
		}
		stats.stale++
		if dryRun {
			continue
		}
		sealed, err := box.Seal(plaintext)
		if err != nil {
			return stats, fmt.Errorf("user %s: reseal: %w", uuidText(row.UserID), err)
		}
		affected, err := queries.ResealUserMFASecret(ctx, db.ResealUserMFASecretParams{
			UserID:   row.UserID,
			Sealed:   sealed,
			Previous: row.TotpSecretSealed,
		})
		if err != nil {
			return stats, fmt.Errorf("user %s: write: %w", uuidText(row.UserID), err)
		}
		if affected == 0 {
			stats.skipped++
			stats.stale--
			continue
		}
		stats.resealed++
		fmt.Fprintf(out, "user %s: TOTP secret resealed\n", uuidText(row.UserID))
	}
	return stats, nil
}
