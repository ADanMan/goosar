package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/adanman/goosar/server/internal/cli"
)

type workspaceMcpServer struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Transport string `json:"transport"`
	Enabled   *bool  `json:"enabled,omitempty"`
}

var workspaceMcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Управление общей библиотекой MCP-серверов рабочего пространства",
	Long: "Библиотека рабочего пространства хранит определения MCP-серверов, общие " +
		"для всего рабочего пространства. Добавление сервера сюда не даёт его НИ ОДНОМУ агенту: " +
		"назначьте его командой " +
		"'goosar agent mcp add <agent-id> <server-id>', где у него появляется и " +
		"переключатель включения для каждого агента — по той же схеме, что и у skills рабочего пространства.\n\n" +
		"Конфигурации доступны только для записи. API никогда не возвращает url, " +
		"command, headers и env сервера, поэтому 'update' заменяет запись целиком: " +
		"передайте конфигурацию заново.",
}

var workspaceMcpListCmd = &cobra.Command{
	Use:   "list",
	Short: "Список MCP-серверов рабочего пространства",
	Args:  exactArgs(0),
	RunE:  runWorkspaceMcpList,
}

var workspaceMcpAddCmd = &cobra.Command{
	Use:   "add <name> -",
	Short: "Добавить MCP-сервер в библиотеку рабочего пространства (конфигурация через stdin)",
	Long: "Добавляет определение сервера — объект, который в `mcpServers` стоит " +
		"под этим именем. В нём не должно быть поля \"enabled\": переключатель " +
		"включения относится к назначению агенту.\n\n" +
		"Передайте '-' и подайте конфигурацию через конвейер:\n\n" +
		"  echo '{\"url\":\"https://mcp.example/sse\"}' | goosar workspace mcp add jira -\n\n" +
		"Конфигурацию можно передать и прямо в аргументе вместо '-', но такие " +
		"записи обычно содержат учётные данные (urls, headers, env), а " +
		"позиционный аргумент виден всем локальным пользователям через `ps` и может " +
		"попасть в историю оболочки — лучше использовать stdin.",
	Args: exactArgs(2),
	RunE: runWorkspaceMcpAdd,
}

var workspaceMcpUpdateCmd = &cobra.Command{
	Use:   "update <server-id> -",
	Short: "Заменить конфигурацию MCP-сервера (новая конфигурация через stdin)",
	Long: "Заменяет сохранённую конфигурацию целиком: сохранённого значения для " +
		"точечной правки нет, потому что API его не возвращает. Флаг --name " +
		"переименовывает сервер; переименование безопасно, назначения привязаны к id сервера.\n\n" +
		"Передайте '-' и подайте новую конфигурацию через конвейер:\n\n" +
		"  echo '{\"url\":\"https://mcp.example/sse\"}' | goosar workspace mcp update <server-id> -\n\n" +
		"Конфигурацию можно передать и прямо в аргументе вместо '-', но тогда она " +
		"видна всем локальным пользователям через `ps` и может попасть в историю " +
		"оболочки — лучше использовать stdin.",
	Args: exactArgs(2),
	RunE: runWorkspaceMcpUpdate,
}

var workspaceMcpRemoveCmd = &cobra.Command{
	Use:   "remove <server-id>",
	Short: "Удалить MCP-сервер из библиотеки рабочего пространства",
	Long:  "Удаляет определение и все его назначения агентам в одной транзакции.",
	Args:  exactArgs(1),
	RunE:  runWorkspaceMcpRemove,
}

func init() {
	workspaceCmd.AddCommand(workspaceMcpCmd)
	workspaceMcpCmd.AddCommand(workspaceMcpListCmd)
	workspaceMcpCmd.AddCommand(workspaceMcpAddCmd)
	workspaceMcpCmd.AddCommand(workspaceMcpUpdateCmd)
	workspaceMcpCmd.AddCommand(workspaceMcpRemoveCmd)

	workspaceMcpUpdateCmd.Flags().String("name", "", "Новое имя сервера")
	for _, cmd := range []*cobra.Command{workspaceMcpListCmd, workspaceMcpAddCmd, workspaceMcpUpdateCmd} {
		cmd.Flags().String("output", "table", "Формат вывода: table или json")
	}
}

func workspaceMcpPath(parts ...string) string {
	path := "/api/workspace-mcp-servers"
	for _, part := range parts {
		path += "/" + url.PathEscape(part)
	}
	return path
}

func readMcpServerConfig(arg string) (json.RawMessage, error) {
	raw := strings.TrimSpace(arg)
	if raw == "-" {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return nil, fmt.Errorf("read configuration from stdin: %w", err)
		}
		raw = strings.TrimSpace(string(data))
	}
	if !json.Valid([]byte(raw)) {

		return nil, fmt.Errorf("configuration must be a JSON object")
	}
	return json.RawMessage(raw), nil
}

func printWorkspaceMcpServers(cmd *cobra.Command, servers []workspaceMcpServer) error {
	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, servers)
	}
	headers := []string{"ID", "NAME", "TRANSPORT", "ENABLED"}
	rows := make([][]string, 0, len(servers))
	for _, server := range servers {
		enabled := "-"
		if server.Enabled != nil {
			enabled = fmt.Sprintf("%t", *server.Enabled)
		}
		rows = append(rows, []string{server.ID, server.Name, server.Transport, enabled})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runWorkspaceMcpList(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var servers []workspaceMcpServer
	if err := client.GetJSON(ctx, workspaceMcpPath(), &servers); err != nil {
		return fmt.Errorf("list workspace mcp servers: %w", err)
	}
	return printWorkspaceMcpServers(cmd, servers)
}

func runWorkspaceMcpAdd(cmd *cobra.Command, args []string) error {
	конфиг, err := readMcpServerConfig(args[1])
	if err != nil {
		return err
	}
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var server workspaceMcpServer
	body := map[string]any{"name": strings.TrimSpace(args[0]), "config": конфиг}
	if err := client.PostJSON(ctx, workspaceMcpPath(), body, &server); err != nil {
		return fmt.Errorf("add workspace mcp server: %w", err)
	}
	return printWorkspaceMcpServers(cmd, []workspaceMcpServer{server})
}

func runWorkspaceMcpUpdate(cmd *cobra.Command, args []string) error {
	конфиг, err := readMcpServerConfig(args[1])
	if err != nil {
		return err
	}
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	body := map[string]any{"config": конфиг}
	if name, _ := cmd.Flags().GetString("name"); strings.TrimSpace(name) != "" {
		body["name"] = strings.TrimSpace(name)
	}
	var server workspaceMcpServer
	if err := client.PutJSON(ctx, workspaceMcpPath(strings.TrimSpace(args[0])), body, &server); err != nil {
		return fmt.Errorf("update workspace mcp server: %w", err)
	}
	return printWorkspaceMcpServers(cmd, []workspaceMcpServer{server})
}

func runWorkspaceMcpRemove(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	if err := client.DeleteJSON(ctx, workspaceMcpPath(strings.TrimSpace(args[0]))); err != nil {
		return fmt.Errorf("remove workspace mcp server: %w", err)
	}
	fmt.Fprintln(os.Stdout, "MCP server removed")
	return nil
}
