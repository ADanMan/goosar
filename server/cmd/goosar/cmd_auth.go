package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/adanman/goosar/server/internal/auth"
	"github.com/adanman/goosar/server/internal/cli"
)

var loginTokenPrefixes = []string{auth.PATPrefix, auth.CloudPATPrefix}

func validateLoginTokenPrefix(token string) error {
	for _, p := range loginTokenPrefixes {
		if strings.HasPrefix(token, p) {
			return nil
		}
	}
	return fmt.Errorf("invalid token format: must start with %s", strings.Join(loginTokenPrefixes, " or "))
}

var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Вход в Goosar из CLI",
}

var authStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Показать текущий статус входа",
	RunE:  runAuthStatus,
}

var authLogoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "Удалить сохранённый токен",
	RunE:  runAuthLogout,
}

const callbackHostFlag = "callback-host"

const callbackHostFlagHelp = "Хост или IP в адресе обратного вызова OAuth, если браузер может обратиться к этому CLI напрямую. Для машин, доступных только по SSH, используйте подсказку про туннель, которая выводится при входе."

func init() {
	authCmd.AddCommand(authStatusCmd)
	authCmd.AddCommand(authLogoutCmd)
}

func resolveToken(cmd *cobra.Command) string {
	if v := strings.TrimSpace(os.Getenv("GOOSAR_TOKEN")); v != "" {
		return v
	}

	if inDaemonManagedExecutionContext() {
		return ""
	}
	профиль := resolveProfile(cmd)
	cfg, _ := cli.LoadCLIConfigForProfile(профиль)
	return cfg.Token
}

func resolveAppURL(cmd *cobra.Command) string {
	for _, key := range []string{"GOOSAR_APP_URL", "FRONTEND_ORIGIN"} {
		if val := strings.TrimSpace(os.Getenv(key)); val != "" {
			return strings.TrimRight(val, "/")
		}
	}
	профиль := resolveProfile(cmd)
	cfg, err := cli.LoadCLIConfigForProfile(профиль)
	if err == nil && cfg.AppURL != "" {
		return strings.TrimRight(cfg.AppURL, "/")
	}
	fmt.Fprintln(os.Stderr, "No app URL configured. Run 'goosar setup' first.")
	os.Exit(1)
	return ""
}

func openBrowser(url string) error {
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		cmd = "open"
		args = []string{url}
	case "linux":
		cmd = "xdg-open"
		args = []string{url}
	case "windows":
		cmd = "rundll32"
		args = []string{"url.dll,FileProtocolHandler", url}
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
	return exec.Command(cmd, args...).Start()
}

func runAuthLogin(cmd *cobra.Command, args []string) error {
	if cmd.Flags().Changed("token") {
		tokenFlag, _ := cmd.Flags().GetString("token")

		if tokenFlag == tokenPromptSentinel && len(args) == 1 {
			tokenFlag = args[0]
		}
		return runAuthLoginToken(cmd, tokenFlag)
	}
	return runAuthLoginBrowser(cmd)
}

func resolveCallbackBinding(flagHost, serverURL, appURL string, detectOutbound func(string) net.IP) (callbackHost, bindAddr string) {

	if h := strings.TrimSpace(flagHost); h != "" {
		return h, "0.0.0.0"
	}

	appIP := urlPrivateIP(appURL)
	if appIP == nil {

		return "localhost", "127.0.0.1"
	}

	cliIP := detectOutbound(serverURL)
	if cliIP == nil {

		return appIP.String(), "0.0.0.0"
	}
	if cliIP.Equal(appIP) {
		return "localhost", "127.0.0.1"
	}
	return cliIP.String(), "0.0.0.0"
}

func urlPrivateIP(rawURL string) net.IP {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil
	}
	ip := net.ParseIP(parsed.Hostname())
	if ip == nil || !ip.IsPrivate() {
		return nil
	}
	return ip
}

func detectOutboundIP(serverURL string) net.IP {
	parsed, err := url.Parse(serverURL)
	if err != nil || parsed.Hostname() == "" {
		return nil
	}
	port := parsed.Port()
	if port == "" {
		if parsed.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	conn, err := net.Dial("udp4", net.JoinHostPort(parsed.Hostname(), port))
	if err != nil {
		return nil
	}
	defer conn.Close()
	local, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok || local.IP == nil {
		return nil
	}

	if v4 := local.IP.To4(); v4 != nil {
		return v4
	}
	return local.IP
}

func runAuthLoginBrowser(cmd *cobra.Command) error {
	serverURL := resolveServerURL(cmd)
	appURL := resolveAppURL(cmd)

	flagHost := callbackHostFlagValue(cmd)
	callbackHost, bindAddr := resolveCallbackBinding(flagHost, serverURL, appURL, detectOutboundIP)

	listener, err := net.Listen("tcp4", bindAddr+":0")
	if err != nil {
		return fmt.Errorf("could not start the local login callback server (used to receive the browser sign-in); a firewall or another process may be blocking local ports: %w", err)
	}
	defer listener.Close()

	port := listener.Addr().(*net.TCPAddr).Port
	callbackURL := fmt.Sprintf("http://%s:%d/callback", callbackHost, port)

	stateBytes := make([]byte, 16)
	if _, err := rand.Read(stateBytes); err != nil {
		return fmt.Errorf("failed to generate state: %w", err)
	}
	state := hex.EncodeToString(stateBytes)

	loginURL := fmt.Sprintf("%s/login?cli_callback=%s&cli_state=%s", appURL, url.QueryEscape(callbackURL), url.QueryEscape(state))

	jwtCh := make(chan string, 1)
	errCh := make(chan error, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		token := r.URL.Query().Get("token")
		if token == "" {
			http.Error(w, "missing token", http.StatusBadRequest)
			return
		}
		returnedState := r.URL.Query().Get("state")
		if returnedState != state {
			http.Error(w, "invalid state parameter", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(callbackSuccessHTML))
		jwtCh <- token
	})

	srv := &http.Server{Handler: mux}
	go func() {
		if err := srv.Serve(listener); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()
	defer srv.Close()

	fmt.Fprintln(os.Stderr, "Opening browser to authenticate...")
	if err := openBrowser(loginURL); err != nil {
		fmt.Fprintf(os.Stderr, "Could not open browser automatically.\n")
	}
	fmt.Fprint(os.Stderr, browserLoginInstructions(loginURL, callbackHost, port, runningInSSHSession()))

	var jwtToken string
	select {
	case jwtToken = <-jwtCh:
	case err := <-errCh:
		return fmt.Errorf("local server error: %w", err)
	case <-time.After(5 * time.Minute):
		return fmt.Errorf("timed out waiting for authentication")
	}

	client := cli.NewAPIClient(serverURL, "", jwtToken)

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "unknown"
	}
	patName := fmt.Sprintf("CLI (%s)", hostname)
	expiresInDays := 90

	var patResp struct {
		Token string `json:"token"`
	}
	err = client.PostJSON(ctx, "/api/tokens", map[string]any{
		"name":            patName,
		"expires_in_days": expiresInDays,
	}, &patResp)
	if err != nil {
		return cli.WithUserMessage("Sign-in did not complete: the server could not issue an access token for the CLI. Run `goosar login` again.", err)
	}

	patClient := cli.NewAPIClient(serverURL, "", patResp.Token)
	var me struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	}
	if err := patClient.GetJSON(ctx, "/api/me", &me); err != nil {
		return cli.WithUserMessage("Sign-in did not complete: the server did not accept the new credential. Run `goosar login` again.", err)
	}

	профиль := resolveProfile(cmd)
	cfg, _ := cli.LoadCLIConfigForProfile(профиль)
	cfg.WorkspaceID = ""
	cfg.Token = patResp.Token
	cfg.ServerURL = serverURL
	cfg.AppURL = appURL
	if err := cli.SaveCLIConfigForProfile(cfg, профиль); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Authenticated as %s (%s)\nToken saved to config.\n", me.Name, me.Email)
	return nil
}

func runningInSSHSession() bool {
	for _, key := range []string{"SSH_CONNECTION", "SSH_CLIENT", "SSH_TTY"} {
		if strings.TrimSpace(os.Getenv(key)) != "" {
			return true
		}
	}
	return false
}

func callbackHostFlagValue(cmd *cobra.Command) string {
	for c := cmd; c != nil; c = c.Parent() {
		if value := nonEmptyFlagValue(c.Flags(), callbackHostFlag); value != "" {
			return value
		}
		if value := nonEmptyFlagValue(c.PersistentFlags(), callbackHostFlag); value != "" {
			return value
		}
		if value := nonEmptyFlagValue(c.InheritedFlags(), callbackHostFlag); value != "" {
			return value
		}
	}
	return ""
}

func nonEmptyFlagValue(flags *pflag.FlagSet, name string) string {
	if flag := flags.Lookup(name); flag != nil {
		return strings.TrimSpace(flag.Value.String())
	}
	return ""
}

func callbackHostIsLoopback(host string) bool {
	h := strings.Trim(strings.TrimSpace(host), "[]")
	if h == "localhost" {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

func browserLoginInstructions(loginURL, callbackHost string, port int, remoteSSH bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "If the browser didn't open, visit:\n  %s\n", loginURL)
	if remoteSSH && callbackHostIsLoopback(callbackHost) {
		fmt.Fprintf(&b, "\nRemote SSH session detected. Before opening that URL on your local computer, forward the callback port in another terminal:\n  ssh -L %d:127.0.0.1:%d <user>@<remote-host>\nThen open the URL above in your local browser.\n", port, port)
	}
	fmt.Fprintln(&b, "\nWaiting for authentication...")
	return b.String()
}

func runAuthLoginToken(cmd *cobra.Command, providedToken string) error {

	if providedToken == tokenPromptSentinel {
		providedToken = ""
	}
	token := strings.TrimSpace(providedToken)
	if token == "" {
		fmt.Print("Enter your personal access token: ")
		scanner := bufio.NewScanner(os.Stdin)
		if !scanner.Scan() {
			return fmt.Errorf("no input")
		}
		token = strings.TrimSpace(scanner.Text())
	}
	if token == "" {
		return fmt.Errorf("token is required")
	}
	if err := validateLoginTokenPrefix(token); err != nil {
		return err
	}

	serverURL := resolveLoginTokenServerURL(cmd)
	client := cli.NewAPIClient(serverURL, "", token)

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var me struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	}
	if err := client.GetJSON(ctx, "/api/me", &me); err != nil {
		return cli.WithUserMessage("Could not sign in with that token — make sure it is valid and not expired, then run `goosar login --token <token>` again.", err)
	}

	профиль := resolveProfile(cmd)
	cfg, _ := cli.LoadCLIConfigForProfile(профиль)
	cfg.WorkspaceID = ""
	cfg.Token = token
	cfg.ServerURL = serverURL
	if cfg.AppURL == "" && serverURL == defaultCloudServerURL {
		cfg.AppURL = defaultCloudAppURL
	}
	if err := cli.SaveCLIConfigForProfile(cfg, профиль); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Authenticated as %s (%s)\nToken saved to config.\n", me.Name, me.Email)
	return nil
}

func runAuthStatus(cmd *cobra.Command, _ []string) error {
	token := resolveToken(cmd)
	serverURL := resolveServerURL(cmd)

	if token == "" {
		fmt.Fprintln(os.Stderr, "Not authenticated. Run 'goosar login' to authenticate.")
		return nil
	}

	client := cli.NewAPIClient(serverURL, "", token)

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var me struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	}
	if err := client.GetJSON(ctx, "/api/me", &me); err != nil {
		fmt.Fprintf(os.Stderr, "Token is invalid or expired: %v\nRun 'goosar login' to re-authenticate.\n", err)
		return nil
	}

	prefix := token
	if len(prefix) > 12 {
		prefix = prefix[:12] + "..."
	}

	fmt.Fprintf(os.Stderr, "Server:  %s\nUser:    %s (%s)\nToken:   %s\n", serverURL, me.Name, me.Email, prefix)
	return nil
}

const callbackSuccessHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Goosar — Authenticated</title>
<style>
  *, *::before, *::after { box-sizing: border-box; margin: 0; padding: 0; }
  @media (prefers-color-scheme: dark) {
    :root { --bg: #0b0b0f; --card-bg: #16161d; --border: rgba(255,255,255,0.10); --fg: #f5f5f5; --fg2: #a1a1aa; --accent: #22c55e; --accent-bg: rgba(34,197,94,0.12); }
  }
  @media (prefers-color-scheme: light) {
    :root { --bg: #f8f8fa; --card-bg: #ffffff; --border: rgba(0,0,0,0.08); --fg: #0f0f12; --fg2: #71717a; --accent: #16a34a; --accent-bg: rgba(22,163,74,0.08); }
  }
  body { font-family: -apple-system, "Segoe UI", Helvetica, Arial, sans-serif; background: var(--bg); color: var(--fg); display: flex; align-items: center; justify-content: center; min-height: 100vh; }
  .card { width: 100%; max-width: 380px; border: 1px solid var(--border); border-radius: 12px; background: var(--card-bg); padding: 40px 32px; text-align: center; }
  .icon-wrap { width: 48px; height: 48px; margin: 0 auto 24px; background: var(--accent-bg); border-radius: 50%; display: flex; align-items: center; justify-content: center; }
  .icon-wrap svg { width: 24px; height: 24px; color: var(--accent); }
  .brand { display: flex; align-items: center; justify-content: center; gap: 6px; margin-bottom: 8px; }
  .asterisk { display: inline-block; width: 14px; height: 14px; background: var(--fg); clip-path: polygon(45% 62.1%,45% 100%,55% 100%,55% 62.1%,81.8% 88.9%,88.9% 81.8%,62.1% 55%,100% 55%,100% 45%,62.1% 45%,88.9% 18.2%,81.8% 11.1%,55% 37.9%,55% 0%,45% 0%,45% 37.9%,18.2% 11.1%,11.1% 18.2%,37.9% 45%,0% 45%,0% 55%,37.9% 55%,11.1% 81.8%,18.2% 88.9%); }
  h1 { font-size: 20px; font-weight: 600; margin-bottom: 8px; }
  p { font-size: 14px; color: var(--fg2); line-height: 1.5; }
  .hint { margin-top: 24px; font-size: 13px; color: var(--fg2); opacity: 0.7; }
</style>
</head>
<body>
  <div class="card">
    <div class="icon-wrap">
      <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="2" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" d="M4.5 12.75l6 6 9-13.5"/></svg>
    </div>
    <div class="brand"><span class="asterisk"></span></div>
    <h1>Authentication successful</h1>
    <p>You can close this tab and return to the terminal.</p>
    <p class="hint">Your CLI session is now authenticated.</p>
  </div>
  <script>setTimeout(function(){window.close()},3000)</script>
</body>
</html>`

func runAuthLogout(cmd *cobra.Command, _ []string) error {
	профиль := resolveProfile(cmd)
	cfg, _ := cli.LoadCLIConfigForProfile(профиль)
	if cfg.Token == "" {
		fmt.Fprintln(os.Stderr, "Not authenticated.")
		return nil
	}

	cfg.Token = ""
	if err := cli.SaveCLIConfigForProfile(cfg, профиль); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Fprintln(os.Stderr, "Token removed. You are now logged out.")
	return nil
}
