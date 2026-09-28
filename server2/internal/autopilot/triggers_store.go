package autopilot

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/adanman/goosar/server2/internal/store"
)

var ErrTriggerNotFound = errors.New("autopilot: триггер не найден")

// ErrNotWebhookTrigger — rotate-webhook-token/signing-secret на триггере,
// чей kind != webhook.
var ErrNotWebhookTrigger = errors.New("autopilot: триггер не является webhook-триггером")

// strig_webhook_path (открытый токен) удалена миграцией 400 (T-029 доводка,
// см. server2/docs/decisions.md): маршрутизация входящего вебхука теперь по
// strig_webhook_token_digest, поэтому колонка не читается ни здесь, ни в
// triggerByWebhookToken ниже — Trigger.WebhookPath заполняется только
// вручную, в CreateTrigger/RotateWebhookToken, тем же секретом, что и
// PlainToken (тот же принцип: виден только в ответе create/rotate).
const triggerColumns = `id, sentinel_id, strig_kind, strig_enabled, strig_cron_expression, strig_timezone,
	strig_next_run_at, strig_provider, strig_signing_secret_sealed IS NOT NULL,
	strig_label, strig_last_fired_at, strig_event_filters, created_at, updated_at`

func scanTrigger(row pgx.Row) (Trigger, error) {
	var t Trigger
	var filters []byte
	if err := row.Scan(&t.ID, &t.AutopilotID, &t.Kind, &t.Enabled, &t.CronExpression, &t.Timezone,
		&t.NextRunAt, &t.Provider, &t.HasSigningSecret,
		&t.Label, &t.LastFiredAt, &filters, &t.CreatedAt, &t.UpdatedAt); err != nil {
		return Trigger{}, err
	}
	if len(filters) > 0 {
		t.EventFilters = filters
	}
	if t.HasSigningSecret {
		hint := "••••"
		t.SigningSecretHint = &hint
	}
	return t, nil
}

// CreateTriggerParams — вход CreateTrigger (createAutopilotTrigger).
type CreateTriggerParams struct {
	Kind           string
	CronExpression *string
	Timezone       *string
	Label          *string
	Provider       *string
	EventFilters   []byte // json.RawMessage, "[]" если не задано
}

// CreateTrigger вставляет sentinel_triggers; kind=schedule требует
// cron_expression валидный (уже провалидирован обработчиком) и сразу
// вычисляет next_run_at; kind=webhook генерирует уникальный токен (до 3
// попыток при коллизии strig_webhook_path — contract:
// rotateAutopilotTriggerWebhookToken тоже "до 3 раз при коллизии", здесь то
// же правило применено и к первому созданию).
func (s *Store) CreateTrigger(ctx context.Context, autopilotID string, p CreateTriggerParams) (Trigger, error) {
	var nextRun *time.Time
	if p.Kind == "schedule" {
		loc, err := LoadTimezone(strOr(p.Timezone, ""))
		if err != nil {
			return Trigger{}, err
		}
		sched, err := ParseCron(strOr(p.CronExpression, ""))
		if err != nil {
			return Trigger{}, err
		}
		if next, ok := sched.NextInLocation(time.Now().UTC(), loc); ok {
			nextRun = &next
		}
	}
	filters := p.EventFilters
	if len(filters) == 0 {
		filters = []byte(`[]`)
	}

	var token, path string
	var digest []byte
	if p.Kind == "webhook" {
		var err error
		token, path, digest, err = generateWebhookToken()
		if err != nil {
			return Trigger{}, err
		}
	}

	// strig_webhook_token_digest — единственное, что попадает в БД (T-029
	// доводка, миграция 400): маршрутизация входящего вебхука и уникальность
	// — по нему, открытый токен (path) нигде не сохраняется, только
	// возвращается вызывающему один раз (см. ниже, t.WebhookPath/PlainToken).
	const maxAttempts = 3
	var id string
	var err error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		row := s.db.Pool.QueryRow(ctx, `
			INSERT INTO sentinel_triggers (
				sentinel_id, strig_kind, strig_cron_expression, strig_timezone, strig_next_run_at,
				strig_webhook_token_digest, strig_provider, strig_label, strig_event_filters
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING id`,
			autopilotID, p.Kind, p.CronExpression, p.Timezone, nextRun,
			nullableDigest(digest), p.Provider, p.Label, filters)
		if err = row.Scan(&id); err == nil {
			break
		}
		if p.Kind == "webhook" && store.IsUniqueViolation(err) {
			token, path, digest, err = generateWebhookToken()
			if err != nil {
				return Trigger{}, err
			}
			continue
		}
		return Trigger{}, fmt.Errorf("autopilot: создание триггера: %w", err)
	}
	if err != nil {
		return Trigger{}, fmt.Errorf("autopilot: создание триггера (коллизия дайджеста токена): %w", err)
	}

	t, err := s.GetTrigger(ctx, autopilotID, id)
	if err != nil {
		return Trigger{}, err
	}
	if token != "" {
		t.PlainToken = token
		t.WebhookPath = &path
	}
	return t, nil
}

func strOr(p *string, def string) string {
	if p == nil {
		return def
	}
	return *p
}

func nullableString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// generateWebhookToken создаёт новый секрет `awt_<32 случайных байта в
// base64url>`; strig_webhook_path хранит его как есть (это и есть
// маршрутизирующий ключ для публичного POST /api/webhooks/autopilots/{token} —
// без сохранения токена в открытом виде у сервера не было бы способа найти
// строку по входящему запросу без полного перебора, см.
// server2/docs/decisions.md, раздел T-028).
func generateWebhookToken() (token, path string, digest []byte, err error) {
	buf := make([]byte, 32)
	if _, err = rand.Read(buf); err != nil {
		return "", "", nil, fmt.Errorf("autopilot: генерация токена вебхука: %w", err)
	}
	token = "awt_" + base64.RawURLEncoding.EncodeToString(buf)
	sum := sha256.Sum256([]byte(token))
	return token, token, sum[:], nil
}

func nullableDigest(digest []byte) *string {
	if len(digest) == 0 {
		return nil
	}
	hexDigest := hex.EncodeToString(digest)
	return &hexDigest
}

func (s *Store) GetTrigger(ctx context.Context, autopilotID, triggerID string) (Trigger, error) {
	row := s.db.Pool.QueryRow(ctx, `SELECT `+triggerColumns+` FROM sentinel_triggers
		WHERE sentinel_id = $1 AND id = $2`, autopilotID, triggerID)
	t, err := scanTrigger(row)
	if store.IsNoRows(err) {
		return Trigger{}, ErrTriggerNotFound
	}
	if err != nil {
		return Trigger{}, fmt.Errorf("autopilot: получение триггера: %w", err)
	}
	return t, nil
}

// ListTriggers — все триггеры автопилота (getAutopilot: "triggers": [...]).
func (s *Store) ListTriggers(ctx context.Context, autopilotID string) ([]Trigger, error) {
	rows, err := s.db.Pool.Query(ctx, `SELECT `+triggerColumns+` FROM sentinel_triggers
		WHERE sentinel_id = $1 ORDER BY created_at`, autopilotID)
	if err != nil {
		return nil, fmt.Errorf("autopilot: список триггеров: %w", err)
	}
	defer rows.Close()
	var out []Trigger
	for rows.Next() {
		t, err := scanTrigger(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// TriggerUpdatePatch — вход UpdateTrigger (updateAutopilotTrigger).
type TriggerUpdatePatch struct {
	Enabled  *bool
	CronExpr *string
	Timezone *string
	Label    *string
	Filters  []byte // nil — не менять
}

// UpdateTrigger применяет патч; при изменении cron_expression/timezone
// пересчитывает strig_next_run_at (contract: "изменить триггер... расписание").
func (s *Store) UpdateTrigger(ctx context.Context, autopilotID, triggerID string, p TriggerUpdatePatch) (Trigger, error) {
	current, err := s.GetTrigger(ctx, autopilotID, triggerID)
	if err != nil {
		return Trigger{}, err
	}
	cronExpr, tz := current.CronExpression, current.Timezone
	recompute := false
	if p.CronExpr != nil {
		cronExpr = p.CronExpr
		recompute = true
	}
	if p.Timezone != nil {
		tz = p.Timezone
		recompute = true
	}
	var nextRun *time.Time = current.NextRunAt
	if recompute && current.Kind == "schedule" {
		loc, err := LoadTimezone(strOr(tz, ""))
		if err != nil {
			return Trigger{}, err
		}
		sched, err := ParseCron(strOr(cronExpr, ""))
		if err != nil {
			return Trigger{}, err
		}
		if next, ok := sched.NextInLocation(time.Now().UTC(), loc); ok {
			nextRun = &next
		} else {
			nextRun = nil
		}
	}

	setClauses, args := []string{}, []any{autopilotID, triggerID}
	add := func(col string, val any) {
		args = append(args, val)
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", col, len(args)))
	}
	if p.Enabled != nil {
		add("strig_enabled", *p.Enabled)
	}
	if p.CronExpr != nil {
		add("strig_cron_expression", *p.CronExpr)
	}
	if p.Timezone != nil {
		add("strig_timezone", *p.Timezone)
	}
	if p.Label != nil {
		add("strig_label", *p.Label)
	}
	if p.Filters != nil {
		add("strig_event_filters", p.Filters)
	}
	if recompute {
		add("strig_next_run_at", nextRun)
	}
	if len(setClauses) == 0 {
		return current, nil
	}
	query := "UPDATE sentinel_triggers SET " + joinClauses(setClauses) + ", updated_at = now() WHERE sentinel_id = $1 AND id = $2"
	tag, err := s.db.Pool.Exec(ctx, query, args...)
	if err != nil {
		return Trigger{}, fmt.Errorf("autopilot: изменение триггера: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return Trigger{}, ErrTriggerNotFound
	}
	return s.GetTrigger(ctx, autopilotID, triggerID)
}

func (s *Store) DeleteTrigger(ctx context.Context, autopilotID, triggerID string) error {
	tag, err := s.db.Pool.Exec(ctx, `DELETE FROM sentinel_triggers WHERE sentinel_id = $1 AND id = $2`, autopilotID, triggerID)
	if err != nil {
		return fmt.Errorf("autopilot: удаление триггера: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrTriggerNotFound
	}
	return nil
}

// RotateWebhookToken перевыпускает strig_webhook_token_digest (старый токен
// сразу недействителен — уникальный индекс не позволяет двум строкам делить
// один дайджест, и старое значение просто перезаписывается); открытый
// токен, как и при создании (T-029 доводка, миграция 400), в БД не попадает
// вовсе — возвращается вызывающему только этим ответом.
func (s *Store) RotateWebhookToken(ctx context.Context, autopilotID, triggerID string) (Trigger, error) {
	current, err := s.GetTrigger(ctx, autopilotID, triggerID)
	if err != nil {
		return Trigger{}, err
	}
	if current.Kind != "webhook" {
		return Trigger{}, ErrNotWebhookTrigger
	}
	const maxAttempts = 3
	var token, path string
	for attempt := 0; attempt < maxAttempts; attempt++ {
		var digest []byte
		var genErr error
		token, path, digest, genErr = generateWebhookToken()
		if genErr != nil {
			return Trigger{}, genErr
		}
		tag, err := s.db.Pool.Exec(ctx, `
			UPDATE sentinel_triggers SET strig_webhook_token_digest = $3, updated_at = now()
			WHERE sentinel_id = $1 AND id = $2`, autopilotID, triggerID, nullableDigest(digest))
		if err == nil {
			if tag.RowsAffected() == 0 {
				return Trigger{}, ErrTriggerNotFound
			}
			break
		}
		if !store.IsUniqueViolation(err) {
			return Trigger{}, fmt.Errorf("autopilot: ротация токена вебхука: %w", err)
		}
	}
	t, err := s.GetTrigger(ctx, autopilotID, triggerID)
	if err != nil {
		return Trigger{}, err
	}
	t.PlainToken = token
	t.WebhookPath = &path
	return t, nil
}

// SetSigningSecret — setAutopilotTriggerSigningSecret. secret=="" снимает
// секрет; иначе запечатывает его AES-GCM(GOOSAR_MCP_SECRET_KEY) в
// strig_signing_secret_sealed (см. crypto.go).
func (s *Store) SetSigningSecret(ctx context.Context, autopilotID, triggerID, mcpKey, secret string) (Trigger, error) {
	current, err := s.GetTrigger(ctx, autopilotID, triggerID)
	if err != nil {
		return Trigger{}, err
	}
	if current.Kind != "webhook" {
		return Trigger{}, ErrNotWebhookTrigger
	}
	var sealed []byte
	if secret != "" {
		sealed, err = sealSecret(mcpKey, secret)
		if err != nil {
			return Trigger{}, err
		}
	}
	tag, err := s.db.Pool.Exec(ctx, `
		UPDATE sentinel_triggers SET strig_signing_secret_sealed = $3, updated_at = now()
		WHERE sentinel_id = $1 AND id = $2`, autopilotID, triggerID, sealed)
	if err != nil {
		return Trigger{}, fmt.Errorf("autopilot: установка секрета подписи: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return Trigger{}, ErrTriggerNotFound
	}
	return s.GetTrigger(ctx, autopilotID, triggerID)
}

// signingSecretFor возвращает расшифрованный секрет подписи триггера, если
// он задан (для проверки HMAC входящего вебхука, см. handlers_webhook.go).
// mcpKeyPrev — GOOSAR_MCP_SECRET_KEY_PREVIOUS: секрет, запечатанный ещё
// старым ключом (до `goosar_admin rotate-secrets`), должен по-прежнему
// проверяться, не отказывать всем входящим вебхукам сразу после ротации.
func (s *Store) signingSecretFor(ctx context.Context, triggerID, mcpKey, mcpKeyPrev string) (string, bool, error) {
	var sealed []byte
	err := s.db.Pool.QueryRow(ctx, `SELECT strig_signing_secret_sealed FROM sentinel_triggers WHERE id = $1`, triggerID).Scan(&sealed)
	if err != nil {
		return "", false, fmt.Errorf("autopilot: чтение секрета подписи: %w", err)
	}
	if len(sealed) == 0 {
		return "", false, nil
	}
	secret, err := openSecret(mcpKey, mcpKeyPrev, sealed)
	if err != nil {
		return "", false, err
	}
	return secret, true, nil
}

// triggerByWebhookPath — резолв публичного {token} на строку триггера, её
// автопилот и воркспейс, одним запросом (webhooksReceiveAutopilotTrigger).
type resolvedTrigger struct {
	Trigger
	SentinelWorkspaceID string
	SentinelStatus      string
}

// triggerByWebhookToken резолвит входящий публичный {token} на строку
// триггера по sha256-дайджесту (T-029 доводка, миграция 400) — сервер больше
// не хранит и не ищет по открытому токену.
func (s *Store) triggerByWebhookToken(ctx context.Context, token string) (resolvedTrigger, error) {
	digest := sha256.Sum256([]byte(token))
	row := s.db.Pool.QueryRow(ctx, `
		SELECT `+prefixColumns("st", triggerColumns)+`, s.workspace_id, s.sen_status
		FROM sentinel_triggers st JOIN sentinels s ON s.id = st.sentinel_id
		WHERE st.strig_webhook_token_digest = $1 AND st.strig_kind = 'webhook'`, hex.EncodeToString(digest[:]))
	var rt resolvedTrigger
	var filters []byte
	if err := row.Scan(&rt.ID, &rt.AutopilotID, &rt.Kind, &rt.Enabled, &rt.CronExpression, &rt.Timezone,
		&rt.NextRunAt, &rt.Provider, &rt.HasSigningSecret,
		&rt.Label, &rt.LastFiredAt, &filters, &rt.CreatedAt, &rt.UpdatedAt,
		&rt.SentinelWorkspaceID, &rt.SentinelStatus); err != nil {
		if store.IsNoRows(err) {
			return resolvedTrigger{}, ErrTriggerNotFound
		}
		return resolvedTrigger{}, fmt.Errorf("autopilot: резолв токена вебхука: %w", err)
	}
	if len(filters) > 0 {
		rt.EventFilters = filters
	}
	return rt, nil
}

// prefixColumns добавляет "alias." перед каждой колонкой из запятую-разделённого
// списка columns — нужно, когда triggerColumns переиспользуется в запросе с
// JOIN (тот же приём для того же случая, что chat.prefixColumns/splitColumns,
// внутренний хелпер этого файла, не общий SQL-пакет).
func prefixColumns(alias, cols string) string {
	out := ""
	first := true
	for _, c := range strings.Split(cols, ",") {
		if !first {
			out += ", "
		}
		out += alias + "." + strings.TrimSpace(c)
		first = false
	}
	return out
}
