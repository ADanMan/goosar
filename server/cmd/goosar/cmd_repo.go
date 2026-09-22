package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/adanman/goosar/server/internal/cli"
)

var repoCmd = &cobra.Command{
	Use:   "repo",
	Short: "Работа с репозиториями",
}

var repoListCmd = &cobra.Command{
	Use:   "list",
	Short: "Список репозиториев рабочего пространства",
	Long:  "Показывает реестр репозиториев текущего рабочего пространства. Это репозитории уровня рабочего пространства, они отделены от ресурсов проекта.",
	Args:  cobra.NoArgs,
	RunE:  runRepoList,
}

var repoAddCmd = &cobra.Command{
	Use:   "add [url]...",
	Short: "Добавить репозитории в реестр рабочего пространства",
	Long: "Добавляет один или несколько URL репозиториев в реестр репозиториев текущего рабочего пространства. " +
		"Уже добавленные URL не дублируются. Если нужен контекст, относящийся к конкретному проекту, используйте ресурсы проекта.",
	Args: cobra.ArbitraryArgs,
	RunE: runRepoAdd,
}

var repoRemoveCmd = &cobra.Command{
	Use:     "remove [url]...",
	Aliases: []string{"rm"},
	Short:   "Убрать репозитории из реестра рабочего пространства",
	Long:    "Убирает один или несколько URL репозиториев из реестра репозиториев текущего рабочего пространства.",
	Args:    cobra.ArbitraryArgs,
	RunE:    runRepoRemove,
}

var repoCheckoutCmd = &cobra.Command{
	Use:   "checkout <url>",
	Short: "Загрузить репозиторий в рабочий каталог",
	Long:  "Создаёт git worktree из кэша bare-клона демона. Агенты используют команду, чтобы получать репозитории по запросу.",
	Args:  exactArgs(1),
	RunE:  runRepoCheckout,
}

var repoCheckoutRef string

func init() {
	repoListCmd.Flags().String("output", "table", "Формат вывода: table или json")

	repoAddCmd.Flags().StringArray("url", nil, "URL репозитория для добавления (флаг можно повторять)")
	repoAddCmd.Flags().String("description", "", "Описание (необязательно); допустимо только при добавлении одного URL")
	repoAddCmd.Flags().String("output", "json", "Формат вывода: table или json")

	repoRemoveCmd.Flags().StringArray("url", nil, "URL репозитория для удаления (флаг можно повторять)")
	repoRemoveCmd.Flags().String("output", "json", "Формат вывода: table или json")

	repoCheckoutCmd.Flags().StringVar(&repoCheckoutRef, "ref", "", "Ветка, тег или коммит для загрузки вместо ветки по умолчанию на удалённом сервере")

	repoCmd.AddCommand(repoListCmd)
	repoCmd.AddCommand(repoAddCmd)
	repoCmd.AddCommand(repoRemoveCmd)
	repoCmd.AddCommand(repoCheckoutCmd)
}

type workspaceRepo struct {
	URL         string `json:"url"`
	Description string `json:"description,omitempty"`
}

type repoWorkspaceResponse struct {
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Slug  string          `json:"slug"`
	Repos []workspaceRepo `json:"repos"`
}

type repoMutationResult struct {
	WorkspaceID string          `json:"workspace_id"`
	Added       []workspaceRepo `json:"added,omitempty"`
	Updated     []workspaceRepo `json:"updated,omitempty"`
	Removed     []workspaceRepo `json:"removed,omitempty"`
	Repos       []workspaceRepo `json:"repos"`
}

func repoURLsFromArgsAndFlags(cmd *cobra.Command, args []string) ([]string, error) {
	flagURLs, _ := cmd.Flags().GetStringArray("url")
	raw := append([]string{}, flagURLs...)
	raw = append(raw, args...)
	if len(raw) == 0 {
		return nil, fmt.Errorf("at least one repository URL is required")
	}

	urls := make([]string, 0, len(raw))
	seen := make(map[string]struct{}, len(raw))
	for _, u := range raw {
		u = strings.TrimSpace(u)
		if u == "" {
			return nil, fmt.Errorf("repository URL cannot be empty")
		}
		if _, ok := seen[u]; ok {
			continue
		}
		seen[u] = struct{}{}
		urls = append(urls, u)
	}
	return urls, nil
}

func fetchRepoWorkspace(ctx context.Context, client *cli.APIClient, workspaceID string) (repoWorkspaceResponse, error) {
	var ws repoWorkspaceResponse
	if err := client.GetJSON(ctx, "/api/workspaces/"+workspaceID, &ws); err != nil {
		return repoWorkspaceResponse{}, fmt.Errorf("get workspace: %w", err)
	}
	if ws.Repos == nil {
		ws.Repos = []workspaceRepo{}
	}
	return ws, nil
}

func patchWorkspaceRepos(ctx context.Context, client *cli.APIClient, workspaceID string, repos []workspaceRepo) (repoWorkspaceResponse, error) {
	var ws repoWorkspaceResponse
	if err := client.PatchJSON(ctx, "/api/workspaces/"+workspaceID, map[string]any{"repos": repos}, &ws); err != nil {
		return repoWorkspaceResponse{}, fmt.Errorf("update workspace repos: %w", err)
	}
	if ws.Repos == nil {
		ws.Repos = []workspaceRepo{}
	}
	return ws, nil
}

func repoCommandClient(cmd *cobra.Command) (*cli.APIClient, string, error) {
	workspaceID, err := requireWorkspaceID(cmd)
	if err != nil {
		return nil, "", err
	}
	client, err := newAPIClient(cmd)
	if err != nil {
		return nil, "", err
	}
	return client, workspaceID, nil
}

func runRepoList(cmd *cobra.Command, _ []string) error {
	client, workspaceID, err := repoCommandClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	ws, err := fetchRepoWorkspace(ctx, client, workspaceID)
	if err != nil {
		return err
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, ws.Repos)
	}
	if len(ws.Repos) == 0 {
		fmt.Fprintln(os.Stderr, "No repositories found.")
		return nil
	}
	rows := make([][]string, 0, len(ws.Repos))
	for _, repo := range ws.Repos {
		rows = append(rows, []string{repo.URL, repo.Description})
	}
	cli.PrintTable(os.Stdout, []string{"URL", "DESCRIPTION"}, rows)
	return nil
}

func runRepoAdd(cmd *cobra.Command, args []string) error {
	urls, err := repoURLsFromArgsAndFlags(cmd, args)
	if err != nil {
		return err
	}
	description, _ := cmd.Flags().GetString("description")
	descriptionChanged := cmd.Flags().Changed("description")
	if descriptionChanged && len(urls) > 1 {
		return fmt.Errorf("--description can only be used when adding one repository URL")
	}

	client, workspaceID, err := repoCommandClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	ws, err := fetchRepoWorkspace(ctx, client, workspaceID)
	if err != nil {
		return err
	}

	indexByURL := make(map[string]int, len(ws.Repos))
	for i, repo := range ws.Repos {
		indexByURL[repo.URL] = i
	}

	added := []workspaceRepo{}
	updated := []workspaceRepo{}
	repos := append([]workspaceRepo{}, ws.Repos...)
	for _, u := range urls {
		if idx, ok := indexByURL[u]; ok {
			if descriptionChanged && repos[idx].Description != description {
				repos[idx].Description = description
				updated = append(updated, repos[idx])
			}
			continue
		}
		repo := workspaceRepo{URL: u}
		if descriptionChanged {
			repo.Description = description
		}
		indexByURL[u] = len(repos)
		repos = append(repos, repo)
		added = append(added, repo)
	}

	if len(added) > 0 || len(updated) > 0 {
		ws, err = patchWorkspaceRepos(ctx, client, workspaceID, repos)
		if err != nil {
			return err
		}
	} else {
		ws.Repos = repos
	}

	result := repoMutationResult{
		WorkspaceID: ws.ID,
		Added:       added,
		Updated:     updated,
		Repos:       ws.Repos,
	}
	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	if len(added) == 0 && len(updated) == 0 {
		fmt.Fprintln(os.Stdout, "No repository changes.")
		return nil
	}
	rows := make([][]string, 0, len(added)+len(updated))
	for _, repo := range added {
		rows = append(rows, []string{"added", repo.URL, repo.Description})
	}
	for _, repo := range updated {
		rows = append(rows, []string{"updated", repo.URL, repo.Description})
	}
	cli.PrintTable(os.Stdout, []string{"ACTION", "URL", "DESCRIPTION"}, rows)
	return nil
}

func runRepoRemove(cmd *cobra.Command, args []string) error {
	urls, err := repoURLsFromArgsAndFlags(cmd, args)
	if err != nil {
		return err
	}

	client, workspaceID, err := repoCommandClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	ws, err := fetchRepoWorkspace(ctx, client, workspaceID)
	if err != nil {
		return err
	}

	removeSet := make(map[string]struct{}, len(urls))
	for _, u := range urls {
		removeSet[u] = struct{}{}
	}
	removedSet := make(map[string]struct{}, len(urls))
	removed := []workspaceRepo{}
	repos := make([]workspaceRepo, 0, len(ws.Repos))
	for _, repo := range ws.Repos {
		if _, ok := removeSet[repo.URL]; ok {
			removed = append(removed, repo)
			removedSet[repo.URL] = struct{}{}
			continue
		}
		repos = append(repos, repo)
	}
	missing := []string{}
	for _, u := range urls {
		if _, ok := removedSet[u]; !ok {
			missing = append(missing, u)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("repository not found in workspace registry: %s", strings.Join(missing, ", "))
	}

	ws, err = patchWorkspaceRepos(ctx, client, workspaceID, repos)
	if err != nil {
		return err
	}

	result := repoMutationResult{
		WorkspaceID: ws.ID,
		Removed:     removed,
		Repos:       ws.Repos,
	}
	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	rows := make([][]string, 0, len(removed))
	for _, repo := range removed {
		rows = append(rows, []string{repo.URL, repo.Description})
	}
	cli.PrintTable(os.Stdout, []string{"REMOVED URL", "DESCRIPTION"}, rows)
	return nil
}

func runRepoCheckout(cmd *cobra.Command, args []string) error {
	repoURL := args[0]

	daemonPort := os.Getenv("GOOSAR_DAEMON_PORT")
	if daemonPort == "" {
		return fmt.Errorf("GOOSAR_DAEMON_PORT not set (this command is intended to be run by an agent inside a daemon task)")
	}

	workspaceID := os.Getenv("GOOSAR_WORKSPACE_ID")
	agentName := os.Getenv("GOOSAR_AGENT_NAME")
	taskID := os.Getenv("GOOSAR_TASK_ID")
	taskToken := os.Getenv("GOOSAR_TOKEN")
	if taskToken == "" {
		return fmt.Errorf("GOOSAR_TOKEN not set (repo checkout requires the active task credential)")
	}

	workDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}

	reqBody := map[string]string{
		"url":           repoURL,
		"workspace_id":  workspaceID,
		"workdir":       workDir,
		"ref":           repoCheckoutRef,
		"agent_name":    agentName,
		"task_id":       taskID,
		"checkout_mode": strings.TrimSpace(os.Getenv("GOOSAR_REPO_CHECKOUT_MODE")),
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("encode request: %w", err)
	}

	client := &http.Client{Timeout: 5 * time.Minute}
	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("http://127.0.0.1:%s/repo/checkout", daemonPort), bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("create daemon checkout request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+taskToken)
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("connect to daemon: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("checkout failed: %s", string(body))
	}

	var result struct {
		Path       string `json:"path"`
		BranchName string `json:"branch_name"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("parse response: %w", err)
	}

	fmt.Fprintf(os.Stdout, "%s\n", result.Path)
	fmt.Fprintf(os.Stderr, "Checked out %s → %s (branch: %s)\n", repoURL, result.Path, result.BranchName)

	return nil
}
