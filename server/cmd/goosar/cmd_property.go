package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/adanman/goosar/server/internal/cli"
)

type propertyOptionDTO struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
}

type propertyDTO struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description"`
	Icon        string `json:"icon"`
	Config      struct {
		Options []propertyOptionDTO `json:"options"`
	} `json:"config"`
	Position   float64 `json:"position"`
	Archived   bool    `json:"archived"`
	UsageCount int64   `json:"usage_count"`
	CreatedAt  string  `json:"created_at"`
	UpdatedAt  string  `json:"updated_at"`
}

var propertyCmd = &cobra.Command{
	Use:   "property",
	Short: "Управление пользовательскими свойствами issue в рабочем пространстве",
}

var propertyListCmd = &cobra.Command{
	Use:   "list",
	Short: "Показать определения свойств",
	Args:  exactArgs(0),
	RunE:  runPropertyList,
}

var propertyGetCmd = &cobra.Command{
	Use:   "get <id-or-name>",
	Short: "Показать определение свойства",
	Args:  exactArgs(1),
	RunE:  runPropertyGet,
}

var propertyCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Создать определение свойства (только owner и admin рабочего пространства)",
	Long: `Создаёт определение свойства. Типы: text, number, select, multi_select,
date, checkbox, url. Для типов select можно передать флаг --option несколько раз:
  goosar property create --name Severity --type select \
      --option "Critical:#ef4444" --option "Major:#f59e0b" --option "Minor:#6b7280"
Суффикс цвета ":#rrggbb" необязателен.`,
	Args: exactArgs(0),
	RunE: runPropertyCreate,
}

var propertyUpdateCmd = &cobra.Command{
	Use:   "update <id-or-name>",
	Short: "Изменить определение свойства (только owner и admin; тип менять нельзя)",
	Long: `Изменяет определение свойства. Флаги --option ЗАМЕНЯЮТ весь список вариантов;
существующие варианты сопоставляются по имени, поэтому их id (и значения в issue) сохраняются.`,
	Args: exactArgs(1),
	RunE: runPropertyUpdate,
}

var propertyArchiveCmd = &cobra.Command{
	Use:   "archive <id-or-name>",
	Short: "Отправить определение свойства в архив (скрывается из выбора; значения сохраняются)",
	Args:  exactArgs(1),
	RunE:  makePropertyArchiveRun(true),
}

var propertyUnarchiveCmd = &cobra.Command{
	Use:   "unarchive <id-or-name>",
	Short: "Вернуть определение свойства из архива",
	Args:  exactArgs(1),
	RunE:  makePropertyArchiveRun(false),
}

var issuePropertyCmd = &cobra.Command{
	Use:   "property",
	Short: "Управление значениями пользовательских свойств в issue",
}

var issuePropertyListCmd = &cobra.Command{
	Use:   "list <issue-id>",
	Short: "Показать значения пользовательских свойств, заданные в issue",
	Args:  exactArgs(1),
	RunE:  runIssuePropertyList,
}

var issuePropertySetCmd = &cobra.Command{
	Use:   "set <issue-id>",
	Short: "Задать значение пользовательского свойства в issue",
	Long: `Задаёт значение пользовательского свойства. Свойство указывается через --name
(без учёта регистра) или по UUID. Формы значения по типам:
  select        --value Staging            (имя или id варианта)
  multi_select  --value "iOS,Android"      (имена или id вариантов через запятую)
  checkbox      --value true|false
  number        --value 3.5
  date          --value 2026-07-13
  text / url    --value "любая строка"`,
	Args: exactArgs(1),
	RunE: runIssuePropertySet,
}

var issuePropertyUnsetCmd = &cobra.Command{
	Use:   "unset <issue-id>",
	Short: "Убрать значение пользовательского свойства из issue",
	Args:  exactArgs(1),
	RunE:  runIssuePropertyUnset,
}

func init() {
	propertyCmd.AddCommand(propertyListCmd)
	propertyCmd.AddCommand(propertyGetCmd)
	propertyCmd.AddCommand(propertyCreateCmd)
	propertyCmd.AddCommand(propertyUpdateCmd)
	propertyCmd.AddCommand(propertyArchiveCmd)
	propertyCmd.AddCommand(propertyUnarchiveCmd)

	propertyListCmd.Flags().String("output", "table", "Формат вывода: table или json")
	propertyListCmd.Flags().Bool("include-archived", false, "Включить свойства из архива")
	propertyGetCmd.Flags().String("output", "json", "Формат вывода: table или json")
	propertyCreateCmd.Flags().String("output", "table", "Формат вывода: table или json")
	propertyCreateCmd.Flags().String("name", "", "Название свойства (обязательно)")
	propertyCreateCmd.Flags().String("type", "", "Тип свойства: text, number, select, multi_select, date, checkbox, url (обязательно)")
	propertyCreateCmd.Flags().String("description", "", "Описание свойства")
	propertyCreateCmd.Flags().String("icon", "", "Ключ значка свойства из выбора в веб-интерфейсе (например, flag, tag или shield)")
	propertyCreateCmd.Flags().StringArray("option", nil, `Вариант для select как "Name" или "Name:#rrggbb" (флаг можно повторять; только для типов select)`)
	propertyUpdateCmd.Flags().String("output", "table", "Формат вывода: table или json")
	propertyUpdateCmd.Flags().String("name", "", "Новое название свойства")
	propertyUpdateCmd.Flags().String("description", "", "Новое описание свойства")
	propertyUpdateCmd.Flags().String("icon", "", "Новый ключ значка свойства из выбора в веб-интерфейсе; пустое значение очищает")
	propertyUpdateCmd.Flags().StringArray("option", nil, `Новый список вариантов как "Name" или "Name:#rrggbb" (флаг можно повторять)`)
	propertyArchiveCmd.Flags().String("output", "table", "Формат вывода: table или json")
	propertyUnarchiveCmd.Flags().String("output", "table", "Формат вывода: table или json")

	issuePropertyCmd.AddCommand(issuePropertyListCmd)
	issuePropertyCmd.AddCommand(issuePropertySetCmd)
	issuePropertyCmd.AddCommand(issuePropertyUnsetCmd)

	issuePropertyListCmd.Flags().String("output", "table", "Формат вывода: table или json")
	issuePropertySetCmd.Flags().String("output", "table", "Формат вывода: table или json")
	issuePropertySetCmd.Flags().String("name", "", "Название свойства или UUID (обязательно)")
	issuePropertySetCmd.Flags().String("value", "", "Значение свойства (обязательно; формы по типам см. в --help)")
	issuePropertyUnsetCmd.Flags().String("output", "table", "Формат вывода: table или json")
	issuePropertyUnsetCmd.Flags().String("name", "", "Название свойства или UUID (обязательно)")

	issueCmd.AddCommand(issuePropertyCmd)
}

func fetchProperties(ctx context.Context, client *cli.APIClient) ([]propertyDTO, error) {
	var result struct {
		Properties []propertyDTO `json:"properties"`
	}
	if err := client.GetJSON(ctx, "/api/properties?include_archived=true", &result); err != nil {
		return nil, fmt.Errorf("list properties: %w", err)
	}
	return result.Properties, nil
}

func resolvePropertyRef(properties []propertyDTO, ref string) (propertyDTO, error) {
	for _, p := range properties {
		if p.ID == ref {
			return p, nil
		}
	}
	lower := strings.ToLower(strings.TrimSpace(ref))
	for _, p := range properties {
		if strings.ToLower(p.Name) == lower {
			return p, nil
		}
	}
	names := make([]string, len(properties))
	for i, p := range properties {
		names[i] = p.Name
	}
	return propertyDTO{}, fmt.Errorf("property %q not found; available: %s", ref, strings.Join(names, ", "))
}

const defaultOptionColor = "#6b7280"

func parseOptionFlags(flags []string, existing []propertyOptionDTO) []map[string]string {
	byName := make(map[string]string, len(existing))
	for _, opt := range existing {
		byName[strings.ToLower(opt.Name)] = opt.ID
	}
	out := make([]map[string]string, 0, len(flags))
	for _, raw := range flags {
		name := raw
		color := defaultOptionColor
		if idx := strings.LastIndex(raw, ":#"); idx > 0 {
			name = raw[:idx]
			color = raw[idx+1:]
		}
		name = strings.TrimSpace(name)
		opt := map[string]string{"name": name, "color": color}
		if id, ok := byName[strings.ToLower(name)]; ok {
			opt["id"] = id
		}
		out = append(out, opt)
	}
	return out
}

func printPropertyTable(properties []propertyDTO) {
	headers := []string{"ID", "ICON", "NAME", "TYPE", "OPTIONS", "USED", "ARCHIVED"}
	rows := make([][]string, 0, len(properties))
	for _, p := range properties {
		names := make([]string, len(p.Config.Options))
		for i, opt := range p.Config.Options {
			names[i] = opt.Name
		}
		archived := ""
		if p.Archived {
			archived = "yes"
		}
		rows = append(rows, []string{p.ID, p.Icon, p.Name, p.Type, strings.Join(names, ", "), strconv.FormatInt(p.UsageCount, 10), archived})
	}
	cli.PrintTable(os.Stdout, headers, rows)
}

func runPropertyList(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	includeArchived, _ := cmd.Flags().GetBool("include-archived")
	path := "/api/properties"
	if includeArchived {
		path += "?include_archived=true"
	}
	var result struct {
		Properties []propertyDTO `json:"properties"`
	}
	if err := client.GetJSON(ctx, path, &result); err != nil {
		return fmt.Errorf("list properties: %w", err)
	}
	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result.Properties)
	}
	printPropertyTable(result.Properties)
	return nil
}

func runPropertyGet(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	properties, err := fetchProperties(ctx, client)
	if err != nil {
		return err
	}
	property, err := resolvePropertyRef(properties, args[0])
	if err != nil {
		return err
	}
	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, property)
	}
	printPropertyTable([]propertyDTO{property})
	return nil
}

func runPropertyCreate(cmd *cobra.Command, _ []string) error {
	name, _ := cmd.Flags().GetString("name")
	propType, _ := cmd.Flags().GetString("type")
	if name == "" {
		return fmt.Errorf("--name is required")
	}
	if propType == "" {
		return fmt.Errorf("--type is required")
	}
	description, _ := cmd.Flags().GetString("description")
	icon, _ := cmd.Flags().GetString("icon")
	optionFlags, _ := cmd.Flags().GetStringArray("option")

	body := map[string]any{"name": name, "type": propType, "description": description, "icon": icon}
	if len(optionFlags) > 0 {
		body["config"] = map[string]any{"options": parseOptionFlags(optionFlags, nil)}
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var created propertyDTO
	if err := client.PostJSON(ctx, "/api/properties", body, &created); err != nil {
		return fmt.Errorf("create property: %w", err)
	}
	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, created)
	}
	fmt.Fprintf(os.Stdout, "Property %q created.\n", created.Name)
	printPropertyTable([]propertyDTO{created})
	return nil
}

func runPropertyUpdate(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	properties, err := fetchProperties(ctx, client)
	if err != nil {
		return err
	}
	property, err := resolvePropertyRef(properties, args[0])
	if err != nil {
		return err
	}

	body := map[string]any{}
	if cmd.Flags().Changed("name") {
		name, _ := cmd.Flags().GetString("name")
		body["name"] = name
	}
	if cmd.Flags().Changed("description") {
		description, _ := cmd.Flags().GetString("description")
		body["description"] = description
	}
	if cmd.Flags().Changed("icon") {
		icon, _ := cmd.Flags().GetString("icon")
		body["icon"] = icon
	}
	if cmd.Flags().Changed("option") {
		optionFlags, _ := cmd.Flags().GetStringArray("option")
		body["config"] = map[string]any{"options": parseOptionFlags(optionFlags, property.Config.Options)}
	}
	if len(body) == 0 {
		return fmt.Errorf("nothing to update; pass --name, --description, --icon, or --option")
	}

	var updated propertyDTO
	if err := client.PatchJSON(ctx, "/api/properties/"+property.ID, body, &updated); err != nil {
		return fmt.Errorf("update property: %w", err)
	}
	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, updated)
	}
	fmt.Fprintf(os.Stdout, "Property %q updated.\n", updated.Name)
	printPropertyTable([]propertyDTO{updated})
	return nil
}

func makePropertyArchiveRun(archive bool) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		client, err := newAPIClient(cmd)
		if err != nil {
			return err
		}
		ctx, cancel := cli.APIContext(context.Background())
		defer cancel()

		properties, err := fetchProperties(ctx, client)
		if err != nil {
			return err
		}
		property, err := resolvePropertyRef(properties, args[0])
		if err != nil {
			return err
		}
		var updated propertyDTO
		if err := client.PatchJSON(ctx, "/api/properties/"+property.ID, map[string]any{"archived": archive}, &updated); err != nil {
			if archive {
				return fmt.Errorf("archive property: %w", err)
			}
			return fmt.Errorf("unarchive property: %w", err)
		}
		output, _ := cmd.Flags().GetString("output")
		if output == "json" {
			return cli.PrintJSON(os.Stdout, updated)
		}
		if archive {
			fmt.Fprintf(os.Stdout, "Property %q archived.\n", updated.Name)
		} else {
			fmt.Fprintf(os.Stdout, "Property %q restored.\n", updated.Name)
		}
		return nil
	}
}

func encodeIssuePropertyValue(property propertyDTO, raw string) (json.RawMessage, error) {
	optionNames := make([]string, len(property.Config.Options))
	for i, opt := range property.Config.Options {
		optionNames[i] = opt.Name
	}
	resolveOption := func(ref string) (string, error) {
		ref = strings.TrimSpace(ref)
		for _, opt := range property.Config.Options {
			if opt.ID == ref || strings.EqualFold(opt.Name, ref) {
				return opt.ID, nil
			}
		}
		return "", fmt.Errorf("option %q not found on property %q; valid options: %s", ref, property.Name, strings.Join(optionNames, ", "))
	}

	switch property.Type {
	case "select":
		id, err := resolveOption(raw)
		if err != nil {
			return nil, err
		}
		return json.Marshal(id)
	case "multi_select":
		parts := strings.Split(raw, ",")
		ids := make([]string, 0, len(parts))
		for _, part := range parts {
			if strings.TrimSpace(part) == "" {
				continue
			}
			id, err := resolveOption(part)
			if err != nil {
				return nil, err
			}
			ids = append(ids, id)
		}
		if len(ids) == 0 {
			return nil, fmt.Errorf("--value must list at least one option; valid options: %s", strings.Join(optionNames, ", "))
		}
		return json.Marshal(ids)
	case "number":
		if _, err := strconv.ParseFloat(raw, 64); err != nil {
			return nil, fmt.Errorf("value %q is not a valid number", raw)
		}
		return json.RawMessage(raw), nil
	case "checkbox":
		if raw != "true" && raw != "false" {
			return nil, fmt.Errorf("value %q is not a valid bool (expected true or false)", raw)
		}
		return json.RawMessage(raw), nil
	default:
		return json.Marshal(raw)
	}
}

func formatIssuePropertyValue(property propertyDTO, value any) string {
	optionName := func(id string) string {
		for _, opt := range property.Config.Options {
			if opt.ID == id {
				return opt.Name
			}
		}
		return id
	}
	switch property.Type {
	case "select":
		if s, ok := value.(string); ok {
			return optionName(s)
		}
	case "multi_select":
		if items, ok := value.([]any); ok {
			names := make([]string, 0, len(items))
			for _, item := range items {
				if s, ok := item.(string); ok {
					names = append(names, optionName(s))
				}
			}
			return strings.Join(names, ", ")
		}
	case "checkbox":
		if b, ok := value.(bool); ok {
			if b {
				return "✓"
			}
			return "✗"
		}
	}
	return formatMetadataValue(value)
}

type issuePropertyValueRow struct {
	PropertyID string `json:"property_id"`
	Name       string `json:"name"`
	Type       string `json:"type"`
	Value      any    `json:"value"`
	Display    string `json:"display"`
	Archived   bool   `json:"archived,omitempty"`
}

func buildIssuePropertyRows(properties []propertyDTO, bag map[string]any) []issuePropertyValueRow {
	rows := make([]issuePropertyValueRow, 0, len(bag))
	for _, p := range properties {
		value, present := bag[p.ID]
		if !present {
			continue
		}
		rows = append(rows, issuePropertyValueRow{
			PropertyID: p.ID,
			Name:       p.Name,
			Type:       p.Type,
			Value:      value,
			Display:    formatIssuePropertyValue(p, value),
			Archived:   p.Archived,
		})
	}
	return rows
}

func fetchIssuePropertyBag(ctx context.Context, client *cli.APIClient, issueID string) (map[string]any, error) {
	var issue struct {
		Properties map[string]any `json:"properties"`
	}
	if err := client.GetJSON(ctx, "/api/issues/"+issueID, &issue); err != nil {
		return nil, fmt.Errorf("get issue: %w", err)
	}
	if issue.Properties == nil {
		return map[string]any{}, nil
	}
	return issue.Properties, nil
}

func printIssuePropertyRows(rows []issuePropertyValueRow) {
	headers := []string{"NAME", "VALUE", "TYPE"}
	tableRows := make([][]string, len(rows))
	for i, row := range rows {
		tableRows[i] = []string{row.Name, row.Display, row.Type}
	}
	cli.PrintTable(os.Stdout, headers, tableRows)
}

func runIssuePropertyList(cmd *cobra.Command, args []string) error {
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
	properties, err := fetchProperties(ctx, client)
	if err != nil {
		return err
	}
	bag, err := fetchIssuePropertyBag(ctx, client, issueRef.ID)
	if err != nil {
		return err
	}
	rows := buildIssuePropertyRows(properties, bag)
	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, rows)
	}
	printIssuePropertyRows(rows)
	return nil
}

func runIssuePropertySet(cmd *cobra.Command, args []string) error {
	name, _ := cmd.Flags().GetString("name")
	if name == "" {
		return fmt.Errorf("--name is required")
	}
	if !cmd.Flags().Changed("value") {
		return fmt.Errorf("--value is required")
	}
	rawValue, _ := cmd.Flags().GetString("value")

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
	properties, err := fetchProperties(ctx, client)
	if err != nil {
		return err
	}
	property, err := resolvePropertyRef(properties, name)
	if err != nil {
		return err
	}
	value, err := encodeIssuePropertyValue(property, rawValue)
	if err != nil {
		return err
	}

	var result struct {
		Properties map[string]any `json:"properties"`
	}
	path := "/api/issues/" + issueRef.ID + "/properties/" + property.ID
	if err := client.PutJSON(ctx, path, map[string]any{"value": value}, &result); err != nil {
		return fmt.Errorf("set property: %w", err)
	}
	rows := buildIssuePropertyRows(properties, result.Properties)
	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, rows)
	}
	printIssuePropertyRows(rows)
	return nil
}

func runIssuePropertyUnset(cmd *cobra.Command, args []string) error {
	name, _ := cmd.Flags().GetString("name")
	if name == "" {
		return fmt.Errorf("--name is required")
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
	properties, err := fetchProperties(ctx, client)
	if err != nil {
		return err
	}
	property, err := resolvePropertyRef(properties, name)
	if err != nil {
		return err
	}

	path := "/api/issues/" + issueRef.ID + "/properties/" + property.ID
	if err := client.DeleteJSON(ctx, path); err != nil {
		return fmt.Errorf("unset property: %w", err)
	}
	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, map[string]any{"deleted": true})
	}
	fmt.Fprintf(os.Stdout, "Property %q unset.\n", property.Name)
	return nil
}
