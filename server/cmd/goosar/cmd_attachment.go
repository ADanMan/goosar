package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/adanman/goosar/server/internal/cli"
)

var attachmentCmd = &cobra.Command{
	Use:   "attachment",
	Short: "Работа с вложениями",
}

var attachmentDownloadCmd = &cobra.Command{
	Use:   "download <attachment-id>",
	Short: "Скачать вложение в локальный файл",
	Long:  "Скачивает вложение по его ID в локальный файл.",
	Example: `  # Скачать вложение-изображение в текущий каталог
  $ goosar attachment download abc123

  # Скачать в указанный каталог
  $ goosar attachment download abc123 -o /tmp/images`,
	Args: exactArgs(1),
	RunE: runAttachmentDownload,
}

var attachmentUploadCmd = &cobra.Command{
	Use:   "upload <path>",
	Short: "Загрузить файл во вложение к ответу в чате",
	Long: `Загружает локальный файл и прикрепляет его к ответу текущей task чата.

Команда предназначена для агентов, работающих внутри task чата: файл помечается
этой task, и когда она завершается, сервер привязывает его к ответу ассистента —
он появляется карточкой вложения под вашим ответом, даже если вы ничего не
вставляли. Команда также возвращает markdown-фрагмент, который можно вставить
отдельной строкой, чтобы задать место элемента: файлы используют !file[name](url)
(карточка), изображения — ![name](url) (в тексте).

ID task берётся из GOOSAR_TASK_ID (его задаёт демон внутри task);
при необходимости переопределите его флагом --task.`,
	Example: `  # Прикрепить изображение к текущему ответу в чате
  $ goosar attachment upload ./chart.png`,
	Args: exactArgs(1),
	RunE: runAttachmentUpload,
}

func init() {
	attachmentCmd.AddCommand(attachmentDownloadCmd)
	attachmentCmd.AddCommand(attachmentUploadCmd)

	attachmentDownloadCmd.Flags().StringP("output-dir", "o", ".", "Каталог для сохранения скачанного файла")
	attachmentUploadCmd.Flags().String("task", "", "ID task чата, к которой прикрепить файл (по умолчанию GOOSAR_TASK_ID)")
}

func runAttachmentUpload(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	taskID, _ := cmd.Flags().GetString("task")
	if taskID == "" {
		taskID = client.TaskID
	}
	if taskID == "" {
		return fmt.Errorf("no chat task in context: run inside a chat task (GOOSAR_TASK_ID set) or pass --task <id>")
	}

	path := args[0]
	if isHTTPURL(path) {
		return fmt.Errorf("upload accepts a local file path, not a URL: %s", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read file %s: %w", path, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), cli.AtLeastAPITimeout(60*time.Second))
	defer cancel()

	att, err := client.UploadChatAttachment(ctx, data, path, taskID)
	if err != nil {
		return fmt.Errorf("upload attachment: %w", err)
	}

	filename := filepath.Base(path)

	label := escapeMarkdownLabel(filename)
	markdown := fmt.Sprintf("!file[%s](%s)", label, att.MarkdownURL)
	if strings.HasPrefix(att.ContentType, "image/") {
		markdown = fmt.Sprintf("![%s](%s)", label, att.MarkdownURL)
	}
	fmt.Fprintln(os.Stderr, "Uploaded:", filename)

	return cli.PrintJSON(os.Stdout, map[string]any{
		"id":           att.ID,
		"filename":     filename,
		"markdown_url": att.MarkdownURL,
		"markdown":     markdown,
	})
}

func escapeMarkdownLabel(s string) string {
	return strings.NewReplacer(
		`\`, `\\`,
		`[`, `\[`,
		`]`, `\]`,
		`(`, `\(`,
		`)`, `\)`,
	).Replace(s)
}

func runAttachmentDownload(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), cli.AtLeastAPITimeout(60*time.Second))
	defer cancel()

	var att map[string]any
	if err := client.GetJSON(ctx, "/api/attachments/"+args[0], &att); err != nil {
		return fmt.Errorf("get attachment: %w", err)
	}

	downloadURL := strVal(att, "download_url")
	if downloadURL == "" {
		return fmt.Errorf("attachment has no download URL")
	}

	filename := filepath.Base(strVal(att, "filename"))
	if filename == "" || filename == "." {
		filename = args[0]
	}

	data, err := client.DownloadFile(ctx, downloadURL)
	if err != nil {
		return fmt.Errorf("download file: %w", err)
	}

	outputDir, _ := cmd.Flags().GetString("output-dir")
	destPath := filepath.Join(outputDir, filename)

	if err := os.WriteFile(destPath, data, 0o644); err != nil {
		return fmt.Errorf("write file: %w", err)
	}

	abs, err := filepath.Abs(destPath)
	if err != nil {
		abs = destPath
	}
	fmt.Fprintln(os.Stderr, "Downloaded:", abs)

	return cli.PrintJSON(os.Stdout, map[string]any{
		"id":       strVal(att, "id"),
		"filename": filename,
		"path":     abs,
		"size":     strVal(att, "size_bytes"),
	})
}
