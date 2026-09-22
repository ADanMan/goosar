package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/adanman/goosar/server/internal/handler"
	db "github.com/adanman/goosar/server/pkg/db/generated"
)

func provisionRoles(ctx context.Context, pool *pgxpool.Pool, queries *db.Queries, out io.Writer) error {
	result, err := handler.ProvisionRoleWorkspaces(ctx, pool, queries, handler.MCPSecretBoxFromEnv())
	if err != nil {
		return err
	}
	if result.Deferred != "" {

		fmt.Fprintf(out, "deferred: %s\n", result.Deferred)
		return errors.New("no role workspace was provisioned")
	}
	if len(result.Outcomes) == 0 {
		fmt.Fprintln(out, "no enabled workspace templates — nothing to provision")
		return nil
	}

	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	for _, o := range result.Outcomes {
		detail := o.Detail
		if detail == "" {
			detail = o.WorkspaceID
		}
		fmt.Fprintf(tw, "%s\t%s\t%d autopilot(s)\t%s\n", o.Status, o.TemplateKey, o.Autopilots, detail)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	autopilots := 0
	for _, o := range result.Outcomes {
		autopilots += o.Autopilots
	}
	fmt.Fprintf(out, "created %d, skipped %d, errors %d, autopilots %d\n",
		result.Created, result.Skipped, result.Errors, autopilots)
	if result.Errors > 0 {
		return fmt.Errorf("%d role workspace(s) could not be provisioned", result.Errors)
	}
	return nil
}

var errProvisionRolesArgs = errors.New("provision-roles takes no arguments")
