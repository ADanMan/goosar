package workspace

import (
	"context"

	"github.com/adanman/goosar/server2/internal/httpapi"
)

// httpapiMembership даёт доменам T-027 без {id} в пути (Projects/Inbox/Chat)
// один общий способ резолвить X-Workspace-Slug/X-Workspace-ID в id
// пространства и роль вызывающего, вместо того чтобы каждый заводил свою
// копию этой пары запросов к spaces/space_members.
type httpapiMembership struct{ backing *Store }

// HTTPAPIMembership отдаёт httpapiMembership как httpapi.WorkspaceMembership.
func (s *Store) HTTPAPIMembership() httpapi.WorkspaceMembership {
	return httpapiMembership{backing: s}
}

func (m httpapiMembership) ResolveWorkspaceID(ctx context.Context, ref string, isSlug bool) (id string, found bool, err error) {
	resolve := m.backing.GetWorkspace
	if isSlug {
		resolve = m.backing.GetWorkspaceBySlug
	}
	w, lookupErr := resolve(ctx, ref)
	switch lookupErr {
	case nil:
		return w.ID, true, nil
	case ErrNotFound:
		return "", false, nil
	default:
		return "", false, lookupErr
	}
}

func (m httpapiMembership) MemberRole(ctx context.Context, workspaceID, userID string) (role httpapi.Role, found bool, err error) {
	member, lookupErr := m.backing.GetMemberByUser(ctx, workspaceID, userID)
	switch lookupErr {
	case nil:
		return httpapi.Role(member.Role), true, nil
	case ErrNotFound:
		return "", false, nil
	default:
		return "", false, lookupErr
	}
}
