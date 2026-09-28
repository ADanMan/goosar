// Правка T-028: daemonReportTaskMessages/daemonListTaskMessages — стенограмма
// запуска (dispatch_messages), запись и чтение "с since" вдобавок к
// MessagesForJob (T-027, store.go, читает всё целиком).
package dispatch

import (
	"context"
	"encoding/json"
	"fmt"
)

// MessageInput — одна запись TaskMessageInput контракта.
type MessageInput struct {
	Seq     int
	Kind    string // text/thinking/tool_use/tool_result/error
	Tool    string
	Content string
	Input   json.RawMessage
	Output  string
}

// AppendMessages вставляет сообщения транскрипта одно за другим (dm_seq —
// собственный порядковый номер запуска, присланный демоном; UNIQUE
// (dispatch_job_id, dm_seq) делает повторную доставку идемпотентной —
// повтор той же (job, seq) просто не вставляется второй раз). Возвращает
// сохранённые строки в виде Message (для последующей публикации task:message
// вызывающим доменом — internal/daemon, у него есть Publisher, у Store нет).
func (s *Store) AppendMessages(ctx context.Context, q Querier, jobID string, in []MessageInput) ([]Message, error) {
	var out []Message
	for _, m := range in {
		if m.Kind == "" {
			continue
		}
		row := q.QueryRow(ctx, `
			INSERT INTO dispatch_messages (dispatch_job_id, dm_seq, dm_kind, dm_tool, dm_body, dm_input, dm_output)
			VALUES ($1, $2, $3, NULLIF($4,''), NULLIF($5,''), $6, NULLIF($7,''))
			ON CONFLICT (dispatch_job_id, dm_seq) DO NOTHING
			RETURNING id, dispatch_job_id, dm_seq, dm_kind, dm_tool, dm_body, dm_input, dm_output, created_at`,
			jobID, m.Seq, m.Kind, m.Tool, m.Content, nullableJSON(m.Input), m.Output)
		var msg Message
		var input []byte
		if err := row.Scan(&msg.ID, &msg.JobID, &msg.Seq, &msg.Kind, &msg.Tool, &msg.Body, &input, &msg.Output, &msg.CreatedAt); err != nil {
			continue // ON CONFLICT DO NOTHING не возвращает строку — обычный путь для дублей, не ошибка
		}
		if len(input) > 0 {
			msg.Input = input
		}
		out = append(out, msg)
	}
	return out, nil
}

// ListMessagesSince — то же, что MessagesForJob, но только seq > since
// (GET /api/daemon/tasks/{taskId}/messages?since=).
func (s *Store) ListMessagesSince(ctx context.Context, q Querier, jobID string, since int) ([]Message, error) {
	rows, err := q.Query(ctx, `
		SELECT id, dispatch_job_id, dm_seq, dm_kind, dm_tool, dm_body, dm_input, dm_output, created_at
		FROM dispatch_messages WHERE dispatch_job_id = $1 AND dm_seq > $2 ORDER BY dm_seq`, jobID, since)
	if err != nil {
		return nil, fmt.Errorf("dispatch: стенограмма запуска (since): %w", err)
	}
	defer rows.Close()
	var out []Message
	for rows.Next() {
		var m Message
		var input []byte
		if err := rows.Scan(&m.ID, &m.JobID, &m.Seq, &m.Kind, &m.Tool, &m.Body, &input, &m.Output, &m.CreatedAt); err != nil {
			return nil, err
		}
		if len(input) > 0 {
			m.Input = input
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// --- построчный расход токенов/стоимости (daemonReportTaskUsage) -----------
//
// dispatch_usage, аддитивно к T-027 (store.go задаёт только чтение
// UsageForTicket).

// UsageEntry — одна строка TaskUsageEntry контракта.
type UsageEntry struct {
	Provider         string
	Model            string
	InputTokens      int64
	OutputTokens     int64
	CacheReadTokens  int64
	CacheWriteTokens int64
	CostUSDTicks     int64
}

// AppendUsage вставляет каждую валидную строку (Model непустой) отдельным
// INSERT — контракт требует, чтобы некорректные строки пропускались без
// ошибки всего запроса, поэтому одна неудачная вставка не откатывает
// остальные (в отличие от одного batch-INSERT в общей транзакции).
// Возвращает число сохранённых строк.
func (s *Store) AppendUsage(ctx context.Context, q Querier, jobID string, entries []UsageEntry) (saved int, err error) {
	for _, e := range entries {
		if e.Model == "" {
			continue
		}
		_, execErr := q.Exec(ctx, `
			INSERT INTO dispatch_usage (
				dispatch_job_id, du_provider, du_model, du_input_tokens, du_output_tokens,
				du_cache_read_tokens, du_cache_write_tokens, du_cost_usd_ticks
			) VALUES ($1, NULLIF($2,''), $3, $4, $5, $6, $7, $8)`,
			jobID, e.Provider, e.Model, e.InputTokens, e.OutputTokens,
			e.CacheReadTokens, e.CacheWriteTokens, e.CostUSDTicks)
		if execErr != nil {
			continue // отдельная строка не прошла — контракт требует пропустить, не проваливать весь запрос
		}
		saved++
	}
	return saved, nil
}
