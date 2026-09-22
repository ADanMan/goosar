package main

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/adanman/goosar/server/internal/cli"
)

var userCmd = &cobra.Command{
	Use:   "user",
	Short: "Работа с вашим аккаунтом",
}

var userProfileCmd = &cobra.Command{
	Use:   "profile",
	Short: "Показать или изменить ваш личный профиль",
	Long: "Управляет личным профилем, который видят агенты, когда берут задачу " +
		"от вашего имени. Описание попадает в бриф агента в раздел " +
		"`## Requesting User`, поэтому напишите в нём о своей роли, стеке и " +
		"предпочтениях в работе.",
}

var userProfileGetCmd = &cobra.Command{
	Use:   "get",
	Short: "Показать ваш текущий профиль",
	RunE:  runUserProfileGet,
}

var userProfileUpdateCmd = &cobra.Command{
	Use:   "update",
	Short: "Изменить ваш профиль (сейчас — описание профиля)",
	Long: "Задаёт описание личного профиля, которое попадает в брифы агентов " +
		"в раздел `## Requesting User`. Чтобы очистить описание, передайте пустое значение.\n\n" +
		"Выберите способ ввода, который сохранит ваш текст:\n" +
		"  --description \"...\"          прямо в команде (раскрывает \\n / \\t)\n" +
		"  --description-stdin           передать через HEREDOC (без изменений)\n" +
		"  --description-file <path>     прочитать файл в UTF-8 (надёжно в Windows)\n",
	RunE: runUserProfileUpdate,
}

func init() {
	userCmd.AddCommand(userProfileCmd)
	userProfileCmd.AddCommand(userProfileGetCmd)
	userProfileCmd.AddCommand(userProfileUpdateCmd)

	userProfileGetCmd.Flags().String("output", "table", "Формат вывода: table или json")

	userProfileUpdateCmd.Flags().String("description", "", "Новое описание профиля (раскрывает \\n, \\r, \\t, \\\\; чтобы сохранить обратные слэши как есть, передайте текст через --description-stdin)")
	userProfileUpdateCmd.Flags().Bool("description-stdin", false, "Прочитать описание из stdin (многострочный текст сохраняется без изменений)")
	userProfileUpdateCmd.Flags().String("description-file", "", "Прочитать описание из файла в UTF-8 (многострочный текст сохраняется без изменений; в Windows используйте этот флаг, если передача через stdin портит не-ASCII символы). Путь должен быть внутри текущего каталога, если не задан --allow-external-file.")
	userProfileUpdateCmd.Flags().Bool("allow-external-file", false, "Разрешить --description-file читать путь вне текущего каталога. По умолчанию выключено, чтобы случайно не подхватить устаревший временный файл от другого запуска или окружения (MUL-4252).")
	userProfileUpdateCmd.Flags().Bool("clear", false, "Очистить описание профиля (то же, что --description \"\")")
	userProfileUpdateCmd.Flags().String("output", "table", "Формат вывода: table или json")
}

func runUserProfileGet(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var me map[string]any
	if err := client.GetJSON(ctx, "/api/me", &me); err != nil {
		return fmt.Errorf("get user profile: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, me)
	}

	printUserProfileTable(os.Stdout, me)
	return nil
}

func runUserProfileUpdate(cmd *cobra.Command, _ []string) error {

	clearFlag, _ := cmd.Flags().GetBool("clear")
	desc, hasDesc, err := resolveTextFlag(cmd, "description")
	if err != nil {
		return err
	}

	if clearFlag && hasDesc {
		return fmt.Errorf("--clear cannot be combined with --description / --description-stdin / --description-file")
	}
	if !clearFlag && !hasDesc && !cmd.Flags().Changed("description") {
		return fmt.Errorf("nothing to update; pass --description, --description-stdin, --description-file, or --clear")
	}

	if clearFlag {
		desc = ""
	}

	body := map[string]any{"profile_description": desc}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var me map[string]any
	if err := client.PatchJSON(ctx, "/api/me", body, &me); err != nil {
		return fmt.Errorf("update user profile: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, me)
	}

	printUserProfileTable(os.Stdout, me)
	return nil
}

func printUserProfileTable(out *os.File, me map[string]any) {
	w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	defer w.Flush()

	fmt.Fprintf(w, "ID\t%s\n", strVal(me, "id"))
	fmt.Fprintf(w, "NAME\t%s\n", strVal(me, "name"))
	fmt.Fprintf(w, "EMAIL\t%s\n", strVal(me, "email"))
	desc := strVal(me, "profile_description")
	if desc == "" {
		desc = "(not set)"
	}
	fmt.Fprintf(w, "PROFILE DESCRIPTION\t%s\n", desc)
}
