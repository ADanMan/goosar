package main

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/spf13/cobra"

	"github.com/adanman/goosar/server/internal/cli"
	"github.com/adanman/goosar/server/internal/preflight"
	"github.com/adanman/goosar/server/pkg/redact"
)

var supportBundleCmd = newSupportBundleCmd()

func newSupportBundleCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "support-bundle",
		Short: "Собрать диагностический архив для обращения в поддержку",
		Long: "Собирает версии, отчёт `goosar doctor`, хвосты логов демона, действующую конфигурацию, " +
			"факты об ОС и доступность сервера в один архив tar.gz.\n\n" +
			"Если в окружении есть DATABASE_URL (то есть команда запущена на хосте сервера), в архив " +
			"дополнительно попадают состояние миграций, готовность, размеры таблиц и объём журнала аудита.\n\n" +
			"Все текстовые файлы проходят через маскирование секретов и персональных данных. " +
			"`--dry-run` печатает состав архива, ничего не собирая.",
		RunE:          runSupportBundle,
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	cmd.Flags().String("output", "", "Путь к архиву (по умолчанию goosar-support-<время>.tar.gz в текущем каталоге)")
	cmd.Flags().Int("lines", 500, "Сколько последних строк логов демона включить")
	cmd.Flags().Bool("dry-run", false, "Показать, что будет собрано, и выйти")
	return cmd
}

type supportBundleOptions struct {
	ServerURL   string
	DatabaseURL string
	Profile     string
	DaemonDir   string
	HealthPort  int
	LogLines    int
	Now         time.Time
	HTTP        *http.Client

	Preflight func(context.Context) preflight.Report
}

const bundleUnavailable = "недоступен"

type bundleSection struct {
	File    string
	Note    string
	Collect func(context.Context, supportBundleOptions) string
}

func supportBundleSections(opts supportBundleOptions) []bundleSection {
	sections := []bundleSection{
		{"versions.txt", "версии CLI, демона, сервера (включая min_daemon_version) и desktop", collectVersions},
		{"doctor.json", "отчёт goosar doctor --output json", collectDoctor},
		{"daemon.log.txt", "хвост daemon.log", collectDaemonLog},
		{"daemon.err.log.txt", "хвост daemon.err.log (аварийный вывод)", collectDaemonErrLog},
		{"config.txt", "действующая конфигурация CLI и переменные окружения (секреты замаскированы)", collectConfig},
		{"os.txt", "факты об ОС: платформа, ядра, имя хоста, время", collectOS},
		{"network.txt", "доступность сервера: /healthz, /readyz, /api/config", collectNetwork},
	}
	if opts.DatabaseURL != "" {
		sections = append(sections, bundleSection{
			"server.txt", "состояние БД: миграции, размеры таблиц, объём аудита", collectServer,
		})
	}
	return sections
}

func runSupportBundle(cmd *cobra.Command, _ []string) error {
	профиль := resolveProfile(cmd)
	lines, _ := cmd.Flags().GetInt("lines")
	opts := supportBundleOptions{
		ServerURL:   resolveDaemonServerURL(cmd, профиль),
		DatabaseURL: strings.TrimSpace(os.Getenv("DATABASE_URL")),
		Profile:     профиль,
		DaemonDir:   daemonDirForProfile(профиль),
		HealthPort:  healthPortForProfile(профиль),
		LogLines:    lines,
		Now:         time.Now(),
		HTTP:        &http.Client{Timeout: 5 * time.Second},
		Preflight: func(ctx context.Context) preflight.Report {
			return preflight.Run(ctx, preflight.Options{})
		},
	}

	if dry, _ := cmd.Flags().GetBool("dry-run"); dry {
		printSupportBundlePlan(cmd.OutOrStdout(), opts)
		return nil
	}

	out, _ := cmd.Flags().GetString("output")
	if out == "" {
		out = fmt.Sprintf("goosar-support-%s.tar.gz", opts.Now.UTC().Format("20060102-150405"))
	}

	f, err := os.OpenFile(out, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("не удалось создать %s: %w", out, err)
	}
	defer f.Close()
	if err := writeSupportBundle(cmd.Context(), f, opts); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Архив собран: %s\nПеред отправкой просмотрите manifest.txt — все текстовые файлы прошли маскирование.\n", out)
	return nil
}

func printSupportBundlePlan(w io.Writer, opts supportBundleOptions) {
	fmt.Fprintf(w, "Будет собрано (профиль: %s):\n\n", valueOrDefault(opts.Profile, "по умолчанию"))
	for _, s := range supportBundleSections(opts) {
		fmt.Fprintf(w, "  %-20s %s\n", s.File, s.Note)
	}
	fmt.Fprintf(w, "  %-20s %s\n", "manifest.txt", "перечень файлов с размером и sha256")
	if opts.DatabaseURL == "" {
		fmt.Fprintln(w, "\nПропущено:")
		fmt.Fprintf(w, "  %-20s %s\n", "server.txt", "в окружении нет DATABASE_URL — это не хост сервера")
	}
	fmt.Fprintln(w, "\nВсе текстовые файлы проходят через маскирование (server/pkg/redact).")
}

func writeSupportBundle(ctx context.Context, w io.Writer, opts supportBundleOptions) error {
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}
	if opts.HTTP == nil {
		opts.HTTP = &http.Client{Timeout: 5 * time.Second}
	}
	if opts.LogLines <= 0 {
		opts.LogLines = 500
	}

	sections := supportBundleSections(opts)
	bodies := make([]string, len(sections))
	manifest := &strings.Builder{}
	fmt.Fprintf(manifest, "# goosar support bundle\n")
	fmt.Fprintf(manifest, "generated=%s\n", opts.Now.UTC().Format(time.RFC3339))
	fmt.Fprintf(manifest, "cli_version=%s\n", version)
	fmt.Fprintf(manifest, "files=%d\n\n", len(sections))

	for i, s := range sections {

		body := redact.Text(s.Collect(ctx, opts))
		bodies[i] = body
		sum := sha256.Sum256([]byte(body))
		fmt.Fprintf(manifest, "file=%s size=%d sha256=%x  # %s\n", s.File, len(body), sum, s.Note)
	}

	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)
	write := func(name, body string) error {
		if err := tw.WriteHeader(&tar.Header{
			Name: name, Mode: 0o600, Size: int64(len(body)), ModTime: opts.Now,
		}); err != nil {
			return err
		}
		_, err := io.WriteString(tw, body)
		return err
	}
	if err := write("manifest.txt", manifest.String()); err != nil {
		return err
	}
	for i, s := range sections {
		if err := write(s.File, bodies[i]); err != nil {
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return gz.Close()
}

func collectVersions(ctx context.Context, opts supportBundleOptions) string {
	var b strings.Builder
	fmt.Fprintf(&b, "cli_version   %s\ncli_commit    %s\ncli_built     %s\ngo            %s\nos_arch       %s/%s\n",
		version, commit, date, runtime.Version(), runtime.GOOS, runtime.GOARCH)

	fmt.Fprintf(&b, "\n[демон]\n")
	if opts.HealthPort > 0 {
		hctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		health := checkDaemonHealthOnPort(hctx, opts.HealthPort)
		cancel()
		fmt.Fprintf(&b, "status        %v\nversion       %v\n", health["status"], health["cli_version"])
	} else {
		fmt.Fprintf(&b, "status        не опрошен (порт health не определён)\n")
	}

	fmt.Fprintf(&b, "\n[сервер %s /api/config]\n", valueOrDefault(opts.ServerURL, "(адрес не задан)"))
	body, err := httpGet(ctx, opts, "/api/config")
	if err != nil {
		fmt.Fprintf(&b, "%s: %v\n", bundleUnavailable, err)
	} else {
		fmt.Fprintln(&b, strings.TrimSpace(body))
	}

	fmt.Fprintf(&b, "\n[desktop]\n%s\n", desktopVersion())
	return b.String()
}

func desktopVersion() string {
	candidates := []string{
		"/Applications/Goosar.app/Contents/Info.plist",
		filepath.Join(os.Getenv("HOME"), "Applications", "Goosar.app", "Contents", "Info.plist"),
	}
	for _, p := range candidates {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}

		if _, after, ok := strings.Cut(string(data), "<key>CFBundleShortVersionString</key>"); ok {
			if _, v, ok := strings.Cut(after, "<string>"); ok {
				if v, _, ok := strings.Cut(v, "</string>"); ok {
					return "версия " + strings.TrimSpace(v) + " (" + p + ")"
				}
			}
		}
		return "установлен, версия не прочитана (" + p + ")"
	}
	return "не найден на этой машине"
}

func collectDoctor(ctx context.Context, opts supportBundleOptions) string {
	if opts.Preflight == nil {
		return `{"error":"preflight не выполнен"}`
	}
	data, err := json.MarshalIndent(opts.Preflight(ctx), "", "  ")
	if err != nil {
		return fmt.Sprintf(`{"error":%q}`, err.Error())
	}
	return string(data) + "\n"
}

func collectDaemonLog(_ context.Context, opts supportBundleOptions) string {
	return logTailSection(filepath.Join(opts.DaemonDir, "daemon.log"), opts.LogLines)
}

func collectDaemonErrLog(_ context.Context, opts supportBundleOptions) string {
	return logTailSection(filepath.Join(opts.DaemonDir, "daemon.err.log"), opts.LogLines)
}

func logTailSection(path string, lines int) string {
	if _, err := os.Stat(path); err != nil {
		return fmt.Sprintf("%s\nнет файла: демон не запускался в фоне на этой машине либо логи удалены\n", path)
	}
	tail := readLogTailSince(path, 0, lines)
	if len(tail) == 0 {
		return fmt.Sprintf("%s\nфайл пуст\n", path)
	}
	return path + "\n\n" + strings.Join(tail, "\n") + "\n"
}

var bundleEnvPrefixes = []string{"GOOSAR_", "LOG_", "KRB5", "DATABASE_URL", "HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "APP_ENV"}

var bundleEnvValueAllowlist = map[string]bool{
	"APP_ENV":                   true,
	"LOG_LEVEL":                 true,
	"LOG_FORMAT":                true,
	"HTTP_PROXY":                true,
	"HTTPS_PROXY":               true,
	"NO_PROXY":                  true,
	"KRB5_CONFIG":               true,
	"KRB5CCNAME":                true,
	"GOOSAR_SERVER_URL":         true,
	"GOOSAR_PROFILE":            true,
	"GOOSAR_DELIVERY_PROFILE":   true,
	"GOOSAR_DEPLOYMENT_PROFILE": true,
	"GOOSAR_LOG_LEVEL":          true,
}

func collectConfig(_ context.Context, opts supportBundleOptions) string {
	var b strings.Builder
	path, _ := cli.CLIConfigPathForProfile(opts.Profile)
	fmt.Fprintf(&b, "[конфигурация CLI] %s\n", path)
	cfg, err := cli.LoadCLIConfigForProfile(opts.Profile)
	if err != nil {
		fmt.Fprintf(&b, "не прочитана: %v\n", err)
	} else if data, err := json.MarshalIndent(cfg, "", "  "); err == nil {
		fmt.Fprintln(&b, string(data))
	}

	fmt.Fprintf(&b, "\n[переменные окружения]\n")
	env := os.Environ()
	sort.Strings(env)
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		for _, p := range bundleEnvPrefixes {
			if strings.HasPrefix(name, p) {
				if bundleEnvValueAllowlist[name] {
					fmt.Fprintln(&b, kv)
				} else {
					fmt.Fprintf(&b, "%s=[REDACTED]\n", name)
				}
				break
			}
		}
	}
	return b.String()
}

func collectOS(_ context.Context, opts supportBundleOptions) string {
	host, err := os.Hostname()
	if err != nil {
		host = "неизвестно"
	}
	wd, _ := os.Getwd()
	return fmt.Sprintf("os            %s\narch          %s\ncpus          %d\nhostname      %s\ncwd           %s\ntime_utc      %s\ntime_local    %s\n",
		runtime.GOOS, runtime.GOARCH, runtime.NumCPU(), host, wd,
		opts.Now.UTC().Format(time.RFC3339), opts.Now.Format(time.RFC3339))
}

func collectNetwork(ctx context.Context, opts supportBundleOptions) string {
	var b strings.Builder
	fmt.Fprintf(&b, "server_url    %s\n\n", valueOrDefault(opts.ServerURL, "(не задан)"))
	for _, path := range []string{"/healthz", "/readyz", "/api/config"} {
		start := time.Now()
		body, err := httpGet(ctx, opts, path)
		took := time.Since(start).Round(time.Millisecond)
		if err != nil {
			fmt.Fprintf(&b, "%-14s %s: %v (%s)\n", path, bundleUnavailable, err, took)
			continue
		}
		fmt.Fprintf(&b, "%-14s ok (%s) %s\n", path, took, firstLine(body))
	}
	return b.String()
}

func httpGet(ctx context.Context, opts supportBundleOptions, path string) (string, error) {
	if opts.ServerURL == "" {
		return "", fmt.Errorf("адрес сервера не задан")
	}
	rctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(rctx, http.MethodGet, strings.TrimRight(opts.ServerURL, "/")+path, nil)
	if err != nil {
		return "", err
	}
	resp, err := opts.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return string(data), nil
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	if len(line) > 200 {
		return line[:200] + "…"
	}
	return line
}

func collectServer(ctx context.Context, opts supportBundleOptions) string {
	var b strings.Builder
	fmt.Fprintln(&b, "[readyz]")
	if body, err := httpGet(ctx, opts, "/readyz"); err != nil {
		fmt.Fprintf(&b, "%s: %v\n", bundleUnavailable, err)
	} else {
		fmt.Fprintln(&b, firstLine(body))
	}

	dctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	pool, err := pgxpool.New(dctx, opts.DatabaseURL)
	if err == nil {
		defer pool.Close()
		err = pool.Ping(dctx)
	}
	if err != nil {
		fmt.Fprintf(&b, "\n[база данных]\nБаза данных недоступна: %v\n", err)
		return b.String()
	}

	fmt.Fprintln(&b, "\n[миграции]")
	var applied int64
	var maxVersion string
	if err := pool.QueryRow(dctx,
		`SELECT count(*), coalesce(max(version)::text, '-') FROM schema_migrations`).Scan(&applied, &maxVersion); err != nil {
		fmt.Fprintf(&b, "не прочитано: %v\n", err)
	} else {
		fmt.Fprintf(&b, "applied=%d max_version=%s\n", applied, maxVersion)
	}

	fmt.Fprintln(&b, "\n[размеры таблиц, топ-15]")
	rows, err := pool.Query(dctx, `
		SELECT c.relname, pg_size_pretty(pg_total_relation_size(c.oid)), pg_total_relation_size(c.oid)
		FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'public' AND c.relkind = 'r'
		ORDER BY 3 DESC LIMIT 15`)
	if err != nil {
		fmt.Fprintf(&b, "не прочитано: %v\n", err)
	} else {
		for rows.Next() {
			var name, pretty string
			var size int64
			if err := rows.Scan(&name, &pretty, &size); err != nil {
				break
			}
			fmt.Fprintf(&b, "%-40s %s\n", name, pretty)
		}
		rows.Close()
	}

	fmt.Fprintln(&b, "\n[журнал аудита за последние 24 часа]")
	for _, table := range []string{"auth_audit", "admin_audit"} {
		var count int64

		if err := pool.QueryRow(dctx, fmt.Sprintf(
			`SELECT CASE WHEN to_regclass('public.%s') IS NULL THEN -1
			             ELSE (SELECT count(*) FROM %s WHERE created_at > now() - interval '24 hours') END`,
			table, table)).Scan(&count); err != nil {
			fmt.Fprintf(&b, "%-20s не прочитано: %v\n", table, err)
			continue
		}
		if count < 0 {
			fmt.Fprintf(&b, "%-20s таблицы нет в этой схеме\n", table)
			continue
		}
		fmt.Fprintf(&b, "%-20s %d\n", table, count)
	}
	return b.String()
}
