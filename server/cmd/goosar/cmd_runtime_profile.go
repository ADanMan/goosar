package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/adanman/goosar/server/internal/cli"
	"github.com/adanman/goosar/server/pkg/agent"
)

var runtimeProfileCmd = &cobra.Command{
	Use:   "profile",
	Short: "Управление пользовательскими профилями сред выполнения",
}

var runtimeProfileListCmd = &cobra.Command{
	Use:   "list",
	Short: "Список пользовательских профилей сред выполнения в рабочем пространстве",
	RunE:  runRuntimeProfileList,
}

var runtimeProfileCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Создать пользовательский профиль среды выполнения",
	RunE:  runRuntimeProfileCreate,
}

var runtimeProfileUpdateCmd = &cobra.Command{
	Use:   "update <profile-id>",
	Short: "Изменить пользовательский профиль среды выполнения (семейство протокола не меняется)",
	Args:  exactArgs(1),
	RunE:  runRuntimeProfileUpdate,
}

var runtimeProfileDeleteCmd = &cobra.Command{
	Use:   "delete <profile-id>",
	Short: "Удалить пользовательский профиль среды выполнения",
	Args:  exactArgs(1),
	RunE:  runRuntimeProfileDelete,
}

var runtimeProfileSetPathCmd = &cobra.Command{
	Use:   "set-path <profile-id>",
	Short: "Закрепить путь к исполняемому файлу профиля на этой машине (только локально)",
	Args:  exactArgs(1),
	RunE:  runRuntimeProfileSetPath,
}

var runtimeProfileUnsetPathCmd = &cobra.Command{
	Use:   "unset-path <profile-id>",
	Short: "Убрать путь к исполняемому файлу профиля, закреплённый на этой машине",
	Args:  exactArgs(1),
	RunE:  runRuntimeProfileUnsetPath,
}

func init() {
	runtimeCmd.AddCommand(runtimeProfileCmd)
	runtimeProfileCmd.AddCommand(runtimeProfileListCmd)
	runtimeProfileCmd.AddCommand(runtimeProfileCreateCmd)
	runtimeProfileCmd.AddCommand(runtimeProfileUpdateCmd)
	runtimeProfileCmd.AddCommand(runtimeProfileDeleteCmd)
	runtimeProfileCmd.AddCommand(runtimeProfileSetPathCmd)
	runtimeProfileCmd.AddCommand(runtimeProfileUnsetPathCmd)

	runtimeProfileListCmd.Flags().String("output", "table", "Формат вывода: table или json")

	runtimeProfileCreateCmd.Flags().String("protocol-family", "", "Поддерживаемый бэкенд, на который направляется профиль (обязательно)")
	runtimeProfileCreateCmd.Flags().String("command-name", "", "Исполняемый файл, который демон ищет в PATH (обязательно)")
	runtimeProfileCreateCmd.Flags().String("display-name", "", "Понятное имя профиля (обязательно)")
	runtimeProfileCreateCmd.Flags().String("description", "", "Описание (необязательно)")
	runtimeProfileCreateCmd.Flags().String("output", "json", "Формат вывода: table или json")

	runtimeProfileUpdateCmd.Flags().String("display-name", "", "Новое отображаемое имя")
	runtimeProfileUpdateCmd.Flags().String("command-name", "", "Новое имя команды")
	runtimeProfileUpdateCmd.Flags().String("description", "", "Новое описание")

	runtimeProfileUpdateCmd.Flags().Bool("enabled", true, "Включить или отключить профиль")
	runtimeProfileUpdateCmd.Flags().String("output", "json", "Формат вывода: table или json")

	runtimeProfileSetPathCmd.Flags().String("path", "", "Абсолютный путь к исполняемому файлу на этой машине (обязательно)")
}

func runtimeProfilesPath(workspaceID string) string {
	return fmt.Sprintf("/api/workspaces/%s/runtime-profiles", workspaceID)
}

func validateProtocolFamily(family string) error {
	if !agent.IsSupportedType(family) {
		return fmt.Errorf("invalid --protocol-family %q: must be one of %s",
			family, strings.Join(agent.SupportedTypes, ", "))
	}
	return nil
}

func runRuntimeProfileList(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	workspaceID, err := requireWorkspaceID(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var resp struct {
		RuntimeProfiles []map[string]any `json:"runtime_profiles"`
	}
	if err := client.GetJSON(ctx, runtimeProfilesPath(workspaceID), &resp); err != nil {
		return fmt.Errorf("list runtime profiles: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, resp.RuntimeProfiles)
	}
	printRuntimeProfileTable(resp.RuntimeProfiles)
	return nil
}

func runRuntimeProfileCreate(cmd *cobra.Command, _ []string) error {
	family, _ := cmd.Flags().GetString("protocol-family")
	commandName, _ := cmd.Flags().GetString("command-name")
	displayName, _ := cmd.Flags().GetString("display-name")
	description, _ := cmd.Flags().GetString("description")

	if strings.TrimSpace(family) == "" {
		return fmt.Errorf("--protocol-family is required")
	}
	if strings.TrimSpace(commandName) == "" {
		return fmt.Errorf("--command-name is required")
	}
	if strings.TrimSpace(displayName) == "" {
		return fmt.Errorf("--display-name is required")
	}
	if err := validateProtocolFamily(family); err != nil {
		return err
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	workspaceID, err := requireWorkspaceID(cmd)
	if err != nil {
		return err
	}

	body := map[string]any{
		"display_name":    displayName,
		"protocol_family": family,
		"command_name":    commandName,
	}
	if description != "" {
		body["description"] = description
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var профиль map[string]any
	if err := client.PostJSON(ctx, runtimeProfilesPath(workspaceID), body, &профиль); err != nil {
		return fmt.Errorf("create runtime profile: %w", err)
	}
	return outputRuntimeProfile(cmd, профиль)
}

func runRuntimeProfileUpdate(cmd *cobra.Command, args []string) error {
	profileID := args[0]

	body := map[string]any{}
	if cmd.Flags().Changed("display-name") {
		v, _ := cmd.Flags().GetString("display-name")
		body["display_name"] = v
	}
	if cmd.Flags().Changed("command-name") {
		v, _ := cmd.Flags().GetString("command-name")
		body["command_name"] = v
	}
	if cmd.Flags().Changed("description") {
		v, _ := cmd.Flags().GetString("description")
		body["description"] = v
	}
	if cmd.Flags().Changed("enabled") {
		v, _ := cmd.Flags().GetBool("enabled")
		body["enabled"] = v
	}

	if len(body) == 0 {
		return fmt.Errorf("no fields to update: pass at least one of --display-name, --command-name, --description, --enabled")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	workspaceID, err := requireWorkspaceID(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	path := runtimeProfilesPath(workspaceID) + "/" + profileID
	var профиль map[string]any
	if err := client.PatchJSON(ctx, path, body, &профиль); err != nil {
		return fmt.Errorf("update runtime profile: %w", err)
	}
	return outputRuntimeProfile(cmd, профиль)
}

func runRuntimeProfileDelete(cmd *cobra.Command, args []string) error {
	profileID := args[0]

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	workspaceID, err := requireWorkspaceID(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	path := runtimeProfilesPath(workspaceID) + "/" + profileID
	if err := client.DeleteJSON(ctx, path); err != nil {

		var httpErr *cli.HTTPError
		if errors.As(err, &httpErr) && httpErr.StatusCode == http.StatusConflict {
			сообщение := strings.TrimSpace(httpErr.Body)
			if сообщение == "" {
				сообщение = "profile still has active agents bound to it"
			}
			return fmt.Errorf("cannot delete runtime profile %s: %s", profileID, сообщение)
		}
		return fmt.Errorf("delete runtime profile: %w", err)
	}
	fmt.Printf("Deleted runtime profile %s\n", profileID)
	return nil
}

func runRuntimeProfileSetPath(cmd *cobra.Command, args []string) error {
	profileID := args[0]
	path, _ := cmd.Flags().GetString("path")
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("--path is required")
	}
	if !filepath.IsAbs(path) {
		return fmt.Errorf("--path must be an absolute path, got %q", path)
	}

	профиль := resolveProfile(cmd)
	cfg, err := cli.LoadCLIConfigForProfile(профиль)
	if err != nil {
		return fmt.Errorf("load CLI config: %w", err)
	}
	if cfg.ProfileCommandOverrides == nil {
		cfg.ProfileCommandOverrides = map[string]string{}
	}
	cfg.ProfileCommandOverrides[profileID] = path
	if err := cli.SaveCLIConfigForProfile(cfg, профиль); err != nil {
		return fmt.Errorf("save CLI config: %w", err)
	}
	fmt.Printf("Pinned runtime profile %s to %s on this machine.\n", profileID, path)
	fmt.Println("Restart the daemon for the change to take effect.")
	return nil
}

func runRuntimeProfileUnsetPath(cmd *cobra.Command, args []string) error {
	profileID := args[0]

	профиль := resolveProfile(cmd)
	cfg, err := cli.LoadCLIConfigForProfile(профиль)
	if err != nil {
		return fmt.Errorf("load CLI config: %w", err)
	}
	if _, ok := cfg.ProfileCommandOverrides[profileID]; !ok {
		fmt.Printf("No per-machine path override set for runtime profile %s.\n", profileID)
		return nil
	}
	delete(cfg.ProfileCommandOverrides, profileID)
	if len(cfg.ProfileCommandOverrides) == 0 {

		cfg.ProfileCommandOverrides = nil
	}
	if err := cli.SaveCLIConfigForProfile(cfg, профиль); err != nil {
		return fmt.Errorf("save CLI config: %w", err)
	}
	fmt.Printf("Removed per-machine path override for runtime profile %s.\n", profileID)
	fmt.Println("Restart the daemon for the change to take effect.")
	return nil
}

func outputRuntimeProfile(cmd *cobra.Command, профиль map[string]any) error {
	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, профиль)
	}
	printRuntimeProfileTable([]map[string]any{профиль})
	return nil
}

func printRuntimeProfileTable(profiles []map[string]any) {
	headers := []string{"ID", "DISPLAY_NAME", "PROTOCOL_FAMILY", "COMMAND_NAME", "ENABLED"}
	rows := make([][]string, 0, len(profiles))
	for _, p := range profiles {
		rows = append(rows, []string{
			strVal(p, "id"),
			strVal(p, "display_name"),
			strVal(p, "protocol_family"),
			strVal(p, "command_name"),
			strVal(p, "enabled"),
		})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i][1] < rows[j][1] })
	cli.PrintTable(os.Stdout, headers, rows)
}
