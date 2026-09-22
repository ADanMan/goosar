// Команда goosar_admin mcp-library seed: идемпотентное (по имени) заполнение
// библиотеки MCP-серверов развёртывания из переменных GOOSAR_DEPLOYMENT_*_URL.
// Здесь только CLI-обвязка; логика живёт в internal/service/mcplibrary.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"text/tabwriter"

	"github.com/adanman/goosar/server/internal/handler"
	"github.com/adanman/goosar/server/internal/service/mcplibrary"
	db "github.com/adanman/goosar/server/pkg/db/generated"
)

func parseMcpLibraryArgs(args []string) (dryRun bool, err error) {
	if len(args) == 0 || args[0] != "seed" {
		return false, fmt.Errorf("mcp-library: expected \"seed\" as the first argument")
	}
	for _, arg := range args[1:] {
		switch arg {
		case "--dry-run":
			dryRun = true
		default:
			return false, fmt.Errorf("mcp-library seed: unknown argument %q", arg)
		}
	}
	return dryRun, nil
}

func mcpLibrarySeed(ctx context.Context, queries *db.Queries, out io.Writer, dryRun bool) error {
	box := handler.MCPSecretBoxFromEnv()
	if box == nil {
		return mcplibrary.ErrNoSecretKey
	}
	specs := mcplibrary.BuildSpecs(os.Getenv)
	if len(specs) == 0 {
		fmt.Fprintln(out, "mcp-library seed: no GOOSAR_DEPLOYMENT_*_URL variables are set, nothing to do")
		return nil
	}

	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "SERVICE\tOUTCOME\tDETAIL")
	results, err := mcplibrary.Seed(ctx, queries, box, specs, dryRun, nil)
	for _, r := range results {
		fmt.Fprintf(tw, "%s\t%s\t%s\n", r.Name, r.Outcome, r.Detail)
	}
	if flushErr := tw.Flush(); flushErr != nil {
		return flushErr
	}
	if err != nil {
		return err
	}

	warned := 0
	for _, r := range results {
		if r.Outcome == mcplibrary.OutcomeSkippedManual {
			warned++
			fmt.Fprintf(out, "warning: %s was changed after seeding and was left alone (%s)\n", r.Name, r.Detail)
		}
	}
	if warned > 0 {
		fmt.Fprintf(out, "%d record(s) protected from an automatic overwrite; review and reconcile manually if needed\n", warned)
	}
	return nil
}
