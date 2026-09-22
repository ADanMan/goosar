package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	lumberjack "gopkg.in/natefinch/lumberjack.v2"

	"github.com/adanman/goosar/server/internal/cli"
	"github.com/adanman/goosar/server/internal/daemon"
	logger_pkg "github.com/adanman/goosar/server/internal/logger"
	"github.com/adanman/goosar/server/internal/selfexec"
	"github.com/adanman/goosar/server/internal/util"
)

var daemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "Управление локальным демоном сред выполнения агентов",
}

var daemonStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Запустить локальный демон сред выполнения агентов",
	Long:  "Запускает процесс демона, который опрашивает сервер на наличие задач и выполняет их через установленные локально CLI сред выполнения агентов.\nПо умолчанию работает в фоне. Чтобы запустить в текущем терминале, используйте --foreground.",
	RunE:  runDaemonStart,
}

var daemonStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Остановить запущенный демон",
	RunE:  runDaemonStop,
}

var daemonStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Показать статус демона",
	RunE:  runDaemonStatus,
}

var daemonProbeRuntimesCmd = &cobra.Command{
	Use:    "probe-runtimes",
	Short:  "Проверить локально настроенные среды выполнения для приложения Desktop",
	Hidden: true,
	RunE:   runDaemonProbeRuntimes,
}

var daemonRestartCmd = &cobra.Command{
	Use:   "restart",
	Short: "Перезапустить запущенный демон (остановка и запуск)",
	RunE:  runDaemonRestart,
}

var daemonLogsCmd = &cobra.Command{
	Use:   "logs",
	Short: "Показать логи демона",
	RunE:  runDaemonLogs,
}

var daemonDiskUsageCmd = &cobra.Command{
	Use:   "disk-usage",
	Short: "Показать, сколько места на диске занимают рабочие каталоги демона (по задачам или рабочим пространствам)",
	Long: "Обходит корневой каталог рабочих пространств демона и показывает занятое место по задачам или по рабочим пространствам.\n" +
		"По умолчанию выводится вид по задачам, от больших к меньшим. --by-workspace переключает на сводку по рабочим пространствам;\n" +
		"--top N оставляет только N самых больших записей.\n\n" +
		"По умолчанию сканируется только корень текущего профиля. --all-profiles объединяет данные по всем корням\n" +
		"рабочих пространств — корню по умолчанию и каждому корню ~/.goosar/profiles/*, включая отдельный корень\n" +
		"приложения Desktop `desktop-<host>` — и выводит разбивку по корням с общим итогом. В этом режиме --top\n" +
		"применяется внутри каждого корня, а --workspaces-root использовать нельзя.\n\n" +
		"Размер делится на общий и ту часть, которую можно очистить как артефакты (по умолчанию node_modules, .next, .turbo;\n" +
		"меняется через GOOSAR_GC_ARTIFACT_PATTERNS), поэтому отчёт совпадает с тем, что освобождает GC.\n" +
		"Обход пропускает .git и не переходит по символическим ссылкам. Демону не нужно быть запущенным.",
	RunE: runDaemonDiskUsage,
}

func init() {
	f := daemonStartCmd.Flags()
	f.Bool("foreground", false, "Запустить в текущем терминале, а не в фоне")
	f.String("daemon-id", "", "Уникальный идентификатор демона (env: GOOSAR_DAEMON_ID)")
	f.String("device-name", "", "Понятное имя устройства (env: GOOSAR_DAEMON_DEVICE_NAME)")
	f.String("runtime-name", "", "Отображаемое имя среды выполнения (env: GOOSAR_AGENT_RUNTIME_NAME)")
	f.Duration("poll-interval", 0, "Интервал опроса задач (env: GOOSAR_DAEMON_POLL_INTERVAL)")
	f.Duration("heartbeat-interval", 0, "Интервал heartbeat (env: GOOSAR_DAEMON_HEARTBEAT_INTERVAL)")
	f.Duration("agent-timeout", 0, "Предельная длительность одной задачи по часам; 0 — без ограничения, работают только сторожевые таймеры (env: GOOSAR_AGENT_TIMEOUT)")
	f.Duration("runtime-e-semantic-inactivity-timeout", 0, "Таймаут семантической неактивности Runtime E (env: GOOSAR_RUNTIME_E_SEMANTIC_INACTIVITY_TIMEOUT)")
	f.Duration("runtime-e-handshake-timeout", 0, "Таймаут стартового RPC-вызова app-server Runtime E (env: GOOSAR_RUNTIME_E_HANDSHAKE_TIMEOUT)")
	f.Int("max-concurrent-tasks", 0, "Максимум одновременно выполняемых задач (env: GOOSAR_DAEMON_MAX_CONCURRENT_TASKS)")
	f.Bool("no-auto-update", false, "Отключить периодическое автообновление CLI (env: GOOSAR_DAEMON_AUTO_UPDATE=false)")
	f.Duration("auto-update-interval", 0, "Как часто проверять GitHub на наличие нового релиза (env: GOOSAR_DAEMON_AUTO_UPDATE_INTERVAL)")

	daemonLogsCmd.Flags().BoolP("follow", "f", false, "Следить за выводом логов")
	daemonLogsCmd.Flags().IntP("lines", "n", 50, "Сколько строк показать")

	daemonStatusCmd.Flags().String("output", "table", "Формат вывода: table или json")

	daemonStopCmd.Flags().Bool("force", false, "Отменить выполняющиеся задачи, а не дожидаться их завершения")

	rf := daemonRestartCmd.Flags()
	rf.Bool("foreground", false, "Запустить в текущем терминале, а не в фоне")
	rf.String("daemon-id", "", "Уникальный идентификатор демона (env: GOOSAR_DAEMON_ID)")
	rf.String("device-name", "", "Понятное имя устройства (env: GOOSAR_DAEMON_DEVICE_NAME)")
	rf.String("runtime-name", "", "Отображаемое имя среды выполнения (env: GOOSAR_AGENT_RUNTIME_NAME)")
	rf.Duration("poll-interval", 0, "Интервал опроса задач (env: GOOSAR_DAEMON_POLL_INTERVAL)")
	rf.Duration("heartbeat-interval", 0, "Интервал heartbeat (env: GOOSAR_DAEMON_HEARTBEAT_INTERVAL)")
	rf.Duration("agent-timeout", 0, "Предельная длительность одной задачи по часам; 0 — без ограничения, работают только сторожевые таймеры (env: GOOSAR_AGENT_TIMEOUT)")
	rf.Duration("runtime-e-semantic-inactivity-timeout", 0, "Таймаут семантической неактивности Runtime E (env: GOOSAR_RUNTIME_E_SEMANTIC_INACTIVITY_TIMEOUT)")
	rf.Duration("runtime-e-handshake-timeout", 0, "Таймаут стартового RPC-вызова app-server Runtime E (env: GOOSAR_RUNTIME_E_HANDSHAKE_TIMEOUT)")
	rf.Int("max-concurrent-tasks", 0, "Максимум одновременно выполняемых задач (env: GOOSAR_DAEMON_MAX_CONCURRENT_TASKS)")
	rf.Bool("no-auto-update", false, "Отключить периодическое автообновление CLI (env: GOOSAR_DAEMON_AUTO_UPDATE=false)")
	rf.Duration("auto-update-interval", 0, "Как часто проверять GitHub на наличие нового релиза (env: GOOSAR_DAEMON_AUTO_UPDATE_INTERVAL)")

	df := daemonDiskUsageCmd.Flags()
	df.Bool("by-workspace", false, "Сгруппировать вывод по рабочим пространствам, а не по задачам")
	df.Bool("by-task", false, "Вид по задачам (по умолчанию; несовместим с --by-workspace)")
	df.Int("top", 0, "Оставить только N самых больших записей (в режиме --all-profiles — для каждого корня)")
	df.String("output", "table", "Формат вывода: table или json")
	df.String("workspaces-root", "", "Указать другой корневой каталог рабочих пространств (по умолчанию тот же, что у демона)")
	df.Bool("all-profiles", false, "Сканировать все корни рабочих пространств (корень по умолчанию и все ~/.goosar/profiles/*, включая корень приложения Desktop) и показать общий итог")

	daemonCmd.AddCommand(daemonStartCmd)
	daemonCmd.AddCommand(daemonStopCmd)
	daemonCmd.AddCommand(daemonRestartCmd)
	daemonCmd.AddCommand(daemonStatusCmd)
	daemonCmd.AddCommand(daemonProbeRuntimesCmd)
	daemonCmd.AddCommand(daemonLogsCmd)
	daemonCmd.AddCommand(daemonDiskUsageCmd)
}

type daemonRuntimeProbe struct {
	ProbeResult     string         `json:"probe_result"`
	RuntimeCount    int            `json:"runtime_count"`
	ProviderSummary map[string]int `json:"provider_summary"`
}

func runDaemonProbeRuntimes(cmd *cobra.Command, _ []string) error {
	cfg, err := daemon.LoadConfig(daemon.Overrides{
		Profile:       resolveProfile(cmd),
		AllowNoAgents: true,
	})
	if err != nil {
		return err
	}
	probe := daemonRuntimeProbeFromAgents(cfg.Agents)
	return json.NewEncoder(cmd.OutOrStdout()).Encode(probe)
}

func daemonRuntimeProbeFromAgents(agents map[string]daemon.AgentEntry) daemonRuntimeProbe {
	probe := daemonRuntimeProbe{
		ProbeResult:     "success",
		RuntimeCount:    len(agents),
		ProviderSummary: make(map[string]int, len(agents)),
	}
	for provider := range agents {
		probe.ProviderSummary[provider]++
	}
	return probe
}

func daemonDirForProfile(профиль string) string {
	dir, err := cli.ProfileDir(профиль)
	if err != nil {
		return ""
	}
	return dir
}

func daemonPIDPathForProfile(профиль string) string {
	return filepath.Join(daemonDirForProfile(профиль), "daemon.pid")
}

func daemonLogPathForProfile(профиль string) string {
	return filepath.Join(daemonDirForProfile(профиль), "daemon.log")
}

func daemonStderrLogPathForProfile(профиль string) string {
	return filepath.Join(daemonDirForProfile(профиль), "daemon.err.log")
}

const (
	defaultDaemonLogMaxSizeMB  = 20
	defaultDaemonLogMaxBackups = 5
	defaultDaemonLogMaxAgeDays = 30

	errLogMaxBytes = 5 * 1024 * 1024
)

func newDaemonLogRotator(logPath string) *lumberjack.Logger {
	return &lumberjack.Logger{
		Filename:   logPath,
		MaxSize:    envPositiveIntOrDefault("GOOSAR_DAEMON_LOG_MAX_SIZE_MB", defaultDaemonLogMaxSizeMB),
		MaxBackups: envPositiveIntOrDefault("GOOSAR_DAEMON_LOG_MAX_BACKUPS", defaultDaemonLogMaxBackups),
		MaxAge:     envPositiveIntOrDefault("GOOSAR_DAEMON_LOG_MAX_AGE_DAYS", defaultDaemonLogMaxAgeDays),
		Compress:   true,
	}
}

func envPositiveIntOrDefault(key string, def int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return def
	}
	return n
}

func openBoundedErrLog(path string) (*os.File, error) {
	if fi, err := os.Stat(path); err == nil && fi.Size() >= errLogMaxBytes {

		_ = os.Rename(path, path+".1")
	}
	return os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
}

func healthPortForProfile(профиль string) int {
	if профиль == "" {
		return daemon.DefaultHealthPort
	}

	var h int
	for _, b := range []byte(профиль) {
		h += int(b)
	}
	return daemon.DefaultHealthPort + 1 + (h % 1000)
}

var daemonExecutable = selfexec.Resolve

func requireDaemonAuth(профиль string) error {
	cfg, err := cli.LoadCLIConfigForProfile(профиль)
	if err != nil {
		return fmt.Errorf("load CLI config: %w", err)
	}
	if cfg.Token == "" {
		loginHint := "goosar login"
		if профиль != "" {
			loginHint = fmt.Sprintf("goosar login --profile %s", профиль)
		}
		return fmt.Errorf("you are not logged in. Run '%s' first, then start the daemon", loginHint)
	}
	return nil
}

func runDaemonStart(cmd *cobra.Command, _ []string) error {
	foreground, _ := cmd.Flags().GetBool("foreground")
	if foreground {
		return runDaemonForeground(cmd)
	}
	return runDaemonBackground(cmd)
}

func runDaemonBackground(cmd *cobra.Command) error {
	профиль := resolveProfile(cmd)
	healthPort := healthPortForProfile(профиль)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	health := checkDaemonHealthOnPort(ctx, healthPort)
	if daemonAlive(health) {
		label := "daemon"
		if профиль != "" {
			label = fmt.Sprintf("daemon [%s]", профиль)
		}
		pid, _ := health["pid"].(float64)
		return fmt.Errorf("%s is already running (pid %v). Use 'daemon restart' to restart it", label, int(pid))
	}

	if err := requireDaemonAuth(профиль); err != nil {
		return err
	}

	exePath, err := daemonExecutable()
	if err != nil {
		return fmt.Errorf("resolve executable path: %w", err)
	}

	args := buildDaemonStartArgs(cmd)

	dir := daemonDirForProfile(профиль)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create daemon directory: %w", err)
	}

	errLogPath := daemonStderrLogPathForProfile(профиль)
	logFile, err := openBoundedErrLog(errLogPath)
	if err != nil {
		return fmt.Errorf("open log file %s: %w", errLogPath, err)
	}

	logPath := daemonLogPathForProfile(профиль)

	var logOffset int64
	if info, statErr := os.Stat(logPath); statErr == nil {
		logOffset = info.Size()
	}
	var errLogOffset int64
	if info, statErr := os.Stat(errLogPath); statErr == nil {
		errLogOffset = info.Size()
	}

	child := exec.Command(exePath, args...)
	child.Stdout = logFile
	child.Stderr = logFile

	child.SysProcAttr = daemonSysProcAttr(true)

	if err := child.Start(); err != nil {
		if isAccessDeniedSpawnErr(err) {

			child = exec.Command(exePath, args...)
			child.Stdout = logFile
			child.Stderr = logFile
			child.SysProcAttr = daemonSysProcAttr(false)
			if err := child.Start(); err != nil {
				logFile.Close()
				return fmt.Errorf("start daemon (no breakaway): %w", err)
			}
		} else {
			logFile.Close()
			return fmt.Errorf("start daemon: %w", err)
		}
	}
	logFile.Close()
	pid := child.Process.Pid

	waitCh := make(chan error, 1)
	go func() { waitCh <- child.Wait() }()

	pidPath := daemonPIDPathForProfile(профиль)
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(pid)), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not write PID file: %v\n", err)
	}

	const startupTimeout = 45 * time.Second
	deadline := time.Now().Add(startupTimeout)
	started := false
	lastStatus := ""
	for time.Now().Before(deadline) {
		select {
		case waitErr := <-waitCh:
			return daemonStartupFailureError(daemonStartupLogs{
				logPath:      logPath,
				logOffset:    logOffset,
				errLogPath:   errLogPath,
				errLogOffset: errLogOffset,
			}, waitErr, профиль, resolveDaemonServerURL(cmd, профиль))
		case <-time.After(500 * time.Millisecond):
		}
		hctx, hcancel := context.WithTimeout(context.Background(), 2*time.Second)
		health = checkDaemonHealthOnPort(hctx, healthPort)
		hcancel()
		lastStatus, _ = health["status"].(string)
		if lastStatus == "running" {
			started = true
			break
		}
	}
	if !started {
		return daemonStillStartingError(lastStatus, logPath, errLogPath, startupTimeout)
	}

	if профиль != "" {
		fmt.Fprintf(os.Stderr, "Daemon [%s] started (pid %d, version %s)\n", профиль, pid, version)
	} else {
		fmt.Fprintf(os.Stderr, "Daemon started (pid %d, version %s)\n", pid, version)
	}
	fmt.Fprintf(os.Stderr, "Logs: %s\n", logPath)
	return nil
}

func daemonStillStartingError(lastStatus, logPath, errLogPath string, window time.Duration) error {
	var сообщение string
	if lastStatus == "starting" {
		сообщение = fmt.Sprintf("daemon did not confirm readiness within %s (agent detection / workspace sync is taking longer than expected). It may still come up; check:\n  %s", window, logPath)
	} else {
		сообщение = fmt.Sprintf("daemon did not confirm readiness within %s. Check logs:\n  %s\n  %s (crash output)", window, logPath, errLogPath)
	}
	return cli.WithUserMessage(сообщение, &cli.NetworkError{Kind: cli.KindNetworkTimeout, Op: "daemon start", Err: errors.New("daemon readiness timeout")})
}

func resolveDaemonServerURL(cmd *cobra.Command, профиль string) string {
	serverURL := cli.FlagOrEnv(cmd, "server-url", "GOOSAR_SERVER_URL", "")
	if serverURL == "" {
		if c, err := cli.LoadCLIConfigForProfile(профиль); err == nil && c.ServerURL != "" {
			serverURL = c.ServerURL
		}
	}
	return serverURL
}

type daemonStartupLogs struct {
	logPath      string
	logOffset    int64
	errLogPath   string
	errLogOffset int64
}

func daemonStartupFailureError(logs daemonStartupLogs, waitErr error, профиль, serverURL string) error {
	lines := readLogTailSince(logs.logPath, logs.logOffset, 40)
	crashLines := readLogTailSince(logs.errLogPath, logs.errLogOffset, 40)
	joined := strings.Join(append(append([]string{}, lines...), crashLines...), "\n")

	loginHint := "goosar login"
	if профиль != "" {
		loginHint += " --profile " + профиль
	}

	caHint := "goosar setup self-host --ca-file <stand-root-ca.crt>"
	if профиль != "" {
		caHint += " --profile " + профиль
	}

	switch {

	case strings.Contains(joined, "certificate signed by unknown authority") ||
		strings.Contains(joined, "certificate is not trusted"):
		return fmt.Errorf("daemon failed to start: the server's certificate is not trusted on this machine (it is signed by the stand's own CA).\nPass the stand's CA once — '%s' — or install it into the system trust store, then rerun 'goosar daemon start'.\nFull log: %s", caHint, logs.logPath)
	case strings.Contains(joined, "certificate has expired or is not yet valid"):
		return fmt.Errorf("daemon failed to start: the server's certificate is not valid at this machine's current time.\nCheck the system clock (and the certificate's dates if the clock is right), then rerun 'goosar daemon start'.\nFull log: %s", logs.logPath)
	case strings.Contains(joined, "auth token rejected") ||
		strings.Contains(joined, "returned 401") ||
		strings.Contains(joined, "not authenticated"):
		return fmt.Errorf("daemon failed to start: the server rejected your login token (it may have expired or been revoked).\nRun '%s' to sign in again, then rerun 'goosar daemon start'.\nFull log: %s", loginHint, logs.logPath)
	case strings.Contains(joined, "connection refused") ||
		strings.Contains(joined, "no such host") ||
		strings.Contains(joined, "i/o timeout") ||
		strings.Contains(joined, "network is unreachable"):
		target := "the Goosar server"
		if serverURL != "" {
			target += " at " + serverURL
		}
		return fmt.Errorf("daemon failed to start: cannot reach %s.\nMake sure the server is running and reachable, then rerun 'goosar daemon start'.\nFull log: %s", target, logs.logPath)
	}

	var b strings.Builder
	if exitErr := (&exec.ExitError{}); errors.As(waitErr, &exitErr) {
		fmt.Fprintf(&b, "daemon exited during startup (%s).", exitErr)
	} else {
		b.WriteString("daemon exited during startup.")
	}
	excerpt := filterDaemonLogNoise(lines, 5)

	if len(crashLines) > 5 {
		crashLines = crashLines[len(crashLines)-5:]
	}
	excerpt = append(excerpt, crashLines...)
	if len(excerpt) > 0 {
		b.WriteString("\nLog output:")
		for _, line := range excerpt {
			fmt.Fprintf(&b, "\n  %s", line)
		}
	}
	fmt.Fprintf(&b, "\nFull log: %s", logs.logPath)
	if len(crashLines) > 0 {
		fmt.Fprintf(&b, "\nCrash output: %s", logs.errLogPath)
	}
	return errors.New(b.String())
}

func filterDaemonLogNoise(lines []string, max int) []string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.Contains(line, " DBG ") || strings.Contains(line, " INF ") {
			continue
		}
		out = append(out, line)
	}
	if len(out) > max {
		out = out[len(out)-max:]
	}
	return out
}

func readLogTailSince(logPath string, sinceOffset int64, maxLines int) []string {
	f, err := os.Open(logPath)
	if err != nil {
		return nil
	}
	defer f.Close()

	const readCap = 64 * 1024
	if info, err := f.Stat(); err == nil && info.Size()-sinceOffset > readCap {
		sinceOffset = info.Size() - readCap
	}
	if _, err := f.Seek(sinceOffset, io.SeekStart); err != nil {
		return nil
	}
	data, err := io.ReadAll(io.LimitReader(f, readCap))
	if err != nil || len(data) == 0 {
		return nil
	}

	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	out := make([]string, 0, maxLines)
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		out = append(out, line)
	}
	if len(out) > maxLines {
		out = out[len(out)-maxLines:]
	}
	return out
}

func buildDaemonStartArgs(cmd *cobra.Command) []string {
	args := []string{"daemon", "start", "--foreground"}

	if v := flagString(cmd, "daemon-id"); v != "" {
		args = append(args, "--daemon-id", v)
	}
	if v := flagString(cmd, "device-name"); v != "" {
		args = append(args, "--device-name", v)
	}
	if v := flagString(cmd, "runtime-name"); v != "" {
		args = append(args, "--runtime-name", v)
	}
	if d, _ := cmd.Flags().GetDuration("poll-interval"); d > 0 {
		args = append(args, "--poll-interval", d.String())
	}
	if d, _ := cmd.Flags().GetDuration("heartbeat-interval"); d > 0 {
		args = append(args, "--heartbeat-interval", d.String())
	}

	if cmd.Flags().Changed("agent-timeout") {
		d, _ := cmd.Flags().GetDuration("agent-timeout")
		args = append(args, "--agent-timeout", d.String())
	}
	if d, _ := cmd.Flags().GetDuration("runtime-e-semantic-inactivity-timeout"); d > 0 {
		args = append(args, "--runtime-e-semantic-inactivity-timeout", d.String())
	}
	if d, _ := cmd.Flags().GetDuration("runtime-e-handshake-timeout"); d > 0 {
		args = append(args, "--runtime-e-handshake-timeout", d.String())
	}
	if n, _ := cmd.Flags().GetInt("max-concurrent-tasks"); n > 0 {
		args = append(args, "--max-concurrent-tasks", strconv.Itoa(n))
	}
	if b, _ := cmd.Flags().GetBool("no-auto-update"); b {
		args = append(args, "--no-auto-update")
	}
	if d, _ := cmd.Flags().GetDuration("auto-update-interval"); d > 0 {
		args = append(args, "--auto-update-interval", d.String())
	}

	if v, _ := cmd.Flags().GetString("server-url"); v != "" {
		args = append(args, "--server-url", v)
	}
	if v := resolveProfile(cmd); v != "" {
		args = append(args, "--profile", v)
	}

	return args
}

func runDaemonForeground(cmd *cobra.Command) error {
	util.EnsureHiddenConsole()

	профиль := resolveProfile(cmd)

	fileCfg, _ := cli.LoadCLIConfigForProfile(профиль)

	var (
		logger     *slog.Logger
		logRotator *lumberjack.Logger
	)
	if logger_pkg.StderrIsTerminal() {
		logger = logger_pkg.NewLogger("daemon")
	} else {

		repointStdioToErrLog(daemonStderrLogPathForProfile(профиль))
		logRotator = newDaemonLogRotator(daemonLogPathForProfile(профиль))
		defer logRotator.Close()

		logger = logger_pkg.NewWriterLoggerDefault("daemon", logRotator)
	}

	serverURL := resolveDaemonServerURL(cmd, профиль)

	deviceNameFlag := resolveDaemonStringOverride(
		flagString(cmd, "device-name"),
		"GOOSAR_DAEMON_DEVICE_NAME",
		fileCfg.DeviceName,
	)
	runtimeNameFlag := resolveDaemonStringOverride(
		flagString(cmd, "runtime-name"),
		"GOOSAR_AGENT_RUNTIME_NAME",
		fileCfg.RuntimeName,
	)

	overrides := daemon.Overrides{
		ServerURL:   serverURL,
		DaemonID:    flagString(cmd, "daemon-id"),
		DeviceName:  deviceNameFlag,
		RuntimeName: runtimeNameFlag,
		Profile:     профиль,
		HealthPort:  healthPortForProfile(профиль),
	}
	pollFlag, _ := cmd.Flags().GetDuration("poll-interval")
	pollOverride, err := resolveDaemonDurationOverride(pollFlag, "GOOSAR_DAEMON_POLL_INTERVAL", fileCfg.PollInterval)
	if err != nil {
		return err
	}
	if pollOverride > 0 {
		overrides.PollInterval = pollOverride
	}
	heartbeatFlag, _ := cmd.Flags().GetDuration("heartbeat-interval")
	heartbeatOverride, err := resolveDaemonDurationOverride(heartbeatFlag, "GOOSAR_DAEMON_HEARTBEAT_INTERVAL", fileCfg.HeartbeatInterval)
	if err != nil {
		return err
	}
	if heartbeatOverride > 0 {
		overrides.HeartbeatInterval = heartbeatOverride
	}

	agentTimeoutOverride, err := resolveDaemonAgentTimeoutOverride(cmd, "GOOSAR_AGENT_TIMEOUT", fileCfg.AgentTimeout)
	if err != nil {
		return err
	}
	if agentTimeoutOverride != nil {
		overrides.AgentTimeout = agentTimeoutOverride
	}
	semanticFlag, _ := cmd.Flags().GetDuration("runtime-e-semantic-inactivity-timeout")
	semanticOverride, err := resolveDaemonDurationOverride(semanticFlag, "GOOSAR_RUNTIME_E_SEMANTIC_INACTIVITY_TIMEOUT", fileCfg.RuntimeESemanticInactivityTimeout)
	if err != nil {
		return err
	}
	if semanticOverride > 0 {
		overrides.RuntimeESemanticInactivityTimeout = semanticOverride
	}
	handshakeFlag, _ := cmd.Flags().GetDuration("runtime-e-handshake-timeout")
	handshakeOverride, err := resolveDaemonDurationOverride(handshakeFlag, "GOOSAR_RUNTIME_E_HANDSHAKE_TIMEOUT", fileCfg.RuntimeEHandshakeTimeout)
	if err != nil {
		return err
	}
	if handshakeOverride > 0 {
		overrides.RuntimeEHandshakeTimeout = handshakeOverride
	}
	maxFlag, _ := cmd.Flags().GetInt("max-concurrent-tasks")
	if n := resolveDaemonIntOverride(maxFlag, "GOOSAR_DAEMON_MAX_CONCURRENT_TASKS", fileCfg.MaxConcurrentTasks); n > 0 {
		overrides.MaxConcurrentTasks = n
	}

	noAutoUpdateFlag, _ := cmd.Flags().GetBool("no-auto-update")
	if resolveDaemonDisableAutoUpdate(noAutoUpdateFlag, "GOOSAR_DAEMON_AUTO_UPDATE", fileCfg.DisableAutoUpdate) {
		overrides.DisableAutoUpdate = true
	}
	autoUpdateFlag, _ := cmd.Flags().GetDuration("auto-update-interval")
	autoUpdateOverride, err := resolveDaemonDurationOverride(autoUpdateFlag, "GOOSAR_DAEMON_AUTO_UPDATE_INTERVAL", fileCfg.AutoUpdateCheckInterval)
	if err != nil {
		return err
	}
	if autoUpdateOverride > 0 {
		overrides.AutoUpdateCheckInterval = autoUpdateOverride
	}

	cfg, err := daemon.LoadConfig(overrides)
	if err != nil {
		return err
	}
	cfg.CLIVersion = version

	cfg.LaunchedBy = os.Getenv("GOOSAR_LAUNCHED_BY")

	ctx, stop := notifyShutdownContext(context.Background())
	defer stop()

	d := daemon.New(cfg, logger)

	if dir := daemonDirForProfile(профиль); dir != "" {
		os.MkdirAll(dir, 0o755)
		os.WriteFile(daemonPIDPathForProfile(профиль), []byte(strconv.Itoa(os.Getpid())), 0o644)
	}
	defer os.Remove(daemonPIDPathForProfile(профиль))

	if err := d.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}

	if restartBin := d.RestartBinary(); restartBin != "" {
		logger.Info("restarting daemon with updated binary", "path", restartBin)

		if logRotator != nil {
			logger = logger_pkg.NewWriterLoggerDefault("daemon", os.Stderr)
			_ = logRotator.Close()
		}

		args := buildDaemonStartArgs(cmd)
		child := exec.Command(restartBin, args...)

		errLogPath := daemonStderrLogPathForProfile(профиль)
		logFile, err := openBoundedErrLog(errLogPath)
		if err != nil {
			logger.Error("failed to open log file for restart", "error", err)

			return fmt.Errorf("failed to open daemon log file %s for restart: %w", errLogPath, err)
		}
		child.Stdout = logFile
		child.Stderr = logFile

		child.SysProcAttr = daemonSysProcAttr(true)

		if err := child.Start(); err != nil {

			if isAccessDeniedSpawnErr(err) {
				child = exec.Command(restartBin, args...)
				child.Stdout = logFile
				child.Stderr = logFile
				child.SysProcAttr = daemonSysProcAttr(false)
				if err := child.Start(); err != nil {
					logFile.Close()
					logger.Error("failed to start new daemon (no breakaway)", "error", err)
					return fmt.Errorf("failed to start new daemon at %s without breakaway: %w", restartBin, err)
				}
			} else {
				logFile.Close()
				logger.Error("failed to start new daemon", "error", err)
				return fmt.Errorf("failed to start new daemon at %s: %w", restartBin, err)
			}
		}
		logFile.Close()
		child.Process.Release()

		pidPath := daemonPIDPathForProfile(профиль)
		os.WriteFile(pidPath, []byte(strconv.Itoa(child.Process.Pid)), 0o644)

		logger.Info("new daemon started", "pid", child.Process.Pid)
	}

	return nil
}

func requireDaemonRestartPreflight(cmd *cobra.Command, профиль string) error {
	if err := requireDaemonAuth(профиль); err != nil {
		return err
	}
	cfg, err := cli.LoadCLIConfigForProfile(профиль)
	if err != nil {
		return fmt.Errorf("load CLI config: %w", err)
	}

	rawURL := resolveDaemonServerURL(cmd, профиль)
	if rawURL == "" {
		rawURL = daemon.DefaultServerURL
	}
	baseURL, err := daemon.NormalizeServerBaseURL(rawURL)
	if err != nil {
		return fmt.Errorf("refusing to restart: invalid server URL %q: %w", rawURL, err)
	}

	loginHint := "goosar login"
	if профиль != "" {
		loginHint += " --profile " + профиль
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()
	if err := cli.NewAPIClient(baseURL, "", cfg.Token).GetJSON(ctx, "/api/me", nil); err != nil {
		var httpErr *cli.HTTPError
		if errors.As(err, &httpErr) {
			if httpErr.StatusCode == http.StatusUnauthorized {
				return fmt.Errorf("refusing to restart: the server rejected your login token (it may have expired or been revoked); the running daemon was left untouched.\nRun '%s' to sign in again, then rerun 'goosar daemon restart'", loginHint)
			}
			return fmt.Errorf("refusing to restart: preflight check against %s failed (%w); the running daemon was left untouched", baseURL, err)
		}
		return fmt.Errorf("refusing to restart: cannot reach the Goosar server at %s (%w); the running daemon was left untouched.\nMake sure the server is running and reachable, then rerun 'goosar daemon restart'", baseURL, err)
	}
	return nil
}

func runDaemonRestart(cmd *cobra.Command, args []string) error {
	профиль := resolveProfile(cmd)
	healthPort := healthPortForProfile(профиль)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	health := checkDaemonHealthOnPort(ctx, healthPort)
	if daemonAlive(health) {

		if err := requireDaemonRestartPreflight(cmd, профиль); err != nil {
			return err
		}
		pid, _ := health["pid"].(float64)
		if pid > 0 {
			fmt.Fprintf(os.Stderr, "Stopping daemon (pid %d), letting running tasks finish...\n", int(pid))

			if err := requestDaemonShutdown(healthPort, false); err != nil {
				if p, perr := os.FindProcess(int(pid)); perr == nil {
					_ = p.Kill()
				}
			}

			deadline := time.Now().Add(daemonStopWait())
			for time.Now().Before(deadline) {
				time.Sleep(500 * time.Millisecond)
				sctx, scancel := context.WithTimeout(context.Background(), 1*time.Second)
				h := checkDaemonHealthOnPort(sctx, healthPort)
				scancel()
				if !daemonAlive(h) {
					break
				}
			}
		}
	}

	return runDaemonStart(cmd, args)
}

func runDaemonStop(cmd *cobra.Command, _ []string) error {
	профиль := resolveProfile(cmd)
	healthPort := healthPortForProfile(профиль)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	health := checkDaemonHealthOnPort(ctx, healthPort)
	if !daemonAlive(health) {
		label := "Daemon"
		if профиль != "" {
			label = fmt.Sprintf("Daemon [%s]", профиль)
		}
		fmt.Fprintf(os.Stderr, "%s is not running.\n", label)
		return nil
	}

	pid, ok := health["pid"].(float64)
	if !ok || pid == 0 {
		return fmt.Errorf("could not determine daemon PID from health endpoint")
	}

	process, err := os.FindProcess(int(pid))
	if err != nil {
		return fmt.Errorf("find process %d: %w", int(pid), err)
	}

	force, _ := cmd.Flags().GetBool("force")
	if err := requestDaemonShutdown(healthPort, force); err != nil {
		fmt.Fprintf(os.Stderr, "Graceful shutdown request failed: %v — falling back to forced kill.\n", err)
		if kerr := process.Kill(); kerr != nil {
			return fmt.Errorf("kill daemon (pid %d): %w", int(pid), kerr)
		}
	}

	if force {
		fmt.Fprintf(os.Stderr, "Stopping daemon (pid %d), cancelling running tasks...\n", int(pid))
	} else {
		fmt.Fprintf(os.Stderr, "Stopping daemon (pid %d), letting running tasks finish (--force to cancel them)...\n", int(pid))
	}

	deadline := time.Now().Add(daemonStopWait())
	var lastReport time.Time
	for time.Now().Before(deadline) {
		time.Sleep(500 * time.Millisecond)
		ctx2, cancel2 := context.WithTimeout(context.Background(), 1*time.Second)
		h := checkDaemonHealthOnPort(ctx2, healthPort)
		cancel2()
		if !daemonAlive(h) {
			os.Remove(daemonPIDPathForProfile(профиль))
			fmt.Fprintln(os.Stderr, "Daemon stopped.")
			return nil
		}
		if active, ok := h["active_task_count"].(float64); ok && active > 0 && time.Since(lastReport) >= 5*time.Second {
			fmt.Fprintf(os.Stderr, "Waiting for %d running task(s) to finish...\n", int(active))
			lastReport = time.Now()
		}
	}

	fmt.Fprintln(os.Stderr, "Daemon is still stopping. It may be finishing a running task; re-run with --force to cancel it.")
	return nil
}

func daemonStopWait() time.Duration {
	grace := daemon.DefaultDrainTimeout
	if raw := strings.TrimSpace(os.Getenv("GOOSAR_DAEMON_DRAIN_TIMEOUT")); raw != "" {
		if v, err := time.ParseDuration(raw); err == nil && v > 0 {
			grace = v
		}
	}
	return grace + 30*time.Second
}

func requestDaemonShutdown(healthPort int, force bool) error {
	url := fmt.Sprintf("http://127.0.0.1:%d/shutdown", healthPort)
	if force {
		url += "?force=1"
	}
	req, err := http.NewRequest(http.MethodPost, url, nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	return nil
}

func runDaemonStatus(cmd *cobra.Command, _ []string) error {
	профиль := resolveProfile(cmd)
	healthPort := healthPortForProfile(профиль)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	health := checkDaemonHealthOnPort(ctx, healthPort)

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, health)
	}

	label := "Daemon"
	if профиль != "" {
		label = fmt.Sprintf("Daemon [%s]", профиль)
	}

	switch health["status"] {
	case "running":
		printDaemonStatusReport(os.Stdout, label, health)
	case "starting":
		fmt.Fprintf(os.Stdout, "%s: starting (pid %v)\n", label, health["pid"])
	default:
		fmt.Fprintf(os.Stdout, "%s: stopped\n", label)
	}
	return nil
}

func printDaemonStatusReport(w io.Writer, label string, health map[string]any) {
	type row struct{ key, value string }
	rows := []row{
		{label, fmt.Sprintf("running (pid %v, uptime %v)", health["pid"], health["uptime"])},
	}
	if version, ok := health["cli_version"].(string); ok && version != "" {
		rows = append(rows, row{"Version", version})
	}
	if agents, ok := health["agents"].([]any); ok && len(agents) > 0 {
		parts := make([]string, len(agents))
		for i, a := range agents {
			parts[i] = fmt.Sprint(a)
		}
		rows = append(rows, row{"Agents", strings.Join(parts, ", ")})
	}
	if ws, ok := health["workspaces"].([]any); ok {
		rows = append(rows, row{"Workspaces", strconv.Itoa(len(ws))})
	}

	keyWidth := 0
	for _, r := range rows {
		if n := len(r.key); n > keyWidth {
			keyWidth = n
		}
	}
	for _, r := range rows {
		fmt.Fprintf(w, "%-*s  %s\n", keyWidth+1, r.key+":", r.value)
	}
}

func runDaemonLogs(cmd *cobra.Command, _ []string) error {
	профиль := resolveProfile(cmd)
	logPath := daemonLogPathForProfile(профиль)
	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		return fmt.Errorf("no log file found at %s\nThe daemon may not have been started in background mode", logPath)
	}

	follow, _ := cmd.Flags().GetBool("follow")
	lines, _ := cmd.Flags().GetInt("lines")

	return tailLogFile(logPath, lines, follow)
}

func daemonAlive(health map[string]any) bool {
	switch health["status"] {
	case "running", "starting":
		return true
	default:
		return false
	}
}

func checkDaemonHealthOnPort(ctx context.Context, port int) map[string]any {
	addr := fmt.Sprintf("http://127.0.0.1:%d/health", port)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, addr, nil)
	if err != nil {
		return map[string]any{"status": "stopped"}
	}

	httpClient := &http.Client{Timeout: 2 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		return map[string]any{"status": "stopped"}
	}
	defer resp.Body.Close()

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return map[string]any{"status": "stopped"}
	}
	return result
}

func flagString(cmd *cobra.Command, name string) string {
	val, _ := cmd.Flags().GetString(name)
	return val
}

func envUnset(name string) bool {
	return strings.TrimSpace(os.Getenv(name)) == ""
}

func resolveDaemonStringOverride(flagValue, envName, cfgValue string) string {
	if flagValue != "" {
		return flagValue
	}
	if !envUnset(envName) {

		return ""
	}
	return cfgValue
}

func resolveDaemonDurationOverride(flagValue time.Duration, envName, cfgValue string) (time.Duration, error) {
	if flagValue > 0 {
		return flagValue, nil
	}
	if !envUnset(envName) || cfgValue == "" {
		return 0, nil
	}
	parsed, err := time.ParseDuration(cfgValue)
	if err != nil {
		return 0, fmt.Errorf("config value %q for %s is not a valid duration: %w", cfgValue, envName, err)
	}
	if parsed <= 0 {
		return 0, fmt.Errorf("config value %q for %s must be positive", cfgValue, envName)
	}
	return parsed, nil
}

func resolveDaemonIntOverride(flagValue int, envName string, cfgValue int) int {
	if flagValue > 0 {
		return flagValue
	}
	if !envUnset(envName) {
		return 0
	}
	if cfgValue > 0 {
		return cfgValue
	}
	return 0
}

func resolveDaemonAgentTimeoutOverride(cmd *cobra.Command, envName string, cfgValue *string) (*time.Duration, error) {
	if cmd.Flags().Changed("agent-timeout") {
		d, _ := cmd.Flags().GetDuration("agent-timeout")
		return &d, nil
	}
	if !envUnset(envName) {
		return nil, nil
	}
	if cfgValue == nil || *cfgValue == "" {
		return nil, nil
	}
	parsed, err := time.ParseDuration(strings.TrimSpace(*cfgValue))
	if err != nil {
		return nil, fmt.Errorf("config value %q for %s is not a valid duration: %w", *cfgValue, envName, err)
	}
	if parsed < 0 {
		return nil, fmt.Errorf("config value %q for %s must be >= 0", *cfgValue, envName)
	}
	return &parsed, nil
}

func resolveDaemonDisableAutoUpdate(flagValue bool, envName string, cfgValue bool) bool {
	if flagValue {
		return true
	}
	if v := strings.TrimSpace(os.Getenv(envName)); v != "" {
		switch strings.ToLower(v) {
		case "false", "0", "no", "off":
			return true
		}

		return false
	}
	return cfgValue
}

func runDaemonDiskUsage(cmd *cobra.Command, _ []string) error {
	профиль := resolveProfile(cmd)
	rootOverride, _ := cmd.Flags().GetString("workspaces-root")
	byWorkspace, _ := cmd.Flags().GetBool("by-workspace")
	byTask, _ := cmd.Flags().GetBool("by-task")
	top, _ := cmd.Flags().GetInt("top")
	output, _ := cmd.Flags().GetString("output")
	allProfiles, _ := cmd.Flags().GetBool("all-profiles")

	if byWorkspace && byTask {
		return fmt.Errorf("--by-workspace and --by-task are mutually exclusive")
	}
	if top < 0 {
		return fmt.Errorf("--top must be a non-negative integer")
	}
	if allProfiles && rootOverride != "" {
		return fmt.Errorf("--all-profiles and --workspaces-root are mutually exclusive")
	}

	if allProfiles {
		return runDaemonDiskUsageAggregate(byWorkspace, top, output)
	}

	workspacesRoot, err := daemon.ResolveWorkspacesRoot(профиль, rootOverride)
	if err != nil {
		return fmt.Errorf("resolve workspaces root: %w", err)
	}

	report, err := daemon.ScanDiskUsage(workspacesRoot, daemon.ArtifactPatternsFromEnv())
	if err != nil {
		return err
	}

	if top > 0 {
		if byWorkspace {
			if top < len(report.Workspaces) {
				report.Workspaces = report.Workspaces[:top]
			}
		} else if top < len(report.Tasks) {
			report.Tasks = report.Tasks[:top]
		}
	}

	if output == "json" {
		return cli.PrintJSON(os.Stdout, report)
	}

	if byWorkspace {
		printDiskUsageWorkspaceTable(os.Stdout, report)
		printDiskUsageOtherRootsHint(os.Stdout, report, профиль, rootOverride)
		return nil
	}
	printDiskUsageTaskTable(os.Stdout, report)
	printDiskUsageOtherRootsHint(os.Stdout, report, профиль, rootOverride)
	return nil
}

func runDaemonDiskUsageAggregate(byWorkspace bool, top int, output string) error {
	roots, err := enumerateDiskUsageRoots()
	if err != nil {
		return err
	}
	agg, err := daemon.ScanDiskUsageRoots(roots, daemon.ArtifactPatternsFromEnv())
	if err != nil {
		return err
	}

	if top > 0 {
		for i := range agg.Roots {
			r := &agg.Roots[i].Report
			if byWorkspace {
				if top < len(r.Workspaces) {
					r.Workspaces = r.Workspaces[:top]
				}
			} else if top < len(r.Tasks) {
				r.Tasks = r.Tasks[:top]
			}
		}
	}

	if output == "json" {
		return cli.PrintJSON(os.Stdout, agg)
	}
	printAggregateDiskUsage(os.Stdout, agg, byWorkspace)
	return nil
}

func enumerateDiskUsageRoots() ([]daemon.DiskUsageRoot, error) {
	seen := map[string]bool{}
	out := make([]daemon.DiskUsageRoot, 0)

	if root, err := daemon.ResolveWorkspacesRoot("", ""); err == nil {
		out = append(out, daemon.DiskUsageRoot{Profile: "", Root: root})
		seen[root] = true
	}

	profilesRoot, err := profilesRootDir()
	if err != nil {
		return out, nil
	}
	entries, err := os.ReadDir(profilesRoot)
	if err != nil {
		return out, nil
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		root, err := daemon.ResolveWorkspacesRoot(name, "")
		if err != nil || seen[root] {
			continue
		}

		if info, statErr := os.Stat(root); statErr != nil || !info.IsDir() {
			continue
		}
		seen[root] = true
		out = append(out, daemon.DiskUsageRoot{Profile: name, Root: root})
	}
	return out, nil
}

func printAggregateDiskUsage(w io.Writer, agg daemon.AggregateDiskUsageReport, byWorkspace bool) {
	fmt.Fprintf(w, "Scanned %d workspace root(s).\n", len(agg.Roots))
	for _, root := range agg.Roots {
		fmt.Fprintln(w)
		label := "default"
		if root.Profile != "" {
			label = root.Profile
		}
		fmt.Fprintf(w, "[%s]\n", label)
		if byWorkspace {
			printDiskUsageWorkspaceTable(w, root.Report)
		} else {
			printDiskUsageTaskTable(w, root.Report)
		}
	}
	fmt.Fprintf(w, "\nGrand total: %s across %d task(s) in %d root(s); %s reclaimable as artifacts (%.1f%%).\n",
		formatBytes(agg.TotalSizeBytes), agg.TotalTaskCount, len(agg.Roots),
		formatBytes(agg.TotalArtifactSizeBytes), agg.TotalArtifactRatio*100)
}

func printDiskUsageTaskTable(w io.Writer, report daemon.DiskUsageReport) {
	fmt.Fprintf(w, "Workspaces root: %s\n", report.WorkspacesRoot)
	if report.TotalTaskCount == 0 {
		fmt.Fprintln(w, "(no task directories)")
		return
	}
	rows := make([][]string, 0, len(report.Tasks))
	var displayedSize, displayedArtifact int64
	for _, задача := range report.Tasks {
		displayedSize += задача.SizeBytes
		displayedArtifact += задача.ArtifactSizeBytes
		rows = append(rows, []string{
			задача.WorkspaceShort + "/" + задача.TaskShort,
			задача.Kind,
			emptyDash(задача.ParentStatus),
			formatAge(задача.AgeSeconds),
			formatBytes(задача.SizeBytes),
			formatBytes(задача.ArtifactSizeBytes),
		})
	}
	cli.PrintTable(w, []string{"PATH", "KIND", "STATUS", "AGE", "SIZE", "ARTIFACTS"}, rows)

	if len(report.Tasks) < report.TotalTaskCount {

		fmt.Fprintf(w, "\nShowing top %d of %d task(s). Displayed: %s (%s artifacts). Scan total: %s (%s artifacts, %.1f%% reclaimable).\n",
			len(report.Tasks), report.TotalTaskCount,
			formatBytes(displayedSize), formatBytes(displayedArtifact),
			formatBytes(report.TotalSizeBytes), formatBytes(report.TotalArtifactSizeBytes),
			report.TotalArtifactRatio*100)
		return
	}
	fmt.Fprintf(w, "\nTotal: %s across %d task(s); %s reclaimable as artifacts (%.1f%%).\n",
		formatBytes(report.TotalSizeBytes), report.TotalTaskCount,
		formatBytes(report.TotalArtifactSizeBytes), report.TotalArtifactRatio*100)
}

func printDiskUsageWorkspaceTable(w io.Writer, report daemon.DiskUsageReport) {
	fmt.Fprintf(w, "Workspaces root: %s\n", report.WorkspacesRoot)
	if report.TotalWorkspaceCount == 0 {
		fmt.Fprintln(w, "(no workspaces)")
		return
	}
	rows := make([][]string, 0, len(report.Workspaces))
	var displayedSize, displayedArtifact int64
	for _, ws := range report.Workspaces {
		displayedSize += ws.SizeBytes
		displayedArtifact += ws.ArtifactSizeBytes
		rows = append(rows, []string{
			ws.WorkspaceShort,
			strconv.Itoa(ws.TaskCount),
			formatBytes(ws.SizeBytes),
			formatBytes(ws.ArtifactSizeBytes),
			formatRatio(ws.ArtifactRatio),
			formatAge(ws.OldestAgeSeconds),
		})
	}
	cli.PrintTable(w, []string{"WORKSPACE", "TASKS", "SIZE", "ARTIFACTS", "ARTIFACT %", "OLDEST"}, rows)

	if len(report.Workspaces) < report.TotalWorkspaceCount {
		fmt.Fprintf(w, "\nShowing top %d of %d workspace(s). Displayed: %s (%s artifacts). Scan total: %s (%s artifacts, %.1f%% reclaimable).\n",
			len(report.Workspaces), report.TotalWorkspaceCount,
			formatBytes(displayedSize), formatBytes(displayedArtifact),
			formatBytes(report.TotalSizeBytes), formatBytes(report.TotalArtifactSizeBytes),
			report.TotalArtifactRatio*100)
		return
	}
	fmt.Fprintf(w, "\nTotal: %s across %d workspace(s); %s reclaimable as artifacts (%.1f%%).\n",
		formatBytes(report.TotalSizeBytes), report.TotalWorkspaceCount,
		formatBytes(report.TotalArtifactSizeBytes), report.TotalArtifactRatio*100)
}

func printDiskUsageOtherRootsHint(w io.Writer, report daemon.DiskUsageReport, профиль, rootOverride string) {
	if rootOverride != "" {
		return
	}
	suggestions := diskUsageProfileSuggestions(профиль, report.WorkspacesRoot)
	if len(suggestions) == 0 {
		return
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Other workspace roots contain task directories:")
	for _, s := range suggestions {
		fmt.Fprintf(w, "  %s  # %s (%d task%s)\n",
			s.Command, s.Root, s.TaskCount, pluralS(s.TaskCount))
	}
	fmt.Fprintln(w, "Run 'goosar daemon disk-usage --all-profiles' for a combined total across all roots.")
}

type diskUsageProfileSuggestion struct {
	Profile   string
	Command   string
	Root      string
	TaskCount int
}

func diskUsageProfileSuggestions(currentProfile, currentRoot string) []diskUsageProfileSuggestion {
	out := make([]diskUsageProfileSuggestion, 0)
	if currentProfile != "" {
		if root, err := daemon.ResolveWorkspacesRoot("", ""); err == nil && !samePath(root, currentRoot) {
			if taskCount := countDiskUsageTaskDirs(root); taskCount > 0 {
				out = append(out, diskUsageProfileSuggestion{
					Profile:   "",
					Command:   "goosar daemon disk-usage",
					Root:      root,
					TaskCount: taskCount,
				})
			}
		}
	}

	profilesRoot, err := profilesRootDir()
	if err != nil {
		return out
	}
	entries, err := os.ReadDir(profilesRoot)
	if err != nil {
		return out
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		профиль := entry.Name()
		if профиль == currentProfile {
			continue
		}
		root, err := daemon.ResolveWorkspacesRoot(профиль, "")
		if err != nil || samePath(root, currentRoot) {
			continue
		}
		taskCount := countDiskUsageTaskDirs(root)
		if taskCount == 0 {
			continue
		}
		out = append(out, diskUsageProfileSuggestion{
			Profile:   профиль,
			Command:   "goosar --profile " + shellQuoteArg(профиль) + " daemon disk-usage",
			Root:      root,
			TaskCount: taskCount,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].TaskCount == out[j].TaskCount {
			return out[i].Profile < out[j].Profile
		}
		return out[i].TaskCount > out[j].TaskCount
	})
	const maxSuggestions = 5
	if len(out) > maxSuggestions {
		out = out[:maxSuggestions]
	}
	return out
}

func shellQuoteArg(s string) string {
	if s == "" {
		return "''"
	}
	if strings.IndexFunc(s, func(r rune) bool {
		return !(r == '-' || r == '_' || r == '.' || r == '/' ||
			r >= '0' && r <= '9' ||
			r >= 'A' && r <= 'Z' ||
			r >= 'a' && r <= 'z')
	}) == -1 {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

func countDiskUsageTaskDirs(root string) int {
	wsEntries, err := os.ReadDir(root)
	if err != nil {
		return 0
	}
	count := 0
	for _, wsEntry := range wsEntries {
		if !wsEntry.IsDir() || wsEntry.Name() == ".repos" {
			continue
		}
		taskEntries, err := os.ReadDir(filepath.Join(root, wsEntry.Name()))
		if err != nil {
			continue
		}
		for _, taskEntry := range taskEntries {
			if taskEntry.IsDir() {
				count++
			}
		}
	}
	return count
}

func profilesRootDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".goosar", "profiles"), nil
}

func samePath(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	aa, errA := filepath.Abs(a)
	bb, errB := filepath.Abs(b)
	if errA != nil || errB != nil {
		return a == b
	}
	return aa == bb
}

func pluralS(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func formatRatio(r float64) string {
	if r != r || r < 0 {
		return "0.0%"
	}
	return fmt.Sprintf("%.1f%%", r*100)
}

func emptyDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	prefix := "KMGTPE"[exp]
	return fmt.Sprintf("%.1f %ciB", float64(b)/float64(div), prefix)
}

func formatAge(seconds int64) string {
	if seconds <= 0 {
		return "0s"
	}
	d := time.Duration(seconds) * time.Second
	switch {
	case d >= 24*time.Hour:
		return fmt.Sprintf("%dd %dh", int(d/(24*time.Hour)), int((d%(24*time.Hour))/time.Hour))
	case d >= time.Hour:
		return fmt.Sprintf("%dh %dm", int(d/time.Hour), int((d%time.Hour)/time.Minute))
	case d >= time.Minute:
		return fmt.Sprintf("%dm %ds", int(d/time.Minute), int((d%time.Minute)/time.Second))
	default:
		return fmt.Sprintf("%ds", seconds)
	}
}
