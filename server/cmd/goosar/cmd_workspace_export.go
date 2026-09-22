package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/adanman/goosar/server/internal/cli"
)

var workspaceExportCmd = &cobra.Command{
	Use:   "export [workspace-id|slug|prefix]",
	Short: "Выгрузить данные рабочего пространства в архив tar.gz (только владелец)",
	Long: "Запускает выгрузку рабочего пространства и по умолчанию дожидается её завершения, " +
		"сохраняя архив tar.gz в каталог --output-dir.\n\n" +
		"В архив попадают issue, комментарии, свойства, метки, участники, агенты, " +
		"автопилоты, чат-сессии и сообщения, определения MCP-серверов (БЕЗ учётных " +
		"данных), записи журнала аудита рабочего пространства, файлы вложений и манифест с " +
		"версией схемы и количеством записей.\n\n" +
		"Выгрузку может запускать только владелец рабочего пространства и только под учётной " +
		"записью человека. Одновременно выполняется не более одной выгрузки на " +
		"рабочее пространство. Готовый архив сервер хранит ограниченное время " +
		"(GOOSAR_EXPORT_RETENTION, по умолчанию 7 дней), после чего удаляет.\n\n" +
		"С `--wait=false` команда только ставит выгрузку в очередь и печатает её " +
		"состояние — это режим для больших рабочих пространств.",
	Example: `  # Выгрузить текущее рабочее пространство в ~/exports
  $ goosar workspace export -o ~/exports

  # Поставить выгрузку в очередь и не ждать
  $ goosar workspace export 3f2a... --wait=false`,
	Args: cobra.MaximumNArgs(1),
	RunE: runWorkspaceExport,
}

func init() {
	workspaceCmd.AddCommand(workspaceExportCmd)
	workspaceExportCmd.Flags().StringP("output-dir", "o", ".", "Каталог для сохранения архива")
	workspaceExportCmd.Flags().Bool("wait", true, "Дождаться завершения выгрузки и скачать архив")
	workspaceExportCmd.Flags().String("output", "json", "Формат вывода: json")
}

type exportJob struct {
	ID          string          `json:"id"`
	WorkspaceID string          `json:"workspace_id"`
	Status      string          `json:"status"`
	Error       *string         `json:"error"`
	SizeBytes   int64           `json:"size_bytes"`
	CreatedAt   string          `json:"created_at"`
	CompletedAt *string         `json:"completed_at"`
	Manifest    json.RawMessage `json:"manifest"`
	DownloadURL *string         `json:"download_url"`
}

func exportPollInterval() time.Duration {
	if raw := strings.TrimSpace(os.Getenv("GOOSAR_EXPORT_POLL_INTERVAL")); raw != "" {
		if d, err := time.ParseDuration(raw); err == nil && d > 0 {
			return d
		}
	}
	return 3 * time.Second
}

func runWorkspaceExport(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	wsID := ""
	if len(args) > 0 {
		wsID = args[0]
	}
	if wsID == "" {
		wsID = resolveWorkspaceID(cmd)
	}
	if wsID == "" {
		return fmt.Errorf("не указан воркспейс: передайте его аргументом, флагом --workspace-id или выберите через 'goosar workspace switch'")
	}

	base := "/api/workspaces/" + wsID + "/export"

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var job exportJob
	if err := client.PostJSON(ctx, base, map[string]any{}, &job); err != nil {
		return fmt.Errorf("запуск выгрузки: %w", err)
	}
	cancel()

	wait, _ := cmd.Flags().GetBool("wait")
	if !wait {
		return cli.PrintJSON(os.Stdout, job)
	}

	job, err = waitForExport(cmd.Context(), client, base, job)
	if err != nil {
		return err
	}
	if job.DownloadURL == nil || *job.DownloadURL == "" {
		return fmt.Errorf("выгрузка завершена, но сервер не вернул ссылку на скачивание")
	}

	outputDir, _ := cmd.Flags().GetString("output-dir")
	path, size, err := downloadExportArchive(client, outputDir, job)
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "Архив сохранён: %s (%d байт)\n", path, size)
	return cli.PrintJSON(os.Stdout, map[string]any{
		"id":           job.ID,
		"workspace_id": job.WorkspaceID,
		"status":       job.Status,
		"size_bytes":   size,
		"manifest":     job.Manifest,
		"path":         path,
	})
}

func waitForExport(parent context.Context, client *cli.APIClient, base string, job exportJob) (exportJob, error) {
	if parent == nil {
		parent = context.Background()
	}
	interval := exportPollInterval()
	for {
		switch job.Status {
		case "completed":
			return job, nil
		case "failed":
			reason := "причина не указана"
			if job.Error != nil && *job.Error != "" {
				reason = *job.Error
			}
			return job, fmt.Errorf("выгрузка не удалась: %s", reason)
		}

		select {
		case <-parent.Done():
			return job, parent.Err()
		case <-time.After(interval):
		}

		ctx, cancel := cli.APIContext(parent)
		err := client.GetJSON(ctx, base+"/"+job.ID, &job)
		cancel()
		if err != nil {
			return job, fmt.Errorf("опрос состояния выгрузки: %w", err)
		}
	}
}

func downloadExportArchive(client *cli.APIClient, outputDir string, job exportJob) (string, int64, error) {

	if err := os.MkdirAll(outputDir, 0o700); err != nil {
		return "", 0, fmt.Errorf("создание каталога %s: %w", outputDir, err)
	}
	path := filepath.Join(outputDir, fmt.Sprintf("goosar-workspace-%s-%s.tar.gz", shortID(job.WorkspaceID), shortID(job.ID)))
	tmp := path + ".part"

	file, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return "", 0, fmt.Errorf("создание файла %s: %w", tmp, err)
	}

	size, err := client.DownloadTo(context.Background(), *job.DownloadURL, file)
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(tmp)
		return "", 0, fmt.Errorf("скачивание архива: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return "", 0, fmt.Errorf("сохранение архива: %w", err)
	}
	return path, size, nil
}

func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}
