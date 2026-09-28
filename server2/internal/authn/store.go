package authn

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/adanman/goosar/server2/internal/store"
)

// ErrNotFound — строка не найдена (учётная запись, сессия, токен, ...).
var ErrNotFound = errors.New("authn: не найдено")

// Account — строка accounts, минимальный набор полей, нужный этому пакету.
// Схема User целиком собирается доменом identity — authn работает только со
// столбцами, нужными для входа/сессий.
type Account struct {
	ID          string
	Email       string
	Name        string
	TokenEpoch  int
}

// Store — доступ к таблицам identity (001_identity.up.sql).
type Store struct {
	db *store.Store
}

func NewStore(db *store.Store) *Store { return &Store{db: db} }

// FindAccountByEmail ищет учётную запись по email (регистронезависимо).
func (s *Store) FindAccountByEmail(ctx context.Context, email string) (Account, error) {
	var a Account
	err := s.db.Pool.QueryRow(ctx, `
		SELECT id, acct_email, acct_full_name, acct_token_epoch
		FROM accounts WHERE lower(acct_email) = lower($1)`, email,
	).Scan(&a.ID, &a.Email, &a.Name, &a.TokenEpoch)
	if errors.Is(err, pgx.ErrNoRows) {
		return Account{}, ErrNotFound
	}
	if err != nil {
		return Account{}, fmt.Errorf("authn: поиск аккаунта по email: %w", err)
	}
	return a, nil
}

// FindAccountByID — то же по id.
func (s *Store) FindAccountByID(ctx context.Context, id string) (Account, error) {
	var a Account
	err := s.db.Pool.QueryRow(ctx, `
		SELECT id, acct_email, acct_full_name, acct_token_epoch
		FROM accounts WHERE id = $1`, id,
	).Scan(&a.ID, &a.Email, &a.Name, &a.TokenEpoch)
	if errors.Is(err, pgx.ErrNoRows) {
		return Account{}, ErrNotFound
	}
	if err != nil {
		return Account{}, fmt.Errorf("authn: поиск аккаунта по id: %w", err)
	}
	return a, nil
}

// CreateAccount создаёт учётную запись и её email login binding в одной
// транзакции (используется только когда ALLOW_SIGNUP=true).
func (s *Store) CreateAccount(ctx context.Context, email, name string) (Account, error) {
	var a Account
	err := s.db.WithTx(ctx, func(tx pgx.Tx) error {
		row := tx.QueryRow(ctx, `
			INSERT INTO accounts (acct_email, acct_full_name)
			VALUES ($1, $2)
			RETURNING id, acct_email, acct_full_name, acct_token_epoch`, email, name)
		if err := row.Scan(&a.ID, &a.Email, &a.Name, &a.TokenEpoch); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO auth_bindings (account_id, ab_method, ab_external_email)
			VALUES ($1, 'email', $2)
			ON CONFLICT (account_id, ab_method) DO NOTHING`, a.ID, email)
		return err
	})
	if err != nil {
		if store.IsUniqueViolation(err) {
			// гонка: параллельный запрос уже создал аккаунт с этим email.
			return s.FindAccountByEmail(ctx, email)
		}
		return Account{}, fmt.Errorf("authn: создание аккаунта: %w", err)
	}
	return a, nil
}

// BumpTokenEpoch увеличивает acct_token_epoch (используется authRevokeAllSessions,
// чтобы все ранее выпущенные JWT сразу перестали проходить проверку tv).
func (s *Store) BumpTokenEpoch(ctx context.Context, accountID string) (int, error) {
	var epoch int
	err := s.db.Pool.QueryRow(ctx, `
		UPDATE accounts SET acct_token_epoch = acct_token_epoch + 1, updated_at = now()
		WHERE id = $1 RETURNING acct_token_epoch`, accountID).Scan(&epoch)
	if err != nil {
		return 0, fmt.Errorf("authn: bump token epoch: %w", err)
	}
	return epoch, nil
}

// --- login codes -----------------------------------------------------------

// LastCodeSentAt возвращает время последнего невыданного кода для email
// (для троттлинга "1 код в 60с на email").
func (s *Store) LastCodeSentAt(ctx context.Context, email, purpose string) (time.Time, bool, error) {
	var t time.Time
	err := s.db.Pool.QueryRow(ctx, `
		SELECT created_at FROM login_codes
		WHERE lower(lc_email) = lower($1) AND lc_purpose = $2
		ORDER BY created_at DESC LIMIT 1`, email, purpose).Scan(&t)
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, fmt.Errorf("authn: чтение последнего кода: %w", err)
	}
	return t, true, nil
}

// StoreCode сохраняет отпечаток кода с TTL.
func (s *Store) StoreCode(ctx context.Context, email, code, purpose string, ttl time.Duration) error {
	_, err := s.db.Pool.Exec(ctx, `
		INSERT INTO login_codes (lc_email, lc_code_digest, lc_purpose, lc_valid_until)
		VALUES ($1, $2, $3, now() + $4::interval)`,
		email, digest(code), purpose, fmt.Sprintf("%d seconds", int(ttl.Seconds())))
	if err != nil {
		return fmt.Errorf("authn: сохранение кода: %w", err)
	}
	return nil
}

// ConsumeCode ищет непогашенный неистёкший код с данным отпечатком для email
// и, если находит, помечает его использованным. Возвращает ErrNotFound, если
// код неверный/истёк/уже использован.
func (s *Store) ConsumeCode(ctx context.Context, email, code, purpose string) error {
	tag, err := s.db.Pool.Exec(ctx, `
		UPDATE login_codes SET lc_consumed_at = now()
		WHERE lower(lc_email) = lower($1) AND lc_purpose = $2 AND lc_code_digest = $3
		  AND lc_consumed_at IS NULL AND lc_valid_until > now()`,
		email, purpose, digest(code))
	if err != nil {
		return fmt.Errorf("authn: погашение кода: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// --- sessions ----------------------------------------------------------------

type Session struct {
	ID          string
	AccountID   string
	UserAgent   string
	CreatedAt   time.Time
	LastPingAt  time.Time
	Invalidated bool
}

// CreateSession создаёт строку login_sessions и возвращает её id + непрозрачный секрет.
func (s *Store) CreateSession(ctx context.Context, accountID, userAgent string) (Session, error) {
	secret, err := randomToken("sess_", 24)
	if err != nil {
		return Session{}, err
	}
	var sess Session
	err = s.db.Pool.QueryRow(ctx, `
		INSERT INTO login_sessions (account_id, sess_secret_digest, sess_client_agent)
		VALUES ($1, $2, $3)
		RETURNING id, account_id, sess_client_agent, created_at, sess_last_ping_at`,
		accountID, digest(secret), userAgent,
	).Scan(&sess.ID, &sess.AccountID, &sess.UserAgent, &sess.CreatedAt, &sess.LastPingAt)
	if err != nil {
		return Session{}, fmt.Errorf("authn: создание сессии: %w", err)
	}
	return sess, nil
}

// IsSessionValid — существует ли сессия с данным id для accountID и не отозвана ли она.
func (s *Store) IsSessionValid(ctx context.Context, sessionID, accountID string) (bool, error) {
	var ok bool
	err := s.db.Pool.QueryRow(ctx, `
		SELECT true FROM login_sessions
		WHERE id = $1 AND account_id = $2 AND sess_invalidated_at IS NULL
		  AND (sess_valid_until IS NULL OR sess_valid_until > now())`,
		sessionID, accountID).Scan(&ok)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("authn: проверка сессии: %w", err)
	}
	return ok, nil
}

// ListSessions возвращает активные сессии аккаунта, самые новые первыми.
func (s *Store) ListSessions(ctx context.Context, accountID string) ([]Session, error) {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT id, account_id, sess_client_agent, created_at, sess_last_ping_at
		FROM login_sessions
		WHERE account_id = $1 AND sess_invalidated_at IS NULL
		ORDER BY created_at DESC`, accountID)
	if err != nil {
		return nil, fmt.Errorf("authn: список сессий: %w", err)
	}
	defer rows.Close()
	var out []Session
	for rows.Next() {
		var sess Session
		if err := rows.Scan(&sess.ID, &sess.AccountID, &sess.UserAgent, &sess.CreatedAt, &sess.LastPingAt); err != nil {
			return nil, err
		}
		out = append(out, sess)
	}
	return out, rows.Err()
}

// RevokeSession отзывает одну сессию аккаунта, возвращает число затронутых строк.
func (s *Store) RevokeSession(ctx context.Context, sessionID, accountID string) (int64, error) {
	tag, err := s.db.Pool.Exec(ctx, `
		UPDATE login_sessions SET sess_invalidated_at = now()
		WHERE id = $1 AND account_id = $2 AND sess_invalidated_at IS NULL`, sessionID, accountID)
	if err != nil {
		return 0, fmt.Errorf("authn: отзыв сессии: %w", err)
	}
	return tag.RowsAffected(), nil
}

// RevokeAllSessions отзывает все живые сессии аккаунта.
func (s *Store) RevokeAllSessions(ctx context.Context, accountID string) (int64, error) {
	tag, err := s.db.Pool.Exec(ctx, `
		UPDATE login_sessions SET sess_invalidated_at = now()
		WHERE account_id = $1 AND sess_invalidated_at IS NULL`, accountID)
	if err != nil {
		return 0, fmt.Errorf("authn: отзыв всех сессий: %w", err)
	}
	return tag.RowsAffected(), nil
}

// --- personal access tokens --------------------------------------------------

type PAT struct {
	ID         string
	AccountID  string
	Name       string
	Prefix     string
	ExpiresAt  *time.Time
	LastUsedAt *time.Time
	CreatedAt  time.Time
}

const patPrefix = "gsl_"

// CreatePAT создаёт PAT и возвращает строку + значение в открытом виде (один раз).
func (s *Store) CreatePAT(ctx context.Context, accountID, name string, expiresInDays int) (PAT, string, error) {
	secret, err := randomToken(patPrefix, 24)
	if err != nil {
		return PAT{}, "", err
	}
	prefix := secret[:len(patPrefix)+6]
	var expiry any
	if expiresInDays > 0 {
		expiry = fmt.Sprintf("%d days", expiresInDays)
	}
	var p PAT
	row := s.db.Pool.QueryRow(ctx, `
		INSERT INTO access_keys (account_id, ak_title, ak_secret_digest, ak_secret_prefix, ak_valid_until)
		VALUES ($1, $2, $3, $4, CASE WHEN $5::text IS NULL THEN NULL ELSE now() + $5::interval END)
		RETURNING id, account_id, ak_title, ak_secret_prefix, ak_valid_until, ak_last_used_at, created_at`,
		accountID, name, digest(secret), prefix, expiry)
	if err := row.Scan(&p.ID, &p.AccountID, &p.Name, &p.Prefix, &p.ExpiresAt, &p.LastUsedAt, &p.CreatedAt); err != nil {
		return PAT{}, "", fmt.Errorf("authn: создание PAT: %w", err)
	}
	return p, secret, nil
}

// ListPATs — токены аккаунта, самые новые первыми.
func (s *Store) ListPATs(ctx context.Context, accountID string) ([]PAT, error) {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT id, account_id, ak_title, ak_secret_prefix, ak_valid_until, ak_last_used_at, created_at
		FROM access_keys WHERE account_id = $1 ORDER BY created_at DESC`, accountID)
	if err != nil {
		return nil, fmt.Errorf("authn: список PAT: %w", err)
	}
	defer rows.Close()
	var out []PAT
	for rows.Next() {
		var p PAT
		if err := rows.Scan(&p.ID, &p.AccountID, &p.Name, &p.Prefix, &p.ExpiresAt, &p.LastUsedAt, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// RevokePAT — идемпотентное удаление PAT аккаунта.
func (s *Store) RevokePAT(ctx context.Context, id, accountID string) error {
	_, err := s.db.Pool.Exec(ctx, `DELETE FROM access_keys WHERE id = $1 AND account_id = $2`, id, accountID)
	if err != nil {
		return fmt.Errorf("authn: отзыв PAT: %w", err)
	}
	return nil
}

// FindPATByToken ищет PAT по значению в открытом виде (проверка формата
// gsl_ остаётся на вызывающем коде), обновляет ak_last_used_at при находке.
func (s *Store) FindPATByToken(ctx context.Context, token string) (PAT, error) {
	if !strings.HasPrefix(token, patPrefix) {
		return PAT{}, ErrNotFound
	}
	var p PAT
	row := s.db.Pool.QueryRow(ctx, `
		UPDATE access_keys SET ak_last_used_at = now()
		WHERE ak_secret_digest = $1 AND (ak_valid_until IS NULL OR ak_valid_until > now())
		RETURNING id, account_id, ak_title, ak_secret_prefix, ak_valid_until, ak_last_used_at, created_at`,
		digest(token))
	if err := row.Scan(&p.ID, &p.AccountID, &p.Name, &p.Prefix, &p.ExpiresAt, &p.LastUsedAt, &p.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return PAT{}, ErrNotFound
		}
		return PAT{}, fmt.Errorf("authn: поиск PAT: %w", err)
	}
	return p, nil
}

// RenewPAT продлевает срок действия ещё на 90 дней, только если он истекает
// в ближайшие 7 дней. Бессрочный токен (ExpiresAt == nil) не трогается.
func (s *Store) RenewPAT(ctx context.Context, id string) (expiresAt *time.Time, renewed bool, err error) {
	row := s.db.Pool.QueryRow(ctx, `
		UPDATE access_keys SET ak_valid_until = now() + interval '90 days'
		WHERE id = $1 AND ak_valid_until IS NOT NULL AND ak_valid_until <= now() + interval '7 days'
		RETURNING ak_valid_until`, id)
	var t time.Time
	scanErr := row.Scan(&t)
	if errors.Is(scanErr, pgx.ErrNoRows) {
		// либо бессрочный, либо ещё не подошёл срок — вернуть текущее значение как есть.
		var cur *time.Time
		if getErr := s.db.Pool.QueryRow(ctx, `SELECT ak_valid_until FROM access_keys WHERE id = $1`, id).Scan(&cur); getErr != nil {
			return nil, false, fmt.Errorf("authn: чтение срока PAT: %w", getErr)
		}
		return cur, false, nil
	}
	if scanErr != nil {
		return nil, false, fmt.Errorf("authn: продление PAT: %w", scanErr)
	}
	return &t, true, nil
}

// --- MFA (минимально: статус; enroll/confirm/disable — 501, см. decisions.md) ---

type MFAStatus struct {
	Enabled                bool
	PendingEnrollment      bool
	EnabledAt              *time.Time
	RecoveryCodesRemaining int
}

func (s *Store) MFAStatus(ctx context.Context, accountID string) (MFAStatus, error) {
	var st MFAStatus
	var enabledAt *time.Time
	var pendingSince time.Time
	err := s.db.Pool.QueryRow(ctx, `
		SELECT mfa_enabled_at, mfa_pending_since FROM mfa_factors WHERE account_id = $1`, accountID,
	).Scan(&enabledAt, &pendingSince)
	if errors.Is(err, pgx.ErrNoRows) {
		return MFAStatus{}, nil
	}
	if err != nil {
		return MFAStatus{}, fmt.Errorf("authn: статус MFA: %w", err)
	}
	st.EnabledAt = enabledAt
	st.Enabled = enabledAt != nil
	st.PendingEnrollment = enabledAt == nil
	if st.Enabled {
		if err := s.db.Pool.QueryRow(ctx, `
			SELECT count(*) FROM mfa_recovery_codes WHERE account_id = $1 AND mrc_used_at IS NULL`, accountID,
		).Scan(&st.RecoveryCodesRemaining); err != nil {
			return MFAStatus{}, fmt.Errorf("authn: подсчёт recovery codes: %w", err)
		}
	}
	return st, nil
}
