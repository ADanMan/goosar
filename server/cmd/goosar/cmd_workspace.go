package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/adanman/goosar/server/internal/cli"
)

var workspaceCmd = &cobra.Command{
	Use:   "workspace",
	Short: "Работа с рабочими пространствами",
}

var workspaceListCmd = &cobra.Command{
	Use:   "list",
	Short: "Список рабочих пространств, в которых вы состоите",
	RunE:  runWorkspaceList,
}

var workspaceCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Создать рабочее пространство",
	Long: "Создаёт рабочее пространство и делает вас его владельцем (owner). " +
		"Флаги --name и --slug обязательны; slug задаётся навсегда (строчные " +
		"буквы, цифры и дефисы) и после создания не меняется.\n\n" +
		"Создание рабочего пространства НЕ меняет рабочее пространство по " +
		"умолчанию для этого профиля — чтобы следующие команды работали с новым " +
		"пространством, выполните 'goosar workspace switch <slug>'.",
	Example: "  goosar workspace create --name \"Support Team\" --slug support-team --issue-prefix SUP",
	Args:    cobra.NoArgs,
	RunE:    runWorkspaceCreate,
}

var workspaceGetCmd = &cobra.Command{
	Use:   "get [workspace-id|slug|prefix]",
	Short: "Показать сведения о рабочем пространстве",
	Long: "Выводит все сведения о рабочем пространстве. В аргументе можно указать " +
		"полный UUID, slug или короткий префикс UUID (от 4 hex-символов), как в " +
		"выводе 'workspace list'. Если аргумент не указан, используется рабочее " +
		"пространство по умолчанию.",
	Args: cobra.MaximumNArgs(1),
	RunE: runWorkspaceGet,
}

var workspaceMemberCmd = &cobra.Command{
	Use:   "member",
	Short: "Управление участниками рабочего пространства",
}

var workspaceMemberListCmd = &cobra.Command{
	Use:   "list [workspace-id|slug|prefix]",
	Short: "Список участников рабочего пространства",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runWorkspaceMembers,
}

var workspaceMemberInviteCmd = &cobra.Command{
	Use:   "invite <email> [workspace-id|slug|prefix]",
	Short: "Пригласить участника в рабочее пространство по почте",
	Long: "Отправляет приглашение в рабочее пространство на адрес почты. " +
		"Приглашённый получает приглашение, которое нужно принять, — в " +
		"пространство он попадает не сразу. В необязательном аргументе " +
		"пространства можно указать полный UUID, slug или короткий префикс " +
		"UUID (от 4 hex-символов), как в выводе 'workspace list'; если " +
		"аргумент не указан, используется рабочее пространство по умолчанию " +
		"(--workspace-id / GOOSAR_WORKSPACE_ID / значение профиля).\n\n" +
		"По умолчанию роль — 'member'; чтобы пригласить администратора, " +
		"передайте '--role admin'. Пригласить owner нельзя.",
	Args: cobra.RangeArgs(1, 2),
	RunE: runWorkspaceMemberInvite,
}

var workspaceUpdateCmd = &cobra.Command{
	Use:   "update [workspace-id|slug|prefix]",
	Short: "Изменить данные рабочего пространства (только admin и owner)",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runWorkspaceUpdate,
}

var workspaceSwitchCmd = &cobra.Command{
	Use:   "switch <workspace-id|slug|prefix>",
	Short: "Выбрать рабочее пространство по умолчанию для этого профиля",
	Long: "Выбирает рабочее пространство по умолчанию для текущего профиля, " +
		"предварительно проверив, что у вас есть к нему доступ. Принимает полный " +
		"UUID, slug или короткий префикс UUID (от 4 hex-символов), как в выводе " +
		"'workspace list'. Следующие команды без --workspace-id и " +
		"GOOSAR_WORKSPACE_ID будут работать с этим рабочим пространством.\n\n" +
		"Приоритет выбора (от высшего к низшему): флаг --workspace-id, " +
		"переменная окружения GOOSAR_WORKSPACE_ID, значение профиля по " +
		"умолчанию (его задаёт эта команда).\n\n" +
		"Для низкоуровневой работы 'goosar config set workspace_id <id>' " +
		"записывает ту же настройку без проверки.",
	Args: exactArgs(1),
	RunE: runWorkspaceSwitch,
}

func init() {
	workspaceCmd.AddCommand(workspaceListCmd)
	workspaceCmd.AddCommand(workspaceCreateCmd)
	workspaceCmd.AddCommand(workspaceGetCmd)
	workspaceCmd.AddCommand(workspaceMemberCmd)
	workspaceMemberCmd.AddCommand(workspaceMemberListCmd)
	workspaceMemberCmd.AddCommand(workspaceMemberInviteCmd)
	workspaceCmd.AddCommand(workspaceUpdateCmd)
	workspaceCmd.AddCommand(workspaceSwitchCmd)

	workspaceListCmd.Flags().String("output", "table", "Формат вывода: table или json")
	workspaceListCmd.Flags().Bool("full-id", false, "Показывать полные UUID в таблице")
	workspaceCreateCmd.Flags().String("name", "", "Название рабочего пространства")
	workspaceCreateCmd.Flags().String("slug", "", "Slug рабочего пространства")
	workspaceCreateCmd.Flags().String("description", "", "Описание рабочего пространства (раскрывает \\n, \\r, \\t, \\\\; чтобы сохранить обратные слэши как есть, передайте текст через --description-stdin)")
	workspaceCreateCmd.Flags().Bool("description-stdin", false, "Прочитать описание из stdin (многострочный текст сохраняется без изменений)")
	workspaceCreateCmd.Flags().String("context", "", "Контекст рабочего пространства (раскрывает \\n, \\r, \\t, \\\\; чтобы сохранить обратные слэши как есть, передайте текст через --context-stdin)")
	workspaceCreateCmd.Flags().Bool("context-stdin", false, "Прочитать контекст из stdin (многострочный текст сохраняется без изменений)")
	workspaceCreateCmd.Flags().String("issue-prefix", "", "Префикс issue (сервер приведёт его к верхнему регистру)")
	workspaceCreateCmd.Flags().String("output", "json", "Формат вывода: table или json")
	workspaceGetCmd.Flags().String("output", "json", "Формат вывода: table или json")
	workspaceMemberListCmd.Flags().String("output", "table", "Формат вывода: table или json")
	workspaceMemberInviteCmd.Flags().String("role", "member", "Роль участника: member или admin (owner недоступна)")
	workspaceMemberInviteCmd.Flags().String("output", "table", "Формат вывода: table или json")

	workspaceUpdateCmd.Flags().String("name", "", "Новое название рабочего пространства")
	workspaceUpdateCmd.Flags().String("description", "", "Новое описание (раскрывает \\n, \\r, \\t, \\\\; чтобы сохранить обратные слэши как есть, передайте текст через --description-stdin)")
	workspaceUpdateCmd.Flags().Bool("description-stdin", false, "Прочитать описание из stdin (многострочный текст сохраняется без изменений)")
	workspaceUpdateCmd.Flags().String("context", "", "Новый контекст рабочего пространства (раскрывает \\n, \\r, \\t, \\\\; чтобы сохранить обратные слэши как есть, передайте текст через --context-stdin)")
	workspaceUpdateCmd.Flags().Bool("context-stdin", false, "Прочитать контекст из stdin (многострочный текст сохраняется без изменений)")
	workspaceUpdateCmd.Flags().String("issue-prefix", "", "Новый префикс issue (сервер приведёт его к верхнему регистру)")
	workspaceUpdateCmd.Flags().String("output", "json", "Формат вывода: table или json")
}

type workspaceSummary struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

func fetchWorkspaces(ctx context.Context, cmd *cobra.Command) ([]workspaceSummary, error) {
	serverURL := resolveServerURL(cmd)
	token := resolveToken(cmd)
	if token == "" {
		return nil, fmt.Errorf("not authenticated: run 'goosar login' first")
	}

	client := cli.NewAPIClient(serverURL, "", token)
	var workspaces []workspaceSummary
	if err := client.GetJSON(ctx, "/api/workspaces", &workspaces); err != nil {
		return nil, fmt.Errorf("list workspaces: %w", err)
	}
	return workspaces, nil
}

func runWorkspaceList(cmd *cobra.Command, _ []string) error {
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	workspaces, err := fetchWorkspaces(ctx, cmd)
	if err != nil {
		return err
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, workspaces)
	}

	if len(workspaces) == 0 {
		fmt.Fprintln(os.Stderr, "No workspaces found.")
		return nil
	}

	currentID := resolveWorkspaceID(cmd)
	fullID, _ := cmd.Flags().GetBool("full-id")
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "\tID\tNAME\tSLUG")
	for _, ws := range workspaces {
		marker := " "
		if ws.ID == currentID {
			marker = "*"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", marker, displayID(ws.ID, fullID), ws.Name, ws.Slug)
	}
	if err := w.Flush(); err != nil {
		return err
	}
	if currentID != "" {
		fmt.Fprintln(os.Stderr, "\n* = current default workspace (use 'goosar workspace switch <id|slug|prefix>' to change)")
	} else {
		fmt.Fprintln(os.Stderr, "\nNo default workspace set. Use 'goosar workspace switch <id|slug|prefix>' to pick one.")
	}
	fmt.Fprintln(os.Stderr, "Tip: pass the ID column, SLUG, or full UUID (--full-id) to 'workspace get/update/switch'.")
	return nil
}

func buildWorkspaceCreateBody(cmd *cobra.Command) (map[string]any, error) {
	name, _ := cmd.Flags().GetString("name")
	if strings.TrimSpace(name) == "" {
		return nil, fmt.Errorf("--name is required")
	}

	slug, _ := cmd.Flags().GetString("slug")
	if strings.TrimSpace(slug) == "" {
		return nil, fmt.Errorf("--slug is required")
	}

	descStdin, _ := cmd.Flags().GetBool("description-stdin")
	ctxStdin, _ := cmd.Flags().GetBool("context-stdin")
	if descStdin && ctxStdin {
		return nil, fmt.Errorf("--description-stdin and --context-stdin cannot be combined; a single stdin cannot feed both fields — pass one of them inline")
	}

	body := map[string]any{"name": name, "slug": slug}
	if cmd.Flags().Changed("description") || cmd.Flags().Changed("description-stdin") {
		desc, _, err := resolveTextFlag(cmd, "description")
		if err != nil {
			return nil, err
		}
		body["description"] = desc
	}
	if cmd.Flags().Changed("context") || cmd.Flags().Changed("context-stdin") {
		ctxText, _, err := resolveTextFlag(cmd, "context")
		if err != nil {
			return nil, err
		}
		body["context"] = ctxText
	}
	if cmd.Flags().Changed("issue-prefix") {
		v, _ := cmd.Flags().GetString("issue-prefix")
		if strings.TrimSpace(v) == "" {
			return nil, fmt.Errorf("--issue-prefix cannot be empty; omit it to use the server-generated prefix")
		}
		body["issue_prefix"] = v
	}
	return body, nil
}

func runWorkspaceCreate(cmd *cobra.Command, _ []string) error {
	body, err := buildWorkspaceCreateBody(cmd)
	if err != nil {
		return err
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	client.WorkspaceID = ""

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var ws map[string]any
	if err := client.PostJSON(ctx, "/api/workspaces", body, &ws); err != nil {
		return fmt.Errorf("create workspace: %w", err)
	}
	return printWorkspace(cmd, ws)
}

func resolveWorkspaceByIDOrSlug(workspaces []workspaceSummary, target string) (workspaceSummary, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return workspaceSummary{}, fmt.Errorf("workspace id, slug, or id prefix is required")
	}

	lowered := strings.ToLower(target)
	for _, ws := range workspaces {
		if strings.ToLower(ws.ID) == lowered {
			return ws, nil
		}
	}
	for _, ws := range workspaces {
		if ws.Slug != "" && strings.ToLower(ws.Slug) == lowered {
			return ws, nil
		}
	}

	if prefix, err := normalizeUUIDPrefix(target); err == nil {
		matches := make([]workspaceSummary, 0, 1)
		for _, ws := range workspaces {
			if strings.HasPrefix(compactUUID(ws.ID), prefix) {
				matches = append(matches, ws)
			}
		}
		switch len(matches) {
		case 0:

		case 1:
			return matches[0], nil
		default:
			return workspaceSummary{}, ambiguousWorkspacePrefixError(target, matches)
		}
	}

	return workspaceSummary{}, fmt.Errorf("workspace %q not found or you do not have access; run 'goosar workspace list' to see options", target)
}

func ambiguousWorkspacePrefixError(input string, matches []workspaceSummary) error {
	parts := make([]string, 0, len(matches))
	for _, m := range matches {
		label := m.Name
		if m.Slug != "" {
			label = fmt.Sprintf("%s (%s)", m.Name, m.Slug)
		}
		parts = append(parts, fmt.Sprintf("  %s  %s", m.ID, label))
	}
	return fmt.Errorf("ambiguous workspace id prefix %q; matches:\n%s\nUse more characters, the slug, or the full UUID", input, strings.Join(parts, "\n"))
}

func resolveWorkspaceRef(ctx context.Context, cmd *cobra.Command, input string) (workspaceSummary, error) {
	target := strings.TrimSpace(input)
	if target == "" {
		return workspaceSummary{}, fmt.Errorf("workspace id, slug, or id prefix is required")
	}
	workspaces, err := fetchWorkspaces(ctx, cmd)
	if err != nil {
		return workspaceSummary{}, err
	}
	return resolveWorkspaceByIDOrSlug(workspaces, target)
}

func runWorkspaceSwitch(cmd *cobra.Command, args []string) error {
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	ws, err := resolveWorkspaceRef(ctx, cmd, args[0])
	if err != nil {
		return err
	}

	профиль := resolveProfile(cmd)
	cfg, err := cli.LoadCLIConfigForProfile(профиль)
	if err != nil {
		return err
	}
	cfg.WorkspaceID = ws.ID
	if err := cli.SaveCLIConfigForProfile(cfg, профиль); err != nil {
		return err
	}

	fmt.Fprintf(os.Stdout, "Switched to workspace: %s (%s)\n", ws.Name, ws.ID)
	return nil
}

func resolveWorkspaceArg(cmd *cobra.Command, args []string) (string, error) {
	if len(args) > 0 {
		trimmed := strings.TrimSpace(args[0])
		if uuidRegexp.MatchString(trimmed) {
			return trimmed, nil
		}
		ctx, cancel := cli.APIContext(context.Background())
		defer cancel()
		ws, err := resolveWorkspaceRef(ctx, cmd, trimmed)
		if err != nil {
			return "", err
		}
		return ws.ID, nil
	}
	return resolveWorkspaceID(cmd), nil
}

func runWorkspaceGet(cmd *cobra.Command, args []string) error {
	wsID, err := resolveWorkspaceArg(cmd, args)
	if err != nil {
		return err
	}
	if wsID == "" {
		return fmt.Errorf("workspace ID is required: pass an id/slug/prefix as argument or set GOOSAR_WORKSPACE_ID")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var ws map[string]any
	if err := client.GetJSON(ctx, "/api/workspaces/"+wsID, &ws); err != nil {
		return fmt.Errorf("get workspace: %w", err)
	}

	return printWorkspace(cmd, ws)
}

func printWorkspace(cmd *cobra.Command, ws map[string]any) error {
	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		desc := strVal(ws, "description")
		if utf8.RuneCountInString(desc) > 60 {
			runes := []rune(desc)
			desc = string(runes[:57]) + "..."
		}
		wsContext := strVal(ws, "context")
		if utf8.RuneCountInString(wsContext) > 60 {
			runes := []rune(wsContext)
			wsContext = string(runes[:57]) + "..."
		}
		headers := []string{"ID", "NAME", "SLUG", "DESCRIPTION", "CONTEXT"}
		rows := [][]string{{
			strVal(ws, "id"),
			strVal(ws, "name"),
			strVal(ws, "slug"),
			desc,
			wsContext,
		}}
		cli.PrintTable(os.Stdout, headers, rows)
		return nil
	}

	return cli.PrintJSON(os.Stdout, ws)
}

func buildWorkspaceUpdateBody(cmd *cobra.Command) (map[string]any, error) {
	body := map[string]any{}
	if cmd.Flags().Changed("name") {
		v, _ := cmd.Flags().GetString("name")
		body["name"] = v
	}
	if cmd.Flags().Changed("description") || cmd.Flags().Changed("description-stdin") {
		desc, _, err := resolveTextFlag(cmd, "description")
		if err != nil {
			return nil, err
		}
		body["description"] = desc
	}
	if cmd.Flags().Changed("context") || cmd.Flags().Changed("context-stdin") {
		ctxText, _, err := resolveTextFlag(cmd, "context")
		if err != nil {
			return nil, err
		}
		body["context"] = ctxText
	}
	if cmd.Flags().Changed("issue-prefix") {
		v, _ := cmd.Flags().GetString("issue-prefix")

		if strings.TrimSpace(v) == "" {
			return nil, fmt.Errorf("--issue-prefix cannot be empty; clearing the prefix is not supported")
		}
		body["issue_prefix"] = v
	}
	return body, nil
}

func runWorkspaceUpdate(cmd *cobra.Command, args []string) error {
	wsID, err := resolveWorkspaceArg(cmd, args)
	if err != nil {
		return err
	}
	if wsID == "" {
		return fmt.Errorf("workspace ID is required: pass an id/slug/prefix as argument or set GOOSAR_WORKSPACE_ID")
	}

	body, err := buildWorkspaceUpdateBody(cmd)
	if err != nil {
		return err
	}
	if len(body) == 0 {
		return fmt.Errorf("no fields to update; use --name, --description, --context, or --issue-prefix")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var ws map[string]any
	if err := client.PatchJSON(ctx, "/api/workspaces/"+wsID, body, &ws); err != nil {
		return fmt.Errorf("update workspace: %w", err)
	}

	return printWorkspace(cmd, ws)
}

func runWorkspaceMembers(cmd *cobra.Command, args []string) error {
	wsID, err := resolveWorkspaceArg(cmd, args)
	if err != nil {
		return err
	}
	if wsID == "" {
		return fmt.Errorf("workspace ID is required: pass an id/slug/prefix as argument or set GOOSAR_WORKSPACE_ID")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var members []map[string]any
	if err := client.GetJSON(ctx, "/api/workspaces/"+wsID+"/members", &members); err != nil {
		return fmt.Errorf("list members: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, members)
	}

	headers := []string{"USER ID", "NAME", "EMAIL", "ROLE"}
	rows := make([][]string, 0, len(members))
	for _, m := range members {
		rows = append(rows, []string{
			strVal(m, "user_id"),
			strVal(m, "name"),
			strVal(m, "email"),
			strVal(m, "role"),
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runWorkspaceMemberInvite(cmd *cobra.Command, args []string) error {
	email := strings.ToLower(strings.TrimSpace(args[0]))
	if email == "" {
		return fmt.Errorf("email is required")
	}

	role, _ := cmd.Flags().GetString("role")
	role = strings.ToLower(strings.TrimSpace(role))
	if role == "" {
		role = "member"
	}
	switch role {
	case "member", "admin":
	case "owner":
		return fmt.Errorf("cannot invite as owner; use --role member or --role admin")
	default:
		return fmt.Errorf("invalid --role %q; expected member or admin", role)
	}

	wsID, err := resolveWorkspaceArg(cmd, args[1:])
	if err != nil {
		return err
	}
	if wsID == "" {
		return fmt.Errorf("workspace ID is required: pass an id/slug/prefix as argument or set GOOSAR_WORKSPACE_ID")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	body := map[string]any{"email": email, "role": role}
	var inv map[string]any
	if err := client.PostJSON(ctx, "/api/workspaces/"+wsID+"/members", body, &inv); err != nil {
		return fmt.Errorf("invite member: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, inv)
	}

	fmt.Fprintf(os.Stdout, "Invitation sent to %s (role: %s, status: %s)\n",
		strVal(inv, "invitee_email"), strVal(inv, "role"), strVal(inv, "status"))
	return nil
}
