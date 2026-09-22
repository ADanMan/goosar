package slack

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adanman/goosar/server/internal/integrations/channel/engine"
	db "github.com/adanman/goosar/server/pkg/db/generated"
)

const BindingTokenTTL = 15 * time.Minute

var (
	ErrBindingTokenInvalid = errors.New("slack: binding token invalid or expired")

	ErrBindingAlreadyAssigned = errors.New("slack: user id is already bound to a different user")

	ErrBindingNotWorkspaceMember = errors.New("slack: redeemer is not a workspace member")
)

type BindingToken struct {
	Raw       string
	ExpiresAt time.Time
}

type RedeemedBindingToken struct {
	WorkspaceID    pgtype.UUID
	InstallationID pgtype.UUID
	SlackUserID    string
}

type BindingTokenService struct {
	q   *db.Queries
	tx  engine.TxStarter
	now func() time.Time
}

func NewBindingTokenService(q *db.Queries, tx engine.TxStarter) *BindingTokenService {
	return &BindingTokenService{q: q, tx: tx, now: time.Now}
}

func (s *BindingTokenService) Mint(ctx context.Context, workspaceID, installationID pgtype.UUID, slackUserID string) (BindingToken, error) {
	raw, err := randomBindingToken(32)
	if err != nil {
		return BindingToken{}, fmt.Errorf("generate token: %w", err)
	}
	expiresAt := s.now().Add(BindingTokenTTL)
	if _, err := s.q.CreateChannelBindingToken(ctx, db.CreateChannelBindingTokenParams{
		TokenHash:      hashBindingToken(raw),
		WorkspaceID:    workspaceID,
		InstallationID: installationID,
		ChannelType:    string(TypeSlack),
		ChannelUserID:  slackUserID,
		ExpiresAt:      pgtype.Timestamptz{Time: expiresAt, Valid: true},
	}); err != nil {
		return BindingToken{}, fmt.Errorf("persist token: %w", err)
	}
	return BindingToken{Raw: raw, ExpiresAt: expiresAt}, nil
}

func (s *BindingTokenService) RedeemAndBind(ctx context.Context, raw string, goosarUserID pgtype.UUID) (RedeemedBindingToken, error) {
	if s.tx == nil {
		return RedeemedBindingToken{}, errors.New("slack: BindingTokenService missing TxStarter")
	}
	tx, err := s.tx.Begin(ctx)
	if err != nil {
		return RedeemedBindingToken{}, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := s.q.WithTx(tx)

	row, err := qtx.ConsumeChannelBindingToken(ctx, hashBindingToken(raw))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return RedeemedBindingToken{}, ErrBindingTokenInvalid
		}
		return RedeemedBindingToken{}, fmt.Errorf("consume token: %w", err)
	}

	if _, err := qtx.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
		UserID:      goosarUserID,
		WorkspaceID: row.WorkspaceID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return RedeemedBindingToken{}, ErrBindingNotWorkspaceMember
		}
		return RedeemedBindingToken{}, fmt.Errorf("check membership: %w", err)
	}

	if _, err := qtx.CreateChannelUserBinding(ctx, db.CreateChannelUserBindingParams{
		WorkspaceID:    row.WorkspaceID,
		GoosarUserID:   goosarUserID,
		InstallationID: row.InstallationID,
		ChannelType:    string(TypeSlack),
		ChannelUserID:  row.ChannelUserID,
		Config:         []byte(`{}`),
	}); err != nil {

		if errors.Is(err, pgx.ErrNoRows) {
			return RedeemedBindingToken{}, ErrBindingAlreadyAssigned
		}
		return RedeemedBindingToken{}, fmt.Errorf("create binding: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return RedeemedBindingToken{}, fmt.Errorf("commit: %w", err)
	}
	return RedeemedBindingToken{
		WorkspaceID:    row.WorkspaceID,
		InstallationID: row.InstallationID,
		SlackUserID:    row.ChannelUserID,
	}, nil
}

func randomBindingToken(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func hashBindingToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
