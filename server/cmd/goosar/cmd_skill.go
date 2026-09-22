package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/adanman/goosar/server/internal/cli"
)

var skillCmd = &cobra.Command{
	Use:   "skill",
	Short: "Работа со skills",
}

var skillListCmd = &cobra.Command{
	Use:   "list",
	Short: "Показать skills рабочего пространства",
	RunE:  runSkillList,
}

var skillGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Показать сведения о skill (вместе с файлами)",
	Args:  exactArgs(1),
	RunE:  runSkillGet,
}

var skillCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Создать skill",
	RunE:  runSkillCreate,
}

var skillUpdateCmd = &cobra.Command{
	Use:   "update <id>",
	Short: "Изменить skill",
	Args:  exactArgs(1),
	RunE:  runSkillUpdate,
}

var skillDeleteCmd = &cobra.Command{
	Use:   "delete <id>",
	Short: "Удалить skill",
	Args:  exactArgs(1),
	RunE:  runSkillDelete,
}

var skillImportCmd = &cobra.Command{
	Use:   "import",
	Short: "Импортировать skill по URL (clawhub.ai, skills.sh, github.com) или из локального архива .skill/.zip",
	RunE:  runSkillImport,
}

var skillSearchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "Найти skills, доступные для установки",
	Args:  exactArgs(1),
	RunE:  runSkillSearch,
}

var skillFilesCmd = &cobra.Command{
	Use:   "files",
	Short: "Работа с файлами skill",
}

var skillFilesListCmd = &cobra.Command{
	Use:   "list <skill-id>",
	Short: "Показать файлы skill",
	Args:  exactArgs(1),
	RunE:  runSkillFilesList,
}

var skillFilesUpsertCmd = &cobra.Command{
	Use:   "upsert <skill-id>",
	Short: "Создать или обновить файл skill",
	Args:  exactArgs(1),
	RunE:  runSkillFilesUpsert,
}

var skillFilesDeleteCmd = &cobra.Command{
	Use:   "delete <skill-id> <file-id>",
	Short: "Удалить файл skill",
	Args:  exactArgs(2),
	RunE:  runSkillFilesDelete,
}

func init() {
	skillCmd.AddCommand(skillListCmd)
	skillCmd.AddCommand(skillGetCmd)
	skillCmd.AddCommand(skillCreateCmd)
	skillCmd.AddCommand(skillUpdateCmd)
	skillCmd.AddCommand(skillDeleteCmd)
	skillCmd.AddCommand(skillImportCmd)
	skillCmd.AddCommand(skillSearchCmd)
	skillCmd.AddCommand(skillFilesCmd)

	skillFilesCmd.AddCommand(skillFilesListCmd)
	skillFilesCmd.AddCommand(skillFilesUpsertCmd)
	skillFilesCmd.AddCommand(skillFilesDeleteCmd)

	skillListCmd.Flags().String("output", "table", "Формат вывода: table или json")

	skillGetCmd.Flags().String("output", "json", "Формат вывода: table или json")

	skillCreateCmd.Flags().String("name", "", "Название skill (обязательно)")
	skillCreateCmd.Flags().String("description", "", "Описание skill")
	skillCreateCmd.Flags().String("content", "", "Содержимое skill (тело SKILL.md)")
	skillCreateCmd.Flags().Bool("content-stdin", false, "Прочитать содержимое skill из stdin. Несовместимо с --content и --content-file.")
	skillCreateCmd.Flags().String("content-file", "", "Прочитать содержимое skill из файла в UTF-8. Несовместимо с --content и --content-stdin.")
	skillCreateCmd.Flags().String("config", "", "Конфигурация skill в виде строки JSON")
	skillCreateCmd.Flags().String("output", "json", "Формат вывода: table или json")

	skillUpdateCmd.Flags().String("name", "", "Новое название")
	skillUpdateCmd.Flags().String("description", "", "Новое описание")
	skillUpdateCmd.Flags().String("content", "", "Новое содержимое")
	skillUpdateCmd.Flags().Bool("content-stdin", false, "Прочитать новое содержимое из stdin. Несовместимо с --content и --content-file.")
	skillUpdateCmd.Flags().String("content-file", "", "Прочитать новое содержимое из файла в UTF-8. Несовместимо с --content и --content-stdin.")
	skillUpdateCmd.Flags().String("config", "", "Новая конфигурация в виде строки JSON")
	skillUpdateCmd.Flags().String("output", "json", "Формат вывода: table или json")

	skillDeleteCmd.Flags().Bool("yes", false, "Пропустить запрос подтверждения")

	skillImportCmd.Flags().String("url", "", "URL, откуда импортировать (clawhub.ai, skills.sh или github.com). Несовместимо с --file.")
	skillImportCmd.Flags().String("file", "", "Путь к локальному архиву skill (.skill или .zip) для импорта. Несовместимо с --url.")
	skillImportCmd.Flags().String("on-conflict", "fail", "Что делать, если skill с таким названием уже есть: fail, overwrite, rename или skip")
	skillImportCmd.Flags().String("output", "json", "Формат вывода: table или json")

	skillSearchCmd.Flags().String("output", "json", "Формат вывода: table или json")

	skillFilesListCmd.Flags().String("output", "table", "Формат вывода: table или json")

	skillFilesUpsertCmd.Flags().String("path", "", "Путь к файлу внутри skill (обязательно)")
	skillFilesUpsertCmd.Flags().String("content", "", "Содержимое файла (обязательно)")
	skillFilesUpsertCmd.Flags().Bool("content-stdin", false, "Прочитать содержимое из stdin. Несовместимо с --content и --content-file.")
	skillFilesUpsertCmd.Flags().String("content-file", "", "Прочитать содержимое из локального файла в UTF-8. Несовместимо с --content и --content-stdin.")
	skillFilesUpsertCmd.Flags().String("output", "json", "Формат вывода: table или json")
}

func resolveSkillContentFlag(cmd *cobra.Command) (string, bool, error) {
	useStdin, _ := cmd.Flags().GetBool("content-stdin")
	inline, _ := cmd.Flags().GetString("content")
	filePath, _ := cmd.Flags().GetString("content-file")
	inlineSet := cmd.Flags().Changed("content")

	sources := 0
	if inlineSet {
		sources++
	}
	if useStdin {
		sources++
	}
	if filePath != "" {
		sources++
	}
	if sources > 1 {
		return "", false, fmt.Errorf("--content, --content-stdin, and --content-file are mutually exclusive")
	}

	if useStdin {
		noInput, statErr := stdinHasNoPipedInput()
		if statErr != nil {
			return "", false, fmt.Errorf("stat stdin for --content-stdin: %w", statErr)
		}
		if noInput {
			return "", false, emptyStdinError("content-stdin", "content-file", "content")
		}
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", false, fmt.Errorf("read stdin for --content-stdin: %w", err)
		}
		if len(data) == 0 {
			return "", false, emptyStdinError("content-stdin", "content-file", "content")
		}
		return skillContentBytesToString(data, "stdin content for --content-stdin")
	}
	if filePath != "" {
		data, err := os.ReadFile(filePath)
		if err != nil {
			return "", false, fmt.Errorf("read file for --content-file: %w", err)
		}
		return skillContentBytesToString(data, "file content for --content-file")
	}
	if inlineSet {
		return inline, true, nil
	}
	return "", false, nil
}

func skillContentBytesToString(data []byte, label string) (string, bool, error) {
	if len(data) == 0 {
		return "", false, fmt.Errorf("%s is empty", label)
	}
	if !utf8.Valid(data) {
		return "", false, fmt.Errorf("%s must be valid UTF-8", label)
	}
	return string(data), true, nil
}

func runSkillList(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var skills []map[string]any
	if err := client.GetJSON(ctx, "/api/skills", &skills); err != nil {
		return fmt.Errorf("list skills: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, skills)
	}

	headers := []string{"ID", "NAME", "DESCRIPTION", "CREATED_AT"}
	rows := make([][]string, 0, len(skills))
	for _, s := range skills {
		rows = append(rows, []string{
			strVal(s, "id"),
			strVal(s, "name"),
			strVal(s, "description"),
			strVal(s, "created_at"),
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runSkillGet(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var skill map[string]any
	if err := client.GetJSON(ctx, "/api/skills/"+args[0], &skill); err != nil {
		return fmt.Errorf("get skill: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, skill)
	}

	headers := []string{"ID", "NAME", "DESCRIPTION", "CREATED_AT"}
	rows := [][]string{{
		strVal(skill, "id"),
		strVal(skill, "name"),
		strVal(skill, "description"),
		strVal(skill, "created_at"),
	}}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runSkillCreate(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	name, _ := cmd.Flags().GetString("name")
	if name == "" {
		return fmt.Errorf("--name is required")
	}

	body := map[string]any{
		"name": name,
	}
	if v, _ := cmd.Flags().GetString("description"); v != "" {
		body["description"] = v
	}
	content, hasContent, err := resolveSkillContentFlag(cmd)
	if err != nil {
		return err
	}
	if hasContent && content != "" {
		body["content"] = content
	}
	if cmd.Flags().Changed("config") {
		v, _ := cmd.Flags().GetString("config")
		var конфиг any
		if err := json.Unmarshal([]byte(v), &конфиг); err != nil {
			return fmt.Errorf("--config must be valid JSON: %w", err)
		}
		body["config"] = конфиг
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var result map[string]any
	if err := client.PostJSON(ctx, "/api/skills", body, &result); err != nil {
		return fmt.Errorf("create skill: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}

	fmt.Printf("Skill created: %s (%s)\n", strVal(result, "name"), strVal(result, "id"))
	return nil
}

func runSkillUpdate(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	body := map[string]any{}
	if cmd.Flags().Changed("name") {
		v, _ := cmd.Flags().GetString("name")
		body["name"] = v
	}
	if cmd.Flags().Changed("description") {
		v, _ := cmd.Flags().GetString("description")
		body["description"] = v
	}
	content, hasContent, err := resolveSkillContentFlag(cmd)
	if err != nil {
		return err
	}
	if hasContent {
		body["content"] = content
	}
	if cmd.Flags().Changed("config") {
		v, _ := cmd.Flags().GetString("config")
		var конфиг any
		if err := json.Unmarshal([]byte(v), &конфиг); err != nil {
			return fmt.Errorf("--config must be valid JSON: %w", err)
		}
		body["config"] = конфиг
	}

	if len(body) == 0 {
		return fmt.Errorf("no fields to update; use --name, --description, --content, or --config")
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var result map[string]any
	if err := client.PutJSON(ctx, "/api/skills/"+args[0], body, &result); err != nil {
		return fmt.Errorf("update skill: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}

	fmt.Printf("Skill updated: %s (%s)\n", strVal(result, "name"), strVal(result, "id"))
	return nil
}

func runSkillDelete(cmd *cobra.Command, args []string) error {
	yes, _ := cmd.Flags().GetBool("yes")
	if !yes {
		fmt.Printf("Are you sure you want to delete skill %s? This cannot be undone. [y/N] ", args[0])
		reader := bufio.NewReader(os.Stdin)
		answer, _ := reader.ReadString('\n')
		answer = strings.TrimSpace(strings.ToLower(answer))
		if answer != "y" && answer != "yes" {
			fmt.Println("Aborted.")
			return nil
		}
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	if err := client.DeleteJSON(ctx, "/api/skills/"+args[0]); err != nil {
		return fmt.Errorf("delete skill: %w", err)
	}

	fmt.Printf("Skill deleted: %s\n", args[0])
	return nil
}

func runSkillImport(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	importURL, _ := cmd.Flags().GetString("url")
	importFile, _ := cmd.Flags().GetString("file")
	switch {
	case importURL == "" && importFile == "":
		return fmt.Errorf("either --url or --file is required")
	case importURL != "" && importFile != "":
		return fmt.Errorf("--url and --file are mutually exclusive")
	}
	onConflict, _ := cmd.Flags().GetString("on-conflict")
	if !validSkillImportConflictStrategy(onConflict) {
		return fmt.Errorf("--on-conflict must be one of: fail, overwrite, rename, skip")
	}

	ctx, cancel := context.WithTimeout(context.Background(), cli.AtLeastAPITimeout(60*time.Second))
	defer cancel()

	var result map[string]any
	if importFile != "" {
		fileData, readErr := os.ReadFile(importFile)
		if readErr != nil {
			return fmt.Errorf("read skill archive: %w", readErr)
		}
		if err := client.ImportSkillFile(ctx, fileData, filepath.Base(importFile), onConflict, &result); err != nil {
			if handledErr := handleSkillImportError(cmd, err); handledErr != nil {
				return handledErr
			}
			return fmt.Errorf("import skill: %w", err)
		}
		return printSkillImportResult(cmd, result)
	}

	body := map[string]any{
		"url":         importURL,
		"on_conflict": onConflict,
	}
	if err := client.PostJSON(ctx, "/api/skills/import", body, &result); err != nil {
		if handledErr := handleSkillImportError(cmd, err); handledErr != nil {
			return handledErr
		}
		return fmt.Errorf("import skill: %w", err)
	}

	return printSkillImportResult(cmd, result)
}

func validSkillImportConflictStrategy(strategy string) bool {
	switch strategy {
	case "fail", "overwrite", "rename", "skip":
		return true
	}
	return false
}

func handleSkillImportError(cmd *cobra.Command, err error) error {
	var httpErr *cli.HTTPError
	if !errors.As(err, &httpErr) || strings.TrimSpace(httpErr.Body) == "" {
		return nil
	}

	var body map[string]any
	if json.Unmarshal([]byte(httpErr.Body), &body) != nil {
		return nil
	}
	if _, ok := body["status"]; !ok {
		if _, hasExisting := body["existing_skill"]; !hasExisting {
			return nil
		}
		body = normalizeLegacySkillImportConflict(body)
	}

	if err := printSkillImportResult(cmd, body); err != nil {
		return err
	}
	reason := strVal(body, "reason")
	if reason == "" {
		reason = strVal(body, "error")
	}
	if reason == "" {
		reason = "skill import conflict"
	}
	return errors.New(reason)
}

func normalizeLegacySkillImportConflict(body map[string]any) map[string]any {
	reason := strVal(body, "error")
	if reason == "" {
		reason = "a skill with this name already exists"
	}
	reason += "; use --on-conflict overwrite to replace it or --on-conflict rename to import a copy"
	return map[string]any{
		"status":         "conflict",
		"reason":         reason,
		"existing_skill": body["existing_skill"],
	}
}

func printSkillImportResult(cmd *cobra.Command, result map[string]any) error {
	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}

	status := strVal(result, "status")
	if status == "" {
		fmt.Printf("Skill imported: %s (%s)\n", strVal(result, "name"), strVal(result, "id"))
		return nil
	}

	skill := nestedMap(result, "skill")
	existing := nestedMap(result, "existing_skill")
	reason := strVal(result, "reason")
	switch status {
	case "created":
		fmt.Printf("Skill imported: %s (%s)\n", strVal(skill, "name"), strVal(skill, "id"))
	case "updated":
		fmt.Printf("Skill updated: %s (%s)\n", strVal(skill, "name"), strVal(skill, "id"))
	case "skipped":
		fmt.Printf("Skill skipped: %s (%s)\n", strVal(existing, "name"), strVal(existing, "id"))
	case "conflict":
		fmt.Printf("Skill import conflict: %s (%s)\n", strVal(existing, "name"), strVal(existing, "id"))
	case "failed":
		fmt.Printf("Skill import failed: %s\n", reason)
	default:
		fmt.Printf("Skill import %s\n", status)
	}
	if reason != "" && status != "failed" {
		fmt.Printf("Reason: %s\n", reason)
	}
	return nil
}

func nestedMap(m map[string]any, key string) map[string]any {
	nested, _ := m[key].(map[string]any)
	if nested == nil {
		return map[string]any{}
	}
	return nested
}

func runSkillSearch(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	query := strings.TrimSpace(args[0])
	if query == "" {
		return fmt.Errorf("query is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), cli.AtLeastAPITimeout(60*time.Second))
	defer cancel()

	var results []map[string]any
	path := "/api/skills/search?q=" + url.QueryEscape(query)
	if err := client.GetJSON(ctx, path, &results); err != nil {
		return fmt.Errorf("search skills: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, results)
	}

	headers := []string{"NAME", "URL", "SOURCE", "INSTALLS", "DESCRIPTION"}
	rows := make([][]string, 0, len(results))
	for _, result := range results {
		rows = append(rows, []string{
			strVal(result, "name"),
			strVal(result, "url"),
			strVal(result, "source"),
			strVal(result, "install_count"),
			strVal(result, "description"),
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runSkillFilesList(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var files []map[string]any
	if err := client.GetJSON(ctx, "/api/skills/"+args[0]+"/files", &files); err != nil {
		return fmt.Errorf("list skill files: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, files)
	}

	headers := []string{"ID", "PATH", "CREATED_AT", "UPDATED_AT"}
	rows := make([][]string, 0, len(files))
	for _, f := range files {
		rows = append(rows, []string{
			strVal(f, "id"),
			strVal(f, "path"),
			strVal(f, "created_at"),
			strVal(f, "updated_at"),
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runSkillFilesUpsert(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	filePath, _ := cmd.Flags().GetString("path")
	if filePath == "" {
		return fmt.Errorf("--path is required")
	}
	content, hasContent, err := resolveSkillContentFlag(cmd)
	if err != nil {
		return err
	}
	if !hasContent || content == "" {
		return fmt.Errorf("--content is required")
	}

	body := map[string]any{
		"path":    filePath,
		"content": content,
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var result map[string]any
	if err := client.PutJSON(ctx, "/api/skills/"+args[0]+"/files", body, &result); err != nil {
		return fmt.Errorf("upsert skill file: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}

	fmt.Printf("Skill file upserted: %s (%s)\n", strVal(result, "path"), strVal(result, "id"))
	return nil
}

func runSkillFilesDelete(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	if err := client.DeleteJSON(ctx, "/api/skills/"+args[0]+"/files/"+args[1]); err != nil {
		return fmt.Errorf("delete skill file: %w", err)
	}

	fmt.Printf("Skill file deleted: %s\n", args[1])
	return nil
}
