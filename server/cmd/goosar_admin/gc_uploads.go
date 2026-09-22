package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/adanman/goosar/server/internal/storage"
	"github.com/adanman/goosar/server/internal/uploadgc"
	db "github.com/adanman/goosar/server/pkg/db/generated"
)

func parseGCUploadsFlags(args []string) (uploadgc.Options, error) {
	opts := uploadgc.Options{}
	for _, arg := range args {
		switch {
		case arg == "--dry-run":
			opts.DryRun = true
		case strings.HasPrefix(arg, "--grace="):
			grace, err := time.ParseDuration(strings.TrimPrefix(arg, "--grace="))
			if err != nil {
				return opts, fmt.Errorf("invalid --grace: %w", err)
			}
			if grace <= 0 {
				return opts, errors.New("--grace must be positive; a zero or negative grace would reclaim uploads that are still in flight")
			}
			opts.Grace = grace
		case strings.HasPrefix(arg, "--limit="):
			limit, err := strconv.Atoi(strings.TrimPrefix(arg, "--limit="))
			if err != nil {
				return opts, fmt.Errorf("invalid --limit: %w", err)
			}
			if limit <= 0 {
				return opts, errors.New("--limit must be positive")
			}
			opts.BatchSize = int32(limit)
		default:
			return opts, fmt.Errorf("unknown gc-uploads flag %q", arg)
		}
	}
	return opts, nil
}

func gcUploads(ctx context.Context, queries *db.Queries, out io.Writer, opts uploadgc.Options) error {
	store := storage.FromEnv()
	if store == nil {
		return errors.New("no storage backend configured (set S3_BUCKET or LOCAL_UPLOAD_DIR); refusing to delete attachment rows whose objects would then be unreachable")
	}
	grace := opts.Grace
	if grace == 0 {
		grace = uploadgc.DefaultGrace
	}

	res, err := uploadgc.Sweep(ctx, queries, store, opts)
	if err != nil {
		return err
	}
	for _, url := range res.URLs {
		fmt.Fprintln(out, url)
	}
	if opts.DryRun {
		fmt.Fprintf(out, "dry run: %d orphaned attachment(s) older than %s would be removed\n", res.Found, grace)
	} else {
		fmt.Fprintf(out, "removed %d orphaned attachment(s) older than %s\n", res.Deleted, grace)
	}

	res.Log(slog.Default(), grace, opts.DryRun)
	return nil
}
