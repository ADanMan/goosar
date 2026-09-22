package middleware

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/adanman/goosar/server/internal/auth"
	"github.com/adanman/goosar/server/internal/util"
	db "github.com/adanman/goosar/server/pkg/db/generated"
)

func ownerLookupFor(queries *db.Queries) auth.OwnerLookupFunc {
	if queries == nil {
		return nil
	}
	return func(ctx context.Context, ownerID string) (bool, error) {
		uuid, err := util.ParseUUID(ownerID)
		if err != nil {

			return false, nil
		}
		_, err = queries.GetUser(ctx, uuid)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return false, nil
			}
			return false, err
		}
		return true, nil
	}
}
