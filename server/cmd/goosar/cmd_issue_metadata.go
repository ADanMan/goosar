package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/adanman/goosar/server/internal/cli"
)

func buildMetadataFilterQueryParam(pairs []string) (string, error) {
	if len(pairs) == 0 {
		return "", nil
	}
	out := make(map[string]any, len(pairs))
	for _, pair := range pairs {
		idx := strings.IndexByte(pair, '=')
		if idx <= 0 {
			return "", fmt.Errorf("--metadata %q must be in key=value form", pair)
		}
		key := pair[:idx]
		raw := pair[idx+1:]
		if _, dup := out[key]; dup {
			return "", fmt.Errorf("--metadata key %q given more than once; combine into a single filter", key)
		}
		encoded, err := parseMetadataValue(raw, "")
		if err != nil {
			return "", fmt.Errorf("--metadata %s: %w", key, err)
		}
		var v any
		if err := json.Unmarshal(encoded, &v); err != nil {
			return "", fmt.Errorf("--metadata %s: encode value: %w", key, err)
		}
		out[key] = v
	}
	buf, err := json.Marshal(out)
	if err != nil {
		return "", fmt.Errorf("encode metadata filter: %w", err)
	}
	return string(buf), nil
}

var issueMetadataCmd = &cobra.Command{
	Use:   "metadata",
	Short: "Управление метаданными issue (ключ-значение)",
}

var issueMetadataListCmd = &cobra.Command{
	Use:   "list <issue-id>",
	Short: "Список всех ключей метаданных issue",
	Args:  exactArgs(1),
	RunE:  runIssueMetadataList,
}

var issueMetadataGetCmd = &cobra.Command{
	Use:   "get <issue-id>",
	Short: "Получить значение одного ключа метаданных",
	Args:  exactArgs(1),
	RunE:  runIssueMetadataGet,
}

var issueMetadataSetCmd = &cobra.Command{
	Use:   "set <issue-id>",
	Short: "Задать значение одного ключа метаданных",
	Long: `Задаёт значение одного ключа метаданных. По умолчанию значение разбирается как JSON:
  --value true / --value false  → bool
  --value 3 / --value 3.14      → number
  --value waiting               → string
Флаг --type принудительно задаёт тип. Чтобы получить строку, когда голое значение
иначе определилось бы как число или bool, заключите его в кавычки: '"42"' (JSON-экранирование).`,
	Args: exactArgs(1),
	RunE: runIssueMetadataSet,
}

var issueMetadataDeleteCmd = &cobra.Command{
	Use:   "delete <issue-id>",
	Short: "Удалить один ключ метаданных",
	Args:  exactArgs(1),
	RunE:  runIssueMetadataDelete,
}

func init() {
	issueMetadataCmd.AddCommand(issueMetadataListCmd)
	issueMetadataCmd.AddCommand(issueMetadataGetCmd)
	issueMetadataCmd.AddCommand(issueMetadataSetCmd)
	issueMetadataCmd.AddCommand(issueMetadataDeleteCmd)

	issueMetadataListCmd.Flags().String("output", "table", "Формат вывода: table или json")
	issueMetadataGetCmd.Flags().String("output", "json", "Формат вывода: table или json")
	issueMetadataGetCmd.Flags().String("key", "", "Ключ метаданных (обязательно)")
	issueMetadataSetCmd.Flags().String("output", "table", "Формат вывода: table или json")
	issueMetadataSetCmd.Flags().String("key", "", "Ключ метаданных (обязательно)")
	issueMetadataSetCmd.Flags().String("value", "", "Значение метаданных (обязательно)")
	issueMetadataSetCmd.Flags().String("type", "", "Принудительный тип значения: string, number или bool (по умолчанию определяется автоматически при разборе JSON)")
	issueMetadataDeleteCmd.Flags().String("output", "table", "Формат вывода: table или json")
	issueMetadataDeleteCmd.Flags().String("key", "", "Ключ метаданных (обязательно)")

	issueCmd.AddCommand(issueMetadataCmd)
}

func parseMetadataValue(raw, forcedType string) (json.RawMessage, error) {
	switch forcedType {
	case "string":
		buf, err := json.Marshal(raw)
		if err != nil {
			return nil, fmt.Errorf("encode string value: %w", err)
		}
		return buf, nil
	case "number":
		if _, err := strconv.ParseFloat(raw, 64); err != nil {
			return nil, fmt.Errorf("value %q is not a valid number", raw)
		}
		return json.RawMessage(raw), nil
	case "bool":
		if raw != "true" && raw != "false" {
			return nil, fmt.Errorf("value %q is not a valid bool (expected true or false)", raw)
		}
		return json.RawMessage(raw), nil
	case "":

	default:
		return nil, fmt.Errorf("unknown --type %q (expected string, number, or bool)", forcedType)
	}

	var v any
	if err := json.Unmarshal([]byte(raw), &v); err == nil {
		switch v.(type) {
		case string, bool, float64:
			return json.RawMessage(raw), nil
		}
	}
	buf, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("encode string value: %w", err)
	}
	return buf, nil
}

func runIssueMetadataList(cmd *cobra.Command, args []string) error {
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
	if err := client.GetJSON(ctx, "/api/issues/"+issueRef.ID+"/metadata", &result); err != nil {

		var httpErr *cli.HTTPError
		if errors.As(err, &httpErr) && httpErr.StatusCode == http.StatusNotFound {
			output, _ := cmd.Flags().GetString("output")
			empty := map[string]any{}
			if output == "json" {
				return cli.PrintJSON(os.Stdout, empty)
			}
			printMetadataTable(empty)
			return nil
		}
		return fmt.Errorf("list metadata: %w", err)
	}
	metadata, _ := result["metadata"].(map[string]any)
	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, metadata)
	}
	printMetadataTable(metadata)
	return nil
}

func runIssueMetadataGet(cmd *cobra.Command, args []string) error {
	key, _ := cmd.Flags().GetString("key")
	if key == "" {
		return fmt.Errorf("--key is required")
	}

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
	if err := client.GetJSON(ctx, "/api/issues/"+issueRef.ID+"/metadata", &result); err != nil {
		return fmt.Errorf("get metadata: %w", err)
	}
	metadata, _ := result["metadata"].(map[string]any)
	value, present := metadata[key]
	if !present {
		return fmt.Errorf("key %q not found on issue", key)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, value)
	}
	headers := []string{"KEY", "VALUE", "TYPE"}
	rows := [][]string{{key, formatMetadataValue(value), metadataValueType(value)}}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runIssueMetadataSet(cmd *cobra.Command, args []string) error {
	key, _ := cmd.Flags().GetString("key")
	if key == "" {
		return fmt.Errorf("--key is required")
	}
	if !cmd.Flags().Changed("value") {
		return fmt.Errorf("--value is required")
	}
	rawValue, _ := cmd.Flags().GetString("value")
	forcedType, _ := cmd.Flags().GetString("type")
	value, err := parseMetadataValue(rawValue, forcedType)
	if err != nil {
		return err
	}

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

	body := map[string]any{"value": value}
	var result map[string]any
	path := "/api/issues/" + issueRef.ID + "/metadata/" + key
	if err := client.PutJSON(ctx, path, body, &result); err != nil {
		return fmt.Errorf("set metadata: %w", err)
	}
	metadata, _ := result["metadata"].(map[string]any)

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, metadata)
	}
	printMetadataTable(metadata)
	return nil
}

func runIssueMetadataDelete(cmd *cobra.Command, args []string) error {
	key, _ := cmd.Flags().GetString("key")
	if key == "" {
		return fmt.Errorf("--key is required")
	}

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

	path := "/api/issues/" + issueRef.ID + "/metadata/" + key
	if err := client.DeleteJSON(ctx, path); err != nil {
		return fmt.Errorf("delete metadata: %w", err)
	}

	var result map[string]any
	output, _ := cmd.Flags().GetString("output")
	if err := client.GetJSON(ctx, "/api/issues/"+issueRef.ID+"/metadata", &result); err != nil {
		if output == "json" {
			return cli.PrintJSON(os.Stdout, map[string]any{"deleted": true})
		}
		fmt.Fprintln(os.Stdout, "Key deleted.")
		return nil
	}
	metadata, _ := result["metadata"].(map[string]any)
	if output == "json" {
		return cli.PrintJSON(os.Stdout, metadata)
	}
	printMetadataTable(metadata)
	return nil
}

func printMetadataTable(metadata map[string]any) {
	headers := []string{"KEY", "VALUE", "TYPE"}
	keys := make([]string, 0, len(metadata))
	for k := range metadata {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	rows := make([][]string, 0, len(keys))
	for _, k := range keys {
		v := metadata[k]
		rows = append(rows, []string{k, formatMetadataValue(v), metadataValueType(v)})
	}
	cli.PrintTable(os.Stdout, headers, rows)
}

func formatMetadataValue(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case bool:
		if x {
			return "true"
		}
		return "false"
	case float64:

		if x == float64(int64(x)) {
			return strconv.FormatInt(int64(x), 10)
		}
		return strconv.FormatFloat(x, 'f', -1, 64)
	default:
		buf, _ := json.Marshal(v)
		return string(buf)
	}
}

func metadataValueType(v any) string {
	switch v.(type) {
	case string:
		return "string"
	case bool:
		return "bool"
	case float64:
		return "number"
	default:
		return "unknown"
	}
}
