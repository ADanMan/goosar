package service

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adanman/goosar/server/internal/runtimeapps"
	db "github.com/adanman/goosar/server/pkg/db/generated"
	"github.com/adanman/goosar/server/pkg/featureflag"
)

type FlagGatedOverlayBuilder struct {
	Flags   *featureflag.Service
	Enabled string
	Inner   TaskOverlayBuilder
}

func (b *FlagGatedOverlayBuilder) BuildTaskOverlay(ctx context.Context, originatorUserID pgtype.UUID, agent db.Agent) (runtimeapps.MCPOverlayResult, error) {
	if b == nil || b.Inner == nil {
		return runtimeapps.MCPOverlayResult{}, nil
	}
	if !b.Flags.IsEnabled(ctx, b.Enabled, false) {
		return runtimeapps.MCPOverlayResult{}, nil
	}
	return b.Inner.BuildTaskOverlay(ctx, originatorUserID, agent)
}

func (b *FlagGatedOverlayBuilder) RebindTaskOverlay(overlay []byte, taskID pgtype.UUID) ([]byte, error) {
	if b == nil || b.Inner == nil {
		return nil, nil
	}
	rebinder, ok := b.Inner.(TaskOverlayRebinder)
	if !ok {
		return nil, nil
	}
	return rebinder.RebindTaskOverlay(overlay, taskID)
}
