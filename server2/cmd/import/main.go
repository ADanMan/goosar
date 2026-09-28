// Command import переносит один воркспейс со старого сервера в server2 (T-025).
//
// Источник читается только через GET-запросы контракта docs/50-api-contract.yaml;
// запись в целевую базу идёт по SQL в одной транзакции, поскольку server2 сам
// ещё не реализует контракт как HTTP API (это отдельная задача, T-026+).
// Что именно переносится и что сознательно пропускается — см.
// docs/51-data-model.md, раздел «Перенос данных», и server2/docs/decisions.md.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"os"
	"os/signal"
	"syscall"

	"github.com/adanman/goosar/server2/internal/importer"
)

const usageHeader = "import --source-url URL --token TOKEN --workspace SLUG_OR_ID --database-url DSN [--migrate] [--dry-run]"

func main() {
	os.Exit(runCLI(os.Args[1:], os.Stdout, os.Stderr))
}

// runCLI разбирает флаги, запускает импорт и печатает JSON-отчёт; вынесен из
// main для тестируемости и для явного кода возврата вместо разбросанных
// os.Exit по телу функции.
func runCLI(args []string, stdout, stderr *os.File) int {
	fs := flag.NewFlagSet("import", flag.ContinueOnError)
	fs.SetOutput(stderr)

	opts := &importer.Options{}
	bindFlags(fs, opts)

	if err := fs.Parse(args); err != nil {
		return 2
	}
	if problem := validate(*opts); problem != "" {
		stderr.WriteString(usageHeader + "\n" + problem + "\n")
		fs.PrintDefaults()
		return 2
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	report, err := importer.Run(ctx, *opts)
	if err != nil {
		stderr.WriteString("import: ошибка: " + err.Error() + "\n")
		return 1
	}
	return printReport(stdout, stderr, report)
}

func bindFlags(fs *flag.FlagSet, opts *importer.Options) {
	fs.StringVar(&opts.SourceURL, "source-url", "", "базовый URL исходного сервера, например http://localhost:8199")
	fs.StringVar(&opts.Token, "token", "", "Bearer PAT/JWT с ролью owner в исходном воркспейсе")
	fs.StringVar(&opts.Workspace, "workspace", "", "slug или id исходного воркспейса")
	fs.StringVar(&opts.DatabaseURL, "database-url", "", "DSN целевой базы server2 (обязателен без --dry-run)")
	fs.BoolVar(&opts.Migrate, "migrate", false, "применить server2/migrations к целевой базе перед импортом")
	fs.BoolVar(&opts.DryRun, "dry-run", false, "только прочитать источник и напечатать сводку, не писать в целевую базу")
	fs.StringVar(&opts.MigrationsDir, "migrations-dir", "server2/migrations", "каталог с NNN_*.up.sql")
}

func validate(opts importer.Options) string {
	missing := map[string]bool{
		"source-url": opts.SourceURL == "",
		"token":      opts.Token == "",
		"workspace":  opts.Workspace == "",
	}
	if opts.DatabaseURL == "" && !opts.DryRun {
		missing["database-url"] = true
	}
	for name, isMissing := range missing {
		if isMissing {
			return "не задан обязательный флаг --" + name
		}
	}
	return ""
}

func printReport(stdout, stderr *os.File, report *importer.Report) int {
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(report); err != nil {
		stderr.WriteString("import: не удалось напечатать отчёт: " + err.Error() + "\n")
		return 1
	}
	return 0
}
