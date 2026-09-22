package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/adanman/goosar/server/internal/cli"
	"github.com/adanman/goosar/server/internal/util"
)

func resolveTextFlag(cmd *cobra.Command, flagName string) (string, bool, error) {
	stdinFlag := flagName + "-stdin"
	fileFlag := flagName + "-file"
	useStdin, _ := cmd.Flags().GetBool(stdinFlag)
	inline, _ := cmd.Flags().GetString(flagName)
	filePath, _ := cmd.Flags().GetString(fileFlag)

	sources := 0
	if useStdin {
		sources++
	}
	if inline != "" {
		sources++
	}
	if filePath != "" {
		sources++
	}
	if sources > 1 {
		return "", false, fmt.Errorf("--%s, --%s, and --%s are mutually exclusive", flagName, stdinFlag, fileFlag)
	}

	if useStdin {

		noInput, statErr := stdinHasNoPipedInput()
		if statErr != nil {
			return "", false, fmt.Errorf("stat stdin for --%s: %w", stdinFlag, statErr)
		}
		if noInput {
			return "", false, emptyStdinError(stdinFlag, fileFlag, flagName)
		}
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", false, fmt.Errorf("read stdin for --%s: %w", stdinFlag, err)
		}
		body := strings.TrimSuffix(string(data), "\n")
		if body == "" {
			return "", false, emptyStdinError(stdinFlag, fileFlag, flagName)
		}
		return body, true, nil
	}
	if filePath != "" {
		if err := ensureFileFlagWithinWorkdir(cmd, fileFlag, flagName, filePath); err != nil {
			return "", false, err
		}
		data, err := os.ReadFile(filePath)
		if err != nil {
			return "", false, fmt.Errorf("read file for --%s: %w", fileFlag, err)
		}
		body := strings.TrimSuffix(string(data), "\n")
		if body == "" {
			return "", false, fmt.Errorf("file content for --%s is empty", fileFlag)
		}
		return body, true, nil
	}
	if inline == "" {
		return "", false, nil
	}
	return util.UnescapeBackslashEscapes(inline), true, nil
}

var stdinHasNoPipedInput = func() (bool, error) {
	info, err := os.Stdin.Stat()
	if err != nil {
		return false, err
	}
	return info.Mode()&os.ModeCharDevice != 0, nil
}

func emptyStdinError(stdinFlag, fileFlag, flagName string) error {
	return fmt.Errorf(
		"stdin content for --%s is empty — nothing was piped in. "+
			"Write the body to a UTF-8 file in the working directory and pass --%s ./%s.md, "+
			"or pipe the text into this same command; "+
			"for a short single-line body use --%s \"...\"",
		stdinFlag, fileFlag, flagName, flagName)
}

func ensureFileFlagWithinWorkdir(cmd *cobra.Command, fileFlag, flagName, filePath string) error {
	if allow, _ := cmd.Flags().GetBool("allow-external-file"); allow {
		return nil
	}
	within, err := fileWithinWorkingDir(filePath)
	if err != nil {
		return fmt.Errorf("resolve --%s path %q: %w", fileFlag, filePath, err)
	}
	if !within {
		return fmt.Errorf(
			"--%s path %q resolves outside the current working directory; "+
				"write agent temp files inside the task workdir (e.g. ./%s.md) rather than machine-shared "+
				"paths like /tmp, where another run's stale file can be read by mistake. "+
				"Pass --allow-external-file to override.",
			fileFlag, filePath, flagName)
	}
	return nil
}

func fileWithinWorkingDir(filePath string) (bool, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return false, err
	}
	base := cwd
	if resolved, err := filepath.EvalSymlinks(cwd); err == nil {
		base = resolved
	}
	abs := filePath
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(cwd, abs)
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	} else {

		if resolvedParent, perr := filepath.EvalSymlinks(filepath.Dir(abs)); perr == nil {
			abs = filepath.Join(resolvedParent, filepath.Base(abs))
		} else {
			abs = filepath.Clean(abs)
		}
	}
	rel, err := filepath.Rel(base, abs)
	if err != nil {
		return false, err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false, nil
	}
	return true, nil
}

var issueCmd = &cobra.Command{
	Use:   "issue",
	Short: "Работа с issue",
}

var issueListCmd = &cobra.Command{
	Use:   "list",
	Short: "Показать issue рабочего пространства",
	RunE:  runIssueList,
}

var issueGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Показать сведения об issue",
	Args:  exactArgs(1),
	RunE:  runIssueGet,
}

var issuePullRequestsCmd = &cobra.Command{
	Use:     "pull-requests <id>",
	Aliases: []string{"prs"},
	Short:   "Показать pull request, связанные с issue",
	Args:    exactArgs(1),
	RunE:    runIssuePullRequests,
}

var issueChildrenCmd = &cobra.Command{
	Use:     "children <id>",
	Aliases: []string{"subissues"},
	Short:   "Показать дочерние issue, сгруппированные по этапам",
	Args:    exactArgs(1),
	RunE:    runIssueChildren,
}

var issueCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Создать issue",
	RunE:  runIssueCreate,
}

var issueUpdateCmd = &cobra.Command{
	Use:   "update <id>",
	Short: "Изменить issue",
	Args:  exactArgs(1),
	RunE:  runIssueUpdate,
}

var issueAssignCmd = &cobra.Command{
	Use:   "assign <id>",
	Short: "Назначить issue участнику, агенту или команде",
	Args:  exactArgs(1),
	RunE:  runIssueAssign,
}

var issueStatusCmd = &cobra.Command{
	Use:   "status <id> <status>",
	Short: "Изменить статус issue",
	Long: "Меняет статус issue. Допустимые статусы: " +
		"backlog, todo, in_progress, in_review, done, blocked, cancelled.",
	Args: exactArgs(2),
	RunE: runIssueStatus,
}

var issueReorderCmd = &cobra.Command{
	Use:   "reorder <id>",
	Short: "Переместить issue внутри колонки статуса",
	Long: "Меняет положение issue внутри текущей колонки статуса: вычисляет новую\n" +
		"позицию, ту же самую, что задаёт перетаскивание на доске.\n\n" +
		"Укажите ровно одну цель:\n" +
		"  --before <id>  поставить прямо над другим issue той же колонки\n" +
		"  --after  <id>  поставить прямо под другим issue той же колонки\n" +
		"  --top          поднять в начало колонки\n" +
		"  --bottom       опустить в конец колонки\n\n" +
		"Перемещение остаётся в текущей колонке issue. Чтобы перенести issue в\n" +
		"другую колонку, сначала смените статус командой `goosar issue status`.",
	Args: exactArgs(1),
	RunE: runIssueReorder,
}

var issueCommentCmd = &cobra.Command{
	Use:   "comment",
	Short: "Работа с комментариями к issue",
}

var issueCommentListCmd = &cobra.Command{
	Use:   "list <issue-id>",
	Short: "Показать комментарии к issue",
	Args:  exactArgs(1),
	RunE:  runIssueCommentList,
}

var issueCommentAddCmd = &cobra.Command{
	Use:   "add <issue-id>",
	Short: "Добавить комментарий к issue",
	Args:  exactArgs(1),
	RunE:  runIssueCommentAdd,
}

var issueCommentDeleteCmd = &cobra.Command{
	Use:   "delete <comment-id>",
	Short: "Удалить комментарий",
	Args:  exactArgs(1),
	RunE:  runIssueCommentDelete,
}

var issueCommentResolveCmd = &cobra.Command{
	Use:   "resolve <comment-id>",
	Short: "Отметить ветку комментариев решённой",
	Args:  exactArgs(1),
	RunE:  runIssueCommentResolve,
}

var issueCommentUnresolveCmd = &cobra.Command{
	Use:   "unresolve <comment-id>",
	Short: "Снять отметку «решено» с ветки комментариев",
	Args:  exactArgs(1),
	RunE:  runIssueCommentUnresolve,
}

var issueSubscriberCmd = &cobra.Command{
	Use:   "subscriber",
	Short: "Работа с подписчиками issue",
}

var issueSubscriberListCmd = &cobra.Command{
	Use:   "list <issue-id>",
	Short: "Показать подписчиков issue",
	Args:  exactArgs(1),
	RunE:  runIssueSubscriberList,
}

var issueSubscriberAddCmd = &cobra.Command{
	Use:   "add <issue-id>",
	Short: "Подписать пользователя или агента на issue (по умолчанию — вызывающего)",
	Args:  exactArgs(1),
	RunE:  runIssueSubscriberAdd,
}

var issueSubscriberRemoveCmd = &cobra.Command{
	Use:   "remove <issue-id>",
	Short: "Отписать пользователя или агента от issue (по умолчанию — вызывающего)",
	Args:  exactArgs(1),
	RunE:  runIssueSubscriberRemove,
}

var issueRunsCmd = &cobra.Command{
	Use:   "runs <issue-id>",
	Short: "Показать историю запусков по issue",
	Args:  exactArgs(1),
	RunE:  runIssueRuns,
}

var issueRunMessagesCmd = &cobra.Command{
	Use:   "run-messages <task-id>",
	Short: "Показать сообщения запуска",
	Args:  exactArgs(1),
	RunE:  runIssueRunMessages,
}

var issueUsageCmd = &cobra.Command{
	Use:   "usage <issue-id>",
	Short: "Показать суммарный расход токенов по issue",
	Args:  exactArgs(1),
	RunE:  runIssueUsage,
}

var issueRerunCmd = &cobra.Command{
	Use:   "rerun <id>",
	Short: "Поставить текущее назначение агента по issue в очередь как новую задачу",
	Args:  exactArgs(1),
	RunE:  runIssueRerun,
}

var issueCancelTaskCmd = &cobra.Command{
	Use:   "cancel-task <task-id>",
	Short: "Отменить выполняемую или ожидающую в очереди задачу (прерывает работающего агента)",
	Long: "Отменяет одну задачу по её ID. Принимает короткий префикс ID, который показывает `issue runs`. " +
		"Если префикс неоднозначен, укажите --issue, чтобы искать только среди задач этого issue. " +
		"Демон прерывает работающего агента, и тот сразу перестаёт вызывать инструменты.",
	Args: exactArgs(1),
	RunE: runIssueCancelTask,
}

var issueSearchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "Найти issue по заголовку или описанию",
	Args:  cobra.ExactArgs(1),
	RunE:  runIssueSearch,
}

var validIssueStatuses = []string{
	"backlog", "todo", "in_progress", "in_review", "done", "blocked", "cancelled",
}

var validIssuePriorities = []string{
	"urgent", "high", "medium", "low", "none",
}

var validIssueSortColumns = []string{
	"position", "title", "created_at", "start_date", "due_date", "priority",
}

var directionalIssueSortColumns = func() []string {
	cols := make([]string, 0, len(validIssueSortColumns)-1)
	for _, c := range validIssueSortColumns {
		if c != "position" {
			cols = append(cols, c)
		}
	}
	return cols
}()

func validateIssueStatus(status string) error {
	return validateIssueEnum("status", status, validIssueStatuses)
}

func validateIssuePriority(priority string) error {
	return validateIssueEnum("priority", priority, validIssuePriorities)
}

func validateIssueEnum(field, value string, allowed []string) error {
	for _, a := range allowed {
		if value == a {
			return nil
		}
	}
	return fmt.Errorf("invalid %s %q; valid values: %s", field, value, strings.Join(allowed, ", "))
}

func init() {
	issueCmd.AddCommand(issueListCmd)
	issueCmd.AddCommand(issueGetCmd)
	issueCmd.AddCommand(issuePullRequestsCmd)
	issueCmd.AddCommand(issueChildrenCmd)
	issueCmd.AddCommand(issueCreateCmd)
	issueCmd.AddCommand(issueUpdateCmd)
	issueCmd.AddCommand(issueAssignCmd)
	issueCmd.AddCommand(issueStatusCmd)
	issueCmd.AddCommand(issueReorderCmd)
	issueCmd.AddCommand(issueCommentCmd)
	issueCmd.AddCommand(issueSubscriberCmd)
	issueCmd.AddCommand(issueRunsCmd)
	issueCmd.AddCommand(issueRunMessagesCmd)
	issueCmd.AddCommand(issueUsageCmd)
	issueCmd.AddCommand(issueRerunCmd)
	issueCmd.AddCommand(issueCancelTaskCmd)
	issueCmd.AddCommand(issueSearchCmd)

	issueCommentCmd.AddCommand(issueCommentListCmd)
	issueCommentCmd.AddCommand(issueCommentAddCmd)
	issueCommentCmd.AddCommand(issueCommentDeleteCmd)
	issueCommentCmd.AddCommand(issueCommentResolveCmd)
	issueCommentCmd.AddCommand(issueCommentUnresolveCmd)

	issueSubscriberCmd.AddCommand(issueSubscriberListCmd)
	issueSubscriberCmd.AddCommand(issueSubscriberAddCmd)
	issueSubscriberCmd.AddCommand(issueSubscriberRemoveCmd)

	issueListCmd.Flags().String("output", "table", "Формат вывода: table или json")
	issueListCmd.Flags().Bool("full-id", false, "Показывать полные UUID в таблице")
	issueListCmd.Flags().String("status", "", "Фильтр по статусу")
	issueListCmd.Flags().String("priority", "", "Фильтр по приоритету")
	issueListCmd.Flags().String("assignee", "", "Фильтр по имени исполнителя (участник, агент или команда; нечёткое совпадение)")
	issueListCmd.Flags().String("assignee-id", "", "Фильтр по UUID исполнителя — участника, агента или команды (несовместимо с --assignee)")
	issueListCmd.Flags().String("project", "", "Фильтр по ID проекта")
	issueListCmd.Flags().StringSlice("metadata", nil, "Фильтр по metadata key=value (можно повторять; условия объединяются через AND). Значение разбирается как JSON: 'true'/'false' → bool, числа → number, остальное — строка. Чтобы значение, похожее на число, осталось строкой, оберните его так: '\"42\"'.")
	issueListCmd.Flags().Int("limit", 50, "Максимальное число возвращаемых issue")
	issueListCmd.Flags().Int("offset", 0, "Сколько issue пропустить (для постраничного вывода)")
	issueListCmd.Flags().String("sort", "", "Колонка сортировки: position (по умолчанию, ручной порядок на доске), title, created_at, start_date, due_date, priority")
	issueListCmd.Flags().String("direction", "", "Направление сортировки (asc или desc); требует --sort с любой колонкой, кроме position (position всегда по возрастанию)")

	issueGetCmd.Flags().String("output", "json", "Формат вывода: table или json")

	issuePullRequestsCmd.Flags().String("output", "table", "Формат вывода: table или json")

	issueChildrenCmd.Flags().String("output", "table", "Формат вывода: table или json")
	issueChildrenCmd.Flags().Bool("full-id", false, "Показывать полные UUID в таблице")

	issueCreateCmd.Flags().String("title", "", "Заголовок issue (обязательно)")
	issueCreateCmd.Flags().String("description", "", "Описание issue (раскрывает \\n, \\r, \\t, \\\\; чтобы сохранить обратные косые черты как есть, передайте текст через --description-stdin)")
	issueCreateCmd.Flags().Bool("description-stdin", false, "Прочитать описание issue из stdin (многострочный текст сохраняется без изменений)")
	issueCreateCmd.Flags().String("description-file", "", "Прочитать описание issue из файла в UTF-8 (многострочный текст сохраняется без изменений; в Windows используйте это, если передача через stdin портит не-ASCII байты). Путь должен лежать внутри текущего рабочего каталога, если не задан --allow-external-file.")
	issueCreateCmd.Flags().Bool("allow-external-file", false, "Разрешить --description-file / --attachment читать путь вне текущего рабочего каталога. По умолчанию выключено, чтобы случайно не подхватить устаревший файл из другого запуска или окружения (MUL-4252).")
	issueCreateCmd.Flags().String("status", "", "Статус issue")
	issueCreateCmd.Flags().String("priority", "", "Приоритет issue")
	issueCreateCmd.Flags().String("assignee", "", "Имя исполнителя (участник, агент или команда; нечёткое совпадение)")
	issueCreateCmd.Flags().String("assignee-id", "", "UUID исполнителя — участника, агента или команды (несовместимо с --assignee)")
	issueCreateCmd.Flags().String("parent", "", "ID родительского issue")
	issueCreateCmd.Flags().Int("stage", 0, "Номер этапа (>=1): объединяет дочерние issue родителя в упорядоченную группу-барьер; без флага этапа нет. Исполнитель родительского issue просыпается, только когда завершены все дочерние issue этапа.")
	issueCreateCmd.Flags().String("project", "", "ID проекта")
	issueCreateCmd.Flags().String("start-date", "", "Дата начала (календарный день, YYYY-MM-DD)")
	issueCreateCmd.Flags().String("due-date", "", "Срок (календарный день, YYYY-MM-DD)")
	issueCreateCmd.Flags().Bool("allow-duplicate", false, "Создавать issue, даже если уже есть активный дубликат")
	issueCreateCmd.Flags().String("output", "json", "Формат вывода: table или json")
	issueCreateCmd.Flags().StringSlice("attachment", nil, "Путь к прикрепляемому файлу (можно указывать несколько раз)")
	issueCreateCmd.Flags().StringSlice("attachment-id", nil, "UUID существующего вложения, которое нужно привязать к созданному issue (можно указывать несколько раз)")

	issueUpdateCmd.Flags().String("title", "", "Новый заголовок")
	issueUpdateCmd.Flags().String("description", "", "Новое описание (раскрывает \\n, \\r, \\t, \\\\; чтобы сохранить обратные косые черты как есть, передайте текст через --description-stdin)")
	issueUpdateCmd.Flags().Bool("description-stdin", false, "Прочитать новое описание из stdin (многострочный текст сохраняется без изменений)")
	issueUpdateCmd.Flags().String("description-file", "", "Прочитать новое описание из файла в UTF-8 (многострочный текст сохраняется без изменений; в Windows используйте это, если передача через stdin портит не-ASCII байты). Путь должен лежать внутри текущего рабочего каталога, если не задан --allow-external-file.")
	issueUpdateCmd.Flags().Bool("allow-external-file", false, "Разрешить --description-file читать путь вне текущего рабочего каталога. По умолчанию выключено, чтобы случайно не подхватить устаревший временный файл из другого запуска или окружения (MUL-4252).")
	issueUpdateCmd.Flags().String("status", "", "Новый статус")
	issueUpdateCmd.Flags().String("priority", "", "Новый приоритет")
	issueUpdateCmd.Flags().String("assignee", "", "Имя нового исполнителя (участник, агент или команда; нечёткое совпадение)")
	issueUpdateCmd.Flags().String("assignee-id", "", "UUID нового исполнителя — участника, агента или команды (несовместимо с --assignee)")
	issueUpdateCmd.Flags().String("project", "", "ID проекта")
	issueUpdateCmd.Flags().String("start-date", "", "Новая дата начала (календарный день, YYYY-MM-DD; пустая строка очищает значение)")
	issueUpdateCmd.Flags().String("due-date", "", "Новый срок (календарный день, YYYY-MM-DD)")
	issueUpdateCmd.Flags().String("parent", "", "ID родительского issue (--parent \"\" убирает родителя)")
	issueUpdateCmd.Flags().Int("stage", 0, "Номер этапа (>=1) для этого дочернего issue; см. `issue create --stage`")
	issueUpdateCmd.Flags().Float64("position", 0, "Позиция в колонке доски (меньшее значение выше); для относительных перемещений используйте `issue reorder`")
	issueUpdateCmd.Flags().String("output", "json", "Формат вывода: table или json")

	issueStatusCmd.Flags().String("output", "table", "Формат вывода: table или json")

	registerIssueReorderFlags(issueReorderCmd)

	issueAssignCmd.Flags().String("to", "", "Имя исполнителя (участник, агент или команда; нечёткое совпадение)")
	issueAssignCmd.Flags().String("to-id", "", "UUID исполнителя — участника, агента или команды (несовместимо с --to)")
	issueAssignCmd.Flags().Bool("unassign", false, "Снять текущего исполнителя")
	issueAssignCmd.Flags().String("output", "json", "Формат вывода: table или json")

	issueCommentListCmd.Flags().String("output", "table", "Формат вывода: table или json")
	issueCommentListCmd.Flags().String("since", "", "Вернуть только комментарии, созданные после этого момента (RFC3339)")
	issueCommentListCmd.Flags().String("thread", "", "UUID комментария — вернуть ветку, в которой он находится (корень и все потомки). Можно указать корень или ответ.")
	issueCommentListCmd.Flags().Int("tail", 0, "Только вместе с --thread. Ограничивает число ответов N самыми свежими; корень ветки возвращается всегда (даже при --tail 0). Чтобы прокрутить к более старым ответам, используйте --before/--before-id.")
	issueCommentListCmd.Flags().Int("recent", 0, "Вернуть N последних по активности веток (корень и потомки каждой ветки). Чтобы прокрутить к более старым веткам, передайте --before/--before-id из предыдущего ответа.")
	issueCommentListCmd.Flags().Bool("roots-only", false, "Вернуть только комментарии верхнего уровня (parent_id равен null). У каждого корня есть reply_count и last_activity_at — по ним удобно выбрать ветку, которую открыть.")
	issueCommentListCmd.Flags().Bool("summary", false, "Обрезать содержимое каждого комментария до короткого превью (выставляется content_truncated), чтобы просматривать список, не загружая тексты целиком. Сочетается с любым режимом.")
	issueCommentListCmd.Flags().Bool("full", false, "Вернуть без сокращений все комментарии решённых веток. По умолчанию чтения целых веток (обычный список, --recent, --thread без --tail) сворачиваются: решённая ветка сводится к корню и итогу, а число пропущенных комментариев указывается на корне — так вы не тратите токены на закрытое обсуждение. Передайте --full, если нужно и свёрнутое обсуждение. На чтения с --since/--tail/--roots-only не влияет: они никогда не сворачиваются.")
	issueCommentListCmd.Flags().String("before", "", "Курсор (метка времени RFC3339Nano). С --recent: курсор ветки (last_activity_at). С --thread + --tail: курсор ответа (created_at ответа). Берётся из заголовка ответа X-Goosar-Next-Before; передаётся вместе с --before-id.")
	issueCommentListCmd.Flags().String("before-id", "", "UUID курсора. С --recent: UUID корня ветки. С --thread + --tail: UUID самого старого ответа. Берётся из заголовка ответа X-Goosar-Next-Before-Id; передаётся вместе с --before.")

	issueRunsCmd.Flags().String("output", "table", "Формат вывода: table или json")
	issueRunsCmd.Flags().Bool("full-id", false, "Показывать полные UUID задач в таблице")

	issueUsageCmd.Flags().String("output", "table", "Формат вывода: table или json")

	issueRerunCmd.Flags().String("output", "json", "Формат вывода: table или json")

	issueCancelTaskCmd.Flags().String("output", "json", "Формат вывода: table или json")
	issueCancelTaskCmd.Flags().String("issue", "", "ID или ключ issue, среди задач которого искать короткий префикс ID задачи")

	issueRunMessagesCmd.Flags().String("output", "json", "Формат вывода: table или json")
	issueRunMessagesCmd.Flags().Int("since", 0, "Вернуть только сообщения после этого порядкового номера")
	issueRunMessagesCmd.Flags().String("issue", "", "ID или ключ issue, среди задач которого искать короткий префикс ID задачи")

	issueCommentAddCmd.Flags().String("content", "", "Текст комментария для коротких однострочных сообщений (раскрывает \\n, \\r, \\t, \\\\). Для многострочного текста или текста от агента используйте --content-file <path>: он сохраняет обратные кавычки, $(), кавычки и обратные косые черты как есть.")
	issueCommentAddCmd.Flags().Bool("content-stdin", false, "Прочитать текст комментария из stdin без изменений. Работает, только если эта же командная строка передаёт данные по конвейеру; голый --content-stdin в shell-инструменте агента читает пустой stdin и завершается ошибкой. Лучше используйте --content-file.")
	issueCommentAddCmd.Flags().String("content-file", "", "Прочитать текст комментария из файла в UTF-8 (многострочный текст сохраняется без изменений; в Windows используйте это, если передача через stdin портит не-ASCII байты). Путь должен лежать внутри текущего рабочего каталога, если не задан --allow-external-file.")
	issueCommentAddCmd.Flags().Bool("allow-external-file", false, "Разрешить --content-file / --attachment читать путь вне текущего рабочего каталога. По умолчанию выключено, чтобы случайно не подхватить устаревший файл из другого запуска или окружения (MUL-4252).")
	issueCommentAddCmd.Flags().String("parent", "", "ID родительского комментария, в ответ на который пишется этот. Задача агента, запущенная комментарием, должна отвечать под комментарием-триггером; комментарий верхнего уровня без --parent отклоняется")
	issueCommentAddCmd.Flags().StringSlice("attachment", nil, "Путь к прикрепляемому файлу (можно указывать несколько раз)")
	issueCommentAddCmd.Flags().String("output", "json", "Формат вывода: table или json")

	issueCommentResolveCmd.Flags().String("output", "json", "Формат вывода: table или json")
	issueCommentUnresolveCmd.Flags().String("output", "json", "Формат вывода: table или json")

	issueSearchCmd.Flags().Int("limit", 20, "Максимальное число результатов")
	issueSearchCmd.Flags().Bool("include-closed", false, "Включать issue со статусами done и cancelled")
	issueSearchCmd.Flags().String("output", "table", "Формат вывода: table или json")

	issueSubscriberListCmd.Flags().String("output", "table", "Формат вывода: table или json")

	issueSubscriberAddCmd.Flags().String("user", "", "Имя участника или агента для подписки (нечёткое совпадение; по умолчанию — вызывающий)")
	issueSubscriberAddCmd.Flags().String("user-id", "", "UUID участника или агента для подписки (несовместимо с --user)")
	issueSubscriberAddCmd.Flags().String("output", "json", "Формат вывода: table или json")

	issueSubscriberRemoveCmd.Flags().String("user", "", "Имя участника или агента для отписки (нечёткое совпадение; по умолчанию — вызывающий)")
	issueSubscriberRemoveCmd.Flags().String("user-id", "", "UUID участника или агента для отписки (несовместимо с --user)")
	issueSubscriberRemoveCmd.Flags().String("output", "json", "Формат вывода: table или json")
}

func runIssueList(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	if client.WorkspaceID == "" {
		if _, err := requireWorkspaceID(cmd); err != nil {
			return err
		}
	}

	params := url.Values{}
	params.Set("workspace_id", client.WorkspaceID)
	if v, _ := cmd.Flags().GetString("status"); v != "" {
		params.Set("status", v)
	}
	if v, _ := cmd.Flags().GetString("priority"); v != "" {
		params.Set("priority", v)
	}
	if v, _ := cmd.Flags().GetInt("limit"); v > 0 {
		params.Set("limit", fmt.Sprintf("%d", v))
	}
	_, aID, hasAssignee, resolveErr := pickAssigneeFromFlags(ctx, client, cmd, "assignee", "assignee-id", issueAssigneeKinds)
	if resolveErr != nil {
		return fmt.Errorf("resolve assignee: %w", resolveErr)
	}
	if hasAssignee {
		params.Set("assignee_id", aID)
	}
	if v, _ := cmd.Flags().GetInt("offset"); v > 0 {
		params.Set("offset", fmt.Sprintf("%d", v))
	}
	if v, _ := cmd.Flags().GetString("project"); v != "" {
		project, err := resolveProjectID(ctx, client, v)
		if err != nil {
			return err
		}
		params.Set("project_id", project.ID)
	}
	if mdFlags, _ := cmd.Flags().GetStringSlice("metadata"); len(mdFlags) > 0 {
		filter, err := buildMetadataFilterQueryParam(mdFlags)
		if err != nil {
			return err
		}
		params.Set("metadata", filter)
	}
	sortVal, _ := cmd.Flags().GetString("sort")
	if sortVal != "" {
		valid := false
		for _, c := range validIssueSortColumns {
			if c == sortVal {
				valid = true
				break
			}
		}
		if !valid {
			return fmt.Errorf("invalid --sort %q; valid values: %s", sortVal, strings.Join(validIssueSortColumns, ", "))
		}
		params.Set("sort", sortVal)
	}
	if v, _ := cmd.Flags().GetString("direction"); v != "" {
		d := strings.ToLower(v)
		if d != "asc" && d != "desc" {
			return fmt.Errorf("invalid --direction %q; valid values: asc, desc", v)
		}

		if sortVal == "" || sortVal == "position" {
			return fmt.Errorf("--direction requires --sort to be one of %s; position (the default manual board order) is always ascending", strings.Join(directionalIssueSortColumns, ", "))
		}
		params.Set("direction", d)
	}

	path := "/api/issues"
	if len(params) > 0 {
		path += "?" + params.Encode()
	}

	var result map[string]any
	if err := client.GetJSON(ctx, path, &result); err != nil {
		return fmt.Errorf("list issues: %w", err)
	}

	issuesRaw, _ := result["issues"].([]any)

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		total, _ := result["total"].(float64)
		limit, _ := cmd.Flags().GetInt("limit")
		offset, _ := cmd.Flags().GetInt("offset")
		hasMore := offset+len(issuesRaw) < int(total)
		wrapped := map[string]any{
			"issues":   issuesRaw,
			"total":    int(total),
			"limit":    limit,
			"offset":   offset,
			"has_more": hasMore,
		}
		return cli.PrintJSON(os.Stdout, wrapped)
	}

	fullID, _ := cmd.Flags().GetBool("full-id")
	headers := []string{"KEY", "TITLE", "STATUS", "PRIORITY", "ASSIGNEE", "START DATE", "DUE DATE"}
	if fullID {
		headers = []string{"KEY", "ID", "TITLE", "STATUS", "PRIORITY", "ASSIGNEE", "START DATE", "DUE DATE"}
	}
	actors := loadActorDisplayLookup(ctx, client)
	rows := make([][]string, 0, len(issuesRaw))
	for _, raw := range issuesRaw {
		issue, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		assignee := formatAssignee(issue, actors)
		startDate := strVal(issue, "start_date")
		if startDate != "" && len(startDate) >= 10 {
			startDate = startDate[:10]
		}
		dueDate := strVal(issue, "due_date")
		if dueDate != "" && len(dueDate) >= 10 {
			dueDate = dueDate[:10]
		}
		row := []string{
			issueDisplayKey(issue),
			strVal(issue, "title"),
			strVal(issue, "status"),
			strVal(issue, "priority"),
			assignee,
			startDate,
			dueDate,
		}
		if fullID {
			row = []string{
				issueDisplayKey(issue),
				strVal(issue, "id"),
				strVal(issue, "title"),
				strVal(issue, "status"),
				strVal(issue, "priority"),
				assignee,
				startDate,
				dueDate,
			}
		}
		rows = append(rows, row)
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runIssuePullRequests(cmd *cobra.Command, args []string) error {
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
	if err := client.GetJSON(ctx, "/api/issues/"+url.PathEscape(issueRef.ID)+"/pull-requests", &result); err != nil {
		return fmt.Errorf("list issue pull requests: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}

	prs, _ := result["pull_requests"].([]any)
	printIssuePullRequestsTable(normalizePullRequestList(prs))
	return nil
}

func normalizePullRequestList(raw []any) []map[string]any {
	prs := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		pr, ok := item.(map[string]any)
		if !ok {
			continue
		}
		prs = append(prs, pr)
	}
	return prs
}

func printIssuePullRequestsTable(prs []map[string]any) {
	headers := []string{"NUMBER", "STATE", "TITLE", "URL"}
	rows := make([][]string, 0, len(prs))
	for _, pr := range prs {
		rows = append(rows, []string{
			strVal(pr, "number"),
			strVal(pr, "state"),
			strVal(pr, "title"),
			pullRequestURL(pr),
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
}

func pullRequestURL(pr map[string]any) string {
	if url := strVal(pr, "url"); url != "" {
		return url
	}
	return strVal(pr, "html_url")
}

func runIssueGet(cmd *cobra.Command, args []string) error {
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

	var issue map[string]any
	if err := client.GetJSON(ctx, "/api/issues/"+issueRef.ID, &issue); err != nil {
		return fmt.Errorf("get issue: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		actors := loadActorDisplayLookup(ctx, client)
		assignee := formatAssignee(issue, actors)
		startDate := strVal(issue, "start_date")
		if startDate != "" && len(startDate) >= 10 {
			startDate = startDate[:10]
		}
		dueDate := strVal(issue, "due_date")
		if dueDate != "" && len(dueDate) >= 10 {
			dueDate = dueDate[:10]
		}
		headers := []string{"KEY", "TITLE", "STATUS", "PRIORITY", "ASSIGNEE", "START DATE", "DUE DATE", "DESCRIPTION"}
		rows := [][]string{{
			issueDisplayKey(issue),
			strVal(issue, "title"),
			strVal(issue, "status"),
			strVal(issue, "priority"),
			assignee,
			startDate,
			dueDate,
			strVal(issue, "description"),
		}}
		cli.PrintTable(os.Stdout, headers, rows)
		return nil
	}

	return cli.PrintJSON(os.Stdout, issue)
}

func childStage(m map[string]any) (int, bool) {
	v, ok := m["stage"]
	if !ok || v == nil {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	case json.Number:
		i, err := n.Int64()
		if err != nil {
			return 0, false
		}
		return int(i), true
	}
	return 0, false
}

func runIssueChildren(cmd *cobra.Command, args []string) error {
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

	var resp struct {
		Issues []map[string]any `json:"issues"`
	}
	if err := client.GetJSON(ctx, "/api/issues/"+issueRef.ID+"/children", &resp); err != nil {
		return fmt.Errorf("list child issues: %w", err)
	}
	children := resp.Issues

	sort.SliceStable(children, func(i, j int) bool {
		si, oki := childStage(children[i])
		sj, okj := childStage(children[j])
		if oki != okj {
			return oki
		}
		return si < sj
	})

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		actors := loadActorDisplayLookup(ctx, client)
		headers := []string{"STAGE", "KEY", "TITLE", "STATUS", "PRIORITY", "ASSIGNEE"}
		rows := make([][]string, 0, len(children))
		for _, c := range children {
			stageCell := "-"
			if s, ok := childStage(c); ok {
				stageCell = strconv.Itoa(s)
			}
			rows = append(rows, []string{
				stageCell,
				issueDisplayKey(c),
				strVal(c, "title"),
				strVal(c, "status"),
				strVal(c, "priority"),
				formatAssignee(c, actors),
			})
		}
		cli.PrintTable(os.Stdout, headers, rows)
		return nil
	}

	type stageGroup struct {
		Stage  int              `json:"stage"`
		Total  int              `json:"total"`
		Done   int              `json:"done"`
		Issues []map[string]any `json:"issues"`
	}
	stages := []stageGroup{}
	unstaged := []map[string]any{}
	idxByStage := map[int]int{}
	for _, c := range children {
		s, ok := childStage(c)
		if !ok {
			unstaged = append(unstaged, c)
			continue
		}
		gi, seen := idxByStage[s]
		if !seen {
			stages = append(stages, stageGroup{Stage: s})
			gi = len(stages) - 1
			idxByStage[s] = gi
		}
		stages[gi].Issues = append(stages[gi].Issues, c)
		stages[gi].Total++
		if st := strVal(c, "status"); st == "done" || st == "cancelled" {
			stages[gi].Done++
		}
	}
	return cli.PrintJSON(os.Stdout, map[string]any{
		"total":    len(children),
		"stages":   stages,
		"unstaged": unstaged,
	})
}

func isHTTPURL(path string) bool {
	p := strings.TrimSpace(path)
	return strings.HasPrefix(p, "http://") || strings.HasPrefix(p, "https://")
}

func ensureAttachmentWithinWorkdir(cmd *cobra.Command, filePath string) error {
	if allow, _ := cmd.Flags().GetBool("allow-external-file"); allow {
		return nil
	}
	within, err := fileWithinWorkingDir(filePath)
	if err != nil {
		return fmt.Errorf("resolve --attachment path %q: %w", filePath, err)
	}
	if !within {
		return fmt.Errorf(
			"--attachment path %q resolves outside the current working directory; "+
				"attach files generated inside the task workdir rather than machine-shared "+
				"paths like /tmp, where another run's stale file can be attached by mistake. "+
				"Pass --allow-external-file to override.",
			filePath)
	}
	return nil
}

type pendingAttachment struct {
	path string
	data []byte
}

func collectLocalAttachments(cmd *cobra.Command, attachments []string) ([]pendingAttachment, error) {
	pending := make([]pendingAttachment, 0, len(attachments))
	for _, filePath := range attachments {
		if isHTTPURL(filePath) {
			fmt.Fprintf(os.Stderr, "Skipping --attachment %q: URLs are not supported here, only local file paths.\n", filePath)
			continue
		}
		if err := ensureAttachmentWithinWorkdir(cmd, filePath); err != nil {
			return nil, err
		}
		data, readErr := os.ReadFile(filePath)
		if readErr != nil {
			return nil, fmt.Errorf("read attachment %s: %w", filePath, readErr)
		}
		pending = append(pending, pendingAttachment{path: filePath, data: data})
	}
	return pending, nil
}

func appendUniqueStrings(dst []string, values ...string) []string {
	seen := make(map[string]struct{}, len(dst)+len(values))
	out := make([]string, 0, len(dst)+len(values))
	for _, v := range append(dst, values...) {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

func quickCreateAttachmentIDsFromEnv() ([]string, error) {
	raw := strings.TrimSpace(os.Getenv("GOOSAR_QUICK_CREATE_ATTACHMENT_IDS"))
	if raw == "" {
		return nil, nil
	}
	var ids []string
	if err := json.Unmarshal([]byte(raw), &ids); err != nil {
		return nil, fmt.Errorf("parse GOOSAR_QUICK_CREATE_ATTACHMENT_IDS: %w", err)
	}
	return appendUniqueStrings(nil, ids...), nil
}

func runIssueCreate(cmd *cobra.Command, _ []string) error {
	title, _ := cmd.Flags().GetString("title")
	if title == "" {
		return fmt.Errorf("--title is required")
	}
	statusFlag, _ := cmd.Flags().GetString("status")
	if statusFlag != "" {
		if err := validateIssueStatus(statusFlag); err != nil {
			return err
		}
	}
	priorityFlag, _ := cmd.Flags().GetString("priority")
	if priorityFlag != "" {
		if err := validateIssuePriority(priorityFlag); err != nil {
			return err
		}
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	timeout := cli.APITimeout()
	attachments, _ := cmd.Flags().GetStringSlice("attachment")
	if len(attachments) > 0 {
		timeout = cli.AtLeastAPITimeout(60 * time.Second)
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	body := map[string]any{"title": title}
	desc, hasDesc, err := resolveTextFlag(cmd, "description")
	if err != nil {
		return err
	}
	if hasDesc {
		if err := guardLocalPathLinks(desc, "issue description",
			"Deliver the file itself with `goosar issue create --attachment <path>` (repeatable) and drop the link."); err != nil {
			return err
		}
		body["description"] = desc
	}
	if statusFlag != "" {
		body["status"] = statusFlag
	}
	if priorityFlag != "" {
		body["priority"] = priorityFlag
	}
	if v, _ := cmd.Flags().GetString("parent"); v != "" {
		parent, err := resolveIssueRef(ctx, client, v)
		if err != nil {
			return fmt.Errorf("resolve parent issue: %w", err)
		}
		body["parent_issue_id"] = parent.ID
	}
	if v, _ := cmd.Flags().GetString("project"); v != "" {
		project, err := resolveProjectID(ctx, client, v)
		if err != nil {
			return fmt.Errorf("resolve project: %w", err)
		}
		body["project_id"] = project.ID
	}
	if cmd.Flags().Changed("stage") {
		stage, _ := cmd.Flags().GetInt("stage")
		if stage < 1 {
			return fmt.Errorf("--stage must be >= 1")
		}
		body["stage"] = stage
	}
	if v, _ := cmd.Flags().GetString("start-date"); v != "" {
		body["start_date"] = v
	}
	if v, _ := cmd.Flags().GetString("due-date"); v != "" {
		body["due_date"] = v
	}
	if v, _ := cmd.Flags().GetBool("allow-duplicate"); v {
		body["allow_duplicate"] = true
	}
	aType, aID, hasAssignee, resolveErr := pickAssigneeFromFlags(ctx, client, cmd, "assignee", "assignee-id", issueAssigneeKinds)
	if resolveErr != nil {
		return fmt.Errorf("resolve assignee: %w", resolveErr)
	}
	if hasAssignee {
		body["assignee_type"] = aType
		body["assignee_id"] = aID
	}

	attachmentIDs, _ := cmd.Flags().GetStringSlice("attachment-id")
	envAttachmentIDs, err := quickCreateAttachmentIDsFromEnv()
	if err != nil {
		return err
	}
	attachmentIDs = appendUniqueStrings(attachmentIDs, envAttachmentIDs...)
	if len(attachmentIDs) > 0 {
		body["attachment_ids"] = attachmentIDs
	}

	pending, err := collectLocalAttachments(cmd, attachments)
	if err != nil {
		return err
	}

	var result map[string]any
	if err := client.PostJSON(ctx, "/api/issues", body, &result); err != nil {
		if сообщение, ok := activeDuplicateIssueCreateMessage(err); ok {
			return errors.New(сообщение)
		}
		return fmt.Errorf("create issue: %w", err)
	}

	issueID := strVal(result, "id")
	for _, att := range pending {
		if _, uploadErr := client.UploadFile(ctx, att.data, att.path, issueID); uploadErr != nil {
			fmt.Fprintf(os.Stderr, "warning: upload attachment %s failed (issue already created, %s): %v\n",
				att.path, strVal(result, "identifier"), uploadErr)
			continue
		}
		fmt.Fprintf(os.Stderr, "Uploaded %s\n", att.path)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		headers := []string{"KEY", "TITLE", "STATUS", "PRIORITY"}
		rows := [][]string{{
			issueDisplayKey(result),
			strVal(result, "title"),
			strVal(result, "status"),
			strVal(result, "priority"),
		}}
		cli.PrintTable(os.Stdout, headers, rows)
		return nil
	}

	return cli.PrintJSON(os.Stdout, result)
}

func activeDuplicateIssueCreateMessage(err error) (string, bool) {
	var httpErr *cli.HTTPError
	if !errors.As(err, &httpErr) || httpErr.StatusCode != http.StatusConflict {
		return "", false
	}
	var payload struct {
		Code  string `json:"code"`
		Error string `json:"error"`
	}
	if json.Unmarshal([]byte(httpErr.Body), &payload) != nil {
		return "", false
	}
	if payload.Code != "active_duplicate_issue" || payload.Error == "" {
		return "", false
	}
	return payload.Error, true
}

func runIssueUpdate(cmd *cobra.Command, args []string) error {
	statusChanged := cmd.Flags().Changed("status")
	statusFlag, _ := cmd.Flags().GetString("status")
	if statusChanged {
		if err := validateIssueStatus(statusFlag); err != nil {
			return err
		}
	}
	priorityChanged := cmd.Flags().Changed("priority")
	priorityFlag, _ := cmd.Flags().GetString("priority")
	if priorityChanged {
		if err := validateIssuePriority(priorityFlag); err != nil {
			return err
		}
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

	body := map[string]any{}
	if cmd.Flags().Changed("title") {
		v, _ := cmd.Flags().GetString("title")
		body["title"] = v
	}
	if cmd.Flags().Changed("description") || cmd.Flags().Changed("description-stdin") || cmd.Flags().Changed("description-file") {
		desc, _, err := resolveTextFlag(cmd, "description")
		if err != nil {
			return err
		}

		if err := guardLocalPathLinks(desc, "issue description",
			"`goosar issue update` cannot carry files — deliver the file with `goosar issue comment add <issue-id> --attachment <path>` instead, and drop the link."); err != nil {
			return err
		}
		body["description"] = desc
	}
	if statusChanged {
		body["status"] = statusFlag
	}
	if priorityChanged {
		body["priority"] = priorityFlag
	}
	if cmd.Flags().Changed("project") {
		v, _ := cmd.Flags().GetString("project")
		if v == "" {
			body["project_id"] = nil
		} else {
			project, err := resolveProjectID(ctx, client, v)
			if err != nil {
				return fmt.Errorf("resolve project: %w", err)
			}
			body["project_id"] = project.ID
		}
	}
	if cmd.Flags().Changed("start-date") {
		v, _ := cmd.Flags().GetString("start-date")
		body["start_date"] = v
	}
	if cmd.Flags().Changed("due-date") {
		v, _ := cmd.Flags().GetString("due-date")
		body["due_date"] = v
	}
	if cmd.Flags().Changed("assignee") || cmd.Flags().Changed("assignee-id") {
		aType, aID, hasAssignee, resolveErr := pickAssigneeFromFlags(ctx, client, cmd, "assignee", "assignee-id", issueAssigneeKinds)
		if resolveErr != nil {
			return fmt.Errorf("resolve assignee: %w", resolveErr)
		}
		if hasAssignee {
			body["assignee_type"] = aType
			body["assignee_id"] = aID
		}
	}
	if cmd.Flags().Changed("parent") {
		v, _ := cmd.Flags().GetString("parent")
		if v == "" {
			body["parent_issue_id"] = nil
		} else {
			parent, err := resolveIssueRef(ctx, client, v)
			if err != nil {
				return fmt.Errorf("resolve parent issue: %w", err)
			}
			body["parent_issue_id"] = parent.ID
		}
	}
	if cmd.Flags().Changed("stage") {
		stage, _ := cmd.Flags().GetInt("stage")
		if stage < 1 {
			return fmt.Errorf("--stage must be >= 1")
		}
		body["stage"] = stage
	}
	if cmd.Flags().Changed("position") {
		v, _ := cmd.Flags().GetFloat64("position")
		body["position"] = v
	}

	if len(body) == 0 {
		return fmt.Errorf("no fields to update; use flags like --title, --status, --priority, --assignee, etc.")
	}

	var result map[string]any
	if err := client.PutJSON(ctx, "/api/issues/"+issueRef.ID, body, &result); err != nil {
		return fmt.Errorf("update issue: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		headers := []string{"KEY", "TITLE", "STATUS", "PRIORITY"}
		rows := [][]string{{
			issueDisplayKey(result),
			strVal(result, "title"),
			strVal(result, "status"),
			strVal(result, "priority"),
		}}
		cli.PrintTable(os.Stdout, headers, rows)
		return nil
	}

	return cli.PrintJSON(os.Stdout, result)
}

func runIssueAssign(cmd *cobra.Command, args []string) error {
	toName, _ := cmd.Flags().GetString("to")
	unassign, _ := cmd.Flags().GetBool("unassign")
	toNameSet := cmd.Flags().Changed("to")
	toIDSet := cmd.Flags().Changed("to-id")

	if !toNameSet && !toIDSet && !unassign {
		return fmt.Errorf("provide --to <name>, --to-id <uuid>, or --unassign")
	}
	if (toNameSet || toIDSet) && unassign {
		return fmt.Errorf("--to/--to-id and --unassign are mutually exclusive")
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

	body := map[string]any{}
	displayTarget := toName
	if unassign {
		body["assignee_type"] = nil
		body["assignee_id"] = nil
	} else {
		aType, aID, _, resolveErr := pickAssigneeFromFlags(ctx, client, cmd, "to", "to-id", issueAssigneeKinds)
		if resolveErr != nil {
			return fmt.Errorf("resolve assignee: %w", resolveErr)
		}
		body["assignee_type"] = aType
		body["assignee_id"] = aID
		if displayTarget == "" {
			displayTarget = loadActorDisplayLookup(ctx, client).actor(aType, aID)
		}
	}

	var result map[string]any
	if err := client.PutJSON(ctx, "/api/issues/"+issueRef.ID, body, &result); err != nil {
		return fmt.Errorf("assign issue: %w", err)
	}

	if unassign {
		fmt.Fprintf(os.Stderr, "Issue %s unassigned.\n", issueDisplayKey(result))
	} else {
		fmt.Fprintf(os.Stderr, "Issue %s assigned to %s.\n", issueDisplayKey(result), displayTarget)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		return nil
	}
	return cli.PrintJSON(os.Stdout, result)
}

func runIssueStatus(cmd *cobra.Command, args []string) error {
	id := args[0]
	status := args[1]

	if err := validateIssueStatus(status); err != nil {
		return err
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	issueRef, err := resolveIssueRef(ctx, client, id)
	if err != nil {
		return fmt.Errorf("resolve issue: %w", err)
	}

	body := map[string]any{"status": status}
	var result map[string]any
	if err := client.PutJSON(ctx, "/api/issues/"+issueRef.ID, body, &result); err != nil {
		return fmt.Errorf("update status: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Issue %s status changed to %s.\n", issueDisplayKey(result), status)

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	return nil
}

func registerIssueReorderFlags(cmd *cobra.Command) {
	cmd.Flags().String("before", "", "Поставить issue прямо над этим issue (в той же колонке)")
	cmd.Flags().String("after", "", "Поставить issue прямо под этим issue (в той же колонке)")
	cmd.Flags().Bool("top", false, "Поднять issue в начало колонки статуса")
	cmd.Flags().Bool("bottom", false, "Опустить issue в конец колонки статуса")
	cmd.Flags().String("output", "json", "Формат вывода: table или json")
	cmd.MarkFlagsMutuallyExclusive("before", "after", "top", "bottom")
	cmd.MarkFlagsOneRequired("before", "after", "top", "bottom")
}

func runIssueReorder(cmd *cobra.Command, args []string) error {
	before, _ := cmd.Flags().GetString("before")
	after, _ := cmd.Flags().GetString("after")
	top, _ := cmd.Flags().GetBool("top")
	bottom, _ := cmd.Flags().GetBool("bottom")

	if cmd.Flags().Changed("before") && before == "" {
		return fmt.Errorf("--before requires an issue ID or key")
	}
	if cmd.Flags().Changed("after") && after == "" {
		return fmt.Errorf("--after requires an issue ID or key")
	}
	if cmd.Flags().Changed("top") && !top {
		return fmt.Errorf("--top cannot be set to false; pass it on its own to move the issue to the top of its column")
	}
	if cmd.Flags().Changed("bottom") && !bottom {
		return fmt.Errorf("--bottom cannot be set to false; pass it on its own to move the issue to the bottom of its column")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	wsID := client.WorkspaceID
	if wsID == "" {
		wsID, err = requireWorkspaceID(cmd)
		if err != nil {
			return err
		}
	}

	issueRef, err := resolveIssueRef(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve issue: %w", err)
	}
	target, err := fetchIssue(ctx, client, issueRef.ID)
	if err != nil {
		return fmt.Errorf("get issue: %w", err)
	}
	status := strVal(target, "status")
	if status == "" {
		return fmt.Errorf("issue %s has no status, cannot determine its column", issueRef.Display)
	}

	relative := before != "" || after != ""
	var otherRef resolvedID
	if relative {
		otherInput := before
		if after != "" {
			otherInput = after
		}
		otherRef, err = resolveIssueRef(ctx, client, otherInput)
		if err != nil {
			return fmt.Errorf("resolve target issue: %w", err)
		}
		if otherRef.ID == issueRef.ID {
			return fmt.Errorf("cannot reorder issue %s relative to itself", issueRef.Display)
		}
	}

	projectID := strVal(target, "project_id")
	column, err := fetchIssueColumn(ctx, client, wsID, projectID, status)
	if err != nil {
		return fmt.Errorf("list %s column: %w", status, err)
	}

	positions := make(map[string]float64, len(column))
	ordered := make([]string, 0, len(column))
	for _, raw := range column {
		id := strVal(raw, "id")
		if id == "" {
			continue
		}
		positions[id] = floatVal(raw, "position")
		if id != issueRef.ID {
			ordered = append(ordered, id)
		}
	}
	if len(ordered) == 0 {

		if relative {
			return reorderTargetNotInColumnError(ctx, client, otherRef, issueRef, status)
		}
		fmt.Fprintf(os.Stderr, "Issue %s is the only issue in the %s column; nothing to reorder.\n", issueRef.Display, status)
		return issueReorderOutput(cmd, target)
	}

	insertIdx := 0
	switch {
	case top:
		insertIdx = 0
	case bottom:
		insertIdx = len(ordered)
	default:
		idx := indexOfString(ordered, otherRef.ID)
		if idx == -1 {
			return reorderTargetNotInColumnError(ctx, client, otherRef, issueRef, status)
		}
		if before != "" {
			insertIdx = idx
		} else {
			insertIdx = idx + 1
		}
	}

	reordered := make([]string, 0, len(ordered)+1)
	reordered = append(reordered, ordered[:insertIdx]...)
	reordered = append(reordered, issueRef.ID)
	reordered = append(reordered, ordered[insertIdx:]...)

	currentPos := positions[issueRef.ID]
	newPos := computeReorderPosition(reordered, issueRef.ID, positions, currentPos)
	if newPos == currentPos {
		fmt.Fprintf(os.Stderr, "Issue %s is already in that position.\n", issueRef.Display)
		return issueReorderOutput(cmd, target)
	}

	var result map[string]any
	if err := client.PutJSON(ctx, "/api/issues/"+url.PathEscape(issueRef.ID), map[string]any{"position": newPos}, &result); err != nil {
		return fmt.Errorf("reorder issue: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Issue %s reordered.\n", issueDisplayKey(result))
	return issueReorderOutput(cmd, result)
}

func issueReorderOutput(cmd *cobra.Command, issue map[string]any) error {
	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		headers := []string{"KEY", "TITLE", "STATUS", "PRIORITY"}
		rows := [][]string{{
			issueDisplayKey(issue),
			strVal(issue, "title"),
			strVal(issue, "status"),
			strVal(issue, "priority"),
		}}
		cli.PrintTable(os.Stdout, headers, rows)
		return nil
	}
	return cli.PrintJSON(os.Stdout, issue)
}

func reorderTargetNotInColumnError(ctx context.Context, client *cli.APIClient, otherRef, issueRef resolvedID, status string) error {
	if other, err := fetchIssue(ctx, client, otherRef.ID); err == nil {
		if otherStatus := strVal(other, "status"); otherStatus != "" && otherStatus != status {
			return fmt.Errorf("issue %s is in the %q column but %s is in %q; move one with `goosar issue status` first, or pick a target in the same column", otherRef.Display, otherStatus, issueRef.Display, status)
		}
	}
	return fmt.Errorf("issue %s was not found in the %q column", otherRef.Display, status)
}

func fetchIssue(ctx context.Context, client *cli.APIClient, id string) (map[string]any, error) {
	var issue map[string]any
	if err := client.GetJSON(ctx, "/api/issues/"+url.PathEscape(id), &issue); err != nil {
		return nil, err
	}
	return issue, nil
}

func fetchIssueColumn(ctx context.Context, client *cli.APIClient, workspaceID, projectID, status string) ([]map[string]any, error) {
	var all []map[string]any
	offset := 0
	for {
		params := url.Values{}
		params.Set("workspace_id", workspaceID)
		params.Set("status", status)
		if projectID != "" {
			params.Set("project_id", projectID)
		}
		params.Set("sort", "position")
		params.Set("limit", "100")
		params.Set("offset", fmt.Sprintf("%d", offset))

		var result map[string]any
		if err := client.GetJSON(ctx, "/api/issues?"+params.Encode(), &result); err != nil {
			return nil, err
		}
		page, _ := result["issues"].([]any)
		for _, raw := range page {
			if m, ok := raw.(map[string]any); ok {
				all = append(all, m)
			}
		}
		total, _ := result["total"].(float64)
		offset += len(page)
		if len(page) == 0 || offset >= int(total) {
			break
		}
	}
	return all, nil
}

func computeReorderPosition(ids []string, activeID string, positions map[string]float64, fallback float64) float64 {
	idx := indexOfString(ids, activeID)
	if idx == -1 || len(ids) == 1 {
		return fallback
	}
	if idx == 0 {
		return positions[ids[1]] - 1
	}
	if idx == len(ids)-1 {
		return positions[ids[idx-1]] + 1
	}
	return (positions[ids[idx-1]] + positions[ids[idx+1]]) / 2
}

func indexOfString(s []string, target string) int {
	for i, v := range s {
		if v == target {
			return i
		}
	}
	return -1
}

func floatVal(m map[string]any, key string) float64 {
	if v, ok := m[key].(float64); ok {
		return v
	}
	return 0
}

func runIssueCommentList(cmd *cobra.Command, args []string) error {
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

	since, _ := cmd.Flags().GetString("since")
	thread, _ := cmd.Flags().GetString("thread")
	recent, _ := cmd.Flags().GetInt("recent")
	tail, _ := cmd.Flags().GetInt("tail")
	rootsOnly, _ := cmd.Flags().GetBool("roots-only")
	summary, _ := cmd.Flags().GetBool("summary")
	full, _ := cmd.Flags().GetBool("full")

	recentSet := cmd.Flags().Changed("recent")
	tailSet := cmd.Flags().Changed("tail")
	before, _ := cmd.Flags().GetString("before")
	beforeID, _ := cmd.Flags().GetString("before-id")

	if recentSet && recent <= 0 {
		return fmt.Errorf("--recent must be a positive integer")
	}
	if tailSet && tail < 0 {
		return fmt.Errorf("--tail must be a non-negative integer (0 returns just the thread root)")
	}
	if thread != "" && recentSet {
		return fmt.Errorf("--thread and --recent are mutually exclusive")
	}
	if rootsOnly && thread != "" {
		return fmt.Errorf("--roots-only and --thread are mutually exclusive")
	}
	if rootsOnly && recentSet {
		return fmt.Errorf("--roots-only and --recent are mutually exclusive")
	}
	if rootsOnly && tailSet {
		return fmt.Errorf("--roots-only and --tail are mutually exclusive")
	}
	if rootsOnly && before != "" {
		return fmt.Errorf("--roots-only does not support --before / --before-id")
	}
	if tailSet && thread == "" {
		return fmt.Errorf("--tail requires --thread (it is a thread-scoped limit)")
	}
	if (before == "") != (beforeID == "") {
		return fmt.Errorf("--before and --before-id must be set together (composite cursor for stable pagination)")
	}
	if before != "" && !recentSet && !(thread != "" && tailSet) {
		return fmt.Errorf("--before / --before-id require --recent (thread cursor) or --thread + --tail (reply cursor)")
	}

	params := url.Values{}
	if since != "" {
		params.Set("since", since)
	}
	if rootsOnly {
		params.Set("roots_only", "true")
	}
	if summary {
		params.Set("summary", "true")
	}

	foldEligible := !rootsOnly && since == "" && !tailSet
	if foldEligible && !full {
		params.Set("fold", "true")
	}
	if thread != "" {
		params.Set("thread", thread)
	}
	if tailSet {
		params.Set("tail", fmt.Sprintf("%d", tail))
	}
	if recentSet {
		params.Set("recent", fmt.Sprintf("%d", recent))
	}
	if before != "" {
		params.Set("before", before)
		params.Set("before_id", beforeID)
	}

	path := "/api/issues/" + issueRef.ID + "/comments"
	if len(params) > 0 {
		path += "?" + params.Encode()
	}

	var comments []map[string]any
	respHeaders, err := client.GetJSONWithHeaders(ctx, path, &comments)
	if err != nil {
		return fmt.Errorf("list comments: %w", err)
	}

	if nb := respHeaders.Get("X-Goosar-Next-Before"); nb != "" {
		if nbid := respHeaders.Get("X-Goosar-Next-Before-Id"); nbid != "" {
			label := "Next thread cursor"
			if thread != "" && tailSet {
				label = "Next reply cursor"
			}
			fmt.Fprintf(os.Stderr, "%s: --before %s --before-id %s\n", label, nb, nbid)
		}
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, comments)
	}

	actors := loadActorDisplayLookup(ctx, client)
	headers := []string{"ID", "PARENT", "AUTHOR", "TYPE", "CONTENT", "CREATED"}
	rows := make([][]string, 0, len(comments))
	for _, c := range comments {
		content := strVal(c, "content")
		if utf8.RuneCountInString(content) > 80 {
			runes := []rune(content)
			content = string(runes[:77]) + "..."
		}
		created := strVal(c, "created_at")
		if len(created) >= 16 {
			created = created[:16]
		}
		parentID := strVal(c, "parent_id")
		if parentID == "" {
			parentID = "—"
		}
		rows = append(rows, []string{
			strVal(c, "id"),
			parentID,
			actors.actor(strVal(c, "author_type"), strVal(c, "author_id")),
			strVal(c, "type"),
			content,
			created,
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runIssueCommentAdd(cmd *cobra.Command, args []string) error {
	content, hasContent, err := resolveTextFlag(cmd, "content")
	if err != nil {
		return err
	}
	if !hasContent {
		return fmt.Errorf("--content, --content-stdin, or --content-file is required")
	}
	if err := guardLocalPathLinks(content, "comment body",
		"Deliver the file itself with `goosar issue comment add <issue-id> --attachment <path>` (repeatable) and drop the link."); err != nil {
		return err
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	timeout := cli.APITimeout()
	attachments, _ := cmd.Flags().GetStringSlice("attachment")
	if len(attachments) > 0 {
		timeout = cli.AtLeastAPITimeout(60 * time.Second)
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	issueRef, err := resolveIssueRef(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve issue: %w", err)
	}
	issueID := issueRef.ID

	pending, err := collectLocalAttachments(cmd, attachments)
	if err != nil {
		return err
	}
	var attachmentIDs []string
	for _, att := range pending {
		id, uploadErr := client.UploadFile(ctx, att.data, att.path, issueID)
		if uploadErr != nil {
			return fmt.Errorf("upload attachment %s: %w", att.path, uploadErr)
		}
		attachmentIDs = append(attachmentIDs, id)
		fmt.Fprintf(os.Stderr, "Uploaded %s\n", att.path)
	}

	body := map[string]any{"content": content}
	if parentID, _ := cmd.Flags().GetString("parent"); parentID != "" {
		body["parent_id"] = parentID
	}
	if len(attachmentIDs) > 0 {
		body["attachment_ids"] = attachmentIDs
	}
	var result map[string]any
	if err := client.PostJSON(ctx, "/api/issues/"+issueID+"/comments", body, &result); err != nil {
		return fmt.Errorf("add comment: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Comment added to issue %s.\n", issueRef.Display)

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		return nil
	}
	return cli.PrintJSON(os.Stdout, result)
}

func runIssueCommentDelete(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	if err := client.DeleteJSON(ctx, "/api/comments/"+args[0]); err != nil {
		return fmt.Errorf("delete comment: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Comment %s deleted.\n", args[0])
	return nil
}

func runIssueCommentResolve(cmd *cobra.Command, args []string) error {
	return runIssueCommentResolution(cmd, args[0], true)
}

func runIssueCommentUnresolve(cmd *cobra.Command, args []string) error {
	return runIssueCommentResolution(cmd, args[0], false)
}

func runIssueCommentResolution(cmd *cobra.Command, commentID string, resolve bool) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	path := "/api/comments/" + url.PathEscape(commentID) + "/resolve"
	var result map[string]any
	if resolve {
		if err := client.PostJSON(ctx, path, nil, &result); err != nil {
			return fmt.Errorf("resolve comment: %w", err)
		}
		fmt.Fprintf(os.Stderr, "Comment %s resolved.\n", commentID)
	} else {
		if err := client.DeleteJSONResponse(ctx, path, &result); err != nil {
			return fmt.Errorf("unresolve comment: %w", err)
		}
		fmt.Fprintf(os.Stderr, "Comment %s unresolved.\n", commentID)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		return nil
	}
	return cli.PrintJSON(os.Stdout, result)
}

func runIssueRuns(cmd *cobra.Command, args []string) error {
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

	var runs []map[string]any
	if err := client.GetJSON(ctx, "/api/issues/"+issueRef.ID+"/task-runs", &runs); err != nil {
		return fmt.Errorf("list runs: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, runs)
	}

	actors := loadActorDisplayLookup(ctx, client)
	fullID, _ := cmd.Flags().GetBool("full-id")
	headers := []string{"ID", "AGENT", "STATUS", "STARTED", "COMPLETED", "ERROR"}
	rows := make([][]string, 0, len(runs))
	for _, r := range runs {
		started := strVal(r, "started_at")
		if len(started) >= 16 {
			started = started[:16]
		}
		completed := strVal(r, "completed_at")
		if len(completed) >= 16 {
			completed = completed[:16]
		}
		errMsg := strVal(r, "error")
		if utf8.RuneCountInString(errMsg) > 50 {
			runes := []rune(errMsg)
			errMsg = string(runes[:47]) + "..."
		}
		rows = append(rows, []string{
			displayID(strVal(r, "id"), fullID),
			actors.agent(strVal(r, "agent_id")),
			strVal(r, "status"),
			started,
			completed,
			errMsg,
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runIssueUsage(cmd *cobra.Command, args []string) error {
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
	if err := client.GetJSON(ctx, "/api/issues/"+url.PathEscape(issueRef.ID)+"/usage", &result); err != nil {
		return fmt.Errorf("get issue usage: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}

	headers := []string{"INPUT_TOKENS", "OUTPUT_TOKENS", "CACHE_READ", "CACHE_WRITE", "RUNS"}
	rows := [][]string{{
		formatMetadataValue(result["total_input_tokens"]),
		formatMetadataValue(result["total_output_tokens"]),
		formatMetadataValue(result["total_cache_read_tokens"]),
		formatMetadataValue(result["total_cache_write_tokens"]),
		formatMetadataValue(result["task_count"]),
	}}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runIssueRunMessages(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	issueID := ""
	if issueInput, _ := cmd.Flags().GetString("issue"); issueInput != "" {
		issueRef, err := resolveIssueRef(ctx, client, issueInput)
		if err != nil {
			return fmt.Errorf("resolve issue: %w", err)
		}
		issueID = issueRef.ID
	}
	taskRef, err := resolveTaskRunID(ctx, client, issueID, args[0])
	if err != nil {
		return fmt.Errorf("resolve task run: %w", err)
	}

	path := "/api/tasks/" + url.PathEscape(taskRef.ID) + "/messages"
	if since, _ := cmd.Flags().GetInt("since"); since > 0 {
		path += fmt.Sprintf("?since=%d", since)
	}

	var messages []map[string]any
	if err := client.GetJSON(ctx, path, &messages); err != nil {
		return fmt.Errorf("list run messages: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, messages)
	}

	headers := []string{"SEQ", "TYPE", "TOOL", "CONTENT"}
	rows := make([][]string, 0, len(messages))
	for _, m := range messages {
		content := strVal(m, "content")
		if content == "" {
			content = strVal(m, "output")
		}
		if utf8.RuneCountInString(content) > 80 {
			runes := []rune(content)
			content = string(runes[:77]) + "..."
		}
		seq := ""
		if v, ok := m["seq"]; ok {
			seq = fmt.Sprintf("%v", v)
		}
		rows = append(rows, []string{
			seq,
			strVal(m, "type"),
			strVal(m, "tool"),
			content,
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runIssueRerun(cmd *cobra.Command, args []string) error {
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

	var задача map[string]any
	if err := client.PostJSON(ctx, "/api/issues/"+issueRef.ID+"/rerun", map[string]any{}, &задача); err != nil {
		return fmt.Errorf("rerun issue: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, задача)
	}
	agent := loadActorDisplayLookup(ctx, client).agent(strVal(задача, "agent_id"))
	fmt.Fprintf(os.Stdout, "Re-enqueued task %s on agent %s\n", strVal(задача, "id"), agent)
	return nil
}

func runIssueCancelTask(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	issueScope := ""
	if issueInput, _ := cmd.Flags().GetString("issue"); issueInput != "" {
		issueRef, err := resolveIssueRef(ctx, client, issueInput)
		if err != nil {
			return fmt.Errorf("resolve issue: %w", err)
		}
		issueScope = issueRef.ID
	}
	taskRef, err := resolveTaskRunID(ctx, client, issueScope, args[0])
	if err != nil {
		return fmt.Errorf("resolve task run: %w", err)
	}

	var result map[string]any
	path := "/api/tasks/" + url.PathEscape(taskRef.ID) + "/cancel"
	if err := client.PostJSON(ctx, path, map[string]any{}, &result); err != nil {
		return fmt.Errorf("cancel task: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	status := strVal(result, "status")
	if status == "" {
		status = "cancelled"
	}
	fmt.Fprintf(os.Stdout, "Task %s -> status=%s\n", taskRef.ID, status)
	return nil
}

func runIssueSearch(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	params := url.Values{}
	params.Set("q", args[0])
	if v, _ := cmd.Flags().GetInt("limit"); v > 0 {
		params.Set("limit", fmt.Sprintf("%d", v))
	}
	if v, _ := cmd.Flags().GetBool("include-closed"); v {
		params.Set("include_closed", "true")
	}

	path := "/api/issues/search?" + params.Encode()

	var result map[string]any
	if err := client.GetJSON(ctx, path, &result); err != nil {
		return fmt.Errorf("search issues: %w", err)
	}

	issuesRaw, _ := result["issues"].([]any)

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}

	headers := []string{"KEY", "TITLE", "STATUS", "MATCH"}
	rows := make([][]string, 0, len(issuesRaw))
	for _, raw := range issuesRaw {
		issue, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		matchInfo := strVal(issue, "match_source")
		if snippet := strVal(issue, "matched_snippet"); snippet != "" {
			if utf8.RuneCountInString(snippet) > 50 {
				runes := []rune(snippet)
				snippet = string(runes[:47]) + "..."
			}
			matchInfo += ": " + snippet
		}
		rows = append(rows, []string{
			strVal(issue, "identifier"),
			strVal(issue, "title"),
			strVal(issue, "status"),
			matchInfo,
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runIssueSubscriberList(cmd *cobra.Command, args []string) error {
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

	var subscribers []map[string]any
	if err := client.GetJSON(ctx, "/api/issues/"+issueRef.ID+"/subscribers", &subscribers); err != nil {
		return fmt.Errorf("list subscribers: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, subscribers)
	}

	actors := loadActorDisplayLookup(ctx, client)
	headers := []string{"USER", "REASON", "CREATED"}
	rows := make([][]string, 0, len(subscribers))
	for _, s := range subscribers {
		created := strVal(s, "created_at")
		if len(created) >= 16 {
			created = created[:16]
		}
		rows = append(rows, []string{
			actors.actor(strVal(s, "user_type"), strVal(s, "user_id")),
			strVal(s, "reason"),
			created,
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runIssueSubscriberAdd(cmd *cobra.Command, args []string) error {
	return runIssueSubscriberMutation(cmd, args[0], "subscribe")
}

func runIssueSubscriberRemove(cmd *cobra.Command, args []string) error {
	return runIssueSubscriberMutation(cmd, args[0], "unsubscribe")
}

func runIssueSubscriberMutation(cmd *cobra.Command, issueID, action string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	issueRef, err := resolveIssueRef(ctx, client, issueID)
	if err != nil {
		return fmt.Errorf("resolve issue: %w", err)
	}

	body := map[string]any{}
	userName, _ := cmd.Flags().GetString("user")
	uType, uID, hasUser, resolveErr := pickAssigneeFromFlags(ctx, client, cmd, "user", "user-id", memberOrAgentKinds)
	if resolveErr != nil {
		return fmt.Errorf("resolve user: %w", resolveErr)
	}
	if hasUser {
		body["user_type"] = uType
		body["user_id"] = uID
	}

	var result map[string]any
	path := "/api/issues/" + issueRef.ID + "/" + action
	if err := client.PostJSON(ctx, path, body, &result); err != nil {
		return fmt.Errorf("%s issue: %w", action, err)
	}

	target := "caller"
	if userName != "" {
		target = userName
	} else if hasUser {
		target = loadActorDisplayLookup(ctx, client).actor(uType, uID)
	}
	if action == "subscribe" {
		fmt.Fprintf(os.Stderr, "Subscribed %s to issue %s.\n", target, issueRef.Display)
	} else {
		fmt.Fprintf(os.Stderr, "Unsubscribed %s from issue %s.\n", target, issueRef.Display)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		return nil
	}
	return cli.PrintJSON(os.Stdout, result)
}

type assigneeMatch struct {
	Type string
	ID   string
	Name string
}

type assigneeKinds struct {
	member, agent, squad bool
}

var (
	issueAssigneeKinds = assigneeKinds{member: true, agent: true, squad: true}
	memberOrAgentKinds = assigneeKinds{member: true, agent: true}
)

var assigneeResolveRetrySleep = func(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return true
	case <-timer.C:
		return false
	}
}

func getAssigneeJSON(ctx context.Context, client *cli.APIClient, path string, out any) error {
	delays := []time.Duration{100 * time.Millisecond, 250 * time.Millisecond}
	var err error
	for attempt := 0; attempt <= len(delays); attempt++ {
		err = client.GetJSON(ctx, path, out)
		if err == nil || !isRetryableAssigneeResolveError(err) || attempt == len(delays) {
			return err
		}
		if assigneeResolveRetrySleep(ctx, delays[attempt]) {
			return ctx.Err()
		}
	}
	return err
}

func isRetryableAssigneeResolveError(err error) bool {
	var netErr *cli.NetworkError
	return errors.As(err, &netErr)
}

func (k assigneeKinds) describe() string {
	parts := make([]string, 0, 3)
	if k.member {
		parts = append(parts, "member")
	}
	if k.agent {
		parts = append(parts, "agent")
	}
	if k.squad {
		parts = append(parts, "squad")
	}
	switch len(parts) {
	case 0:
		return "<none>"
	case 1:
		return parts[0]
	case 2:
		return parts[0] + " or " + parts[1]
	default:
		return strings.Join(parts[:len(parts)-1], ", ") + ", or " + parts[len(parts)-1]
	}
}

func resolveAssignee(ctx context.Context, client *cli.APIClient, name string, kinds assigneeKinds) (string, string, error) {
	if client.WorkspaceID == "" {
		return "", "", fmt.Errorf("workspace ID is required to resolve assignees; use --workspace-id or set GOOSAR_WORKSPACE_ID")
	}

	input := normalizeAssigneeLookupInput(name)
	if input == "" {
		return "", "", fmt.Errorf("no %s found matching %q", kinds.describe(), name)
	}
	inputLower := strings.ToLower(input)

	var idMatches, exactMatches, substringMatches []assigneeMatch
	var errs []error
	var fetchAttempts int

	classify := func(entityType, id, displayName string) {
		match := assigneeMatch{Type: entityType, ID: id, Name: displayName}
		if id != "" && (strings.EqualFold(id, input) || strings.EqualFold(truncateID(id), input)) {
			idMatches = append(idMatches, match)
			return
		}
		if strings.EqualFold(displayName, input) {
			exactMatches = append(exactMatches, match)
			return
		}
		if strings.Contains(strings.ToLower(displayName), inputLower) {
			substringMatches = append(substringMatches, match)
		}
	}

	if kinds.member {
		fetchAttempts++
		var members []map[string]any
		if err := getAssigneeJSON(ctx, client, "/api/workspaces/"+client.WorkspaceID+"/members", &members); err != nil {
			errs = append(errs, fmt.Errorf("fetch members: %w", err))
		} else {
			for _, m := range members {
				classify("member", strVal(m, "user_id"), strVal(m, "name"))
			}
		}
	}

	if kinds.agent {
		fetchAttempts++
		var agents []map[string]any
		agentPath := "/api/agents?" + url.Values{"workspace_id": {client.WorkspaceID}}.Encode()
		if err := getAssigneeJSON(ctx, client, agentPath, &agents); err != nil {
			errs = append(errs, fmt.Errorf("fetch agents: %w", err))
		} else {
			for _, a := range agents {
				classify("agent", strVal(a, "id"), strVal(a, "name"))
			}
		}
	}

	if kinds.squad {
		fetchAttempts++
		var squads []map[string]any
		if err := getAssigneeJSON(ctx, client, "/api/squads", &squads); err != nil {
			errs = append(errs, fmt.Errorf("fetch squads: %w", err))
		} else {
			for _, s := range squads {
				if strVal(s, "archived_at") != "" {
					continue
				}
				classify("squad", strVal(s, "id"), strVal(s, "name"))
			}
		}
	}

	if fetchAttempts > 0 && len(errs) == fetchAttempts {
		msgs := make([]string, len(errs))
		for i, e := range errs {
			msgs[i] = e.Error()
		}
		return "", "", fmt.Errorf("failed to resolve assignee: %s", strings.Join(msgs, "; "))
	}

	for _, bucket := range [][]assigneeMatch{idMatches, exactMatches, substringMatches} {
		switch len(bucket) {
		case 0:
			continue
		case 1:
			return bucket[0].Type, bucket[0].ID, nil
		default:
			return "", "", ambiguousAssigneeError(input, bucket)
		}
	}
	return "", "", fmt.Errorf("no %s found matching %q", kinds.describe(), input)
}

func normalizeAssigneeLookupInput(raw string) string {
	input := strings.TrimSpace(raw)
	if m := util.MentionRe.FindStringSubmatch(input); len(m) == 4 && m[0] == input {
		switch m[2] {
		case "member", "agent", "squad":
			return m[3]
		}
	}
	input = strings.TrimLeftFunc(input, func(r rune) bool {
		return r == '@' || r == '＠'
	})
	return strings.TrimSpace(input)
}

func ambiguousAssigneeError(input string, matches []assigneeMatch) error {
	parts := make([]string, 0, len(matches))
	for _, m := range matches {
		parts = append(parts, fmt.Sprintf("  %s %q (%s)", m.Type, m.Name, truncateID(m.ID)))
	}
	return fmt.Errorf("ambiguous assignee %q; matches:\n%s", input, strings.Join(parts, "\n"))
}

func resolveAssigneeByID(ctx context.Context, client *cli.APIClient, id string, kinds assigneeKinds) (string, string, error) {
	if client.WorkspaceID == "" {
		return "", "", fmt.Errorf("workspace ID is required to resolve assignees; use --workspace-id or set GOOSAR_WORKSPACE_ID")
	}
	input := strings.TrimSpace(id)
	if !uuidRegexp.MatchString(input) {
		return "", "", fmt.Errorf("expected a canonical UUID, got %q", id)
	}

	var members []map[string]any
	var memberErr error
	if kinds.member {
		memberErr = getAssigneeJSON(ctx, client, "/api/workspaces/"+client.WorkspaceID+"/members", &members)
	}

	var agents []map[string]any
	var agentErr error
	if kinds.agent {
		agentPath := "/api/agents?" + url.Values{"workspace_id": {client.WorkspaceID}}.Encode()
		agentErr = getAssigneeJSON(ctx, client, agentPath, &agents)
	}

	var squads []map[string]any
	var squadErr error
	if kinds.squad {
		squadErr = getAssigneeJSON(ctx, client, "/api/squads", &squads)
	}

	allFailed := true
	hasFetch := false
	for _, pair := range []struct {
		enabled bool
		err     error
	}{{kinds.member, memberErr}, {kinds.agent, agentErr}, {kinds.squad, squadErr}} {
		if !pair.enabled {
			continue
		}
		hasFetch = true
		if pair.err == nil {
			allFailed = false
		}
	}
	if hasFetch && allFailed {
		return "", "", fmt.Errorf("failed to resolve assignee: %v; %v; %v", memberErr, agentErr, squadErr)
	}

	for _, m := range members {
		if strings.EqualFold(strVal(m, "user_id"), input) {
			return "member", strVal(m, "user_id"), nil
		}
	}
	for _, a := range agents {
		if strings.EqualFold(strVal(a, "id"), input) {
			return "agent", strVal(a, "id"), nil
		}
	}
	for _, s := range squads {
		if strings.EqualFold(strVal(s, "id"), input) {
			return "squad", strVal(s, "id"), nil
		}
	}

	return "", "", fmt.Errorf("no %s found with ID %q", kinds.describe(), input)
}

func pickAssigneeFromFlags(ctx context.Context, client *cli.APIClient, cmd *cobra.Command, nameFlag, idFlag string, kinds assigneeKinds) (string, string, bool, error) {
	nameSet := cmd.Flags().Changed(nameFlag)
	idSet := cmd.Flags().Changed(idFlag)
	if nameSet && idSet {
		return "", "", false, fmt.Errorf("--%s and --%s are mutually exclusive", nameFlag, idFlag)
	}
	if idSet {
		idVal, _ := cmd.Flags().GetString(idFlag)
		t, i, err := resolveAssigneeByID(ctx, client, idVal, kinds)
		if err != nil {
			return "", "", true, err
		}
		return t, i, true, nil
	}
	if nameSet {
		name, _ := cmd.Flags().GetString(nameFlag)
		t, i, err := resolveAssignee(ctx, client, name, kinds)
		if err != nil {
			return "", "", true, err
		}
		return t, i, true, nil
	}
	return "", "", false, nil
}

func formatAssignee(issue map[string]any, actors actorDisplayLookup) string {
	aType := strVal(issue, "assignee_type")
	aID := strVal(issue, "assignee_id")
	if aType == "" || aID == "" {
		return ""
	}
	return actors.actor(aType, aID)
}

func truncateID(id string) string {
	if utf8.RuneCountInString(id) > 8 {
		runes := []rune(id)
		return string(runes[:8])
	}
	return id
}
