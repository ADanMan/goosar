package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/adanman/goosar/server/internal/cli"
)

func tryResolveAppURL(cmd *cobra.Command) string {
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
	return ""
}

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Войти и настроить рабочие пространства",
	Long:  "Выполняет вход в Goosar, затем автоматически находит все ваши рабочие пространства и начинает следить за ними.",

	Args: cobra.MaximumNArgs(1),
	RunE: runLogin,
}

const tokenPromptSentinel = "prompt"

func init() {

	loginCmd.Flags().String("token", "", "Войти с помощью персонального токена доступа (PAT пользователя gsl_... или PAT узла Cloud gsln_...). Передайте --token gsl_... / --token gsln_..., чтобы указать токен сразу, или просто --token, чтобы ввести его в диалоге.")

	loginCmd.Flags().Lookup("token").NoOptDefVal = tokenPromptSentinel
	loginCmd.Flags().String(callbackHostFlag, "", callbackHostFlagHelp)
}

func runLogin(cmd *cobra.Command, args []string) error {

	if err := runAuthLogin(cmd, args); err != nil {
		return err
	}

	if err := autoWatchWorkspaces(cmd); err != nil {
		fmt.Fprintf(os.Stderr, "\nCould not auto-configure workspaces: %v\n", err)
		fmt.Fprintf(os.Stderr, "Run 'goosar workspace list' and 'goosar workspace watch <id>' to set up manually.\n")
		return nil
	}

	fmt.Fprintf(os.Stderr, "\n→ Run 'goosar daemon start' to start your local agent runtime.\n")
	return nil
}

func autoWatchWorkspaces(cmd *cobra.Command) error {
	serverURL := resolveServerURL(cmd)
	token := resolveToken(cmd)
	if token == "" {
		return fmt.Errorf("not authenticated")
	}

	client := cli.NewAPIClient(serverURL, "", token)
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var workspaces []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := client.GetJSON(ctx, "/api/workspaces", &workspaces); err != nil {
		return fmt.Errorf("list workspaces: %w", err)
	}

	if len(workspaces) == 0 {
		var err error
		workspaces, err = waitForWorkspaceCreation(cmd, client)
		if err != nil {
			return err
		}
		if len(workspaces) == 0 {
			fmt.Fprintln(os.Stderr, "\nNo workspaces found.")
			return nil
		}
	}

	профиль := resolveProfile(cmd)
	cfg, err := cli.LoadCLIConfigForProfile(профиль)
	if err != nil {
		return err
	}

	if cfg.WorkspaceID == "" {
		cfg.WorkspaceID = workspaces[0].ID
	}

	if err := cli.SaveCLIConfigForProfile(cfg, профиль); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "\nFound %d workspace(s):\n", len(workspaces))
	for _, ws := range workspaces {
		marker := "  "
		if ws.ID == cfg.WorkspaceID {
			marker = "* "
		}
		fmt.Fprintf(os.Stderr, "%s%s (%s)\n", marker, ws.Name, ws.ID)
	}
	if len(workspaces) > 1 {
		fmt.Fprintln(os.Stderr, "\nUse 'goosar workspace switch <id|slug>' to change the default workspace.")
	}

	return nil
}

func waitForWorkspaceCreation(cmd *cobra.Command, client *cli.APIClient) ([]struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}, error,
) {
	appURL := tryResolveAppURL(cmd)
	if appURL == "" {

		fmt.Fprintln(os.Stderr, "\nNo workspaces found.")
		fmt.Fprintln(os.Stderr, "Create a workspace in the web dashboard, then run 'goosar login' again.")
		return nil, nil
	}

	createWorkspaceURL := appURL + "/workspaces/new"

	fmt.Fprintln(os.Stderr, "\nNo workspaces found. Opening workspace creation in your browser...")
	if err := openBrowser(createWorkspaceURL); err != nil {
		fmt.Fprintf(os.Stderr, "Could not open browser automatically.\n")
	}
	fmt.Fprintf(os.Stderr, "If the browser didn't open, visit:\n  %s\n", createWorkspaceURL)
	fmt.Fprintln(os.Stderr, "\nWaiting for workspace creation...")

	const pollInterval = 2 * time.Second
	const pollTimeout = 5 * time.Minute
	deadline := time.Now().Add(pollTimeout)

	pollRequestTimeout := cli.AtLeastAPITimeout(10 * time.Second)

	for time.Now().Before(deadline) {
		time.Sleep(pollInterval)

		ctx, cancel := context.WithTimeout(context.Background(), pollRequestTimeout)
		var workspaces []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		}
		err := client.GetJSON(ctx, "/api/workspaces", &workspaces)
		cancel()

		if err != nil {
			continue
		}
		if len(workspaces) > 0 {
			return workspaces, nil
		}
	}

	return nil, fmt.Errorf("timed out waiting for workspace creation")
}
