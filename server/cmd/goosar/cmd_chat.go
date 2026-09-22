package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/adanman/goosar/server/internal/cli"
)

var chatCmd = &cobra.Command{
	Use:   "chat",
	Short: "Работа с текущим чатом",
}

var chatHistoryCmd = &cobra.Command{
	Use:   "history",
	Short: "Обзор канала, в котором идёт этот разговор (сообщения и список тредов)",
	Long: `Показывает обзор чат-канала (например, Slack), в котором идёт этот разговор:
последние сообщения верхнего уровня и для каждого треда — thread_id, reply_count
и latest_reply. Содержимое тредов НЕ раскрывается — это оглавление.

Чтобы прочитать сообщения конкретного треда, возьмите отсюда thread_id и выполните
"goosar chat thread <thread_id>".

Команда одна и та же для любого канала, из которого пришёл разговор, и читает
только тот разговор, для которого вы сейчас запущены, — другие сессии и каналы
ей недоступны.`,
	Args: cobra.NoArgs,
	RunE: runChatHistory,
}

var chatThreadCmd = &cobra.Command{
	Use:   "thread [id]",
	Short: "Прочитать сообщения одного треда (текущего или по id)",
	Long: `Читает сообщения одного треда.

Без id читает тред, в котором вы сейчас находитесь (тот, где вас упомянули через @).
С id — thread_id из "goosar chat history" — читает этот тред.
В обоих случаях тред относится к вашему каналу; читать другой канал нельзя.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runChatThread,
}

func init() {
	for _, c := range []*cobra.Command{chatHistoryCmd, chatThreadCmd} {
		c.Flags().Int("limit", 0, "Максимальное число сообщений в ответе (сервер ограничивает диапазон)")
		c.Flags().String("before", "", "Непрозрачный курсор (next_cursor с предыдущей страницы), чтобы прочитать более старые сообщения")
		c.Flags().String("output", "json", "Формат вывода: table или json")
	}
	chatCmd.AddCommand(chatHistoryCmd)
	chatCmd.AddCommand(chatThreadCmd)
}

func runChatHistory(cmd *cobra.Command, _ []string) error {
	resp, err := fetchChatRead(cmd, "/api/chat/history", "")
	if err != nil {
		return err
	}
	return renderChatRead(cmd, resp, true)
}

func runChatThread(cmd *cobra.Command, args []string) error {
	threadID := ""
	if len(args) == 1 {
		threadID = args[0]
	}
	resp, err := fetchChatRead(cmd, "/api/chat/thread", threadID)
	if err != nil {
		return err
	}
	return renderChatRead(cmd, resp, false)
}

func fetchChatRead(cmd *cobra.Command, basePath, threadID string) (map[string]any, error) {
	client, err := newAPIClient(cmd)
	if err != nil {
		return nil, err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	limit, _ := cmd.Flags().GetInt("limit")
	before, _ := cmd.Flags().GetString("before")

	q := url.Values{}
	if threadID != "" {
		q.Set("id", threadID)
	}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	if before != "" {
		q.Set("before", before)
	}
	path := basePath
	if encoded := q.Encode(); encoded != "" {
		path += "?" + encoded
	}

	var resp map[string]any
	if err := client.GetJSON(ctx, path, &resp); err != nil {
		return nil, fmt.Errorf("read chat: %w", err)
	}
	return resp, nil
}

func renderChatRead(cmd *cobra.Command, resp map[string]any, overview bool) error {
	output, _ := cmd.Flags().GetString("output")
	if output != "table" {
		return cli.PrintJSON(os.Stdout, resp)
	}
	if note := strVal(resp, "note"); note != "" {
		fmt.Fprintln(os.Stdout, note)
		return nil
	}
	msgs, _ := resp["messages"].([]any)
	var headers []string
	if overview {
		headers = []string{"TS", "ROLE", "AUTHOR", "THREAD_ID", "REPLIES", "TEXT"}
	} else {
		headers = []string{"TS", "ROLE", "AUTHOR", "TEXT"}
	}
	rows := make([][]string, 0, len(msgs))
	for _, mi := range msgs {
		m, ok := mi.(map[string]any)
		if !ok {
			continue
		}
		if overview {
			rows = append(rows, []string{strVal(m, "ts"), strVal(m, "role"), strVal(m, "author"), strVal(m, "thread_id"), numVal(m, "reply_count"), strVal(m, "text")})
		} else {
			rows = append(rows, []string{strVal(m, "ts"), strVal(m, "role"), strVal(m, "author"), strVal(m, "text")})
		}
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func numVal(m map[string]any, key string) string {
	if v, ok := m[key].(float64); ok && v != 0 {
		return strconv.Itoa(int(v))
	}
	return ""
}
