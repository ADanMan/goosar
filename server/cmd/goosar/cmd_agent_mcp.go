package main

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/spf13/cobra"

	"github.com/adanman/goosar/server/internal/cli"
)

var agentMcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Управление MCP-серверами рабочего пространства, которые использует агент",
	Long: "Назначает агенту MCP-серверы из библиотеки рабочего пространства. " +
		"Запись в библиотеке ничего не делает, пока её не добавили агенту " +
		"этой командой, а у каждого назначения есть свой переключатель " +
		"«включено/выключено» — так же, как у skills рабочего пространства.\n\n" +
		"Это отдельная настройка от собственного mcp_config агента ('agent update " +
		"--mcp-config'), где лежат серверы, доступные только этому агенту. " +
		"Обе части объединяются при получении задачи; при совпадении имён " +
		"побеждает собственная запись агента.",
}

var agentMcpListCmd = &cobra.Command{
	Use:   "list <agent-id>",
	Short: "Показать MCP-серверы рабочего пространства, назначенные агенту",
	Args:  exactArgs(1),
	RunE:  runAgentMcpList,
}

var agentMcpAddCmd = &cobra.Command{
	Use:   "add <agent-id> <server-id>",
	Short: "Назначить агенту MCP-сервер рабочего пространства",
	Long: "Назначает агенту MCP-сервер рабочего пространства во включённом " +
		"состоянии. ID сервера берите из 'goosar workspace mcp list'. Повторное " +
		"добавление ничего не меняет.",
	Args: exactArgs(2),
	RunE: runAgentMcpAdd,
}

var agentMcpEnableCmd = &cobra.Command{
	Use:   "enable <agent-id> <server-id>",
	Short: "Снова включить назначенный агенту MCP-сервер",
	Args:  exactArgs(2),
	RunE:  runAgentMcpEnable,
}

var agentMcpDisableCmd = &cobra.Command{
	Use:   "disable <agent-id> <server-id>",
	Short: "Выключить назначенный агенту MCP-сервер",
	Long: "Агент перестаёт получать этот сервер, но назначение сохраняется, " +
		"поэтому включить его обратно можно одной командой. Изменение " +
		"применяется к следующей задаче агента; ничего пересоздавать не нужно.",
	Args: exactArgs(2),
	RunE: runAgentMcpDisable,
}

var agentMcpRemoveCmd = &cobra.Command{
	Use:   "remove <agent-id> <server-id>",
	Short: "Убрать у агента MCP-сервер рабочего пространства",
	Long: "Убирает назначение. Сама запись в библиотеке рабочего пространства " +
		"не меняется, у других агентов назначения остаются.",
	Args: exactArgs(2),
	RunE: runAgentMcpRemove,
}

func init() {
	agentCmd.AddCommand(agentMcpCmd)
	agentMcpCmd.AddCommand(agentMcpListCmd)
	agentMcpCmd.AddCommand(agentMcpAddCmd)
	agentMcpCmd.AddCommand(agentMcpEnableCmd)
	agentMcpCmd.AddCommand(agentMcpDisableCmd)
	agentMcpCmd.AddCommand(agentMcpRemoveCmd)

	for _, cmd := range []*cobra.Command{
		agentMcpListCmd, agentMcpAddCmd, agentMcpEnableCmd, agentMcpDisableCmd, agentMcpRemoveCmd,
	} {
		cmd.Flags().String("output", "table", "Формат вывода: table или json")
	}
}

func agentMcpPath(agentID string, parts ...string) string {
	path := "/api/agents/" + url.PathEscape(agentID) + "/mcp-servers"
	for _, part := range parts {
		path += "/" + url.PathEscape(part)
	}
	return path
}

func runAgentMcpList(cmd *cobra.Command, args []string) error {
	agentID := strings.TrimSpace(args[0])
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var servers []workspaceMcpServer
	if err := client.GetJSON(ctx, agentMcpPath(agentID), &servers); err != nil {
		return fmt.Errorf("list agent mcp servers: %w", err)
	}
	return printWorkspaceMcpServers(cmd, servers)
}

func runAgentMcpAdd(cmd *cobra.Command, args []string) error {
	agentID, serverID := strings.TrimSpace(args[0]), strings.TrimSpace(args[1])
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var servers []workspaceMcpServer
	body := map[string]any{"server_id": serverID}
	if err := client.PostJSON(ctx, agentMcpPath(agentID), body, &servers); err != nil {
		return fmt.Errorf("add agent mcp server: %w", err)
	}
	return printWorkspaceMcpServers(cmd, servers)
}

func runAgentMcpEnable(cmd *cobra.Command, args []string) error {
	return setAgentMcpEnabled(cmd, args, true)
}

func runAgentMcpDisable(cmd *cobra.Command, args []string) error {
	return setAgentMcpEnabled(cmd, args, false)
}

func setAgentMcpEnabled(cmd *cobra.Command, args []string, enabled bool) error {
	agentID, serverID := strings.TrimSpace(args[0]), strings.TrimSpace(args[1])
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var servers []workspaceMcpServer
	body := map[string]any{"enabled": enabled}
	if err := client.PutJSON(ctx, agentMcpPath(agentID, serverID, "enabled"), body, &servers); err != nil {
		return fmt.Errorf("update agent mcp server: %w", err)
	}
	return printWorkspaceMcpServers(cmd, servers)
}

func runAgentMcpRemove(cmd *cobra.Command, args []string) error {
	agentID, serverID := strings.TrimSpace(args[0]), strings.TrimSpace(args[1])
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var servers []workspaceMcpServer
	if err := client.DeleteJSONResponse(ctx, agentMcpPath(agentID, serverID), &servers); err != nil {
		return fmt.Errorf("remove agent mcp server: %w", err)
	}
	return printWorkspaceMcpServers(cmd, servers)
}
