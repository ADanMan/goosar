// Пакет uploadgc удаляет осиротевшие вложения: файл загружается до появления
// владельца (задачи, комментария, сообщения), и если второй шаг не случился,
// запись остаётся без привязки и подлежит сборке.
package uploadgc

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/adanman/goosar/server/pkg/db/generated"
)

type ObjectStore interface {
	KeyFromURL(rawURL string) string
	DeleteKeys(ctx context.Context, keys []string)
}

const (
	DefaultGrace = 7 * 24 * time.Hour

	DefaultBatchSize = 500
)

var ErrNoStorage = errors.New("uploadgc: no storage backend configured")

type Options struct {
	Grace time.Duration

	BatchSize int32

	DryRun bool
}

type Result struct {
	Found int

	Deleted int64

	URLs []string
}

func Sweep(ctx context.Context, queries *db.Queries, store ObjectStore, opts Options) (Result, error) {
	grace := opts.Grace
	if grace == 0 {
		grace = DefaultGrace
	}
	if grace < 0 {
		return Result{}, fmt.Errorf("uploadgc: negative grace %s", grace)
	}
	batch := opts.BatchSize
	if batch <= 0 {
		batch = DefaultBatchSize
	}
	if store == nil {
		return Result{}, ErrNoStorage
	}

	rows, err := queries.ListOrphanAttachments(ctx, db.ListOrphanAttachmentsParams{
		GraceSecs: grace.Seconds(),
		MaxRows:   batch,
	})
	if err != nil {
		return Result{}, fmt.Errorf("uploadgc: list orphans: %w", err)
	}
	res := Result{Found: len(rows)}
	if len(rows) == 0 {
		return res, nil
	}

	ids := make([]pgtype.UUID, 0, len(rows))
	keys := make([]string, 0, len(rows))
	for _, row := range rows {
		res.URLs = append(res.URLs, row.Url)
		ids = append(ids, row.ID)
		if key := store.KeyFromURL(row.Url); key != "" {
			keys = append(keys, key)
		}
	}
	if opts.DryRun {
		return res, nil
	}

	store.DeleteKeys(ctx, keys)
	deleted, err := queries.DeleteAttachmentsByIDs(ctx, ids)
	if err != nil {
		return res, fmt.Errorf("uploadgc: delete rows: %w", err)
	}
	res.Deleted = deleted
	return res, nil
}

func (r Result) Log(logger *slog.Logger, grace time.Duration, dryRun bool) {
	if r.Found == 0 {
		return
	}
	logger.Info("upload GC: reclaimed orphaned attachments",
		"found", r.Found,
		"deleted", r.Deleted,
		"grace", grace.String(),
		"dry_run", dryRun)
}
