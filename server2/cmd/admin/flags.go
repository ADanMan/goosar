// flags.go — разбор флагов каждой команды goosar_admin, отдельно от
// исполнения (см. main.go): чистые функции без побочных эффектов, чтобы
// unit-тесты (flags_test.go) проверяли разбор без живой БД.
package main

import (
	"flag"
	"fmt"
	"time"
)

// gcUploadsOptions — `gc-uploads [--dry-run] [--grace=168h] [--limit=500]`.
type gcUploadsOptions struct {
	dryRun bool
	grace  time.Duration
	limit  int
}

func parseGCUploadsFlags(args []string, defaultGrace time.Duration) (gcUploadsOptions, error) {
	fs := flag.NewFlagSet("gc-uploads", flag.ContinueOnError)
	opts := gcUploadsOptions{}
	fs.BoolVar(&opts.dryRun, "dry-run", false, "только напечатать список, ничего не удалять")
	fs.DurationVar(&opts.grace, "grace", defaultGrace, "минимальный возраст осиротевшей загрузки (по умолчанию — GOOSAR_UPLOAD_GC_GRACE)")
	fs.IntVar(&opts.limit, "limit", 500, "максимум удаляемых строк за один запуск")
	if err := fs.Parse(args); err != nil {
		return gcUploadsOptions{}, err
	}
	if opts.limit <= 0 {
		return gcUploadsOptions{}, fmt.Errorf("--limit must be positive, got %d", opts.limit)
	}
	return opts, nil
}

// purgeOptions — `purge [--dry-run] [--chat=720h] [--tasks=720h]
// [--closed-issues=8760h] [--activity=720h] [--attachment-grace=168h]`.
type purgeOptions struct {
	dryRun          bool
	chat            time.Duration
	tasks           time.Duration
	closedIssues    time.Duration
	activity        time.Duration
	attachmentGrace time.Duration
}

func parsePurgeFlags(args []string, defaults purgeOptions) (purgeOptions, error) {
	fs := flag.NewFlagSet("purge", flag.ContinueOnError)
	opts := defaults
	fs.BoolVar(&opts.dryRun, "dry-run", false, "только напечатать объём по каждому окну, ничего не удалять")
	fs.DurationVar(&opts.chat, "chat", defaults.chat, "окно хранения чатов")
	fs.DurationVar(&opts.tasks, "tasks", defaults.tasks, "окно хранения задач агентов (dispatch_jobs)")
	fs.DurationVar(&opts.closedIssues, "closed-issues", defaults.closedIssues, "окно хранения закрытых задач")
	fs.DurationVar(&opts.activity, "activity", defaults.activity, "окно хранения активности")
	fs.DurationVar(&opts.attachmentGrace, "attachment-grace", defaults.attachmentGrace, "окно хранения вложений")
	if err := fs.Parse(args); err != nil {
		return purgeOptions{}, err
	}
	return opts, nil
}

// rotateSecretsOptions — `rotate-secrets --mcp [--mfa] [--dry-run]`.
type rotateSecretsOptions struct {
	mcp    bool
	mfa    bool
	dryRun bool
}

func parseRotateSecretsFlags(args []string) (rotateSecretsOptions, error) {
	fs := flag.NewFlagSet("rotate-secrets", flag.ContinueOnError)
	opts := rotateSecretsOptions{}
	fs.BoolVar(&opts.mcp, "mcp", false, "перешифровать значения, запечатанные GOOSAR_MCP_SECRET_KEY")
	fs.BoolVar(&opts.mfa, "mfa", false, "дополнительно покрыть TOTP-секреты")
	fs.BoolVar(&opts.dryRun, "dry-run", false, "только напечатать число затронутых строк")
	if err := fs.Parse(args); err != nil {
		return rotateSecretsOptions{}, err
	}
	if !opts.mcp {
		return rotateSecretsOptions{}, fmt.Errorf("--mcp is required")
	}
	return opts, nil
}

// mcpLibrarySeedOptions — `mcp-library seed [--dry-run]`.
type mcpLibrarySeedOptions struct {
	dryRun bool
}

func parseMcpLibrarySeedFlags(args []string) (mcpLibrarySeedOptions, error) {
	fs := flag.NewFlagSet("mcp-library seed", flag.ContinueOnError)
	opts := mcpLibrarySeedOptions{}
	fs.BoolVar(&opts.dryRun, "dry-run", false, "не записывать, только напечатать план")
	if err := fs.Parse(args); err != nil {
		return mcpLibrarySeedOptions{}, err
	}
	return opts, nil
}
