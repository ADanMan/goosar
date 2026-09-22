// Пакет dataexport формирует машиночитаемый архив данных развёртывания и
// архив по субъекту (запрос по 152-ФЗ): gzip-tar с manifest.json, data/<сущность>.json
// и вложениями.
package dataexport

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const SchemaVersion = 1

const RedactedPlaceholder = "[REDACTED]"

type Querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

type ObjectStore interface {
	KeyFromURL(rawURL string) string
	GetReader(ctx context.Context, key string) (io.ReadCloser, error)
}

type Options struct {
	MaxBytes int64

	MaxAttachmentBytes int64
}

const DefaultMaxBytes int64 = 8 << 30

type Manifest struct {
	SchemaVersion int    `json:"schema_version"`
	Kind          string `json:"kind"`
	WorkspaceID   string `json:"workspace_id,omitempty"`
	UserID        string `json:"user_id,omitempty"`
	GeneratedAt   string `json:"generated_at"`

	Counts map[string]int64 `json:"counts"`

	Attachments AttachmentSummary `json:"attachments"`

	Redacted []string `json:"redacted"`

	Truncated bool `json:"truncated"`

	Notes []string `json:"notes"`
}

type AttachmentSummary struct {
	Count int64 `json:"count"`
	Bytes int64 `json:"bytes"`

	Skipped []string `json:"skipped"`
}

type tableSpec struct {
	name string

	from string

	drop []string

	scrub []string
}

var scrubKeyRe = regexp.MustCompile(`(?i)(token|secret|passwo?rd|api[-_]?key|apikey|credential|authorization|auth[-_]?header|cookie|private[-_]?key|access[-_]?key|bearer)`)

var workspaceTables = []tableSpec{
	{name: "workspace", from: `FROM workspace t WHERE t.id = $1`},
	{name: "members", from: `FROM member t WHERE t.workspace_id = $1`},
	{
		name: "users",

		from: `FROM "user" t WHERE t.id IN (SELECT m.user_id FROM member m WHERE m.workspace_id = $1)`,
		drop: []string{"onboarding_questionnaire", "cloud_waitlist_email", "cloud_waitlist_reason", "token_version"},
	},
	{name: "projects", from: `FROM project t WHERE t.workspace_id = $1`},
	{name: "issues", from: `FROM issue t WHERE t.workspace_id = $1`},
	{name: "comments", from: `FROM comment t WHERE t.workspace_id = $1`},
	{name: "issue_reactions", from: `FROM issue_reaction t WHERE t.workspace_id = $1`},
	{name: "comment_reactions", from: `FROM comment_reaction t WHERE t.workspace_id = $1`},
	{name: "labels", from: `FROM issue_label t WHERE t.workspace_id = $1`},
	{
		name: "issue_labels",
		from: `FROM issue_to_label t WHERE t.issue_id IN (SELECT i.id FROM issue i WHERE i.workspace_id = $1)`,
	},
	{name: "properties", from: `FROM issue_property t WHERE t.workspace_id = $1`},
	{
		name: "agents",
		from: `FROM agent t WHERE t.workspace_id = $1`,

		drop:  []string{"custom_env", "mcp_config"},
		scrub: []string{"runtime_config"},
	},
	{name: "autopilots", from: `FROM autopilot t WHERE t.workspace_id = $1`},
	{name: "squads", from: `FROM squad t WHERE t.workspace_id = $1`},
	{name: "skills", from: `FROM skill t WHERE t.workspace_id = $1`},
	{name: "chat_sessions", from: `FROM chat_session t WHERE t.workspace_id = $1`},
	{
		name: "chat_messages",
		from: `FROM chat_message t WHERE t.chat_session_id IN (SELECT s.id FROM chat_session s WHERE s.workspace_id = $1)`,
	},
	{
		name: "tasks",
		from: `FROM agent_task_queue t WHERE t.agent_id IN (SELECT a.id FROM agent a WHERE a.workspace_id = $1)`,
	},
	{
		name: "task_messages",
		from: `FROM task_message t WHERE t.task_id IN (
			SELECT q.id FROM agent_task_queue q
			JOIN agent a ON a.id = q.agent_id
			WHERE a.workspace_id = $1)`,
	},
	{name: "activity", from: `FROM activity_log t WHERE t.workspace_id = $1`},
	{name: "inbox", from: `FROM inbox_item t WHERE t.workspace_id = $1`},
	{
		name: "mcp_servers",

		from: `FROM workspace_mcp_server t WHERE t.workspace_id = $1`,
		drop: []string{"config"},
	},
	{name: "attachments", from: `FROM attachment t WHERE t.workspace_id = $1`},
	{name: "invitations", from: `FROM workspace_invitation t WHERE t.workspace_id = $1`},
	{name: "audit_auth", from: `FROM auth_audit t WHERE t.workspace_id = $1`},
	{
		name: "audit_admin",

		from: `FROM admin_audit t WHERE t.target_type = 'workspace' AND t.target_id = $1::text`,
	},
}

var userTables = []tableSpec{
	{
		name: "profile",
		from: `FROM "user" t WHERE t.id = $1`,
		drop: []string{"token_version"},
	},
	{name: "memberships", from: `FROM member t WHERE t.user_id = $1`},
	{
		name: "workspaces",
		from: `FROM workspace t WHERE t.id IN (SELECT m.workspace_id FROM member m WHERE m.user_id = $1)`,
	},
	{
		name: "issues_authored",
		from: `FROM issue t WHERE t.creator_type = 'member' AND t.creator_id = $1`,
	},
	{
		name: "issues_assigned",
		from: `FROM issue t WHERE t.assignee_type = 'member' AND t.assignee_id = $1`,
	},
	{
		name: "comments_authored",
		from: `FROM comment t WHERE t.author_type = 'member' AND t.author_id = $1`,
	},
	{
		name: "chat_sessions",
		from: `FROM chat_session t WHERE t.creator_id = $1`,
	},
	{
		name: "chat_messages",
		from: `FROM chat_message t WHERE t.chat_session_id IN (SELECT s.id FROM chat_session s WHERE s.creator_id = $1)`,
	},
	{
		name: "inbox",
		from: `FROM inbox_item t WHERE t.recipient_type = 'member' AND t.recipient_id = $1`,
	},
	{
		name: "notification_preferences",
		from: `FROM notification_preference t WHERE t.user_id = $1`,
	},
	{
		name: "access_tokens",

		from: `FROM personal_access_token t WHERE t.user_id = $1`,
		drop: []string{"token_hash"},
	},
	{
		name: "audit_auth",

		from: `FROM auth_audit t WHERE t.actor_id = $1::text`,
	},
	{
		name: "audit_admin",
		from: `FROM admin_audit t WHERE t.actor_user_id = $1`,
	},
	{
		name: "external_identities",

		from: `FROM (
			SELECT 'channel' AS provider, c.channel_user_id AS external_id, c.workspace_id, c.bound_at AS linked_at
			  FROM channel_user_binding c WHERE c.goosar_user_id = $1
		) t`,
	},
	{
		name: "feedback",
		from: `FROM feedback t WHERE t.user_id = $1`,
	},
	{

		name: "corporate_identities",
		from: `FROM user_identity t WHERE t.user_id = $1`,
	},
	{

		name: "mfa",
		from: `FROM user_mfa t WHERE t.user_id = $1`,
		drop: []string{"totp_secret_sealed"},
	},
	{
		name: "mfa_recovery_codes",
		from: `FROM user_mfa_recovery_code t WHERE t.user_id = $1`,
		drop: []string{"code_hash"},
	},
}

func WriteWorkspace(ctx context.Context, q Querier, store ObjectStore, workspaceID string, w io.Writer, opts Options) (Manifest, error) {
	m := Manifest{Kind: "workspace", WorkspaceID: workspaceID}
	return write(ctx, q, store, workspaceID, w, opts, workspaceTables, m,
		`SELECT t.id, t.url, t.filename FROM attachment t WHERE t.workspace_id = $1 ORDER BY t.created_at`,
		"MCP server credentials (workspace_mcp_user_credential) are never exported: they are sealed with GOOSAR_MCP_SECRET_KEY and are of no use outside this deployment.")
}

func WriteUser(ctx context.Context, q Querier, userID string, w io.Writer, opts Options) (Manifest, error) {
	m := Manifest{Kind: "user", UserID: userID}
	tables := append(append([]tableSpec{}, userTables...), tableSpec{
		name: "attachments_uploaded",
		from: `FROM attachment t WHERE t.uploader_type = 'member' AND t.uploader_id = $1`,
	})
	return write(ctx, q, nil, userID, w, opts, tables, m, "",
		"File contents are not included: they belong to the workspaces that hold them. Ask a workspace owner for the workspace export to obtain the bytes.")
}

func write(
	ctx context.Context,
	q Querier,
	store ObjectStore,
	scope string,
	w io.Writer,
	opts Options,
	tables []tableSpec,
	m Manifest,
	attachmentQuery string,
	notes ...string,
) (Manifest, error) {
	if opts.MaxBytes <= 0 {
		opts.MaxBytes = DefaultMaxBytes
	}
	m.SchemaVersion = SchemaVersion
	m.GeneratedAt = time.Now().UTC().Format(time.RFC3339)
	m.Counts = map[string]int64{}
	m.Redacted = []string{}
	m.Notes = append(m.Notes, notes...)
	m.Attachments.Skipped = []string{}

	counted := &countingWriter{w: w}
	gz := gzip.NewWriter(counted)
	tw := tar.NewWriter(gz)

	for _, spec := range tables {
		payload, count, err := readTable(ctx, q, scope, spec)
		if err != nil {
			return m, fmt.Errorf("dataexport: %s: %w", spec.name, err)
		}
		m.Counts[spec.name] = count
		for _, col := range spec.drop {
			m.Redacted = append(m.Redacted, spec.name+"."+col)
		}
		for _, col := range spec.scrub {
			m.Redacted = append(m.Redacted, spec.name+"."+col+" (credential-looking keys)")
		}
		if err := writeFile(tw, "data/"+spec.name+".json", payload); err != nil {
			return m, err
		}
		if counted.n > opts.MaxBytes {
			m.Truncated = true
			m.Notes = append(m.Notes, fmt.Sprintf("size limit of %d bytes reached while writing rows; the archive is INCOMPLETE", opts.MaxBytes))
			break
		}
	}

	if store != nil && attachmentQuery != "" && !m.Truncated {
		if err := copyAttachments(ctx, q, store, scope, tw, counted, opts, &m, attachmentQuery); err != nil {
			return m, err
		}
	}

	manifestJSON, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return m, fmt.Errorf("dataexport: manifest: %w", err)
	}
	if err := writeFile(tw, "manifest.json", manifestJSON); err != nil {
		return m, err
	}
	if err := tw.Close(); err != nil {
		return m, fmt.Errorf("dataexport: close tar: %w", err)
	}
	if err := gz.Close(); err != nil {
		return m, fmt.Errorf("dataexport: close gzip: %w", err)
	}
	return m, nil
}

func readTable(ctx context.Context, q Querier, scope string, spec tableSpec) ([]byte, int64, error) {

	expr := "to_jsonb(t)"
	for _, col := range spec.drop {
		expr += " - " + quoteLiteral(col)
	}
	rows, err := q.Query(ctx, "SELECT "+expr+" "+spec.from, scope)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	out := []json.RawMessage{}
	var count int64
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, 0, err
		}
		if len(spec.scrub) > 0 {
			raw, err = scrubColumns(raw, spec.scrub)
			if err != nil {
				return nil, 0, err
			}
		}
		out = append(out, json.RawMessage(raw))
		count++
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	payload, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return nil, 0, err
	}
	return payload, count, nil
}

func scrubColumns(raw []byte, columns []string) ([]byte, error) {
	var row map[string]json.RawMessage
	if err := json.Unmarshal(raw, &row); err != nil {

		return raw, nil
	}
	changed := false
	for _, col := range columns {
		value, ok := row[col]
		if !ok || len(value) == 0 {
			continue
		}
		var decoded any
		if err := json.Unmarshal(value, &decoded); err != nil {
			continue
		}
		encoded, err := json.Marshal(Scrub(decoded))
		if err != nil {
			return nil, err
		}
		row[col] = encoded
		changed = true
	}
	if !changed {
		return raw, nil
	}
	return json.Marshal(row)
}

func Scrub(v any) any {
	switch value := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(value))
		for key, item := range value {
			if scrubKeyRe.MatchString(key) {
				out[key] = RedactedPlaceholder
				continue
			}
			out[key] = Scrub(item)
		}
		return out
	case []any:
		out := make([]any, len(value))
		for i, item := range value {
			out[i] = Scrub(item)
		}
		return out
	default:
		return v
	}
}

func copyAttachments(
	ctx context.Context,
	q Querier,
	store ObjectStore,
	scope string,
	tw *tar.Writer,
	counted *countingWriter,
	opts Options,
	m *Manifest,
	query string,
) error {
	rows, err := q.Query(ctx, query, scope)
	if err != nil {
		return fmt.Errorf("dataexport: list attachments: %w", err)
	}
	type item struct{ id, url, filename string }
	var items []item
	for rows.Next() {
		var it item
		if err := rows.Scan(&it.id, &it.url, &it.filename); err != nil {
			rows.Close()
			return fmt.Errorf("dataexport: scan attachment: %w", err)
		}
		items = append(items, it)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("dataexport: list attachments: %w", err)
	}

	for _, it := range items {
		if counted.n > opts.MaxBytes {
			m.Truncated = true
			m.Notes = append(m.Notes, fmt.Sprintf("size limit of %d bytes reached; %s and later attachments are not in this archive", opts.MaxBytes, it.id))
			return nil
		}
		key := store.KeyFromURL(it.url)
		if key == "" {
			m.Attachments.Skipped = append(m.Attachments.Skipped, it.id+": url is not on this deployment's storage backend")
			continue
		}
		reader, err := store.GetReader(ctx, key)
		if err != nil {
			m.Attachments.Skipped = append(m.Attachments.Skipped, it.id+": "+err.Error())
			continue
		}
		body, err := io.ReadAll(readLimit(reader, opts.MaxAttachmentBytes))
		reader.Close()
		if err != nil {
			m.Attachments.Skipped = append(m.Attachments.Skipped, it.id+": "+err.Error())
			continue
		}
		if opts.MaxAttachmentBytes > 0 && int64(len(body)) > opts.MaxAttachmentBytes {
			m.Attachments.Skipped = append(m.Attachments.Skipped,
				fmt.Sprintf("%s: larger than the %d byte per-file limit", it.id, opts.MaxAttachmentBytes))
			continue
		}
		if err := writeFile(tw, "attachments/"+it.id+"-"+safeName(it.filename), body); err != nil {
			return err
		}
		m.Attachments.Count++
		m.Attachments.Bytes += int64(len(body))
	}
	return nil
}

func readLimit(r io.Reader, limit int64) io.Reader {
	if limit <= 0 {
		return r
	}
	return io.LimitReader(r, limit+1)
}

func writeFile(tw *tar.Writer, name string, body []byte) error {
	if err := tw.WriteHeader(&tar.Header{
		Name:    name,
		Mode:    0o600,
		Size:    int64(len(body)),
		ModTime: time.Now().UTC(),
	}); err != nil {
		return fmt.Errorf("dataexport: header %s: %w", name, err)
	}
	if _, err := tw.Write(body); err != nil {
		return fmt.Errorf("dataexport: write %s: %w", name, err)
	}
	return nil
}

func safeName(name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	name = path.Base(path.Clean("/" + name))
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == "/" {
		return "file"
	}
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return '_'
		}
		return r
	}, name)
	const maxName = 120
	if len(name) > maxName {
		name = name[:maxName]
	}
	return name
}

func quoteLiteral(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

type countingWriter struct {
	w io.Writer
	n int64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}

var ErrNoStorage = errors.New("dataexport: no storage backend configured")
