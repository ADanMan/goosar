package main

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/adanman/goosar/server/internal/cli"
)

var issueLabelCmd = &cobra.Command{
	Use:   "label",
	Short: "Управление метками issue",
}

var issueLabelListCmd = &cobra.Command{
	Use:   "list <issue-id>",
	Short: "Список меток issue",
	Args:  exactArgs(1),
	RunE:  runIssueLabelList,
}

var issueLabelAddCmd = &cobra.Command{
	Use:   "add <issue-id> <label-id>",
	Short: "Добавить метку к issue",
	Args:  exactArgs(2),
	RunE:  runIssueLabelAdd,
}

var issueLabelRemoveCmd = &cobra.Command{
	Use:   "remove <issue-id> <label-id>",
	Short: "Убрать метку у issue",
	Args:  exactArgs(2),
	RunE:  runIssueLabelRemove,
}

func init() {
	issueLabelCmd.AddCommand(issueLabelListCmd)
	issueLabelCmd.AddCommand(issueLabelAddCmd)
	issueLabelCmd.AddCommand(issueLabelRemoveCmd)

	issueLabelListCmd.Flags().String("output", "table", "Формат вывода: table или json")
	issueLabelAddCmd.Flags().String("output", "table", "Формат вывода: table или json")
	issueLabelRemoveCmd.Flags().String("output", "table", "Формат вывода: table или json")
	issueLabelListCmd.Flags().Bool("full-id", false, "Показывать полные UUID в таблице")
	issueLabelAddCmd.Flags().Bool("full-id", false, "Показывать полные UUID в таблице")
	issueLabelRemoveCmd.Flags().Bool("full-id", false, "Показывать полные UUID в таблице")

	issueCmd.AddCommand(issueLabelCmd)
}

func runIssueLabelList(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	issueRef, err := resolveIssueRef(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve issue: %w", err)
	}

	var result map[string]any
	if err := client.GetJSON(ctx, "/api/issues/"+issueRef.ID+"/labels", &result); err != nil {
		return fmt.Errorf("list issue labels: %w", err)
	}
	labelsRaw, _ := result["labels"].([]any)

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, labelsRaw)
	}
	fullID, _ := cmd.Flags().GetBool("full-id")
	printLabelTable(labelsRaw, fullID)
	return nil
}

func runIssueLabelAdd(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	issueRef, err := resolveIssueRef(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve issue: %w", err)
	}
	labelRef, err := resolveLabelID(ctx, client, args[1])
	if err != nil {
		return fmt.Errorf("resolve label: %w", err)
	}

	body := map[string]any{"label_id": labelRef.ID}
	var result map[string]any
	if err := client.PostJSON(ctx, "/api/issues/"+issueRef.ID+"/labels", body, &result); err != nil {
		return fmt.Errorf("attach label: %w", err)
	}
	labelsRaw, _ := result["labels"].([]any)

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, labelsRaw)
	}
	fullID, _ := cmd.Flags().GetBool("full-id")
	printLabelTable(labelsRaw, fullID)
	return nil
}

func runIssueLabelRemove(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	issueRef, err := resolveIssueRef(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve issue: %w", err)
	}
	labelRef, err := resolveLabelID(ctx, client, args[1])
	if err != nil {
		return fmt.Errorf("resolve label: %w", err)
	}

	if err := client.DeleteJSON(ctx, "/api/issues/"+issueRef.ID+"/labels/"+labelRef.ID); err != nil {
		return fmt.Errorf("detach label: %w", err)
	}

	var result map[string]any
	output, _ := cmd.Flags().GetString("output")
	if err := client.GetJSON(ctx, "/api/issues/"+issueRef.ID+"/labels", &result); err != nil {
		if output == "json" {
			return cli.PrintJSON(os.Stdout, map[string]any{"detached": true})
		}
		fmt.Fprintln(os.Stdout, "Label detached.")
		return nil
	}
	labelsRaw, _ := result["labels"].([]any)
	if output == "json" {
		return cli.PrintJSON(os.Stdout, labelsRaw)
	}
	fullID, _ := cmd.Flags().GetBool("full-id")
	printLabelTable(labelsRaw, fullID)
	return nil
}

func printLabelTable(labels []any, fullID bool) {
	headers := []string{"ID", "NAME", "COLOR"}
	rows := make([][]string, 0, len(labels))
	for _, raw := range labels {
		l, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		rows = append(rows, []string{
			displayID(strVal(l, "id"), fullID),
			strVal(l, "name"),
			strVal(l, "color"),
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
}
