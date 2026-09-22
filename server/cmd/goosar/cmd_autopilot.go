package main

import (
	"bufio"
	"context"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/adanman/goosar/server/internal/cli"
)

var autopilotCmd = &cobra.Command{
	Use:   "autopilot",
	Short: "Управление автопилотами (автоматизации агентов по расписанию или по событию)",
}

var autopilotListCmd = &cobra.Command{
	Use:   "list",
	Short: "Показать автопилоты рабочего пространства",
	RunE:  runAutopilotList,
}

var autopilotGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Показать сведения об автопилоте (учётные данные webhook по умолчанию скрыты)",
	Args:  exactArgs(1),
	RunE:  runAutopilotGet,
}

var autopilotCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Создать автопилот",
	RunE:  runAutopilotCreate,
}

var autopilotUpdateCmd = &cobra.Command{
	Use:   "update <id>",
	Short: "Изменить автопилот",
	Args:  exactArgs(1),
	RunE:  runAutopilotUpdate,
}

var autopilotDeleteCmd = &cobra.Command{
	Use:   "delete <id>",
	Short: "Удалить автопилот",
	Args:  exactArgs(1),
	RunE:  runAutopilotDelete,
}

var autopilotTriggerCmd = &cobra.Command{
	Use:   "trigger <id>",
	Short: "Запустить автопилот вручную один раз",
	Args:  exactArgs(1),
	RunE:  runAutopilotTrigger,
}

var autopilotRunsCmd = &cobra.Command{
	Use:   "runs <id>",
	Short: "Показать историю запусков автопилота",
	Args:  exactArgs(1),
	RunE:  runAutopilotRuns,
}

var autopilotTriggerAddCmd = &cobra.Command{
	Use:   "trigger-add <autopilot-id>",
	Short: "Добавить автопилоту триггер по расписанию или webhook",
	Args:  exactArgs(1),
	RunE:  runAutopilotTriggerAdd,
}

var autopilotTriggerUpdateCmd = &cobra.Command{
	Use:   "trigger-update <autopilot-id> <trigger-id>",
	Short: "Изменить триггер",
	Args:  exactArgs(2),
	RunE:  runAutopilotTriggerUpdate,
}

var autopilotTriggerDeleteCmd = &cobra.Command{
	Use:   "trigger-delete <autopilot-id> <trigger-id>",
	Short: "Удалить триггер",
	Args:  exactArgs(2),
	RunE:  runAutopilotTriggerDelete,
}

var autopilotTriggerRotateURLCmd = &cobra.Command{
	Use:   "trigger-rotate-url <autopilot-id> <trigger-id>",
	Short: "Заменить URL webhook у триггера-webhook",
	Args:  exactArgs(2),
	RunE:  runAutopilotTriggerRotateURL,
}

func init() {
	autopilotCmd.AddCommand(autopilotListCmd)
	autopilotCmd.AddCommand(autopilotGetCmd)
	autopilotCmd.AddCommand(autopilotCreateCmd)
	autopilotCmd.AddCommand(autopilotUpdateCmd)
	autopilotCmd.AddCommand(autopilotDeleteCmd)
	autopilotCmd.AddCommand(autopilotTriggerCmd)
	autopilotCmd.AddCommand(autopilotRunsCmd)
	autopilotCmd.AddCommand(autopilotTriggerAddCmd)
	autopilotCmd.AddCommand(autopilotTriggerUpdateCmd)
	autopilotCmd.AddCommand(autopilotTriggerDeleteCmd)
	autopilotCmd.AddCommand(autopilotTriggerRotateURLCmd)

	autopilotListCmd.Flags().String("status", "", "Фильтр по статусу (active, paused)")
	autopilotListCmd.Flags().String("output", "table", "Формат вывода: table или json")
	autopilotListCmd.Flags().Bool("full-id", false, "Показывать полные UUID в таблице")

	autopilotGetCmd.Flags().String("output", "json", "Формат вывода: table или json")
	autopilotGetCmd.Flags().Bool("show-secrets", false, "Включить действующие учётные данные webhook в JSON-вывод (небезопасно для логов)")

	autopilotCreateCmd.Flags().String("title", "", "Название автопилота (обязательно)")
	autopilotCreateCmd.Flags().String("description", "", "Описание автопилота (используется как промпт задачи)")
	autopilotCreateCmd.Flags().String("agent", "", "Агент-исполнитель (имя или ID) — обязательно")
	autopilotCreateCmd.Flags().String("mode", "", "Режим выполнения: create_issue или run_only (обязательно)")
	autopilotCreateCmd.Flags().String("priority", "none", "Приоритет создаваемых issue (none, low, medium, high, urgent)")
	autopilotCreateCmd.Flags().String("project", "", "ID проекта (необязательно)")
	autopilotCreateCmd.Flags().String("issue-title-template", "", "Шаблон заголовков issue (режим create_issue). Подставляется только {{date}} (UTC, YYYY-MM-DD); любой другой токен {{...}} отклоняется при создании.")
	autopilotCreateCmd.Flags().StringArray("subscriber", nil, "Участник-подписчик, которого уведомляют об issue, созданных автопилотом (имя или ID пользователя; флаг можно повторять)")
	autopilotCreateCmd.Flags().String("output", "json", "Формат вывода: table или json")

	autopilotUpdateCmd.Flags().String("title", "", "Новое название")
	autopilotUpdateCmd.Flags().String("description", "", "Новое описание")
	autopilotUpdateCmd.Flags().String("agent", "", "Новый агент-исполнитель (имя или ID)")
	autopilotUpdateCmd.Flags().String("project", "", "Новый ID проекта (пустая строка очищает)")
	autopilotUpdateCmd.Flags().String("priority", "", "Новый приоритет")
	autopilotUpdateCmd.Flags().String("status", "", "Новый статус (active, paused)")
	autopilotUpdateCmd.Flags().String("mode", "", "Новый режим выполнения (create_issue или run_only)")
	autopilotUpdateCmd.Flags().String("issue-title-template", "", "Новый шаблон заголовков issue. Подставляется только {{date}} (UTC, YYYY-MM-DD); любой другой токен {{...}} отклоняется.")
	autopilotUpdateCmd.Flags().StringArray("subscriber", nil, "Заменить подписчиков этим участником (имя или ID пользователя; флаг можно повторять)")
	autopilotUpdateCmd.Flags().Bool("clear-subscribers", false, "Убрать всех подписчиков автопилота")
	autopilotUpdateCmd.Flags().String("output", "json", "Формат вывода: table или json")

	autopilotTriggerCmd.Flags().String("output", "json", "Формат вывода: table или json")

	autopilotRunsCmd.Flags().Int("limit", 20, "Максимум запусков в ответе")
	autopilotRunsCmd.Flags().Int("offset", 0, "Смещение для постраничного вывода")
	autopilotRunsCmd.Flags().String("output", "table", "Формат вывода: table или json")

	autopilotTriggerAddCmd.Flags().String("kind", "schedule", "Тип триггера: schedule или webhook")
	autopilotTriggerAddCmd.Flags().String("cron", "", "Выражение cron (обязательно для --kind schedule)")
	autopilotTriggerAddCmd.Flags().String("timezone", "", "Часовой пояс IANA (по умолчанию UTC; только для schedule)")
	autopilotTriggerAddCmd.Flags().String("label", "", "Необязательная понятная подпись")
	autopilotTriggerAddCmd.Flags().String("output", "json", "Формат вывода: table или json")

	autopilotTriggerRotateURLCmd.Flags().String("output", "json", "Формат вывода: table или json")
	autopilotTriggerRotateURLCmd.Flags().BoolP("yes", "y", false, "Пропустить запрос подтверждения")

	autopilotTriggerUpdateCmd.Flags().Bool("enabled", true, "Включить или выключить триггер")
	autopilotTriggerUpdateCmd.Flags().String("cron", "", "Новое выражение cron")
	autopilotTriggerUpdateCmd.Flags().String("timezone", "", "Новый часовой пояс IANA")
	autopilotTriggerUpdateCmd.Flags().String("label", "", "Новая подпись")
	autopilotTriggerUpdateCmd.Flags().String("output", "json", "Формат вывода: table или json")
}

func runAutopilotList(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	if _, err := requireWorkspaceID(cmd); err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	path := "/api/autopilots"
	if status, _ := cmd.Flags().GetString("status"); status != "" {
		path += "?" + url.Values{"status": {status}}.Encode()
	}

	var resp struct {
		Autopilots []map[string]any `json:"autopilots"`
		Total      int              `json:"total"`
	}
	if err := client.GetJSON(ctx, path, &resp); err != nil {
		return fmt.Errorf("list autopilots: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, resp)
	}

	fullID, _ := cmd.Flags().GetBool("full-id")
	actors := loadActorDisplayLookup(ctx, client)
	headers := []string{"ID", "TITLE", "STATUS", "MODE", "ASSIGNEE", "LAST_RUN"}
	rows := make([][]string, 0, len(resp.Autopilots))
	for _, a := range resp.Autopilots {
		rows = append(rows, []string{
			displayID(strVal(a, "id"), fullID),
			strVal(a, "title"),
			strVal(a, "status"),
			strVal(a, "execution_mode"),
			actors.agent(strVal(a, "assignee_id")),
			strVal(a, "last_run_at"),
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runAutopilotGet(cmd *cobra.Command, args []string) error {
	output, _ := cmd.Flags().GetString("output")
	showSecrets, _ := cmd.Flags().GetBool("show-secrets")
	if showSecrets && output != "json" {
		return fmt.Errorf("--show-secrets requires --output json")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	autopilotRef, err := resolveAutopilotID(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve autopilot: %w", err)
	}

	var resp map[string]any
	if err := client.GetJSON(ctx, "/api/autopilots/"+autopilotRef.ID, &resp); err != nil {
		return fmt.Errorf("get autopilot: %w", err)
	}

	if showSecrets {
		fmt.Fprintln(os.Stderr, "Warning: --show-secrets exposes live webhook credentials; keep this output out of logs and shared transcripts.")
	} else {
		redactAutopilotWebhookCredentials(resp)
	}

	if output == "json" {
		return cli.PrintJSON(os.Stdout, resp)
	}

	ap, _ := resp["autopilot"].(map[string]any)
	actors := loadActorDisplayLookup(ctx, client)
	headers := []string{"ID", "TITLE", "STATUS", "MODE", "ASSIGNEE", "LAST_RUN"}
	rows := [][]string{{
		strVal(ap, "id"),
		strVal(ap, "title"),
		strVal(ap, "status"),
		strVal(ap, "execution_mode"),
		actors.agent(strVal(ap, "assignee_id")),
		strVal(ap, "last_run_at"),
	}}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func redactAutopilotWebhookCredentials(resp map[string]any) {
	triggers, ok := resp["triggers"].([]any)
	if !ok {
		return
	}
	for _, raw := range triggers {
		trigger, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		_, hasTokenField := trigger["webhook_token"]
		_, hasPathField := trigger["webhook_path"]
		_, hasURLField := trigger["webhook_url"]
		if !hasTokenField && !hasPathField && !hasURLField {
			continue
		}

		token := strVal(trigger, "webhook_token")
		hasToken, _ := trigger["has_webhook_token"].(bool)
		hasToken = hasToken ||
			strVal(trigger, "kind") == "webhook" ||
			token != "" ||
			strVal(trigger, "webhook_path") != "" ||
			strVal(trigger, "webhook_url") != ""
		trigger["has_webhook_token"] = hasToken
		if hint := webhookTokenHint(token); hint != "" {
			trigger["webhook_token_hint"] = hint
		} else {
			trigger["webhook_token_hint"] = nil
		}
		trigger["webhook_token"] = nil
		trigger["webhook_path"] = nil
		trigger["webhook_url"] = nil
	}
}

func webhookTokenHint(token string) string {
	if len(token) < 4 {
		return ""
	}
	return token[len(token)-4:]
}

func runAutopilotCreate(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	if _, err := requireWorkspaceID(cmd); err != nil {
		return err
	}

	title, _ := cmd.Flags().GetString("title")
	if title == "" {
		return fmt.Errorf("--title is required")
	}
	agent, _ := cmd.Flags().GetString("agent")
	if agent == "" {
		return fmt.Errorf("--agent is required (agent name or ID)")
	}
	mode, _ := cmd.Flags().GetString("mode")
	if mode == "" {
		return fmt.Errorf("--mode is required (create_issue or run_only)")
	}
	if mode != "create_issue" && mode != "run_only" {
		return fmt.Errorf("--mode must be create_issue or run_only")
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	agentID, err := resolveAgent(ctx, client, agent)
	if err != nil {
		return fmt.Errorf("resolve agent: %w", err)
	}

	body := map[string]any{
		"title":          title,
		"assignee_id":    agentID,
		"execution_mode": mode,
	}
	if v, _ := cmd.Flags().GetString("description"); v != "" {
		body["description"] = v
	}
	if cmd.Flags().Changed("priority") {
		v, _ := cmd.Flags().GetString("priority")
		body["priority"] = v
	}
	if v, _ := cmd.Flags().GetString("project"); v != "" {
		projectRef, err := resolveProjectID(ctx, client, v)
		if err != nil {
			return fmt.Errorf("resolve project: %w", err)
		}
		body["project_id"] = projectRef.ID
	}
	if v, _ := cmd.Flags().GetString("issue-title-template"); v != "" {
		body["issue_title_template"] = v
	}
	if subscriberRefs, _ := cmd.Flags().GetStringArray("subscriber"); len(subscriberRefs) > 0 {
		subscribers, err := resolveAutopilotSubscriberInputs(ctx, client, subscriberRefs)
		if err != nil {
			return err
		}
		body["subscribers"] = subscribers
	}

	var result map[string]any
	if err := client.PostJSON(ctx, "/api/autopilots", body, &result); err != nil {
		return fmt.Errorf("create autopilot: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	fmt.Printf("Autopilot created: %s (%s)\n", strVal(result, "title"), strVal(result, "id"))
	return nil
}

func runAutopilotUpdate(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	autopilotRef, err := resolveAutopilotID(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve autopilot: %w", err)
	}

	body := map[string]any{}
	if cmd.Flags().Changed("title") {
		v, _ := cmd.Flags().GetString("title")
		body["title"] = v
	}
	if cmd.Flags().Changed("description") {
		v, _ := cmd.Flags().GetString("description")
		body["description"] = v
	}
	if cmd.Flags().Changed("agent") {
		v, _ := cmd.Flags().GetString("agent")
		agentID, resolveErr := resolveAgent(ctx, client, v)
		if resolveErr != nil {
			return fmt.Errorf("resolve agent: %w", resolveErr)
		}
		body["assignee_type"] = "agent"
		body["assignee_id"] = agentID
	}
	if cmd.Flags().Changed("project") {
		v, _ := cmd.Flags().GetString("project")
		if v == "" {
			body["project_id"] = nil
		} else {
			projectRef, err := resolveProjectID(ctx, client, v)
			if err != nil {
				return fmt.Errorf("resolve project: %w", err)
			}
			body["project_id"] = projectRef.ID
		}
	}
	if cmd.Flags().Changed("priority") {
		v, _ := cmd.Flags().GetString("priority")
		body["priority"] = v
	}
	if cmd.Flags().Changed("status") {
		v, _ := cmd.Flags().GetString("status")
		body["status"] = v
	}
	if cmd.Flags().Changed("mode") {
		v, _ := cmd.Flags().GetString("mode")
		if v != "create_issue" && v != "run_only" {
			return fmt.Errorf("--mode must be create_issue or run_only")
		}
		body["execution_mode"] = v
	}
	if cmd.Flags().Changed("issue-title-template") {
		v, _ := cmd.Flags().GetString("issue-title-template")
		body["issue_title_template"] = v
	}
	clearSubscribers, _ := cmd.Flags().GetBool("clear-subscribers")
	subscriberRefs, _ := cmd.Flags().GetStringArray("subscriber")
	if clearSubscribers && len(subscriberRefs) > 0 {
		return fmt.Errorf("--subscriber and --clear-subscribers are mutually exclusive")
	}
	if clearSubscribers {
		body["subscribers"] = []map[string]string{}
	} else if cmd.Flags().Changed("subscriber") {
		subscribers, err := resolveAutopilotSubscriberInputs(ctx, client, subscriberRefs)
		if err != nil {
			return err
		}
		body["subscribers"] = subscribers
	}

	if len(body) == 0 {
		return fmt.Errorf("no fields to update; use flags like --title, --description, --agent, --status, --mode, etc.")
	}

	var result map[string]any
	if err := client.PatchJSON(ctx, "/api/autopilots/"+autopilotRef.ID, body, &result); err != nil {
		return fmt.Errorf("update autopilot: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	fmt.Printf("Autopilot updated: %s (%s)\n", strVal(result, "title"), strVal(result, "id"))
	return nil
}

func runAutopilotDelete(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	autopilotRef, err := resolveAutopilotID(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve autopilot: %w", err)
	}

	if err := client.DeleteJSON(ctx, "/api/autopilots/"+autopilotRef.ID); err != nil {
		return fmt.Errorf("delete autopilot: %w", err)
	}
	fmt.Printf("Autopilot %s deleted.\n", autopilotRef.Display)
	return nil
}

func runAutopilotTrigger(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), cli.AtLeastAPITimeout(30*time.Second))
	defer cancel()

	autopilotRef, err := resolveAutopilotID(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve autopilot: %w", err)
	}

	var run map[string]any
	if err := client.PostJSON(ctx, "/api/autopilots/"+autopilotRef.ID+"/trigger", nil, &run); err != nil {
		return fmt.Errorf("trigger autopilot: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, run)
	}
	fmt.Printf("Autopilot triggered: run %s (status: %s)\n", strVal(run, "id"), strVal(run, "status"))
	return nil
}

func runAutopilotRuns(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	autopilotRef, err := resolveAutopilotID(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve autopilot: %w", err)
	}

	params := url.Values{}
	if v, _ := cmd.Flags().GetInt("limit"); v > 0 {
		params.Set("limit", fmt.Sprintf("%d", v))
	}
	if v, _ := cmd.Flags().GetInt("offset"); v > 0 {
		params.Set("offset", fmt.Sprintf("%d", v))
	}
	path := "/api/autopilots/" + autopilotRef.ID + "/runs"
	if len(params) > 0 {
		path += "?" + params.Encode()
	}

	var resp struct {
		Runs  []map[string]any `json:"runs"`
		Total int              `json:"total"`
	}
	if err := client.GetJSON(ctx, path, &resp); err != nil {
		return fmt.Errorf("list runs: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, resp)
	}

	headers := []string{"ID", "SOURCE", "STATUS", "ISSUE", "TRIGGERED_AT", "COMPLETED_AT"}
	rows := make([][]string, 0, len(resp.Runs))
	for _, r := range resp.Runs {
		rows = append(rows, []string{
			strVal(r, "id"),
			strVal(r, "source"),
			strVal(r, "status"),
			strVal(r, "issue_id"),
			strVal(r, "triggered_at"),
			strVal(r, "completed_at"),
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runAutopilotTriggerAdd(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	kind, _ := cmd.Flags().GetString("kind")
	if kind == "" {
		kind = "schedule"
	}
	if kind != "schedule" && kind != "webhook" {
		return fmt.Errorf("--kind must be schedule or webhook")
	}
	cron, _ := cmd.Flags().GetString("cron")
	if kind == "schedule" && cron == "" {
		return fmt.Errorf("--cron is required for --kind schedule")
	}
	if kind == "webhook" {
		if v, _ := cmd.Flags().GetString("timezone"); v != "" {
			return fmt.Errorf("--timezone is only valid with --kind schedule")
		}
		if cron != "" {
			return fmt.Errorf("--cron is only valid with --kind schedule")
		}
	}

	body := map[string]any{"kind": kind}
	if kind == "schedule" {
		body["cron_expression"] = cron
		if v, _ := cmd.Flags().GetString("timezone"); v != "" {
			body["timezone"] = v
		}
	}
	if v, _ := cmd.Flags().GetString("label"); v != "" {
		body["label"] = v
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	autopilotRef, err := resolveAutopilotID(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve autopilot: %w", err)
	}

	var result map[string]any
	if err := client.PostJSON(ctx, "/api/autopilots/"+autopilotRef.ID+"/triggers", body, &result); err != nil {
		return fmt.Errorf("create trigger: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	fmt.Printf("Trigger created: %s (kind=%s)\n", strVal(result, "id"), strVal(result, "kind"))
	if kind == "webhook" {
		printWebhookURL(client, result)
	}
	return nil
}

func printWebhookURL(client *cli.APIClient, trigger map[string]any) {
	if u := strVal(trigger, "webhook_url"); u != "" {
		fmt.Printf("Webhook URL: %s\n", u)
		return
	}
	if path := strVal(trigger, "webhook_path"); path != "" {
		base := strings.TrimRight(client.BaseURL, "/")
		fmt.Printf("Webhook URL: %s%s\n", base, path)
	}
}

func runAutopilotTriggerRotateURL(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	autopilotRef, err := resolveAutopilotID(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve autopilot: %w", err)
	}
	triggerRef, err := resolveAutopilotTriggerID(ctx, client, autopilotRef.ID, args[1])
	if err != nil {
		return fmt.Errorf("resolve trigger: %w", err)
	}

	yes, _ := cmd.Flags().GetBool("yes")
	if !yes {
		fmt.Fprintln(os.Stderr, "This will invalidate the current webhook URL immediately. Continue? [y/N] ")
		reader := bufio.NewReader(os.Stdin)
		answer, _ := reader.ReadString('\n')
		answer = strings.TrimSpace(strings.ToLower(answer))
		if answer != "y" && answer != "yes" {
			fmt.Fprintln(os.Stderr, "Aborted.")
			return nil
		}
	}

	var result map[string]any
	path := "/api/autopilots/" + autopilotRef.ID + "/triggers/" + triggerRef.ID + "/rotate-webhook-token"
	if err := client.PostJSON(ctx, path, nil, &result); err != nil {
		return fmt.Errorf("rotate webhook url: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	fmt.Printf("Webhook URL rotated for trigger %s\n", strVal(result, "id"))
	printWebhookURL(client, result)
	return nil
}

func runAutopilotTriggerUpdate(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	body := map[string]any{}
	if cmd.Flags().Changed("enabled") {
		v, _ := cmd.Flags().GetBool("enabled")
		body["enabled"] = v
	}
	if cmd.Flags().Changed("cron") {
		v, _ := cmd.Flags().GetString("cron")
		body["cron_expression"] = v
	}
	if cmd.Flags().Changed("timezone") {
		v, _ := cmd.Flags().GetString("timezone")
		body["timezone"] = v
	}
	if cmd.Flags().Changed("label") {
		v, _ := cmd.Flags().GetString("label")
		body["label"] = v
	}
	if len(body) == 0 {
		return fmt.Errorf("no fields to update; use --enabled, --cron, --timezone, or --label")
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	autopilotRef, err := resolveAutopilotID(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve autopilot: %w", err)
	}
	triggerRef, err := resolveAutopilotTriggerID(ctx, client, autopilotRef.ID, args[1])
	if err != nil {
		return fmt.Errorf("resolve trigger: %w", err)
	}

	var result map[string]any
	path := "/api/autopilots/" + autopilotRef.ID + "/triggers/" + triggerRef.ID
	if err := client.PatchJSON(ctx, path, body, &result); err != nil {
		return fmt.Errorf("update trigger: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	fmt.Printf("Trigger updated: %s\n", strVal(result, "id"))
	return nil
}

func runAutopilotTriggerDelete(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	autopilotRef, err := resolveAutopilotID(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve autopilot: %w", err)
	}
	triggerRef, err := resolveAutopilotTriggerID(ctx, client, autopilotRef.ID, args[1])
	if err != nil {
		return fmt.Errorf("resolve trigger: %w", err)
	}

	path := "/api/autopilots/" + autopilotRef.ID + "/triggers/" + triggerRef.ID
	if err := client.DeleteJSON(ctx, path); err != nil {
		return fmt.Errorf("delete trigger: %w", err)
	}
	fmt.Printf("Trigger %s deleted.\n", triggerRef.ID)
	return nil
}

func resolveAutopilotSubscriberInputs(ctx context.Context, client *cli.APIClient, refs []string) ([]map[string]string, error) {
	inputs := make([]map[string]string, 0, len(refs))
	seen := map[string]struct{}{}
	memberOnly := assigneeKinds{member: true}
	for _, ref := range refs {
		if strings.TrimSpace(ref) == "" {
			return nil, fmt.Errorf("--subscriber cannot be empty")
		}
		userType, userID, err := resolveAssignee(ctx, client, ref, memberOnly)
		if err != nil {
			return nil, fmt.Errorf("resolve subscriber %q: %w", ref, err)
		}
		if userType != "member" {
			return nil, fmt.Errorf("subscriber %q resolved to %s; autopilot subscribers must be members", ref, userType)
		}
		if _, ok := seen[userID]; ok {
			continue
		}
		seen[userID] = struct{}{}
		inputs = append(inputs, map[string]string{
			"user_type": "member",
			"user_id":   userID,
		})
	}
	return inputs, nil
}

var uuidRegexp = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func resolveAgent(ctx context.Context, client *cli.APIClient, nameOrID string) (string, error) {
	if uuidRegexp.MatchString(nameOrID) {
		return nameOrID, nil
	}
	if client.WorkspaceID == "" {
		return "", fmt.Errorf("workspace ID is required to resolve agents; use --workspace-id or set GOOSAR_WORKSPACE_ID")
	}

	var agents []map[string]any
	agentPath := "/api/agents?" + url.Values{"workspace_id": {client.WorkspaceID}}.Encode()
	if err := client.GetJSON(ctx, agentPath, &agents); err != nil {
		return "", fmt.Errorf("fetch agents: %w", err)
	}

	nameLower := strings.ToLower(nameOrID)
	type match struct{ ID, Name string }
	var matches []match
	for _, a := range agents {
		aName := strVal(a, "name")
		if strings.Contains(strings.ToLower(aName), nameLower) {
			matches = append(matches, match{ID: strVal(a, "id"), Name: aName})
		}
	}

	switch len(matches) {
	case 0:
		return "", fmt.Errorf("no agent found matching %q", nameOrID)
	case 1:
		return matches[0].ID, nil
	default:
		var parts []string
		for _, m := range matches {
			parts = append(parts, fmt.Sprintf("  %q (%s)", m.Name, truncateID(m.ID)))
		}
		return "", fmt.Errorf("ambiguous agent %q; matches:\n%s", nameOrID, strings.Join(parts, "\n"))
	}
}
