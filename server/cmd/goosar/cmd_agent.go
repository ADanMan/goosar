package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/adanman/goosar/server/internal/cli"
	"github.com/adanman/goosar/server/internal/daemon"
	"github.com/adanman/goosar/server/internal/daemon/execenv"
)

var agentCmd = &cobra.Command{
	Use:   "agent",
	Short: "Работа с агентами",
}

var agentListCmd = &cobra.Command{
	Use:   "list",
	Short: "Показать агентов рабочего пространства",
	RunE:  runAgentList,
}

var agentGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Показать сведения об агенте",
	Args:  exactArgs(1),
	RunE:  runAgentGet,
}

var agentCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Создать нового агента",
	RunE:  runAgentCreate,
}

var agentUpdateCmd = &cobra.Command{
	Use:   "update <id>",
	Short: "Изменить агента",
	Args:  exactArgs(1),
	RunE:  runAgentUpdate,
}

var agentArchiveCmd = &cobra.Command{
	Use:   "archive <id>",
	Short: "Перенести агента в архив",
	Args:  exactArgs(1),
	RunE:  runAgentArchive,
}

var agentRestoreCmd = &cobra.Command{
	Use:   "restore <id>",
	Short: "Вернуть агента из архива",
	Args:  exactArgs(1),
	RunE:  runAgentRestore,
}

var agentTasksCmd = &cobra.Command{
	Use:   "tasks <id>",
	Short: "Показать задачи агента",
	Args:  exactArgs(1),
	RunE:  runAgentTasks,
}

var agentAvatarCmd = &cobra.Command{
	Use:   "avatar <id>",
	Short: "Загрузить аватар агента",
	Args:  exactArgs(1),
	RunE:  runAgentAvatar,
}

var agentSkillsCmd = &cobra.Command{
	Use:   "skills",
	Short: "Управление назначенными агенту skills",
}

var agentEnvCmd = &cobra.Command{
	Use:   "env",
	Short: "Чтение и изменение пользовательских переменных окружения агента (с записью в журнал аудита)",
}

var agentEnvGetCmd = &cobra.Command{
	Use:   "get <agent-id>",
	Short: "Вывести custom_env агента как JSON-карту (значения открыты только владельцу агента, другим администраторам они скрыты; каждый вызов записывается)",
	Args:  exactArgs(1),
	RunE:  runAgentEnvGet,
}

var agentEnvSetCmd = &cobra.Command{
	Use:   "set <agent-id>",
	Short: "Заменить custom_env агента (владелец агента или owner/admin рабочего пространства; значения **** сохраняют существующую запись)",
	Args:  exactArgs(1),
	RunE:  runAgentEnvSet,
}

var agentSkillsListCmd = &cobra.Command{
	Use:   "list <agent-id>",
	Short: "Показать skills, назначенные агенту",
	Args:  exactArgs(1),
	RunE:  runAgentSkillsList,
}

var agentSkillsSetCmd = &cobra.Command{
	Use:   "set <agent-id>",
	Short: "Задать skills агента (заменяет все текущие назначения)",
	Args:  exactArgs(1),
	RunE:  runAgentSkillsSet,
}

var agentSkillsAddCmd = &cobra.Command{
	Use:   "add <agent-id>",
	Short: "Добавить агенту skills, не заменяя существующие назначения",
	Args:  exactArgs(1),
	RunE:  runAgentSkillsAdd,
}

func init() {
	agentCmd.AddCommand(agentListCmd)
	agentCmd.AddCommand(agentGetCmd)
	agentCmd.AddCommand(agentCreateCmd)
	agentCmd.AddCommand(agentUpdateCmd)
	agentCmd.AddCommand(agentArchiveCmd)
	agentCmd.AddCommand(agentRestoreCmd)
	agentCmd.AddCommand(agentTasksCmd)
	agentCmd.AddCommand(agentAvatarCmd)
	agentCmd.AddCommand(agentSkillsCmd)
	agentCmd.AddCommand(agentEnvCmd)

	agentSkillsCmd.AddCommand(agentSkillsListCmd)
	agentSkillsCmd.AddCommand(agentSkillsSetCmd)
	agentSkillsCmd.AddCommand(agentSkillsAddCmd)

	agentEnvCmd.AddCommand(agentEnvGetCmd)
	agentEnvCmd.AddCommand(agentEnvSetCmd)

	agentListCmd.Flags().String("output", "table", "Формат вывода: table или json")
	agentListCmd.Flags().Bool("include-archived", false, "Показать и архивных агентов")

	agentGetCmd.Flags().String("output", "json", "Формат вывода: table или json")

	agentCreateCmd.Flags().String("name", "", "Имя агента (обязательно)")
	agentCreateCmd.Flags().String("description", "", "Описание агента")
	agentCreateCmd.Flags().String("instructions", "", "Инструкции агента")
	agentCreateCmd.Flags().String("runtime-id", "", "ID среды выполнения (обязательно)")
	agentCreateCmd.Flags().String("runtime-config", "", "Конфигурация среды выполнения в виде JSON-строки")
	agentCreateCmd.Flags().String("model", "", "Идентификатор модели (например, rt-c/sonnet-4-6). Лучше использовать этот флаг, а не --model в --custom-args.")
	agentCreateCmd.Flags().String("thinking-level", "", "Уровень рассуждений (усилий) для среды выполнения агента. Набор допустимых значений зависит от среды выполнения и модели; некорректные значения отклоняются на сервере, а демон проверяет конкретную пару «модель — уровень». Пусто — значение по умолчанию среды выполнения.")
	agentCreateCmd.Flags().String("service-tier", "", "Сервисный уровень выполнения из каталога среды выполнения для выбранной модели (например, priority, в интерфейсе — Fast). Пусто — брать настройки локальной среды выполнения.")
	agentCreateCmd.Flags().String("custom-args", "", "Пользовательские аргументы CLI в виде JSON-массива. Для выбора модели лучше использовать --model; некоторые среды выполнения отклоняют --model в custom_args.")
	agentCreateCmd.Flags().String("custom-env", "", "Пользовательские переменные окружения в виде JSON-объекта, например '{\"KEY\":\"value\"}'. Считаются секретом — CLI их не логирует, но значения, переданные в командной строке, видны в истории оболочки (shell history) и в выводе 'ps'; для настоящих секретов лучше --custom-env-stdin или --custom-env-file. Передайте '{}', чтобы задать пустую карту.")
	agentCreateCmd.Flags().Bool("custom-env-stdin", false, "Прочитать JSON-объект --custom-env из stdin. Секреты не попадают в историю оболочки (shell history) и в 'ps'. Несовместим с --custom-env и --custom-env-file.")
	agentCreateCmd.Flags().String("custom-env-file", "", "Прочитать JSON-объект --custom-env из файла по указанному пути (рекомендуемые права: 0600). Несовместим с --custom-env и --custom-env-stdin.")
	agentCreateCmd.Flags().String("mcp-config", "", "Конфигурация MCP-серверов в виде JSON-объекта, например '{\"mcpServers\":{\"shortcut\":{...}}}'. Считается секретом (в записях MCP часто лежат API-токены) — CLI её не логирует, но значения, переданные в командной строке, видны в истории оболочки (shell history) и в выводе 'ps'; для настоящих секретов лучше --mcp-config-stdin или --mcp-config-file.")
	agentCreateCmd.Flags().Bool("mcp-config-stdin", false, "Прочитать JSON-объект --mcp-config из stdin. Секреты не попадают в историю оболочки (shell history) и в 'ps'. Несовместим с --mcp-config и --mcp-config-file.")
	agentCreateCmd.Flags().String("mcp-config-file", "", "Прочитать JSON-объект --mcp-config из файла по указанному пути (рекомендуемые права: 0600). Несовместим с --mcp-config и --mcp-config-stdin.")
	agentCreateCmd.Flags().String("visibility", "private", "Видимость: private или workspace (сопоставляется с --permission-mode: private -> private, workspace -> public_to с целью workspace)")
	agentCreateCmd.Flags().String("permission-mode", "", "Режим прав на вызов: private (только владелец) или public_to (список разрешённых через --public-to-*). Если задан, приоритетнее --visibility.")
	agentCreateCmd.Flags().Bool("public-to-workspace", false, "public_to: разрешить вызывать агента всем участникам рабочего пространства.")
	agentCreateCmd.Flags().StringSlice("public-to-member", nil, "public_to: разрешить вызывать агента указанным участникам (user id). Можно повторять.")
	agentCreateCmd.Flags().Int32("max-concurrent-tasks", 6, "Максимум одновременных задач")
	agentCreateCmd.Flags().String("output", "json", "Формат вывода: table или json")

	agentUpdateCmd.Flags().String("name", "", "Новое имя")
	agentUpdateCmd.Flags().String("description", "", "Новое описание")
	agentUpdateCmd.Flags().String("instructions", "", "Новые инструкции")
	agentUpdateCmd.Flags().String("runtime-id", "", "Новый ID среды выполнения")
	agentUpdateCmd.Flags().String("runtime-config", "", "Новая конфигурация среды выполнения в виде JSON-строки")
	agentUpdateCmd.Flags().String("model", "", "Новый идентификатор модели. Передайте пустую строку, чтобы сбросить и вернуться к значению по умолчанию среды выполнения.")
	agentUpdateCmd.Flags().String("thinking-level", "", "Новый уровень рассуждений (усилий) для среды выполнения агента. Набор допустимых значений зависит от среды выполнения и модели; некорректные значения отклоняются на сервере, а демон проверяет конкретную пару «модель — уровень». Передайте пустую строку, чтобы сбросить и вернуться к значению по умолчанию среды выполнения.")
	agentUpdateCmd.Flags().String("service-tier", "", "Новый сервисный уровень выполнения из каталога среды выполнения для выбранной модели. Передайте пустую строку, чтобы сбросить и брать настройки локальной среды выполнения.")
	agentUpdateCmd.Flags().String("custom-args", "", "Новые пользовательские аргументы CLI в виде JSON-массива. Для выбора модели лучше использовать --model; некоторые среды выполнения отклоняют --model в custom_args.")

	agentUpdateCmd.Flags().String("mcp-config", "", "Новая конфигурация MCP-серверов в виде JSON-объекта, например '{\"mcpServers\":{...}}'. Передайте 'null', чтобы очистить. Считается секретом — CLI её не логирует, но значения, переданные в командной строке, видны в истории оболочки (shell history) и в выводе 'ps'; для настоящих секретов лучше --mcp-config-stdin или --mcp-config-file.")
	agentUpdateCmd.Flags().Bool("mcp-config-stdin", false, "Прочитать JSON --mcp-config из stdin. Секреты не попадают в историю оболочки (shell history) и в 'ps'. Несовместим с --mcp-config и --mcp-config-file.")
	agentUpdateCmd.Flags().String("mcp-config-file", "", "Прочитать JSON --mcp-config из файла по указанному пути (рекомендуемые права: 0600). Несовместим с --mcp-config и --mcp-config-stdin.")
	agentUpdateCmd.Flags().String("visibility", "", "Новая видимость: private или workspace (сопоставляется с --permission-mode)")
	agentUpdateCmd.Flags().String("permission-mode", "", "Новый режим прав на вызов: private или public_to. Приоритетнее --visibility. Только для владельца.")
	agentUpdateCmd.Flags().Bool("public-to-workspace", false, "public_to: разрешить вызывать агента всем участникам рабочего пространства.")
	agentUpdateCmd.Flags().StringSlice("public-to-member", nil, "public_to: разрешить вызывать агента указанным участникам (user id). Можно повторять.")
	agentUpdateCmd.Flags().String("status", "", "Новый статус")
	agentUpdateCmd.Flags().Int32("max-concurrent-tasks", 0, "Новый максимум одновременных задач")
	agentUpdateCmd.Flags().String("output", "json", "Формат вывода: table или json")

	agentArchiveCmd.Flags().String("output", "json", "Формат вывода: table или json")

	agentRestoreCmd.Flags().String("output", "json", "Формат вывода: table или json")

	agentTasksCmd.Flags().String("output", "table", "Формат вывода: table или json")

	agentAvatarCmd.Flags().String("file", "", "Путь к файлу с изображением аватара (обязательно)")
	agentAvatarCmd.Flags().String("output", "json", "Формат вывода: table или json")

	agentSkillsListCmd.Flags().String("output", "table", "Формат вывода: table или json")

	agentSkillsSetCmd.Flags().StringSlice("skill-ids", nil, "ID skills для назначения (через запятую)")
	agentSkillsSetCmd.Flags().String("output", "json", "Формат вывода: table или json")

	agentSkillsAddCmd.Flags().StringSlice("skill-ids", nil, "ID skills для добавления (через запятую)")
	agentSkillsAddCmd.Flags().String("output", "json", "Формат вывода: table или json")

	agentEnvGetCmd.Flags().String("output", "json", "Формат вывода: json или table")

	agentEnvSetCmd.Flags().String("custom-env", "", "Новый custom_env в виде JSON-объекта, например '{\"KEY\":\"value\"}'. Значения '****' сохраняют существующую запись. Считается секретом — значения, переданные в командной строке, видны в истории оболочки (shell history) и в выводе 'ps'; для настоящих секретов лучше --custom-env-stdin или --custom-env-file. Передайте '{}', чтобы очистить все ключи.")
	agentEnvSetCmd.Flags().Bool("custom-env-stdin", false, "Прочитать новый JSON-объект custom_env из stdin. Секреты не попадают в историю оболочки (shell history) и в 'ps'. Несовместим с --custom-env и --custom-env-file.")
	agentEnvSetCmd.Flags().String("custom-env-file", "", "Прочитать новый JSON-объект custom_env из файла по указанному пути (рекомендуемые права: 0600). Несовместим с --custom-env и --custom-env-stdin.")
	agentEnvSetCmd.Flags().String("output", "json", "Формат вывода: json или table")
}

func resolveProfile(cmd *cobra.Command) string {
	val, _ := cmd.Flags().GetString("profile")
	return val
}

func newAPIClient(cmd *cobra.Command) (*cli.APIClient, error) {
	serverURL := resolveServerURL(cmd)
	workspaceID := resolveWorkspaceID(cmd)
	token := resolveToken(cmd)

	if serverURL == "" {
		return nil, fmt.Errorf("server URL not set: use --server-url flag, GOOSAR_SERVER_URL env, or 'goosar config set server_url <url>'")
	}
	if inDaemonManagedExecutionContext() && !strings.HasPrefix(token, "mat_") {

		if !inAgentExecutionContext() && os.Getenv("GOOSAR_DAEMON_PORT") == "" {
			if markerPath := daemonTaskContextMarkerPath(); markerPath != "" {
				return nil, fmt.Errorf("agent execution context requires GOOSAR_TOKEN to be a task-scoped mat_ token; detected a daemon task marker at %s — if you are not running inside an agent task this is likely a leftover, remove it and retry", markerPath)
			}
		}
		return nil, fmt.Errorf("agent execution context requires GOOSAR_TOKEN to be a task-scoped mat_ token")
	}

	client := cli.NewAPIClient(serverURL, workspaceID, token)

	if agentID := os.Getenv("GOOSAR_AGENT_ID"); agentID != "" {
		client.AgentID = agentID
	}
	if taskID := os.Getenv("GOOSAR_TASK_ID"); taskID != "" {
		client.TaskID = taskID
	}
	return client, nil
}

var (
	defaultCloudServerURL = os.Getenv("GOOSAR_CLOUD_SERVER_URL")
	defaultCloudAppURL    = os.Getenv("GOOSAR_CLOUD_APP_URL")
)

func tryResolveServerURL(cmd *cobra.Command) string {
	val := cli.FlagOrEnv(cmd, "server-url", "GOOSAR_SERVER_URL", "")
	if val != "" {
		return normalizeAPIBaseURL(val)
	}
	профиль := resolveProfile(cmd)
	cfg, err := cli.LoadCLIConfigForProfile(профиль)
	if err == nil && cfg.ServerURL != "" {
		return normalizeAPIBaseURL(cfg.ServerURL)
	}
	return ""
}

func resolveServerURL(cmd *cobra.Command) string {
	if val := tryResolveServerURL(cmd); val != "" {
		return val
	}
	fmt.Fprintln(os.Stderr, "No server configured. Run 'goosar setup' first.")
	os.Exit(1)
	return ""
}

func resolveLoginTokenServerURL(cmd *cobra.Command) string {
	if val := tryResolveServerURL(cmd); val != "" {
		return val
	}
	return defaultCloudServerURL
}

func normalizeAPIBaseURL(raw string) string {
	normalized, err := daemon.NormalizeServerBaseURL(raw)
	if err == nil {
		return normalized
	}
	return raw
}

func inAgentExecutionContext() bool {
	return os.Getenv("GOOSAR_AGENT_ID") != "" || os.Getenv("GOOSAR_TASK_ID") != ""
}

func inDaemonManagedExecutionContext() bool {
	return inAgentExecutionContext() || os.Getenv("GOOSAR_DAEMON_PORT") != "" || hasDaemonTaskContextMarker()
}

func hasDaemonTaskContextMarker() bool {
	return daemonTaskContextMarkerPath() != ""
}

func daemonTaskContextMarkerPath() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	for {
		markerPath := filepath.Join(dir, execenv.TaskContextMarkerRelPath)

		if data, err := os.ReadFile(markerPath); err == nil {
			var marker struct {
				ManagedBy string `json:"managed_by"`
			}
			if json.Unmarshal(data, &marker) == nil && marker.ManagedBy == execenv.TaskContextMarkerManagedBy {
				return markerPath
			}
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

func resolveWorkspaceID(cmd *cobra.Command) string {
	val := cli.FlagOrEnv(cmd, "workspace-id", "GOOSAR_WORKSPACE_ID", "")
	if val != "" {
		return val
	}

	if inDaemonManagedExecutionContext() {
		return ""
	}
	профиль := resolveProfile(cmd)
	cfg, _ := cli.LoadCLIConfigForProfile(профиль)
	return cfg.WorkspaceID
}

func requireWorkspaceID(cmd *cobra.Command) (string, error) {
	id := resolveWorkspaceID(cmd)
	if id == "" {
		if inDaemonManagedExecutionContext() {
			return "", fmt.Errorf("workspace_id is required: GOOSAR_WORKSPACE_ID must be set by the daemon in agent execution context (no fallback to user config)")
		}
		return "", fmt.Errorf("workspace_id is required: use --workspace-id flag, set GOOSAR_WORKSPACE_ID env, or run 'goosar config set workspace_id <id>'")
	}
	return id, nil
}

func runAgentList(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	if client.WorkspaceID == "" {
		if _, err := requireWorkspaceID(cmd); err != nil {
			return err
		}
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var agents []map[string]any
	params := url.Values{}
	params.Set("workspace_id", client.WorkspaceID)
	if v, _ := cmd.Flags().GetBool("include-archived"); v {
		params.Set("include_archived", "true")
	}
	path := "/api/agents"
	if len(params) > 0 {
		path += "?" + params.Encode()
	}
	if err := client.GetJSON(ctx, path, &agents); err != nil {
		return fmt.Errorf("list agents: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, agents)
	}

	headers := []string{"ID", "NAME", "STATUS", "RUNTIME", "ARCHIVED"}
	rows := make([][]string, 0, len(agents))
	for _, a := range agents {
		archived := ""
		if v := strVal(a, "archived_at"); v != "" {
			archived = "yes"
		}
		rows = append(rows, []string{
			strVal(a, "id"),
			strVal(a, "name"),
			strVal(a, "status"),
			strVal(a, "runtime_mode"),
			archived,
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runAgentGet(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var agent map[string]any
	if err := client.GetJSON(ctx, "/api/agents/"+args[0], &agent); err != nil {
		return fmt.Errorf("get agent: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, agent)
	}

	headers := []string{"ID", "NAME", "STATUS", "RUNTIME", "VISIBILITY", "AVATAR_URL", "DESCRIPTION"}
	rows := [][]string{{
		strVal(agent, "id"),
		strVal(agent, "name"),
		strVal(agent, "status"),
		strVal(agent, "runtime_mode"),
		strVal(agent, "visibility"),
		strVal(agent, "avatar_url"),
		strVal(agent, "description"),
	}}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func applyAgentPermissionFlags(cmd *cobra.Command, body map[string]any) {
	hasMode := cmd.Flags().Changed("permission-mode")
	hasWorkspace := cmd.Flags().Changed("public-to-workspace")
	hasMembers := cmd.Flags().Changed("public-to-member")
	if !hasMode && !hasWorkspace && !hasMembers {
		return
	}

	mode := "public_to"
	if hasMode {
		mode, _ = cmd.Flags().GetString("permission-mode")
	}
	body["permission_mode"] = mode

	targets := []map[string]any{}
	if on, _ := cmd.Flags().GetBool("public-to-workspace"); on {
		targets = append(targets, map[string]any{"target_type": "workspace"})
	}
	if members, _ := cmd.Flags().GetStringSlice("public-to-member"); len(members) > 0 {
		for _, m := range members {
			targets = append(targets, map[string]any{"target_type": "member", "target_id": m})
		}
	}
	body["invocation_targets"] = targets
}

func runAgentCreate(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	name, _ := cmd.Flags().GetString("name")
	if name == "" {
		return fmt.Errorf("--name is required")
	}
	runtimeID, _ := cmd.Flags().GetString("runtime-id")
	if runtimeID == "" {
		return fmt.Errorf("--runtime-id is required")
	}

	body := map[string]any{
		"name":       name,
		"runtime_id": runtimeID,
	}
	if v, _ := cmd.Flags().GetString("description"); v != "" {
		body["description"] = v
	}
	if v, _ := cmd.Flags().GetString("instructions"); v != "" {
		body["instructions"] = v
	}
	if cmd.Flags().Changed("runtime-config") {
		v, _ := cmd.Flags().GetString("runtime-config")
		var rc any
		if err := json.Unmarshal([]byte(v), &rc); err != nil {
			return fmt.Errorf("--runtime-config must be valid JSON: %w", err)
		}
		body["runtime_config"] = rc
	}
	if cmd.Flags().Changed("custom-args") {
		v, _ := cmd.Flags().GetString("custom-args")
		ca, err := parseCustomArgs(v)
		if err != nil {
			return err
		}
		body["custom_args"] = ca
	}
	if ce, ok, err := resolveCustomEnv(cmd); err != nil {
		return err
	} else if ok {
		body["custom_env"] = ce
	}
	if mc, ok, err := resolveMcpConfig(cmd); err != nil {
		return err
	} else if ok {
		body["mcp_config"] = mc
	}
	if cmd.Flags().Changed("model") {
		v, _ := cmd.Flags().GetString("model")
		body["model"] = v
	}

	if cmd.Flags().Changed("thinking-level") {
		v, _ := cmd.Flags().GetString("thinking-level")
		body["thinking_level"] = v
	}
	if cmd.Flags().Changed("service-tier") {
		v, _ := cmd.Flags().GetString("service-tier")
		body["service_tier"] = v
	}
	if cmd.Flags().Changed("visibility") {
		v, _ := cmd.Flags().GetString("visibility")
		body["visibility"] = v
	}
	applyAgentPermissionFlags(cmd, body)
	if cmd.Flags().Changed("max-concurrent-tasks") {
		v, _ := cmd.Flags().GetInt32("max-concurrent-tasks")
		body["max_concurrent_tasks"] = v
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var result map[string]any
	if err := client.PostJSON(ctx, "/api/agents", body, &result); err != nil {
		return fmt.Errorf("create agent: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}

	fmt.Printf("Agent created: %s (%s)\n", strVal(result, "name"), strVal(result, "id"))
	return nil
}

func runAgentUpdate(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	body := map[string]any{}
	if cmd.Flags().Changed("name") {
		v, _ := cmd.Flags().GetString("name")
		body["name"] = v
	}
	if cmd.Flags().Changed("description") {
		v, _ := cmd.Flags().GetString("description")
		body["description"] = v
	}
	if cmd.Flags().Changed("instructions") {
		v, _ := cmd.Flags().GetString("instructions")
		body["instructions"] = v
	}
	if cmd.Flags().Changed("runtime-id") {
		v, _ := cmd.Flags().GetString("runtime-id")
		body["runtime_id"] = v
	}
	if cmd.Flags().Changed("runtime-config") {
		v, _ := cmd.Flags().GetString("runtime-config")
		var rc any
		if err := json.Unmarshal([]byte(v), &rc); err != nil {
			return fmt.Errorf("--runtime-config must be valid JSON: %w", err)
		}
		body["runtime_config"] = rc
	}
	if cmd.Flags().Changed("custom-args") {
		v, _ := cmd.Flags().GetString("custom-args")
		ca, err := parseCustomArgs(v)
		if err != nil {
			return err
		}
		body["custom_args"] = ca
	}
	if cmd.Flags().Changed("model") {
		v, _ := cmd.Flags().GetString("model")
		body["model"] = v
	}

	if cmd.Flags().Changed("thinking-level") {
		v, _ := cmd.Flags().GetString("thinking-level")
		body["thinking_level"] = v
	}
	if cmd.Flags().Changed("service-tier") {
		v, _ := cmd.Flags().GetString("service-tier")
		body["service_tier"] = v
	}
	if cmd.Flags().Changed("visibility") {
		v, _ := cmd.Flags().GetString("visibility")
		body["visibility"] = v
	}
	applyAgentPermissionFlags(cmd, body)
	if cmd.Flags().Changed("status") {
		v, _ := cmd.Flags().GetString("status")
		body["status"] = v
	}
	if cmd.Flags().Changed("max-concurrent-tasks") {
		v, _ := cmd.Flags().GetInt32("max-concurrent-tasks")
		body["max_concurrent_tasks"] = v
	}
	if mc, ok, err := resolveMcpConfig(cmd); err != nil {
		return err
	} else if ok {
		body["mcp_config"] = mc
	}

	if len(body) == 0 {
		return fmt.Errorf("no fields to update; use --name, --description, --instructions, --runtime-id, --runtime-config, --model, --thinking-level, --service-tier, --custom-args, --mcp-config, --visibility, --status, or --max-concurrent-tasks (env vars now live behind `goosar agent env set <id>`)")
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var result map[string]any
	if err := client.PutJSON(ctx, "/api/agents/"+args[0], body, &result); err != nil {
		return fmt.Errorf("update agent: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}

	fmt.Printf("Agent updated: %s (%s)\n", strVal(result, "name"), strVal(result, "id"))
	return nil
}

func runAgentArchive(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var result map[string]any
	if err := client.PostJSON(ctx, "/api/agents/"+args[0]+"/archive", nil, &result); err != nil {
		return fmt.Errorf("archive agent: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}

	fmt.Printf("Agent archived: %s (%s)\n", strVal(result, "name"), strVal(result, "id"))
	return nil
}

func runAgentRestore(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var result map[string]any
	if err := client.PostJSON(ctx, "/api/agents/"+args[0]+"/restore", nil, &result); err != nil {
		return fmt.Errorf("restore agent: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}

	fmt.Printf("Agent restored: %s (%s)\n", strVal(result, "name"), strVal(result, "id"))
	return nil
}

func runAgentTasks(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var tasks []map[string]any
	if err := client.GetJSON(ctx, "/api/agents/"+args[0]+"/tasks", &tasks); err != nil {
		return fmt.Errorf("list agent tasks: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, tasks)
	}

	headers := []string{"ID", "ISSUE_ID", "STATUS", "CREATED_AT"}
	rows := make([][]string, 0, len(tasks))
	for _, t := range tasks {
		rows = append(rows, []string{
			strVal(t, "id"),
			strVal(t, "issue_id"),
			strVal(t, "status"),
			strVal(t, "created_at"),
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runAgentAvatar(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	filePath, _ := cmd.Flags().GetString("file")
	if filePath == "" {
		return fmt.Errorf("--file is required")
	}

	info, err := os.Stat(filePath)
	if err != nil {
		return fmt.Errorf("file not found: %w", err)
	}

	ext := strings.ToLower(filepath.Ext(filePath))
	validExts := map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true}
	if !validExts[ext] {
		return fmt.Errorf("unsupported file format %q: must be .png, .jpg, .jpeg, .gif, or .webp", ext)
	}

	const maxSize = 5 << 20
	if info.Size() > maxSize {
		return fmt.Errorf("file too large: %d bytes (max 5MB)", info.Size())
	}

	fileData, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("read file: %w", err)
	}

	if len(fileData) > maxSize {
		return fmt.Errorf("file too large: %d bytes (max 5MB)", len(fileData))
	}

	ctx, cancel := context.WithTimeout(context.Background(), cli.AtLeastAPITimeout(60*time.Second))
	defer cancel()

	var agent map[string]any
	if err := client.GetJSON(ctx, "/api/agents/"+args[0], &agent); err != nil {
		return fmt.Errorf("get agent: %w", err)
	}

	id, url, err := client.UploadFileWithURL(ctx, fileData, filePath)
	if err != nil {
		return fmt.Errorf("upload avatar: %w", err)
	}

	body := map[string]any{"avatar_url": url}
	var result map[string]any
	if err := client.PutJSON(ctx, "/api/agents/"+args[0], body, &result); err != nil {
		return fmt.Errorf("update agent avatar: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, map[string]any{
			"id":         id,
			"agent_id":   args[0],
			"avatar_url": url,
		})
	}

	headers := []string{"ID", "AGENT_ID", "AVATAR_URL"}
	rows := [][]string{{
		id,
		args[0],
		url,
	}}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runAgentSkillsList(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var skills []map[string]any
	if err := client.GetJSON(ctx, "/api/agents/"+args[0]+"/skills", &skills); err != nil {
		return fmt.Errorf("list agent skills: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, skills)
	}

	headers := []string{"ID", "NAME", "DESCRIPTION"}
	rows := make([][]string, 0, len(skills))
	for _, s := range skills {
		rows = append(rows, []string{
			strVal(s, "id"),
			strVal(s, "name"),
			strVal(s, "description"),
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runAgentSkillsSet(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	if !cmd.Flags().Changed("skill-ids") {
		return fmt.Errorf("--skill-ids is required (comma-separated skill IDs; use --skill-ids '' to clear all)")
	}
	cleanIDs := cleanSkillIDsFlag(cmd)
	body := map[string]any{
		"skill_ids": cleanIDs,
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var result json.RawMessage
	if err := client.PutJSON(ctx, "/api/agents/"+args[0]+"/skills", body, &result); err != nil {
		return fmt.Errorf("set agent skills: %w", err)
	}

	return printAgentSkillsMutationResult(cmd, args[0], result)
}

func runAgentSkillsAdd(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	if !cmd.Flags().Changed("skill-ids") {
		return fmt.Errorf("--skill-ids is required (comma-separated skill IDs)")
	}
	cleanIDs := cleanSkillIDsFlag(cmd)
	if len(cleanIDs) == 0 {
		return fmt.Errorf("--skill-ids must include at least one skill ID")
	}
	body := map[string]any{
		"skill_ids": cleanIDs,
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var result json.RawMessage
	if err := client.PostJSON(ctx, "/api/agents/"+args[0]+"/skills/add", body, &result); err != nil {
		return fmt.Errorf("add agent skills: %w", err)
	}

	return printAgentSkillsMutationResult(cmd, args[0], result)
}

func cleanSkillIDsFlag(cmd *cobra.Command) []string {
	skillIDs, _ := cmd.Flags().GetStringSlice("skill-ids")
	cleanIDs := make([]string, 0, len(skillIDs))
	for _, id := range skillIDs {
		id = strings.TrimSpace(id)
		if id != "" {
			cleanIDs = append(cleanIDs, id)
		}
	}
	return cleanIDs
}

func printAgentSkillsMutationResult(cmd *cobra.Command, agentID string, result json.RawMessage) error {
	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		var pretty any
		json.Unmarshal(result, &pretty)
		return cli.PrintJSON(os.Stdout, pretty)
	}

	var skills []map[string]any
	if err := json.Unmarshal(result, &skills); err != nil {
		return fmt.Errorf("decode agent skills response: %w", err)
	}
	if len(skills) == 0 {
		fmt.Printf("No skills assigned to agent %s\n", agentID)
		return nil
	}
	headers := []string{"ID", "NAME", "DESCRIPTION"}
	rows := make([][]string, 0, len(skills))
	for _, s := range skills {
		rows = append(rows, []string{
			strVal(s, "id"),
			strVal(s, "name"),
			strVal(s, "description"),
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runAgentEnvGet(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var resp map[string]any
	if err := client.GetJSON(ctx, "/api/agents/"+args[0]+"/env", &resp); err != nil {
		return fmt.Errorf("get agent env: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, resp)
	}

	masked := resp["values_masked"] == true
	if _, isBool := resp["values_masked"].(bool); !isBool {
		masked = allValuesAreEnvSentinel(resp["custom_env"])
	}

	valueHeader := "VALUE"
	if masked {
		valueHeader = "VALUE (hidden)"
		fmt.Fprintln(os.Stderr,
			"You are not this agent's owner, so its values are hidden. "+
				"The **** below is a placeholder, not a credential. "+
				"You can still set, replace or remove keys with `goosar agent env set`.")
	}

	env, _ := resp["custom_env"].(map[string]any)
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	rows := make([][]string, 0, len(keys))
	for _, k := range keys {
		rows = append(rows, []string{k, fmt.Sprintf("%v", env[k])})
	}
	cli.PrintTable(os.Stdout, []string{"KEY", valueHeader}, rows)
	return nil
}

func allValuesAreEnvSentinel(customEnv any) bool {
	env, ok := customEnv.(map[string]any)
	if !ok || len(env) == 0 {
		return false
	}
	for _, v := range env {
		if v != "****" {
			return false
		}
	}
	return true
}

func runAgentEnvSet(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ce, ok, err := resolveCustomEnv(cmd)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("specify the new env via --custom-env, --custom-env-stdin, or --custom-env-file (pass '{}' to clear)")
	}

	body := map[string]any{"custom_env": ce}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var result map[string]any
	if err := client.PutJSON(ctx, "/api/agents/"+args[0]+"/env", body, &result); err != nil {
		return fmt.Errorf("update agent env: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}

	env, _ := result["custom_env"].(map[string]any)
	fmt.Printf("Env updated for agent %s (%d keys)\n", args[0], len(env))
	return nil
}

func parseCustomEnv(raw string) (map[string]string, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, fmt.Errorf("--custom-env: empty input; pass '{}' to clear")
	}
	var ce map[string]string
	if err := json.Unmarshal([]byte(raw), &ce); err != nil {
		return nil, fmt.Errorf("--custom-env must be a valid JSON object of string keys and string values")
	}
	if ce == nil {
		ce = map[string]string{}
	}
	return ce, nil
}

func parseCustomArgs(raw string) ([]string, error) {
	var ca []string
	if err := json.Unmarshal([]byte(raw), &ca); err != nil {
		return nil, fmt.Errorf("--custom-args must be a valid JSON array of strings")
	}
	return ca, nil
}

func resolveCustomEnv(cmd *cobra.Command) (map[string]string, bool, error) {
	inline := cmd.Flags().Changed("custom-env")
	fromStdin, _ := cmd.Flags().GetBool("custom-env-stdin")
	filePath, _ := cmd.Flags().GetString("custom-env-file")

	fromFile := cmd.Flags().Changed("custom-env-file")

	count := 0
	if inline {
		count++
	}
	if fromStdin {
		count++
	}
	if fromFile {
		count++
	}
	switch {
	case count == 0:
		return nil, false, nil
	case count > 1:
		return nil, false, fmt.Errorf("--custom-env, --custom-env-stdin, and --custom-env-file are mutually exclusive; pick one")
	}

	var raw string
	switch {
	case inline:
		raw, _ = cmd.Flags().GetString("custom-env")
	case fromStdin:

		noInput, statErr := stdinHasNoPipedInput()
		if statErr != nil {
			return nil, false, fmt.Errorf("stat stdin for --custom-env-stdin: %w", statErr)
		}
		if noInput {
			return nil, false, emptyStdinError("custom-env-stdin", "custom-env-file", "custom-env")
		}
		buf, err := io.ReadAll(cmd.InOrStdin())
		if err != nil {
			return nil, false, fmt.Errorf("read --custom-env-stdin: %w", err)
		}
		raw = string(buf)
		if strings.TrimSpace(raw) == "" {
			return nil, false, fmt.Errorf("--custom-env-stdin: empty input; pass '{}' to clear")
		}
	case fromFile:
		if filePath == "" {
			return nil, false, fmt.Errorf("--custom-env-file: path must not be empty")
		}
		buf, err := os.ReadFile(filePath)
		if err != nil {

			return nil, false, fmt.Errorf("read --custom-env-file: %w", err)
		}
		raw = string(buf)
		if strings.TrimSpace(raw) == "" {
			return nil, false, fmt.Errorf("--custom-env-file %q: empty contents; pass '{}' to clear", filePath)
		}
	}

	ce, err := parseCustomEnv(raw)
	if err != nil {
		return nil, false, err
	}
	return ce, true, nil
}

func parseMcpConfig(raw string) (json.RawMessage, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, fmt.Errorf("--mcp-config: empty input; pass 'null' to clear or a JSON object to set")
	}
	var probe any
	if err := json.Unmarshal([]byte(trimmed), &probe); err != nil {
		return nil, fmt.Errorf("--mcp-config must be a valid JSON object, or 'null' to clear")
	}

	if probe == nil {
		return json.RawMessage("null"), nil
	}
	if _, ok := probe.(map[string]any); !ok {
		return nil, fmt.Errorf("--mcp-config must be a JSON object, or 'null' to clear")
	}
	return json.RawMessage(trimmed), nil
}

func resolveMcpConfig(cmd *cobra.Command) (json.RawMessage, bool, error) {
	inline := cmd.Flags().Changed("mcp-config")
	fromStdin, _ := cmd.Flags().GetBool("mcp-config-stdin")
	filePath, _ := cmd.Flags().GetString("mcp-config-file")
	fromFile := cmd.Flags().Changed("mcp-config-file")

	count := 0
	if inline {
		count++
	}
	if fromStdin {
		count++
	}
	if fromFile {
		count++
	}
	switch {
	case count == 0:
		return nil, false, nil
	case count > 1:
		return nil, false, fmt.Errorf("--mcp-config, --mcp-config-stdin, and --mcp-config-file are mutually exclusive; pick one")
	}

	var raw string
	switch {
	case inline:
		raw, _ = cmd.Flags().GetString("mcp-config")
	case fromStdin:

		noInput, statErr := stdinHasNoPipedInput()
		if statErr != nil {
			return nil, false, fmt.Errorf("stat stdin for --mcp-config-stdin: %w", statErr)
		}
		if noInput {
			return nil, false, emptyStdinError("mcp-config-stdin", "mcp-config-file", "mcp-config")
		}
		buf, err := io.ReadAll(cmd.InOrStdin())
		if err != nil {
			return nil, false, fmt.Errorf("read --mcp-config-stdin: %w", err)
		}
		raw = string(buf)
		if strings.TrimSpace(raw) == "" {
			return nil, false, fmt.Errorf("--mcp-config-stdin: empty input; pass 'null' to clear")
		}
	case fromFile:
		if filePath == "" {
			return nil, false, fmt.Errorf("--mcp-config-file: path must not be empty")
		}
		buf, err := os.ReadFile(filePath)
		if err != nil {

			return nil, false, fmt.Errorf("read --mcp-config-file: %w", err)
		}
		raw = string(buf)
		if strings.TrimSpace(raw) == "" {
			return nil, false, fmt.Errorf("--mcp-config-file %q: empty contents; pass 'null' to clear", filePath)
		}
	}

	mc, err := parseMcpConfig(raw)
	if err != nil {
		return nil, false, err
	}
	return mc, true, nil
}

func strVal(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	return fmt.Sprintf("%v", v)
}
