// goosar_admin — серверный второй канал для изменения состава администраторов
// развёртывания. HTTP API только регистрирует заявки; применяет их оператор
// с доступом к DATABASE_URL, как и cmd/migrate.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"text/tabwriter"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/adanman/goosar/server/internal/handler"
	"github.com/adanman/goosar/server/internal/util"
	db "github.com/adanman/goosar/server/pkg/db/generated"
)

const usage = `goosar_admin — server-side confirmation channel for deployment_admin changes

Usage:
  goosar_admin list-pending      list pending grant/revoke requests
  goosar_admin confirm <id>      execute one pending request
  goosar_admin reject <id>       discard one pending request
  goosar_admin grant <email>     break-glass: grant the role to an existing
                                  user directly (recovery, audit-journaled)
  goosar_admin gc-uploads [--dry-run] [--grace=168h] [--limit=500]
                                  reclaim orphaned uploads: attachments bound
                                  to no issue/comment/chat message and older
                                  than --grace. --dry-run lists them and
                                  changes nothing.
  goosar_admin purge [--dry-run] [--chat=720h] [--tasks=720h]
                      [--closed-issues=8760h] [--activity=720h]
                      [--attachment-grace=168h]
                                  apply the retention policy (#399). Windows
                                  default to the GOOSAR_RETENTION_* variables;
                                  a flag overrides one of them. --dry-run
                                  reports the full backlog each window matches
                                  and deletes nothing.
  goosar_admin provision-roles
                                  create the deployment's role workspaces from
                                  the enabled workspace_template rows (#484).
                                  Idempotent: a rerun reports every role as
                                  skipped. Same procedure the backend runs at
                                  startup with GOOSAR_ROLE_WORKSPACES=auto.
  goosar_admin rotate-secrets --mcp [--mfa] [--dry-run]
                                  re-encrypt every column sealed with
                                  GOOSAR_MCP_SECRET_KEY under the current key.
                                  --mfa covers the TOTP secrets of #391.
  goosar_admin mfa-reset <email> break-glass: remove the TOTP second factor
                                  and recovery codes of ONE account and cut its
                                  sessions, for a person who lost their phone.
                                  Direct rather than pending: it changes no
                                  authority, and the recovery path for a
                                  locked-out person must not run through the
                                  system they are locked out of. Journaled.
  goosar_admin mcp-library seed [--dry-run]
                                  idempotent-by-name seeding of the deployment
                                  MCP library (deployment_mcp_server) from the
                                  GOOSAR_DEPLOYMENT_*_URL variables (#641,
                                  ADR-0019): jira, confluence, ews, bitrix24,
                                  mcp-gateway. An unset/empty address
                                  skips that service entirely. A record that
                                  was hand-edited after a previous seed is
                                  left alone with a warning, never
                                  overwritten. --dry-run reports what would
                                  change and writes nothing. Requires
                                  GOOSAR_MCP_SECRET_KEY (the deployment
                                  library is sealed at rest).

Reads DATABASE_URL from the environment (same as cmd/migrate).`

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "goosar_admin: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, out io.Writer) error {
	if len(args) == 0 {
		fmt.Fprintln(out, usage)
		return errors.New("a command is required")
	}
	command := args[0]
	switch command {
	case "list-pending":
		if len(args) != 1 {
			return errors.New("list-pending takes no arguments")
		}
	case "confirm", "reject":
		if len(args) != 2 {
			return fmt.Errorf("%s requires exactly one <id> argument", command)
		}
	case "grant":
		if len(args) != 2 {
			return errors.New("grant requires exactly one <email> argument")
		}
	case "provision-roles":
		if len(args) != 1 {
			return errProvisionRolesArgs
		}
	case "gc-uploads":
		if _, err := parseGCUploadsFlags(args[1:]); err != nil {
			return err
		}
	case "purge":
		if _, err := parsePurgeFlags(args[1:]); err != nil {
			return err
		}
	case "mfa-reset":
		if len(args) != 2 {
			return errors.New("mfa-reset requires exactly one <email> argument")
		}
	case "mcp-library":
		if _, err := parseMcpLibraryArgs(args[1:]); err != nil {
			return err
		}
	case "rotate-secrets":

		return rotateSecrets(ctx, out, args[1:])
	case "help", "-h", "--help":
		fmt.Fprintln(out, usage)
		return nil
	default:
		fmt.Fprintln(out, usage)
		return fmt.Errorf("unknown command %q", command)
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

	switch command {
	case "list-pending":
		return listPending(ctx, queries, out)
	case "confirm":
		return decide(ctx, pool, queries, out, args[1], true)
	case "reject":
		return decide(ctx, pool, queries, out, args[1], false)
	case "grant":
		return grant(ctx, pool, queries, out, args[1])
	case "provision-roles":
		return provisionRoles(ctx, pool, queries, out)
	case "gc-uploads":
		opts, err := parseGCUploadsFlags(args[1:])
		if err != nil {
			return err
		}
		return gcUploads(ctx, queries, out, opts)
	case "purge":
		opts, err := parsePurgeFlags(args[1:])
		if err != nil {
			return err
		}
		return purge(ctx, queries, out, opts)
	case "mfa-reset":
		return resetMFA(ctx, queries, out, args[1:])
	case "mcp-library":
		dryRun, err := parseMcpLibraryArgs(args[1:])
		if err != nil {
			return err
		}
		return mcpLibrarySeed(ctx, queries, out, dryRun)
	}
	return nil
}

func listPending(ctx context.Context, queries *db.Queries, out io.Writer) error {
	rows, err := queries.ListDeploymentAdminPending(ctx)
	if err != nil {
		return fmt.Errorf("list pending: %w", err)
	}
	if len(rows) == 0 {
		fmt.Fprintln(out, "no pending deployment-admin requests")
		return nil
	}
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tACTION\tTARGET\tEMAIL\tREQUESTED BY\tREQUESTED AT")
	for _, row := range rows {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
			util.UUIDToString(row.ID),
			row.Action,
			util.UUIDToString(row.TargetUserID),
			row.TargetEmail.String,
			util.UUIDToString(row.RequestedBy),
			row.RequestedAt.Time.Format("2006-01-02 15:04:05Z07:00"),
		)
	}
	return tw.Flush()
}

func grant(ctx context.Context, pool *pgxpool.Pool, queries *db.Queries, out io.Writer, email string) error {
	result, err := handler.GrantDeploymentAdminByEmail(ctx, pool, queries, email)
	if err != nil {
		return err
	}
	if !result.Created {
		fmt.Fprintf(out, "%s (%s) is already a deployment administrator — nothing to do\n", result.Email, result.UserID)
		return nil
	}
	fmt.Fprintf(out, "granted the deployment administrator role to %s (%s)\n", result.Email, result.UserID)
	return nil
}

func decide(ctx context.Context, pool *pgxpool.Pool, queries *db.Queries, out io.Writer, rawID string, confirm bool) error {
	id, err := util.ParseUUID(rawID)
	if err != nil {
		return fmt.Errorf("invalid request id %q", rawID)
	}
	if confirm {
		decision, err := handler.ConfirmDeploymentAdminPending(ctx, pool, queries, id)
		if err != nil {
			return err
		}
		if decision.AlreadyHeld {
			fmt.Fprintf(out, "confirmed %s of %s (target already held the role — no-op)\n", decision.Action, decision.TargetUserID)
			return nil
		}
		fmt.Fprintf(out, "confirmed %s of %s\n", decision.Action, decision.TargetUserID)
		return nil
	}
	decision, err := handler.RejectDeploymentAdminPending(ctx, pool, queries, id)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "rejected %s of %s\n", decision.Action, decision.TargetUserID)
	return nil
}
