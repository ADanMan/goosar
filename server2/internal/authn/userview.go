package authn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// UserView — форма components/schemas/User контракта. Таблица accounts
// (001_identity.up.sql) принадлежит этому пакету (сессии/коды/PAT), поэтому
// чтение и запись профиля тоже живут здесь; домен identity (GET/PATCH
// /api/me) переиспользует эти методы вместо дублирования SQL по accounts —
// см. server2/docs/adr/0001-stack.md.
type UserView struct {
	ID                      string          `json:"id"`
	Name                    string          `json:"name"`
	Email                   string          `json:"email"`
	AvatarURL               *string         `json:"avatar_url"`
	Language                *string         `json:"language"`
	Timezone                *string         `json:"timezone"`
	OnboardedAt             *time.Time      `json:"onboarded_at"`
	OnboardingQuestionnaire json.RawMessage `json:"onboarding_questionnaire"`
	StarterContentState     *string         `json:"starter_content_state"`
	ProfileDescription      string          `json:"profile_description"`
	CreatedAt               time.Time       `json:"created_at"`
	UpdatedAt               time.Time       `json:"updated_at"`
}

const userViewColumns = `id, acct_full_name, acct_email, acct_avatar_uri, acct_locale, acct_tz,
	acct_onboarded_at, acct_onboarding_survey, acct_starter_state, acct_bio, created_at, updated_at`

func scanUserView(row pgx.Row) (UserView, error) {
	var u UserView
	var questionnaire []byte
	err := row.Scan(&u.ID, &u.Name, &u.Email, &u.AvatarURL, &u.Language, &u.Timezone,
		&u.OnboardedAt, &questionnaire, &u.StarterContentState, &u.ProfileDescription,
		&u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return UserView{}, err
	}
	if len(questionnaire) == 0 {
		questionnaire = []byte("{}")
	}
	u.OnboardingQuestionnaire = questionnaire
	return u, nil
}

// GetUserView читает профиль по id.
func (s *Store) GetUserView(ctx context.Context, id string) (UserView, error) {
	row := s.db.Pool.QueryRow(ctx, `SELECT `+userViewColumns+` FROM accounts WHERE id = $1`, id)
	u, err := scanUserView(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return UserView{}, ErrNotFound
		}
		return UserView{}, fmt.Errorf("authn: чтение профиля: %w", err)
	}
	return u, nil
}

// ProfilePatch — необязательные поля PATCH /api/me (nil-значение = не менять).
type ProfilePatch struct {
	Name               *string
	AvatarURL          *string
	Language           *string
	ProfileDescription *string
	Timezone           *string
}

// UpdateUserView применяет частичный патч и возвращает обновлённый профиль.
func (s *Store) UpdateUserView(ctx context.Context, id string, patch ProfilePatch) (UserView, error) {
	row := s.db.Pool.QueryRow(ctx, `
		UPDATE accounts SET
			acct_full_name = COALESCE($2, acct_full_name),
			acct_avatar_uri = COALESCE($3, acct_avatar_uri),
			acct_locale = COALESCE($4, acct_locale),
			acct_bio = COALESCE($5, acct_bio),
			acct_tz = COALESCE($6, acct_tz),
			updated_at = now()
		WHERE id = $1
		RETURNING `+userViewColumns,
		id, patch.Name, patch.AvatarURL, patch.Language, patch.ProfileDescription, patch.Timezone)
	u, err := scanUserView(row)
	if err != nil {
		return UserView{}, fmt.Errorf("authn: обновление профиля: %w", err)
	}
	return u, nil
}

// MergeOnboardingQuestionnaire мержит переданные поля в acct_onboarding_survey (jsonb).
func (s *Store) MergeOnboardingQuestionnaire(ctx context.Context, id string, patch json.RawMessage) (UserView, error) {
	row := s.db.Pool.QueryRow(ctx, `
		UPDATE accounts SET acct_onboarding_survey = acct_onboarding_survey || $2::jsonb, updated_at = now()
		WHERE id = $1
		RETURNING `+userViewColumns, id, string(patch))
	u, err := scanUserView(row)
	if err != nil {
		return UserView{}, fmt.Errorf("authn: обновление анкеты онбординга: %w", err)
	}
	return u, nil
}

// CompleteOnboarding выставляет acct_onboarded_at, если он ещё не установлен.
func (s *Store) CompleteOnboarding(ctx context.Context, id string) (UserView, error) {
	row := s.db.Pool.QueryRow(ctx, `
		UPDATE accounts SET acct_onboarded_at = COALESCE(acct_onboarded_at, now()), updated_at = now()
		WHERE id = $1
		RETURNING `+userViewColumns, id)
	u, err := scanUserView(row)
	if err != nil {
		return UserView{}, fmt.Errorf("authn: завершение онбординга: %w", err)
	}
	return u, nil
}
