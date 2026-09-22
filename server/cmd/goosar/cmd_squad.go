package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/adanman/goosar/server/internal/cli"
)

var squadCmd = &cobra.Command{
	Use:   "squad",
	Short: "Работа с командами",
}

var squadListCmd = &cobra.Command{
	Use:   "list",
	Short: "Список команд в рабочем пространстве",
	Args:  cobra.NoArgs,
	RunE:  runSquadList,
}

func runSquadList(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var squads []map[string]any
	if err := client.GetJSON(ctx, "/api/squads", &squads); err != nil {
		return fmt.Errorf("list squads: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, squads)
	}

	if len(squads) == 0 {
		fmt.Fprintln(os.Stderr, "No squads found.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tNAME\tLEADER ID\tMEMBERS")
	for _, s := range squads {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
			strVal(s, "id"), strVal(s, "name"), strVal(s, "leader_id"),
			memberCountDisplay(s))
	}
	return w.Flush()
}

func memberCountDisplay(m map[string]any) string {
	v, ok := m["member_count"]
	if !ok || v == nil {
		return "-"
	}
	n, ok := v.(float64)
	if !ok || n <= 0 {
		return "-"
	}
	return strconv.Itoa(int(n))
}

var squadGetCmd = &cobra.Command{
	Use:   "get <squad-id>",
	Short: "Показать сведения о команде",
	Args:  exactArgs(1),
	RunE:  runSquadGet,
}

func runSquadGet(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var squad map[string]any
	if err := client.GetJSON(ctx, "/api/squads/"+args[0], &squad); err != nil {
		return fmt.Errorf("get squad: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, squad)
	}

	fmt.Printf("ID:           %s\n", strVal(squad, "id"))
	fmt.Printf("Name:         %s\n", strVal(squad, "name"))
	fmt.Printf("Description:  %s\n", strVal(squad, "description"))
	fmt.Printf("Leader ID:    %s\n", strVal(squad, "leader_id"))
	fmt.Printf("Created:      %s\n", strVal(squad, "created_at"))
	if inst := strVal(squad, "instructions"); inst != "" {
		fmt.Printf("Instructions: %s\n", inst)
	}
	return nil
}

var squadCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Создать команду",
	Args:  cobra.NoArgs,
	RunE:  runSquadCreate,
}

func runSquadCreate(cmd *cobra.Command, _ []string) error {
	name, _ := cmd.Flags().GetString("name")
	if name == "" {
		return fmt.Errorf("--name is required")
	}
	leader, _ := cmd.Flags().GetString("leader")
	if leader == "" {
		return fmt.Errorf("--leader is required (agent name or ID)")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	leaderID, err := resolveAgent(ctx, client, leader)
	if err != nil {
		return fmt.Errorf("resolve leader: %w", err)
	}

	body := map[string]any{
		"name":      name,
		"leader_id": leaderID,
	}
	if v, _ := cmd.Flags().GetString("description"); v != "" {
		body["description"] = v
	}

	var result map[string]any
	if err := client.PostJSON(ctx, "/api/squads", body, &result); err != nil {
		return fmt.Errorf("create squad: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	fmt.Printf("Squad created: %s (%s)\n", strVal(result, "name"), strVal(result, "id"))
	return nil
}

var squadUpdateCmd = &cobra.Command{
	Use:   "update <squad-id>",
	Short: "Изменить команду",
	Args:  exactArgs(1),
	RunE:  runSquadUpdate,
}

func runSquadUpdate(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

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
	if cmd.Flags().Changed("leader") {
		v, _ := cmd.Flags().GetString("leader")
		leaderID, err := resolveAgent(ctx, client, v)
		if err != nil {
			return fmt.Errorf("resolve leader: %w", err)
		}
		body["leader_id"] = leaderID
	}
	if cmd.Flags().Changed("avatar-url") {
		v, _ := cmd.Flags().GetString("avatar-url")
		body["avatar_url"] = v
	}

	if len(body) == 0 {
		return fmt.Errorf("no fields to update; use flags like --name, --description, --instructions, --leader")
	}

	var result map[string]any
	if err := client.PutJSON(ctx, "/api/squads/"+args[0], body, &result); err != nil {
		return fmt.Errorf("update squad: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	fmt.Printf("Squad updated: %s (%s)\n", strVal(result, "name"), strVal(result, "id"))
	return nil
}

var squadDeleteCmd = &cobra.Command{
	Use:   "delete <squad-id>",
	Short: "Удалить (архивировать) команду",
	Args:  exactArgs(1),
	RunE:  runSquadDelete,
}

func runSquadDelete(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	if err := client.DeleteJSON(ctx, "/api/squads/"+args[0]); err != nil {
		return fmt.Errorf("delete squad: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, map[string]any{"id": args[0], "deleted": true})
	}
	fmt.Fprintf(os.Stderr, "Squad %s deleted.\n", args[0])
	return nil
}

var squadMemberCmd = &cobra.Command{
	Use:   "member",
	Short: "Работа с участниками команды",
}

var squadMemberListCmd = &cobra.Command{
	Use:   "list <squad-id>",
	Short: "Список участников команды",
	Args:  exactArgs(1),
	RunE:  runSquadMemberList,
}

func runSquadMemberList(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var members []map[string]any
	if err := client.GetJSON(ctx, "/api/squads/"+args[0]+"/members", &members); err != nil {
		return fmt.Errorf("list members: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, members)
	}

	if len(members) == 0 {
		fmt.Fprintln(os.Stderr, "No members found.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "MEMBER ID\tTYPE\tROLE")
	for _, m := range members {
		fmt.Fprintf(w, "%s\t%s\t%s\n",
			strVal(m, "member_id"), strVal(m, "member_type"), strVal(m, "role"))
	}
	return w.Flush()
}

var squadMemberAddCmd = &cobra.Command{
	Use:   "add <squad-id>",
	Short: "Добавить участника в команду",
	Args:  exactArgs(1),
	RunE:  runSquadMemberAdd,
}

func runSquadMemberAdd(cmd *cobra.Command, args []string) error {
	memberID, _ := cmd.Flags().GetString("member-id")
	memberType, _ := cmd.Flags().GetString("type")
	role, _ := cmd.Flags().GetString("role")

	if memberID == "" {
		return fmt.Errorf("--member-id is required")
	}
	if memberType != "agent" && memberType != "member" {
		return fmt.Errorf("--type must be 'agent' or 'member'")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	body := map[string]any{
		"member_type": memberType,
		"member_id":   memberID,
		"role":        role,
	}

	var result map[string]any
	if err := client.PostJSON(ctx, "/api/squads/"+args[0]+"/members", body, &result); err != nil {
		return fmt.Errorf("add member: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	fmt.Printf("Member %s added to squad.\n", memberID)
	return nil
}

var squadMemberSetRoleCmd = &cobra.Command{
	Use:   "set-role <squad-id>",
	Short: "Изменить роль участника команды",
	Args:  exactArgs(1),
	RunE:  runSquadMemberSetRole,
}

func runSquadMemberSetRole(cmd *cobra.Command, args []string) error {
	memberID, _ := cmd.Flags().GetString("member-id")
	memberType, _ := cmd.Flags().GetString("member-type")
	role, _ := cmd.Flags().GetString("role")

	if memberID == "" {
		return fmt.Errorf("--member-id is required")
	}
	if memberType != "agent" && memberType != "member" {
		return fmt.Errorf("--member-type must be 'agent' or 'member'")
	}
	if role == "" {
		return fmt.Errorf("--role is required")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	body := map[string]any{
		"member_type": memberType,
		"member_id":   memberID,
		"role":        role,
	}

	var result map[string]any
	if err := client.PatchJSON(ctx, "/api/squads/"+args[0]+"/members/role", body, &result); err != nil {
		return fmt.Errorf("set member role: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	fmt.Fprintf(os.Stderr, "Member %s role updated to %s.\n", memberID, role)
	return nil
}

var squadMemberRemoveCmd = &cobra.Command{
	Use:   "remove <squad-id>",
	Short: "Убрать участника из команды",
	Args:  exactArgs(1),
	RunE:  runSquadMemberRemove,
}

func runSquadMemberRemove(cmd *cobra.Command, args []string) error {
	memberID, _ := cmd.Flags().GetString("member-id")
	memberType, _ := cmd.Flags().GetString("type")

	if memberID == "" {
		return fmt.Errorf("--member-id is required")
	}
	if memberType != "agent" && memberType != "member" {
		return fmt.Errorf("--type must be 'agent' or 'member'")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	body := map[string]any{
		"member_type": memberType,
		"member_id":   memberID,
	}

	if err := client.DeleteJSONWithBody(ctx, "/api/squads/"+args[0]+"/members", body); err != nil {
		return fmt.Errorf("remove member: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, map[string]any{"squad_id": args[0], "member_id": memberID, "removed": true})
	}
	fmt.Fprintf(os.Stderr, "Member %s removed from squad.\n", memberID)
	return nil
}

var squadActivityCmd = &cobra.Command{
	Use:   "activity <issue-id> <outcome>",
	Short: "Записать оценку агента-лидера команды по issue",
	Long: `Записывает решение агента-лидера команды по issue.

Outcome должен быть одним из значений:
  action     — лидер делегировал работу или выполнил действие
  no_action  — лидер оценил ситуацию и решил, что действий не нужно
  failed     — у лидера возникла ошибка

Команду вызывают агенты-лидеры после каждого срабатывания триггера,
чтобы записать своё решение в историю issue.`,
	Args: exactArgs(2),
	RunE: runSquadActivity,
}

func runSquadActivity(cmd *cobra.Command, args []string) error {
	issueID := args[0]
	outcome := args[1]

	if outcome != "action" && outcome != "no_action" && outcome != "failed" {
		return fmt.Errorf("invalid outcome %q; valid values: action, no_action, failed", outcome)
	}

	reason, _ := cmd.Flags().GetString("reason")

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	issueRef, err := resolveIssueRef(ctx, client, issueID)
	if err != nil {
		return fmt.Errorf("resolve issue: %w", err)
	}

	body := map[string]any{
		"outcome": outcome,
		"reason":  reason,
	}
	var result map[string]any
	if err := client.PostJSON(ctx, "/api/issues/"+issueRef.ID+"/squad-evaluated", body, &result); err != nil {
		return fmt.Errorf("record evaluation: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Squad evaluation recorded: %s (issue %s)\n", outcome, issueRef.Display)

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	return nil
}

func init() {

	squadListCmd.Flags().String("output", "table", "Формат вывода: table или json")

	squadGetCmd.Flags().String("output", "table", "Формат вывода: table или json")

	squadCreateCmd.Flags().String("name", "", "Название команды (обязательно)")
	squadCreateCmd.Flags().String("description", "", "Описание команды")
	squadCreateCmd.Flags().String("leader", "", "Агент-лидер (имя или ID) — обязательно")
	squadCreateCmd.Flags().String("output", "json", "Формат вывода: table или json")

	squadUpdateCmd.Flags().String("name", "", "Новое название")
	squadUpdateCmd.Flags().String("description", "", "Новое описание")
	squadUpdateCmd.Flags().String("instructions", "", "Новые инструкции")
	squadUpdateCmd.Flags().String("leader", "", "Новый агент-лидер (имя или ID)")
	squadUpdateCmd.Flags().String("avatar-url", "", "Новый URL аватара")
	squadUpdateCmd.Flags().String("output", "json", "Формат вывода: table или json")

	squadDeleteCmd.Flags().String("output", "table", "Формат вывода: table или json")

	squadMemberListCmd.Flags().String("output", "table", "Формат вывода: table или json")

	squadMemberAddCmd.Flags().String("member-id", "", "ID участника или агента (обязательно)")
	squadMemberAddCmd.Flags().String("type", "agent", "Тип участника: agent или member")
	squadMemberAddCmd.Flags().String("role", "member", "Роль в команде")
	squadMemberAddCmd.Flags().String("output", "json", "Формат вывода: table или json")

	squadMemberRemoveCmd.Flags().String("member-id", "", "ID участника или агента (обязательно)")
	squadMemberRemoveCmd.Flags().String("type", "agent", "Тип участника: agent или member")
	squadMemberRemoveCmd.Flags().String("output", "table", "Формат вывода: table или json")

	squadMemberSetRoleCmd.Flags().String("member-id", "", "ID участника или агента (обязательно)")
	squadMemberSetRoleCmd.Flags().String("member-type", "agent", "Тип участника: agent или member")
	squadMemberSetRoleCmd.Flags().String("role", "", "Новая роль в команде (обязательно)")
	squadMemberSetRoleCmd.Flags().String("output", "json", "Формат вывода: table или json")

	squadActivityCmd.Flags().String("reason", "", "Краткое объяснение решения")
	squadActivityCmd.Flags().String("output", "table", "Формат вывода: table или json")

	squadMemberCmd.AddCommand(squadMemberListCmd)
	squadMemberCmd.AddCommand(squadMemberAddCmd)
	squadMemberCmd.AddCommand(squadMemberRemoveCmd)
	squadMemberCmd.AddCommand(squadMemberSetRoleCmd)

	squadCmd.AddCommand(squadListCmd)
	squadCmd.AddCommand(squadGetCmd)
	squadCmd.AddCommand(squadCreateCmd)
	squadCmd.AddCommand(squadUpdateCmd)
	squadCmd.AddCommand(squadDeleteCmd)
	squadCmd.AddCommand(squadMemberCmd)
	squadCmd.AddCommand(squadActivityCmd)
}
