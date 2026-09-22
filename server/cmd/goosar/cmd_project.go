package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/adanman/goosar/server/internal/cli"
)

var projectCmd = &cobra.Command{
	Use:   "project",
	Short: "Работа с проектами",
}

var projectListCmd = &cobra.Command{
	Use:   "list",
	Short: "Показать проекты рабочего пространства",
	RunE:  runProjectList,
}

var projectGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Показать сведения о проекте",
	Args:  exactArgs(1),
	RunE:  runProjectGet,
}

var projectCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Создать проект",
	RunE:  runProjectCreate,
}

var projectUpdateCmd = &cobra.Command{
	Use:   "update <id>",
	Short: "Изменить проект",
	Args:  exactArgs(1),
	RunE:  runProjectUpdate,
}

var projectDeleteCmd = &cobra.Command{
	Use:   "delete <id>",
	Short: "Удалить проект",
	Args:  exactArgs(1),
	RunE:  runProjectDelete,
}

var projectStatusCmd = &cobra.Command{
	Use:   "status <id> <status>",
	Short: "Изменить статус проекта",
	Args:  exactArgs(2),
	RunE:  runProjectStatus,
}

var projectResourceCmd = &cobra.Command{
	Use:   "resource",
	Short: "Управление ресурсами, прикреплёнными к проекту",
}

var projectResourceListCmd = &cobra.Command{
	Use:   "list <project-id>",
	Short: "Показать ресурсы, прикреплённые к проекту",
	Args:  exactArgs(1),
	RunE:  runProjectResourceList,
}

var projectResourceAddCmd = &cobra.Command{
	Use:   "add <project-id>",
	Short: "Прикрепить ресурс к проекту (например, --type github_repo --url <url>)",
	Args:  exactArgs(1),
	RunE:  runProjectResourceAdd,
}

var projectResourceUpdateCmd = &cobra.Command{
	Use:   "update <project-id> <resource-id>",
	Short: "Изменить прикреплённый ресурс (ref, метку или позицию)",
	Args:  exactArgs(2),
	RunE:  runProjectResourceUpdate,
}

var projectResourceRemoveCmd = &cobra.Command{
	Use:   "remove <project-id> <resource-id>",
	Short: "Открепить ресурс от проекта",
	Args:  exactArgs(2),
	RunE:  runProjectResourceRemove,
}

var validProjectStatuses = []string{
	"planned", "in_progress", "paused", "completed", "cancelled",
}

func validateProjectStatus(status string) error {
	for _, s := range validProjectStatuses {
		if s == status {
			return nil
		}
	}
	return fmt.Errorf("invalid status %q; valid values: %s", status, strings.Join(validProjectStatuses, ", "))
}

func init() {
	projectCmd.AddCommand(projectListCmd)
	projectCmd.AddCommand(projectGetCmd)
	projectCmd.AddCommand(projectCreateCmd)
	projectCmd.AddCommand(projectUpdateCmd)
	projectCmd.AddCommand(projectDeleteCmd)
	projectCmd.AddCommand(projectStatusCmd)
	projectCmd.AddCommand(projectResourceCmd)

	projectResourceCmd.AddCommand(projectResourceListCmd)
	projectResourceCmd.AddCommand(projectResourceAddCmd)
	projectResourceCmd.AddCommand(projectResourceUpdateCmd)
	projectResourceCmd.AddCommand(projectResourceRemoveCmd)

	projectListCmd.Flags().String("output", "table", "Формат вывода: table или json")
	projectListCmd.Flags().Bool("full-id", false, "Показывать полные UUID в таблице")
	projectListCmd.Flags().String("status", "", "Фильтр по статусу")

	projectGetCmd.Flags().String("output", "json", "Формат вывода: table или json")

	projectCreateCmd.Flags().String("title", "", "Название проекта (обязательно)")
	projectCreateCmd.Flags().String("description", "", "Описание проекта")
	projectCreateCmd.Flags().String("status", "", "Статус проекта")
	projectCreateCmd.Flags().String("icon", "", "Значок проекта (эмодзи)")
	projectCreateCmd.Flags().String("lead", "", "Имя ответственного (участник или агент)")
	projectCreateCmd.Flags().String("start-date", "", "Дата начала (календарный день, YYYY-MM-DD)")
	projectCreateCmd.Flags().String("due-date", "", "Срок (календарный день, YYYY-MM-DD)")
	projectCreateCmd.Flags().StringArray("repo", nil, "Прикрепить ресурс github_repo по URL (флаг можно повторять)")
	projectCreateCmd.Flags().String("output", "json", "Формат вывода: table или json")

	projectResourceListCmd.Flags().String("output", "table", "Формат вывода: table или json")
	projectResourceListCmd.Flags().Bool("full-id", false, "Показывать полные UUID в таблице")

	projectResourceAddCmd.Flags().String("type", "github_repo", "Тип ресурса (например, github_repo, local_directory — см. документацию)")
	projectResourceAddCmd.Flags().String("url", "", "Сокращение: URL репозитория (только для --type github_repo)")
	projectResourceAddCmd.Flags().String("default-branch-hint", "", "Сокращение: подсказка о ветке по умолчанию, необязательно (только для --type github_repo)")
	projectResourceAddCmd.Flags().String("local-path", "", "Сокращение: абсолютный путь к рабочему каталогу (только для --type local_directory)")
	projectResourceAddCmd.Flags().String("daemon-id", "", "Сокращение: id демона, которому принадлежит локальный путь (только для --type local_directory)")
	projectResourceAddCmd.Flags().String("ref-label", "", "Сокращение: необязательная метка внутри resource_ref (только для --type local_directory)")
	projectResourceAddCmd.Flags().String("ref", "", "Произвольный JSON-объект resource_ref или ref для checkout github_repo при использовании с --url")
	projectResourceAddCmd.Flags().String("label", "", "Необязательная понятная подпись")
	projectResourceAddCmd.Flags().String("output", "json", "Формат вывода: table или json")

	projectResourceUpdateCmd.Flags().String("url", "", "Сокращение: новый URL репозитория (github_repo)")
	projectResourceUpdateCmd.Flags().String("default-branch-hint", "", "Сокращение: новая подсказка о ветке по умолчанию (github_repo)")
	projectResourceUpdateCmd.Flags().String("local-path", "", "Сокращение: новый абсолютный локальный путь (local_directory)")
	projectResourceUpdateCmd.Flags().String("daemon-id", "", "Сокращение: новый id демона (local_directory)")
	projectResourceUpdateCmd.Flags().String("ref-label", "", "Сокращение: новая метка внутри resource_ref (local_directory)")
	projectResourceUpdateCmd.Flags().String("ref", "", "Произвольный JSON-объект resource_ref или ref для checkout github_repo")
	projectResourceUpdateCmd.Flags().String("label", "", "Новая понятная подпись; пустая строка очищает её")
	projectResourceUpdateCmd.Flags().Bool("clear-label", false, "Очистить понятную подпись")
	projectResourceUpdateCmd.Flags().Int32("position", 0, "Новая позиция в списке")
	projectResourceUpdateCmd.Flags().String("output", "json", "Формат вывода: table или json")

	projectResourceRemoveCmd.Flags().String("output", "table", "Формат вывода: table или json")

	projectUpdateCmd.Flags().String("title", "", "Новое название")
	projectUpdateCmd.Flags().String("description", "", "Новое описание")
	projectUpdateCmd.Flags().String("status", "", "Новый статус")
	projectUpdateCmd.Flags().String("icon", "", "Новый значок (эмодзи)")
	projectUpdateCmd.Flags().String("lead", "", "Имя нового ответственного (участник или агент)")
	projectUpdateCmd.Flags().String("start-date", "", "Новая дата начала (календарный день, YYYY-MM-DD; пустая строка очищает)")
	projectUpdateCmd.Flags().String("due-date", "", "Новый срок (календарный день, YYYY-MM-DD; пустая строка очищает)")
	projectUpdateCmd.Flags().String("output", "json", "Формат вывода: table или json")

	projectDeleteCmd.Flags().String("output", "json", "Формат вывода: table или json")

	projectStatusCmd.Flags().String("output", "table", "Формат вывода: table или json")
}

func runProjectList(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	params := url.Values{}
	if client.WorkspaceID != "" {
		params.Set("workspace_id", client.WorkspaceID)
	}
	if v, _ := cmd.Flags().GetString("status"); v != "" {
		params.Set("status", v)
	}

	path := "/api/projects"
	if len(params) > 0 {
		path += "?" + params.Encode()
	}

	var result map[string]any
	if err := client.GetJSON(ctx, path, &result); err != nil {
		return fmt.Errorf("list projects: %w", err)
	}

	projectsRaw, _ := result["projects"].([]any)

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, projectsRaw)
	}

	fullID, _ := cmd.Flags().GetBool("full-id")
	actors := loadActorDisplayLookup(ctx, client)
	headers := []string{"ID", "TITLE", "STATUS", "LEAD", "CREATED"}
	rows := make([][]string, 0, len(projectsRaw))
	for _, raw := range projectsRaw {
		p, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		lead := formatLead(p, actors)
		created := strVal(p, "created_at")
		if len(created) >= 10 {
			created = created[:10]
		}
		rows = append(rows, []string{
			displayID(strVal(p, "id"), fullID),
			strVal(p, "title"),
			strVal(p, "status"),
			lead,
			created,
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runProjectGet(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	projectRef, err := resolveProjectID(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve project: %w", err)
	}

	var project map[string]any
	if err := client.GetJSON(ctx, "/api/projects/"+projectRef.ID, &project); err != nil {
		return fmt.Errorf("get project: %w", err)
	}

	if n, _ := project["resource_count"].(float64); n > 0 {
		fmt.Fprintf(os.Stderr, "%d resource(s) attached — run `goosar project resource list %s` to view.\n",
			int64(n), strVal(project, "id"))
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		actors := loadActorDisplayLookup(ctx, client)
		lead := formatLead(project, actors)
		headers := []string{"ID", "TITLE", "STATUS", "LEAD", "DESCRIPTION"}
		rows := [][]string{{
			strVal(project, "id"),
			strVal(project, "title"),
			strVal(project, "status"),
			lead,
			strVal(project, "description"),
		}}
		cli.PrintTable(os.Stdout, headers, rows)
		return nil
	}

	return cli.PrintJSON(os.Stdout, project)
}

func runProjectCreate(cmd *cobra.Command, _ []string) error {
	title, _ := cmd.Flags().GetString("title")
	if title == "" {
		return fmt.Errorf("--title is required")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	body := map[string]any{"title": title}
	if v, _ := cmd.Flags().GetString("description"); v != "" {
		body["description"] = v
	}
	if v, _ := cmd.Flags().GetString("status"); v != "" {
		if err := validateProjectStatus(v); err != nil {
			return err
		}
		body["status"] = v
	}
	if v, _ := cmd.Flags().GetString("icon"); v != "" {
		body["icon"] = v
	}
	if v, _ := cmd.Flags().GetString("lead"); v != "" {
		aType, aID, resolveErr := resolveAssignee(ctx, client, v, memberOrAgentKinds)
		if resolveErr != nil {
			return fmt.Errorf("resolve lead: %w", resolveErr)
		}
		body["lead_type"] = aType
		body["lead_id"] = aID
	}
	if v, _ := cmd.Flags().GetString("start-date"); v != "" {
		body["start_date"] = v
	}
	if v, _ := cmd.Flags().GetString("due-date"); v != "" {
		body["due_date"] = v
	}

	repos, _ := cmd.Flags().GetStringArray("repo")
	if len(repos) > 0 {
		resources := make([]map[string]any, 0, len(repos))
		for _, repoURL := range repos {
			repoURL = strings.TrimSpace(repoURL)
			if repoURL == "" {
				continue
			}
			resources = append(resources, map[string]any{
				"resource_type": "github_repo",
				"resource_ref":  map[string]any{"url": repoURL},
			})
		}
		if len(resources) > 0 {
			body["resources"] = resources
		}
	}

	var result map[string]any
	if err := client.PostJSON(ctx, "/api/projects", body, &result); err != nil {
		return fmt.Errorf("create project: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		headers := []string{"ID", "TITLE", "STATUS"}
		rows := [][]string{{
			strVal(result, "id"),
			strVal(result, "title"),
			strVal(result, "status"),
		}}
		cli.PrintTable(os.Stdout, headers, rows)
		return nil
	}

	return cli.PrintJSON(os.Stdout, result)
}

func runProjectUpdate(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	projectRef, err := resolveProjectID(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve project: %w", err)
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
	if cmd.Flags().Changed("status") {
		v, _ := cmd.Flags().GetString("status")
		if err := validateProjectStatus(v); err != nil {
			return err
		}
		body["status"] = v
	}
	if cmd.Flags().Changed("icon") {
		v, _ := cmd.Flags().GetString("icon")
		body["icon"] = v
	}
	if cmd.Flags().Changed("lead") {
		v, _ := cmd.Flags().GetString("lead")
		aType, aID, resolveErr := resolveAssignee(ctx, client, v, memberOrAgentKinds)
		if resolveErr != nil {
			return fmt.Errorf("resolve lead: %w", resolveErr)
		}
		body["lead_type"] = aType
		body["lead_id"] = aID
	}

	if cmd.Flags().Changed("start-date") {
		v, _ := cmd.Flags().GetString("start-date")
		body["start_date"] = v
	}
	if cmd.Flags().Changed("due-date") {
		v, _ := cmd.Flags().GetString("due-date")
		body["due_date"] = v
	}

	if len(body) == 0 {
		return fmt.Errorf("no fields to update; use flags like --title, --status, --description, --icon, --lead, --start-date, --due-date")
	}

	var result map[string]any
	if err := client.PutJSON(ctx, "/api/projects/"+projectRef.ID, body, &result); err != nil {
		return fmt.Errorf("update project: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		headers := []string{"ID", "TITLE", "STATUS"}
		rows := [][]string{{
			strVal(result, "id"),
			strVal(result, "title"),
			strVal(result, "status"),
		}}
		cli.PrintTable(os.Stdout, headers, rows)
		return nil
	}

	return cli.PrintJSON(os.Stdout, result)
}

func runProjectDelete(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	projectRef, err := resolveProjectID(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve project: %w", err)
	}

	if err := client.DeleteJSON(ctx, "/api/projects/"+projectRef.ID); err != nil {
		return fmt.Errorf("delete project: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Project %s deleted.\n", projectRef.Display)
	return nil
}

func runProjectStatus(cmd *cobra.Command, args []string) error {
	id := args[0]
	status := args[1]

	if err := validateProjectStatus(status); err != nil {
		return err
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	projectRef, err := resolveProjectID(ctx, client, id)
	if err != nil {
		return fmt.Errorf("resolve project: %w", err)
	}

	body := map[string]any{"status": status}
	var result map[string]any
	if err := client.PutJSON(ctx, "/api/projects/"+projectRef.ID, body, &result); err != nil {
		return fmt.Errorf("update status: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Project %s status changed to %s.\n", strVal(result, "title"), status)

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	return nil
}

func runProjectResourceList(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	projectRef, err := resolveProjectID(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve project: %w", err)
	}

	var result map[string]any
	if err := client.GetJSON(ctx, "/api/projects/"+projectRef.ID+"/resources", &result); err != nil {
		return fmt.Errorf("list project resources: %w", err)
	}
	resourcesRaw, _ := result["resources"].([]any)

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, resourcesRaw)
	}

	fullID, _ := cmd.Flags().GetBool("full-id")
	headers := []string{"ID", "TYPE", "REF", "LABEL"}
	rows := make([][]string, 0, len(resourcesRaw))
	for _, raw := range resourcesRaw {
		r, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		rows = append(rows, []string{
			displayID(strVal(r, "id"), fullID),
			strVal(r, "resource_type"),
			summarizeResourceRef(r["resource_ref"]),
			strVal(r, "label"),
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runProjectResourceAdd(cmd *cobra.Command, args []string) error {
	resourceType, _ := cmd.Flags().GetString("type")
	resourceType = strings.TrimSpace(resourceType)
	if resourceType == "" {
		return fmt.Errorf("--type is required")
	}

	body := map[string]any{"resource_type": resourceType}

	if ref, ok, err := buildResourceRefFromRefFlag(cmd, resourceType, nil); err != nil {
		return err
	} else if ok {
		body["resource_ref"] = ref
	} else {
		switch resourceType {
		case "github_repo":
			ref, has, err := buildResourceRefFromFlags(cmd, resourceType, nil)
			if err != nil {
				return err
			}
			if !has {
				return fmt.Errorf("github_repo requires --url (or pass a JSON payload via --ref)")
			}
			body["resource_ref"] = ref
		case "local_directory":
			pathVal, _ := cmd.Flags().GetString("local-path")
			pathVal = strings.TrimSpace(pathVal)
			daemonVal, _ := cmd.Flags().GetString("daemon-id")
			daemonVal = strings.TrimSpace(daemonVal)
			if pathVal == "" || daemonVal == "" {
				return fmt.Errorf("local_directory requires --local-path and --daemon-id (or pass a JSON payload via --ref)")
			}
			ref := map[string]any{"local_path": pathVal, "daemon_id": daemonVal}
			if refLabel, _ := cmd.Flags().GetString("ref-label"); strings.TrimSpace(refLabel) != "" {
				ref["label"] = strings.TrimSpace(refLabel)
			}
			body["resource_ref"] = ref
		default:
			return fmt.Errorf("type %q has no built-in CLI shortcut; pass the payload via --ref '<json>'", resourceType)
		}
	}

	if label, _ := cmd.Flags().GetString("label"); label != "" {
		body["label"] = label
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	projectRef, err := resolveProjectID(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve project: %w", err)
	}

	var result map[string]any
	if err := client.PostJSON(ctx, "/api/projects/"+projectRef.ID+"/resources", body, &result); err != nil {
		return fmt.Errorf("add project resource: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		headers := []string{"ID", "TYPE", "REF"}
		rows := [][]string{{
			strVal(result, "id"),
			strVal(result, "resource_type"),
			summarizeResourceRef(result["resource_ref"]),
		}}
		cli.PrintTable(os.Stdout, headers, rows)
		return nil
	}
	return cli.PrintJSON(os.Stdout, result)
}

func runProjectResourceUpdate(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	projectRef, err := resolveProjectID(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve project: %w", err)
	}
	resourceRef, err := resolveProjectResourceID(ctx, client, projectRef.ID, args[1])
	if err != nil {
		return fmt.Errorf("resolve project resource: %w", err)
	}

	var existing map[string]any
	if err := client.GetJSON(ctx, "/api/projects/"+projectRef.ID+"/resources", &existing); err != nil {
		return fmt.Errorf("list project resources: %w", err)
	}
	var resourceType string
	var existingRef map[string]any
	if list, ok := existing["resources"].([]any); ok {
		for _, raw := range list {
			row, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			if strVal(row, "id") == resourceRef.ID {
				resourceType = strVal(row, "resource_type")
				if ref, ok := row["resource_ref"].(map[string]any); ok {
					existingRef = ref
				}
				break
			}
		}
	}

	body := map[string]any{}

	if ref, ok, err := buildResourceRefFromRefFlag(cmd, resourceType, existingRef); err != nil {
		return err
	} else if ok {
		body["resource_ref"] = ref
	} else {
		ref, has, err := buildResourceRefFromFlags(cmd, resourceType, existingRef)
		if err != nil {
			return err
		}
		if has {
			body["resource_ref"] = ref
		}
	}

	clearLabel, _ := cmd.Flags().GetBool("clear-label")
	if clearLabel {
		body["label"] = nil
	} else if cmd.Flags().Changed("label") {
		label, _ := cmd.Flags().GetString("label")
		body["label"] = label
	}

	if cmd.Flags().Changed("position") {
		pos, _ := cmd.Flags().GetInt32("position")
		body["position"] = pos
	}

	if len(body) == 0 {
		return fmt.Errorf("nothing to update — pass --ref / --url / --local-path / --label / --position / --clear-label")
	}

	var result map[string]any
	if err := client.PutJSON(ctx, "/api/projects/"+projectRef.ID+"/resources/"+resourceRef.ID, body, &result); err != nil {
		return fmt.Errorf("update project resource: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		headers := []string{"ID", "TYPE", "REF", "LABEL"}
		rows := [][]string{{
			strVal(result, "id"),
			strVal(result, "resource_type"),
			summarizeResourceRef(result["resource_ref"]),
			strVal(result, "label"),
		}}
		cli.PrintTable(os.Stdout, headers, rows)
		return nil
	}
	return cli.PrintJSON(os.Stdout, result)
}

func buildResourceRefFromRefFlag(cmd *cobra.Command, resourceType string, existingRef map[string]any) (any, bool, error) {
	if !cmd.Flags().Changed("ref") {
		return nil, false, nil
	}
	rawRef, _ := cmd.Flags().GetString("ref")
	rawRef = strings.TrimSpace(rawRef)

	if rawRef != "" && (resourceType != "github_repo" || looksLikeJSONPayload(rawRef)) {
		var ref any
		if err := json.Unmarshal([]byte(rawRef), &ref); err != nil {
			return nil, false, fmt.Errorf("--ref is not valid JSON: %w", err)
		}
		return ref, true, nil
	}
	if resourceType != "github_repo" {
		return nil, false, fmt.Errorf("--ref must be a JSON resource_ref payload for resource type %q", resourceType)
	}
	ref, has, err := buildResourceRefFromFlags(cmd, resourceType, existingRef)
	if err != nil {
		return nil, false, err
	}
	return ref, has, nil
}

func looksLikeJSONPayload(raw string) bool {
	raw = strings.TrimSpace(raw)
	return strings.HasPrefix(raw, "{") || strings.HasPrefix(raw, "[")
}

func buildResourceRefFromFlags(cmd *cobra.Command, resourceType string, existingRef map[string]any) (map[string]any, bool, error) {
	switch resourceType {
	case "github_repo":
		urlSet := cmd.Flags().Changed("url")
		hintSet := cmd.Flags().Changed("default-branch-hint")
		refSet := cmd.Flags().Changed("ref")
		if !urlSet && !hintSet && !refSet {
			return nil, false, nil
		}
		ref := map[string]any{}

		if existingRef != nil {
			if u, ok := existingRef["url"].(string); ok && strings.TrimSpace(u) != "" {
				ref["url"] = strings.TrimSpace(u)
			}
			if h, ok := existingRef["default_branch_hint"].(string); ok && strings.TrimSpace(h) != "" {
				ref["default_branch_hint"] = strings.TrimSpace(h)
			}
			if checkoutRef, ok := existingRef["ref"].(string); ok && strings.TrimSpace(checkoutRef) != "" {
				ref["ref"] = strings.TrimSpace(checkoutRef)
			}
		}
		if urlSet {
			urlVal, _ := cmd.Flags().GetString("url")
			urlVal = strings.TrimSpace(urlVal)
			if urlVal == "" {
				return nil, false, fmt.Errorf("--url cannot be empty")
			}
			ref["url"] = urlVal
		}
		if hintSet {
			hint := strings.TrimSpace(mustString(cmd, "default-branch-hint"))
			if hint == "" {
				delete(ref, "default_branch_hint")
			} else {
				ref["default_branch_hint"] = hint
			}
		}
		if refSet {
			checkoutRef := strings.TrimSpace(mustString(cmd, "ref"))
			if checkoutRef == "" {
				delete(ref, "ref")
			} else {
				ref["ref"] = checkoutRef
			}
		}
		if _, ok := ref["url"]; !ok {
			return nil, false, fmt.Errorf("github_repo: --url is required (no existing url to merge with)")
		}
		return ref, true, nil
	case "local_directory":
		pathSet := cmd.Flags().Changed("local-path")
		daemonSet := cmd.Flags().Changed("daemon-id")
		labelSet := cmd.Flags().Changed("ref-label")
		if !pathSet && !daemonSet && !labelSet {
			return nil, false, nil
		}
		ref := map[string]any{}
		if existingRef != nil {
			if p, ok := existingRef["local_path"].(string); ok && strings.TrimSpace(p) != "" {
				ref["local_path"] = strings.TrimSpace(p)
			}
			if d, ok := existingRef["daemon_id"].(string); ok && strings.TrimSpace(d) != "" {
				ref["daemon_id"] = strings.TrimSpace(d)
			}
			if l, ok := existingRef["label"].(string); ok && strings.TrimSpace(l) != "" {
				ref["label"] = strings.TrimSpace(l)
			}
		}
		if pathSet {
			pathVal := strings.TrimSpace(mustString(cmd, "local-path"))
			if pathVal == "" {
				return nil, false, fmt.Errorf("--local-path cannot be empty")
			}
			ref["local_path"] = pathVal
		}
		if daemonSet {
			daemonVal := strings.TrimSpace(mustString(cmd, "daemon-id"))
			if daemonVal == "" {
				return nil, false, fmt.Errorf("--daemon-id cannot be empty")
			}
			ref["daemon_id"] = daemonVal
		}
		if labelSet {
			refLabel := strings.TrimSpace(mustString(cmd, "ref-label"))
			if refLabel == "" {
				delete(ref, "label")
			} else {
				ref["label"] = refLabel
			}
		}
		if v, ok := ref["local_path"].(string); !ok || v == "" {
			return nil, false, fmt.Errorf("local_directory: --local-path is required (no existing local_path to merge with)")
		}
		if v, ok := ref["daemon_id"].(string); !ok || v == "" {
			return nil, false, fmt.Errorf("local_directory: --daemon-id is required (no existing daemon_id to merge with)")
		}
		return ref, true, nil
	default:

		if cmd.Flags().Changed("url") || cmd.Flags().Changed("default-branch-hint") ||
			cmd.Flags().Changed("local-path") || cmd.Flags().Changed("daemon-id") ||
			cmd.Flags().Changed("ref-label") {
			return nil, false, fmt.Errorf("no built-in shortcut for resource type %q; pass the full payload via --ref '<json>'", resourceType)
		}
		return nil, false, nil
	}
}

func mustString(cmd *cobra.Command, name string) string {
	v, _ := cmd.Flags().GetString(name)
	return v
}

func runProjectResourceRemove(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	projectRef, err := resolveProjectID(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve project: %w", err)
	}
	resourceRef, err := resolveProjectResourceID(ctx, client, projectRef.ID, args[1])
	if err != nil {
		return fmt.Errorf("resolve project resource: %w", err)
	}

	if err := client.DeleteJSON(ctx, "/api/projects/"+projectRef.ID+"/resources/"+resourceRef.ID); err != nil {
		return fmt.Errorf("remove project resource: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Resource %s removed from project %s.\n", resourceRef.Display, projectRef.Display)
	return nil
}

func summarizeResourceRef(raw any) string {
	m, ok := raw.(map[string]any)
	if !ok {
		return ""
	}
	if u, ok := m["url"].(string); ok && u != "" {
		if ref, ok := m["ref"].(string); ok && strings.TrimSpace(ref) != "" {
			return u + " @ " + strings.TrimSpace(ref)
		}
		return u
	}
	if p, ok := m["local_path"].(string); ok && p != "" {
		return p
	}
	if data, err := json.Marshal(m); err == nil {
		return string(data)
	}
	return ""
}

func formatLead(project map[string]any, actors actorDisplayLookup) string {
	lType := strVal(project, "lead_type")
	lID := strVal(project, "lead_id")
	if lType == "" || lID == "" {
		return ""
	}
	return actors.actor(lType, lID)
}
