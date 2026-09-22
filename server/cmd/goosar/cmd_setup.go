package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/adanman/goosar/server/internal/cli"
)

var setupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Настроить CLI, войти в аккаунт и запустить демон",
	Long: `Настраивает CLI для подключения к Goosar Cloud (goosar.ru), затем
выполняет вход через браузер и запускает демон агентов.

Если конфигурация уже есть, перед перезаписью команда спросит подтверждение.

Чтобы подключиться к собственному серверу, используйте 'goosar setup self-host'.

Если вы запускаете команду по SSH на удалённой машине, оставьте localhost в
адресе обратного вызова и следуйте подсказке про SSH-туннель, которая
выводится при входе через браузер. Если браузер может обратиться к этому CLI
напрямую по адресу в частной сети, передайте --callback-host <host-or-ip>.

Чтобы создать изолированную конфигурацию для отдельного окружения, используйте --profile:
  goosar setup self-host --profile staging --server-url https://api-staging.co`,
	RunE: runSetupCloud,
}

var setupCloudCmd = &cobra.Command{
	Use:   "cloud",
	Short: "Настроить CLI для Goosar Cloud (goosar.ru)",
	Long: `Явно настраивает CLI для подключения к Goosar Cloud (goosar.ru).

Если вы запускаете команду по SSH на удалённой машине, оставьте localhost в
адресе обратного вызова и следуйте подсказке про SSH-туннель, которая
выводится при входе через браузер. Если браузер может обратиться к этому CLI
напрямую по адресу в частной сети, передайте --callback-host <host-or-ip>.

Это то же самое, что запустить 'goosar setup' без подкоманды.`,
	RunE: runSetupCloud,
}

var setupSelfHostCmd = &cobra.Command{
	Use:   "self-host",
	Short: "Настроить CLI для собственного сервера Goosar",
	Long: `Настраивает CLI для подключения к собственному серверу Goosar.

По умолчанию подключается к http://localhost:8080 (бэкенд) и http://localhost:3000 (фронтенд).
Чтобы указать другой сервер (например, развёрнутый в вашей инфраструктуре), используйте --server-url и --app-url.

Если вы запускаете команду не на той машине, где работает сервер, передайте
также --callback-host <host-or-ip-the-browser-can-reach-back-to-this-machine-on>,
чтобы при входе через OAuth токен вернулся в CLI.

Если сервер завершает TLS с сертификатом собственного внутреннего центра
сертификации, этому сертификату нужно доверять: передайте --ca-file один раз,
и он сохранится в профиле — вход и демон больше не потребуют его снова.

Примеры:
  goosar setup self-host
  goosar setup self-host --server-url https://api.internal.co --app-url https://app.internal.co
  goosar setup self-host --server-url https://stand.local --app-url https://stand.local --ca-file ./stand-root-ca.crt
  goosar setup self-host --port 9090 --frontend-port 4000`,
	RunE: runSetupSelfHost,
}

func init() {
	setupCmd.Flags().String(callbackHostFlag, "", callbackHostFlagHelp)
	setupCloudCmd.Flags().String(callbackHostFlag, "", callbackHostFlagHelp)
	for _, c := range []*cobra.Command{setupCmd, setupCloudCmd, setupSelfHostCmd} {
		c.Flags().Bool("yes", false, "Не спрашивать подтверждение перед заменой существующей конфигурации (для скриптов; закрытый stdin без --yes — ошибка)")
	}

	setupSelfHostCmd.Flags().String("server-url", "", "URL бэкенда (например, https://api.internal.co) (env: GOOSAR_SERVER_URL)")
	setupSelfHostCmd.Flags().String("app-url", "", "URL веб-приложения (например, https://app.internal.co) (env: GOOSAR_APP_URL)")
	setupSelfHostCmd.Flags().Int("port", 8080, "Порт бэкенда (используется, если не задан --server-url)")
	setupSelfHostCmd.Flags().Int("frontend-port", 3000, "Порт фронтенда (используется, если не задан --app-url)")
	setupSelfHostCmd.Flags().String(callbackHostFlag, "", callbackHostFlagHelp)

	setupCmd.AddCommand(setupCloudCmd)
	setupCmd.AddCommand(setupSelfHostCmd)
}

func printConfigLocation(профиль string) {
	path, err := cli.CLIConfigPathForProfile(профиль)
	if err != nil {
		return
	}
	if профиль != "" {
		fmt.Fprintf(os.Stderr, "  profile:    %s\n", профиль)
	}
	fmt.Fprintf(os.Stderr, "  config:     %s\n", path)
}

func confirmOverwrite(cmd *cobra.Command, профиль, newServerURL, newAppURL string) (bool, error) {
	yes, _ := cmd.Flags().GetBool("yes")
	return confirmOverwriteFrom(os.Stdin, профиль, newServerURL, newAppURL, yes)
}

func confirmOverwriteFrom(in io.Reader, профиль, newServerURL, newAppURL string, yes bool) (bool, error) {
	cfg, err := cli.LoadCLIConfigForProfile(профиль)
	if err != nil {
		return true, nil
	}
	if cfg.ServerURL == "" {
		return true, nil
	}
	if yes {
		return true, nil
	}

	fmt.Fprintln(os.Stderr, "Current configuration:")
	fmt.Fprintf(os.Stderr, "  server_url: %s\n", formatURLChange(cfg.ServerURL, newServerURL))
	fmt.Fprintf(os.Stderr, "  app_url:    %s\n", formatURLChange(cfg.AppURL, newAppURL))
	if cfg.WorkspaceID != "" {
		fmt.Fprintf(os.Stderr, "  workspace:  %s\n", cfg.WorkspaceID)
	}
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprint(os.Stderr, "This will reset your configuration. Continue? [y/N] ")

	reader := bufio.NewReader(in)
	answer, readErr := reader.ReadString('\n')
	if readErr != nil && strings.TrimSpace(answer) == "" {
		return false, fmt.Errorf("no answer on stdin — running without a terminal? Pass --yes to replace the existing configuration")
	}
	answer = strings.TrimSpace(strings.ToLower(answer))
	if answer != "y" && answer != "yes" {
		fmt.Fprintln(os.Stderr, "Aborted.")
		return false, nil
	}
	return true, nil
}

func formatURLChange(oldVal, newVal string) string {
	if newVal != "" && newVal != oldVal {
		return fmt.Sprintf("%s  ->  %s", oldVal, newVal)
	}
	return oldVal
}

func runSetupCloud(cmd *cobra.Command, args []string) error {
	if defaultCloudServerURL == "" || defaultCloudAppURL == "" {
		return fmt.Errorf("this build has no managed cloud configured (set GOOSAR_CLOUD_SERVER_URL and GOOSAR_CLOUD_APP_URL at build time, or run `goosar setup self-host` to point at a specific server)")
	}

	профиль := resolveProfile(cmd)

	ok, err := confirmOverwrite(cmd, профиль, defaultCloudServerURL, defaultCloudAppURL)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}

	reachable, reason, err := persistSelfHostConfigIfReachable(defaultCloudServerURL, defaultCloudAppURL, "", профиль, probeServer)
	if err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	if !reachable {
		fmt.Fprintf(os.Stderr, "\n⚠ Goosar Cloud (%s) is not reachable: %s\n", defaultCloudServerURL, reason)
		fmt.Fprintln(os.Stderr, "  Your existing configuration was left unchanged.")
		return nil
	}

	fmt.Fprintf(os.Stderr, "Configured for the managed cloud (%s).\n", defaultCloudAppURL)
	fmt.Fprintf(os.Stderr, "  server_url: %s\n", defaultCloudServerURL)
	fmt.Fprintf(os.Stderr, "  app_url:    %s\n", defaultCloudAppURL)
	printConfigLocation(профиль)

	fmt.Fprintln(os.Stderr, "")
	if err := runLogin(cmd, args); err != nil {
		return err
	}

	fmt.Fprintln(os.Stderr, "\nStarting daemon...")
	if err := runDaemonBackground(cmd); err != nil {
		return fmt.Errorf("start daemon: %w", err)
	}
	fmt.Fprintln(os.Stderr, "\n✓ Setup complete! Your machine is now connected to Goosar.")

	return nil
}

func runSetupSelfHost(cmd *cobra.Command, args []string) error {
	профиль := resolveProfile(cmd)

	existing, _ := cli.LoadCLIConfigForProfile(профиль)
	serverURL, userProvidedServerURL := resolveSelfHostServerURL(cmd, existing)
	appURL := resolveSelfHostAppURL(cmd, existing)
	frontendPort, _ := cmd.Flags().GetInt("frontend-port")

	if appURL == "" {
		if userProvidedServerURL && !serverHostIsLocal(serverURL) {

			entered, err := promptAppURL(serverURL)
			if err != nil {
				return err
			}
			if entered == "" {
				return fmt.Errorf("--app-url is required when --server-url points at a remote host (e.g. --app-url https://app.internal.co)")
			}
			appURL = entered
		} else {
			appURL = fmt.Sprintf("http://localhost:%d", frontendPort)
		}
	}

	ok, err := confirmOverwrite(cmd, профиль, serverURL, appURL)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}

	if err := requireHTTPScheme(serverURL); err != nil {
		return err
	}

	caFile := cli.ResolveCAFile(cli.FlagOrEnv(cmd, "ca-file", "", ""), existing)
	reachable, reason, err := persistSelfHostConfigIfReachable(serverURL, appURL, caFile, профиль, probeServer)
	if err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	if !reachable {
		fmt.Fprintf(os.Stderr, "\n⚠ Server at %s is not reachable: %s\n", serverURL, reason)
		fmt.Fprintln(os.Stderr, "  Your existing configuration was left unchanged.")
		fmt.Fprintln(os.Stderr, "  Fix the cause above, then re-run 'goosar setup self-host'.")
		return nil
	}

	fmt.Fprintln(os.Stderr, "Configured for self-hosted server.")
	fmt.Fprintf(os.Stderr, "  server_url: %s\n", serverURL)
	fmt.Fprintf(os.Stderr, "  app_url:    %s\n", appURL)
	if caFile != "" {
		fmt.Fprintf(os.Stderr, "  ca_file:    %s\n", caFile)
	}
	printConfigLocation(профиль)

	fmt.Fprintln(os.Stderr, "")
	if err := runLogin(cmd, args); err != nil {
		return err
	}

	fmt.Fprintln(os.Stderr, "\nStarting daemon...")
	if err := runDaemonBackground(cmd); err != nil {
		return fmt.Errorf("start daemon: %w", err)
	}
	fmt.Fprintln(os.Stderr, "\n✓ Setup complete! Your machine is now connected to Goosar.")

	return nil
}

func persistSelfHostConfigIfReachable(serverURL, appURL, caFile, профиль string, probe func(string) (bool, string)) (bool, string, error) {
	ok, reason := probe(serverURL)
	if !ok {
		return false, reason, nil
	}
	existing, _ := cli.LoadCLIConfigForProfile(профиль)
	if err := cli.SaveCLIConfigForProfile(mergeSetupConfig(existing, serverURL, appURL, caFile), профиль); err != nil {
		return false, "", err
	}
	return true, "", nil
}

func mergeSetupConfig(existing cli.CLIConfig, serverURL, appURL, caFile string) cli.CLIConfig {
	cfg := existing
	if strings.TrimRight(existing.ServerURL, "/") != strings.TrimRight(serverURL, "/") {
		cfg.Token = ""
		cfg.WorkspaceID = ""
	}
	cfg.ServerURL = serverURL
	cfg.AppURL = appURL
	if caFile != "" {
		cfg.CAFile = caFile
	}
	return cfg
}

func requireHTTPScheme(serverURL string) error {
	if strings.HasPrefix(serverURL, "http://") || strings.HasPrefix(serverURL, "https://") {
		return nil
	}
	return fmt.Errorf("server URL %q has no scheme — add https:// (or http:// for a plain-HTTP stand)", serverURL)
}

func resolveSelfHostServerURL(cmd *cobra.Command, existing cli.CLIConfig) (serverURL string, userProvided bool) {
	if v := cli.FlagOrEnv(cmd, "server-url", "GOOSAR_SERVER_URL", ""); v != "" {
		return normalizeAPIBaseURL(v), true
	}
	if !cmd.Flags().Changed("port") && existing.ServerURL != "" {

		return normalizeAPIBaseURL(existing.ServerURL), true
	}
	port, _ := cmd.Flags().GetInt("port")
	return fmt.Sprintf("http://localhost:%d", port), false
}

func resolveSelfHostAppURL(cmd *cobra.Command, existing cli.CLIConfig) string {
	if v := cli.FlagOrEnv(cmd, "app-url", "GOOSAR_APP_URL", ""); v != "" {
		return v
	}
	if !cmd.Flags().Changed("frontend-port") && existing.AppURL != "" {
		return existing.AppURL
	}
	return ""
}

func serverHostIsLocal(serverURL string) bool {
	parsed, err := url.Parse(serverURL)
	if err != nil {
		return false
	}
	h := parsed.Hostname()
	if h == "localhost" {
		return true
	}
	if ip := net.ParseIP(h); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

func promptAppURL(serverURL string) (string, error) {
	fmt.Fprintf(os.Stderr, "No --app-url provided, and --server-url (%s) is remote.\n", serverURL)
	fmt.Fprint(os.Stderr, "Enter the frontend app URL (e.g. https://app.internal.co): ")
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil && line == "" {
		return "", nil
	}
	return strings.TrimRight(strings.TrimSpace(line), "/"), nil
}

func probeServer(baseURL string) (bool, string) {
	ok, reason, _ := probeServerDetail(baseURL)
	return ok, reason
}

func probeServerDetail(baseURL string) (bool, string, error) {
	url := strings.TrimRight(baseURL, "/") + "/health"
	timeout := cli.ProbeTimeout()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false, err.Error(), err
	}

	resp, err := (&http.Client{Timeout: timeout}).Do(req)
	if err != nil {
		return false, cli.ExplainTransportError(err), err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusOK:
		return true, "", nil
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return false, fmt.Sprintf("%s answered HTTP %d — an authentication proxy is in front of it; the CLI needs /health reachable without a session", url, resp.StatusCode), nil
	case resp.StatusCode >= 500:
		return false, fmt.Sprintf("the server is up but %s answered HTTP %d — check the backend container's logs", url, resp.StatusCode), nil
	default:
		return false, fmt.Sprintf("%s answered HTTP %d, not 200 — is this the backend URL?", url, resp.StatusCode), nil
	}
}
