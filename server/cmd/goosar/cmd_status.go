package main

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/adanman/goosar/server/internal/cli"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Показать реальное состояние системы этого рабочего пространства (среды выполнения, provisioning, MCP, периметр, LLM)",
	Long: "Показывает реальное состояние системы этого рабочего пространства.\n\n" +
		"Только факты: имена, состояния, количества и время — значения учётных данных не выводятся никогда. " +
		"Раздел, по которому у сервера нет сигнала, получает `unknown`, и это значит «неизвестно»: " +
		"это не сбой и не подтверждение, что всё в порядке.",
	RunE: runStatus,
}

func init() {
	statusCmd.Flags().String("output", "table", "Формат вывода: table или json")
}

func runStatus(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var status map[string]any
	if err := client.GetJSON(ctx, "/api/status", &status); err != nil {
		return fmt.Errorf("get status: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, status)
	}

	headers := []string{"SECTION", "STATE", "DETAIL"}
	rows := [][]string{
		{"workspace", strVal(section(status, "workspace"), "slug"), strVal(section(status, "workspace"), "name")},
		{"caller", strVal(section(status, "caller"), "actor"), callerDetail(section(status, "caller"))},
		{
			"runtimes", strVal(section(status, "runtimes"), "state"),
			fmt.Sprintf("%s/%s online", strVal(section(status, "runtimes"), "online"), strVal(section(status, "runtimes"), "total")),
		},
		{
			"provisioning", strVal(section(status, "provisioning"), "state"),
			fmt.Sprintf("%s pinned, %s delivered", strVal(section(status, "provisioning"), "pinned_packages"), strVal(section(status, "provisioning"), "delivered_packages")),
		},
		{
			"mcp", strVal(section(status, "mcp"), "state"),
			fmt.Sprintf("%s declared, tools %s", strVal(section(status, "mcp"), "workspace_servers"), strVal(section(status, "mcp"), "tools_verified")),
		},

		{
			"perimeter", strVal(section(status, "perimeter"), "delivery_profile"),
			fmt.Sprintf("deployment %s, member access %s, kerberos %s",
				strVal(section(status, "perimeter"), "deployment_profile"),
				strVal(section(status, "perimeter"), "member_access"),
				strVal(section(status, "perimeter"), "kerberos")),
		},
		{"llm", strVal(section(status, "llm"), "state"), strVal(section(status, "llm"), "base_url")},
	}
	cli.PrintTable(os.Stdout, headers, rows)
	fmt.Fprintln(os.Stdout, "\nRun with --output json for the notes explaining every unknown.")
	return nil
}

func section(status map[string]any, key string) map[string]any {
	if sub, ok := status[key].(map[string]any); ok {
		return sub
	}
	return map[string]any{}
}

func callerDetail(caller map[string]any) string {
	name := strVal(caller, "agent_name")
	if name == "" {
		return ""
	}
	return fmt.Sprintf("%s → runtime %s", name, strVal(caller, "runtime_status"))
}
