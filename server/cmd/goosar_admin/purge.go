package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/adanman/goosar/server/internal/retention"
	"github.com/adanman/goosar/server/internal/storage"
	db "github.com/adanman/goosar/server/pkg/db/generated"
)

type purgeOptions struct {
	dryRun bool
	policy retention.Policy
}

func parsePurgeFlags(args []string) (purgeOptions, error) {
	opts := purgeOptions{policy: retention.PolicyFromEnv()}
	windows := map[string]*time.Duration{
		"--chat":             &opts.policy.Chat,
		"--tasks":            &opts.policy.Tasks,
		"--closed-issues":    &opts.policy.ClosedIssues,
		"--activity":         &opts.policy.Activity,
		"--attachment-grace": &opts.policy.AttachmentGrace,
	}
	for _, arg := range args {
		if arg == "--dry-run" {
			opts.dryRun = true
			continue
		}
		name, raw, ok := strings.Cut(arg, "=")
		target, known := windows[name]
		if !ok || !known {
			return opts, fmt.Errorf("unknown purge flag %q", arg)
		}
		value, err := time.ParseDuration(raw)
		if err != nil {
			return opts, fmt.Errorf("invalid %s: %w", name, err)
		}
		if value < 0 {
			return opts, fmt.Errorf("%s must not be negative", name)
		}
		*target = value
	}
	return opts, nil
}

func purge(ctx context.Context, queries *db.Queries, out io.Writer, opts purgeOptions) error {
	if !opts.policy.Enabled() {
		return errors.New("no retention window is configured: every window is \"keep forever\", so there is nothing to purge. Set GOOSAR_RETENTION_* or pass a window flag (see `goosar_admin help`)")
	}

	store := storage.FromEnv()
	if store == nil && opts.policy.AttachmentGrace > 0 {
		fmt.Fprintln(out, "note: no storage backend configured (S3_BUCKET / LOCAL_UPLOAD_DIR); attachment objects will NOT be reclaimed this pass")
	}

	report, err := retention.Run(ctx, queries, storeOrNil(store), opts.policy, opts.dryRun)
	if err != nil {
		return err
	}

	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "WHAT\tWINDOW\tROWS")
	rows := []struct {
		what   string
		window time.Duration
		n      int64
	}{
		{"chat sessions", opts.policy.Chat, report.ChatSessions},
		{"finished tasks", opts.policy.Tasks, report.Tasks},
		{"closed issues", opts.policy.ClosedIssues, report.ClosedIssues},
		{"activity rows", opts.policy.Activity, report.ActivityRows},
		{"expired login codes", 0, report.VerificationCodes},
		{"attachment objects", opts.policy.AttachmentGrace, report.AttachmentObjects},
	}
	for _, row := range rows {
		window := "keep forever"
		if row.window > 0 {
			window = row.window.String()
		}
		if row.what == "expired login codes" {
			window = "always"
		}
		fmt.Fprintf(tw, "%s\t%s\t%d\n", row.what, window, row.n)
	}
	if err := tw.Flush(); err != nil {
		return err
	}

	if opts.dryRun {
		fmt.Fprintln(out, "\ndry run: nothing was deleted. The row counts are the FULL backlog, not one batch.")
		return nil
	}
	fmt.Fprintf(out, "\ndeleted one batch (up to %d rows per table). Re-run until the counts reach zero.\n", retention.BatchSize)
	return nil
}

func storeOrNil(store storage.Storage) retention.ObjectStore {
	if store == nil {
		return nil
	}
	return store
}
