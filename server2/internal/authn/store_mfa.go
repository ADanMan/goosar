package authn

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Factor — строка mfa_factors, включая опечатанный секрет: расшифровка —
// дело вызывающего кода (unsealTOTPSecret), Store только хранит байты.
type Factor struct {
	SealedSecret []byte
	PendingSince time.Time
	EnabledAt    *time.Time
}

// UpsertPendingFactor заводит (или пересоздаёт, если уже был pending/enabled)
// TOTP-фактор аккаунта в состоянии "ожидает подтверждения" — contract
// authEnrollTotp: "генерирует TOTP-секрет ... сохраняет как «ожидающий
// подтверждения»". Разрешать перезапись уже enabled-фактора этим методом
// нельзя — это делает handler (409, см. handleEnrollTotp), Store только
// исполняет команду.
func (s *Store) UpsertPendingFactor(ctx context.Context, accountID string, sealedSecret []byte) error {
	_, err := s.db.Pool.Exec(ctx, `
		INSERT INTO mfa_factors (account_id, mfa_secret_sealed, mfa_pending_since, mfa_enabled_at)
		VALUES ($1, $2, now(), NULL)
		ON CONFLICT (account_id) DO UPDATE SET
			mfa_secret_sealed = EXCLUDED.mfa_secret_sealed,
			mfa_pending_since = now(),
			mfa_enabled_at = NULL,
			updated_at = now()`,
		accountID, sealedSecret)
	if err != nil {
		return fmt.Errorf("authn: сохранение pending MFA-фактора: %w", err)
	}
	return nil
}

// GetFactor читает MFA-фактор аккаунта, если он есть (pending или enabled).
func (s *Store) GetFactor(ctx context.Context, accountID string) (Factor, bool, error) {
	var f Factor
	err := s.db.Pool.QueryRow(ctx, `
		SELECT mfa_secret_sealed, mfa_pending_since, mfa_enabled_at
		FROM mfa_factors WHERE account_id = $1`, accountID,
	).Scan(&f.SealedSecret, &f.PendingSince, &f.EnabledAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Factor{}, false, nil
	}
	if err != nil {
		return Factor{}, false, fmt.Errorf("authn: чтение MFA-фактора: %w", err)
	}
	return f, true, nil
}

// ConfirmFactor включает MFA (mfa_enabled_at = now()) и заводит recoveryDigests
// одной транзакцией — contract authConfirmTotp: "включает MFA, выпускает 10
// одноразовых recovery-кодов".
func (s *Store) ConfirmFactor(ctx context.Context, accountID string, recoveryDigests []string) error {
	return s.db.WithTx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			UPDATE mfa_factors SET mfa_enabled_at = now(), updated_at = now()
			WHERE account_id = $1 AND mfa_enabled_at IS NULL`, accountID)
		if err != nil {
			return fmt.Errorf("authn: включение MFA: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return insertRecoveryCodes(ctx, tx, accountID, recoveryDigests)
	})
}

// RegenerateRecoveryCodes удаляет все старые коды аккаунта и заводит новые —
// contract authRegenerateMfaRecoveryCodes: "старые становятся недействительны".
func (s *Store) RegenerateRecoveryCodes(ctx context.Context, accountID string, recoveryDigests []string) error {
	return s.db.WithTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM mfa_recovery_codes WHERE account_id = $1`, accountID); err != nil {
			return fmt.Errorf("authn: удаление старых recovery-кодов: %w", err)
		}
		return insertRecoveryCodes(ctx, tx, accountID, recoveryDigests)
	})
}

func insertRecoveryCodes(ctx context.Context, tx pgx.Tx, accountID string, digests []string) error {
	for _, d := range digests {
		if _, err := tx.Exec(ctx, `
			INSERT INTO mfa_recovery_codes (account_id, mrc_code_digest) VALUES ($1, $2)`,
			accountID, d); err != nil {
			return fmt.Errorf("authn: сохранение recovery-кода: %w", err)
		}
	}
	return nil
}

// ConsumeRecoveryCode ищет неиспользованный recovery-код по отпечатку и, если
// находит, помечает его использованным. ok=false — код неверный или уже
// использован (contract: не различает эти два случая в ответе).
func (s *Store) ConsumeRecoveryCode(ctx context.Context, accountID, code string) (bool, error) {
	tag, err := s.db.Pool.Exec(ctx, `
		UPDATE mfa_recovery_codes SET mrc_used_at = now()
		WHERE account_id = $1 AND mrc_code_digest = $2 AND mrc_used_at IS NULL`,
		accountID, digest(normalizeRecoveryCode(code)))
	if err != nil {
		return false, fmt.Errorf("authn: погашение recovery-кода: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// DisableFactor удаляет MFA-фактор и все recovery-коды аккаунта — contract
// authDisableTotp: "выключает MFA и удаляет recovery-коды". ok=false, если
// фактора не было (400 "not enrolled" у вызывающего handler'а).
func (s *Store) DisableFactor(ctx context.Context, accountID string) (bool, error) {
	var ok bool
	err := s.db.WithTx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `DELETE FROM mfa_factors WHERE account_id = $1`, accountID)
		if err != nil {
			return fmt.Errorf("authn: удаление MFA-фактора: %w", err)
		}
		ok = tag.RowsAffected() > 0
		if !ok {
			return nil
		}
		if _, err := tx.Exec(ctx, `DELETE FROM mfa_recovery_codes WHERE account_id = $1`, accountID); err != nil {
			return fmt.Errorf("authn: удаление recovery-кодов: %w", err)
		}
		return nil
	})
	return ok, err
}

// --- mfa_pending_logins (340_authn_mfa_pending) ------------------------------

// CreatePendingLogin выпускает mfa_token для LoginResult.mfa_token —
// сессия ещё не началась, пока держатель токена не пройдёт
// POST /api/auth/mfa/verify (contract: "Exchanges the short-lived mfa_token
// ... for a real session").
func (s *Store) CreatePendingLogin(ctx context.Context, accountID string, ttl time.Duration) (string, error) {
	token, err := randomToken("mfat_", 24)
	if err != nil {
		return "", err
	}
	_, err = s.db.Pool.Exec(ctx, `
		INSERT INTO mfa_pending_logins (account_id, mpl_token_digest, mpl_valid_until)
		VALUES ($1, $2, now() + $3::interval)`,
		accountID, digest(token), fmt.Sprintf("%d seconds", int(ttl.Seconds())))
	if err != nil {
		return "", fmt.Errorf("authn: сохранение mfa_token: %w", err)
	}
	return token, nil
}

// FindPendingLogin возвращает accountID по ещё не истёкшему mfa_token, не
// потребляя его — так неверный код/recovery-код не сжигает единственную
// попытку раньше времени (её ограничивает отдельно rate limit по хэшу
// токена, RATE_LIMIT_MFA_VERIFY).
func (s *Store) FindPendingLogin(ctx context.Context, token string) (string, error) {
	var accountID string
	err := s.db.Pool.QueryRow(ctx, `
		SELECT account_id FROM mfa_pending_logins
		WHERE mpl_token_digest = $1 AND mpl_valid_until > now()`, digest(token),
	).Scan(&accountID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("authn: поиск mfa_token: %w", err)
	}
	return accountID, nil
}

// ConsumePendingLogin удаляет mfa_token после успешной проверки второго
// фактора (одноразовый обмен на сессию).
func (s *Store) ConsumePendingLogin(ctx context.Context, token string) error {
	_, err := s.db.Pool.Exec(ctx, `DELETE FROM mfa_pending_logins WHERE mpl_token_digest = $1`, digest(token))
	if err != nil {
		return fmt.Errorf("authn: погашение mfa_token: %w", err)
	}
	return nil
}

// --- auth_bindings (OIDC/LDAP) ------------------------------------------------

// FindAccountByExternalSubject ищет аккаунт, уже привязанный к (method, subject)
// внешнего провайдера (contract: auth_bindings.ab_external_subject).
func (s *Store) FindAccountByExternalSubject(ctx context.Context, method, subject string) (Account, error) {
	var a Account
	err := s.db.Pool.QueryRow(ctx, `
		SELECT a.id, a.acct_email, a.acct_full_name, a.acct_token_epoch
		FROM accounts a
		JOIN auth_bindings b ON b.account_id = a.id
		WHERE b.ab_method = $1 AND b.ab_external_subject = $2`, method, subject,
	).Scan(&a.ID, &a.Email, &a.Name, &a.TokenEpoch)
	if errors.Is(err, pgx.ErrNoRows) {
		return Account{}, ErrNotFound
	}
	if err != nil {
		return Account{}, fmt.Errorf("authn: поиск аккаунта по внешнему subject: %w", err)
	}
	return a, nil
}

// UpsertExternalBinding привязывает (method, subject) к accountID — вызывается
// после первого успешного OIDC/LDAP-входа, когда аккаунт был найден/создан по
// email, а не по subject (второй вход того же пользователя пойдёт уже через
// FindAccountByExternalSubject).
func (s *Store) UpsertExternalBinding(ctx context.Context, accountID, method, subject, externalEmail string) error {
	_, err := s.db.Pool.Exec(ctx, `
		INSERT INTO auth_bindings (account_id, ab_method, ab_external_subject, ab_external_email)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (account_id, ab_method) DO UPDATE SET
			ab_external_subject = EXCLUDED.ab_external_subject,
			ab_external_email = EXCLUDED.ab_external_email,
			updated_at = now()`,
		accountID, method, subject, externalEmail)
	if err != nil {
		return fmt.Errorf("authn: привязка внешней identity: %w", err)
	}
	return nil
}
