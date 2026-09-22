package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/adanman/goosar/server/internal/cli"
)

var runtimeCmd = &cobra.Command{
	Use:   "runtime",
	Short: "Работа со средами выполнения агентов",
}

var runtimeListCmd = &cobra.Command{
	Use:   "list",
	Short: "Список сред выполнения в рабочем пространстве",
	RunE:  runRuntimeList,
}

var runtimeUsageCmd = &cobra.Command{
	Use:   "usage <runtime-id>",
	Short: "Расход токенов среды выполнения",
	Args:  exactArgs(1),
	RunE:  runRuntimeUsage,
}

var runtimeActivityCmd = &cobra.Command{
	Use:   "activity <runtime-id>",
	Short: "Почасовая активность task в среде выполнения",
	Args:  exactArgs(1),
	RunE:  runRuntimeActivity,
}

var runtimeUpdateCmd = &cobra.Command{
	Use:   "update <runtime-id>",
	Short: "Запустить обновление CLI в среде выполнения",
	Args:  exactArgs(1),
	RunE:  runRuntimeUpdate,
}

var runtimeRenameCmd = &cobra.Command{
	Use:   "rename <runtime-id> <name>",
	Short: "Задать своё отображаемое имя среды выполнения",
	Long: "Задать (или очистить) своё отображаемое имя среды выполнения.\n\n" +
		"Передайте пустое имя (\"\"), чтобы очистить своё имя и вернуть имя по умолчанию. " +
		"Флаг --machine применяет имя ко всем средам выполнения на той же машине.",
	Args: exactArgs(2),
	RunE: runRuntimeRename,
}

var runtimeDeleteCmd = &cobra.Command{
	Use:   "delete <runtime-id>",
	Short: "Удалить среду выполнения из рабочего пространства",
	Long: "Удалить регистрацию среды выполнения из рабочего пространства.\n\n" +
		"По умолчанию удаление отклоняется, если к среде выполнения ещё привязаны активные агенты. " +
		"Флаг --cascade архивирует этих агентов, отменяет их queued/running task и удаляет среду выполнения.",
	Args: exactArgs(1),
	RunE: runRuntimeDelete,
}

func init() {
	runtimeCmd.AddCommand(runtimeListCmd)
	runtimeCmd.AddCommand(runtimeUsageCmd)
	runtimeCmd.AddCommand(runtimeActivityCmd)
	runtimeCmd.AddCommand(runtimeUpdateCmd)
	runtimeCmd.AddCommand(runtimeRenameCmd)
	runtimeCmd.AddCommand(runtimeDeleteCmd)

	runtimeListCmd.Flags().String("output", "table", "Формат вывода: table или json")

	runtimeUsageCmd.Flags().String("output", "table", "Формат вывода: table или json")
	runtimeUsageCmd.Flags().Int("days", 90, "За сколько дней получить данные о расходе (максимум 365)")

	runtimeActivityCmd.Flags().String("output", "table", "Формат вывода: table или json")

	runtimeUpdateCmd.Flags().String("target-version", "", "Версия, до которой обновить (обязательно)")
	runtimeUpdateCmd.Flags().String("output", "json", "Формат вывода: table или json")
	runtimeUpdateCmd.Flags().Bool("wait", false, "Дождаться завершения обновления (опрашивать до конца)")

	runtimeRenameCmd.Flags().Bool("machine", false, "Применить имя ко всем средам выполнения на той же машине")
	runtimeRenameCmd.Flags().String("output", "table", "Формат вывода: table или json")

	runtimeDeleteCmd.Flags().Bool("cascade", false, "Архивировать активных агентов, привязанных к среде выполнения, отменить их task, затем удалить среду выполнения")
	runtimeDeleteCmd.Flags().String("output", "table", "Формат вывода: table или json")
}

func runRuntimeList(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var runtimes []map[string]any
	if err := client.GetJSON(ctx, "/api/runtimes", &runtimes); err != nil {
		return fmt.Errorf("list runtimes: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, runtimes)
	}

	headers := []string{"ID", "NAME", "MODE", "PROVIDER", "STATUS", "LAST_SEEN"}
	rows := make([][]string, 0, len(runtimes))
	for _, rt := range runtimes {
		rows = append(rows, []string{
			strVal(rt, "id"),
			strVal(rt, "name"),
			strVal(rt, "runtime_mode"),
			strVal(rt, "provider"),
			strVal(rt, "status"),
			strVal(rt, "last_seen_at"),
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runRuntimeUsage(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	days, _ := cmd.Flags().GetInt("days")
	if days < 1 || days > 365 {
		return fmt.Errorf("--days must be between 1 and 365")
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var usage []map[string]any
	path := fmt.Sprintf("/api/runtimes/%s/usage?days=%d", args[0], days)
	if err := client.GetJSON(ctx, path, &usage); err != nil {
		return fmt.Errorf("get runtime usage: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, usage)
	}

	headers := []string{"DATE", "PROVIDER", "MODEL", "INPUT_TOKENS", "OUTPUT_TOKENS", "CACHE_READ", "CACHE_WRITE"}
	rows := make([][]string, 0, len(usage))
	for _, u := range usage {
		rows = append(rows, []string{
			strVal(u, "date"),
			strVal(u, "provider"),
			strVal(u, "model"),
			strVal(u, "input_tokens"),
			strVal(u, "output_tokens"),
			strVal(u, "cache_read_tokens"),
			strVal(u, "cache_write_tokens"),
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runRuntimeActivity(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var activity []map[string]any
	if err := client.GetJSON(ctx, "/api/runtimes/"+args[0]+"/activity", &activity); err != nil {
		return fmt.Errorf("get runtime activity: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, activity)
	}

	headers := []string{"HOUR", "COUNT"}
	rows := make([][]string, 0, len(activity))
	for _, a := range activity {
		rows = append(rows, []string{
			strVal(a, "hour"),
			strVal(a, "count"),
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runRuntimeDelete(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	runtimeID := args[0]
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	err = client.DeleteJSON(ctx, "/api/runtimes/"+runtimeID)
	if err == nil {
		return printRuntimeDeleteResult(cmd, map[string]any{
			"id":      runtimeID,
			"deleted": true,
		})
	}

	conflict, ok := runtimeDeleteConflict(err)
	if !ok {
		return fmt.Errorf("delete runtime: %w", err)
	}

	cascade, _ := cmd.Flags().GetBool("cascade")
	if !cascade {
		return fmt.Errorf(
			"delete runtime: runtime has active agents bound to it (%s); archive or reassign them first, or rerun with --cascade to archive them and delete the runtime",
			strings.Join(conflict.AgentDisplays(), ", "),
		)
	}

	body := map[string]any{
		"expected_active_agent_ids": conflict.AgentIDs(),
	}
	var result map[string]any
	if err := client.PostJSON(ctx, "/api/runtimes/"+runtimeID+"/archive-agents-and-delete", body, &result); err != nil {
		return fmt.Errorf("cascade delete runtime: %w", err)
	}
	result["id"] = runtimeID
	result["deleted"] = true
	return printRuntimeDeleteResult(cmd, result)
}

func runRuntimeRename(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	machine, _ := cmd.Flags().GetBool("machine")
	body := map[string]any{"custom_name": args[1]}
	if machine {
		body["apply_to_machine"] = true
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var rt map[string]any
	if err := client.PatchJSON(ctx, "/api/runtimes/"+args[0], body, &rt); err != nil {
		return fmt.Errorf("rename runtime: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, rt)
	}

	if strings.TrimSpace(args[1]) == "" {
		fmt.Fprintf(os.Stderr, "Custom name cleared; runtime is now %q.\n", strVal(rt, "name"))
	} else {
		fmt.Fprintf(os.Stderr, "Runtime renamed to %q.\n", strVal(rt, "custom_name"))
	}
	return nil
}

func runRuntimeUpdate(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	targetVersion, _ := cmd.Flags().GetString("target-version")
	if targetVersion == "" {
		return fmt.Errorf("--target-version is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), cli.AtLeastAPITimeout(150*time.Second))
	defer cancel()

	body := map[string]any{
		"target_version": targetVersion,
	}

	var update map[string]any
	if err := client.PostJSON(ctx, "/api/runtimes/"+args[0]+"/update", body, &update); err != nil {
		return fmt.Errorf("initiate update: %w", err)
	}

	wait, _ := cmd.Flags().GetBool("wait")
	if !wait {
		output, _ := cmd.Flags().GetString("output")
		if output == "json" {
			return cli.PrintJSON(os.Stdout, update)
		}
		fmt.Printf("Update initiated: %s (status: %s)\n", strVal(update, "id"), strVal(update, "status"))
		return nil
	}

	updateID := strVal(update, "id")
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timed out waiting for update (last status: %s)", strVal(update, "status"))
		case <-time.After(2 * time.Second):
		}

		if err := client.GetJSON(ctx, "/api/runtimes/"+args[0]+"/update/"+updateID, &update); err != nil {
			return fmt.Errorf("get update status: %w", err)
		}

		status := strVal(update, "status")
		if status == "completed" || status == "failed" || status == "timeout" {
			output, _ := cmd.Flags().GetString("output")
			if output == "json" {
				return cli.PrintJSON(os.Stdout, update)
			}
			if status == "completed" {
				fmt.Printf("Update completed: %s\n", strVal(update, "output"))
			} else {
				fmt.Printf("Update %s: %s\n", status, strVal(update, "error"))
			}
			return nil
		}
	}
}

type runtimeDeleteConflictPayload struct {
	Code         string `json:"code"`
	Error        string `json:"error"`
	ActiveAgents []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"active_agents"`
}

func runtimeDeleteConflict(err error) (runtimeDeleteConflictPayload, bool) {
	var httpErr *cli.HTTPError
	if !errors.As(err, &httpErr) || httpErr.StatusCode != http.StatusConflict {
		return runtimeDeleteConflictPayload{}, false
	}
	var payload runtimeDeleteConflictPayload
	if json.Unmarshal([]byte(httpErr.Body), &payload) != nil {
		return runtimeDeleteConflictPayload{}, false
	}
	if payload.Code != "runtime_has_active_agents" || len(payload.ActiveAgents) == 0 {
		return runtimeDeleteConflictPayload{}, false
	}
	return payload, true
}

func (p runtimeDeleteConflictPayload) AgentIDs() []string {
	ids := make([]string, 0, len(p.ActiveAgents))
	for _, agent := range p.ActiveAgents {
		if agent.ID != "" {
			ids = append(ids, agent.ID)
		}
	}
	return ids
}

func (p runtimeDeleteConflictPayload) AgentDisplays() []string {
	displays := make([]string, 0, len(p.ActiveAgents))
	for _, agent := range p.ActiveAgents {
		switch {
		case agent.Name != "" && agent.ID != "":
			displays = append(displays, fmt.Sprintf("%s (%s)", agent.Name, agent.ID))
		case agent.Name != "":
			displays = append(displays, agent.Name)
		case agent.ID != "":
			displays = append(displays, agent.ID)
		}
	}
	return displays
}

func printRuntimeDeleteResult(cmd *cobra.Command, result map[string]any) error {
	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}

	if agentsArchived, ok := result["agents_archived"]; ok {
		fmt.Fprintf(os.Stderr, "Runtime %s deleted; archived %v agent(s).\n", strVal(result, "id"), agentsArchived)
		return nil
	}
	fmt.Fprintf(os.Stderr, "Runtime %s deleted.\n", strVal(result, "id"))
	return nil
}
