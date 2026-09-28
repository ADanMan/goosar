package workspace

import "context"

// membershipBridge реализует realtime.WorkspaceMembership — узкую щель,
// через которую пакету realtime видно ровно два факта о пространствах:
// какому id соответствует ссылка (slug или id) и состоит ли пользователь в
// нём участником. Никакого другого доступа к Store у /ws нет.
type membershipBridge struct{ backing *Store }

func (s *Store) RealtimeMembership() *membershipBridge { return &membershipBridge{backing: s} }

func (b *membershipBridge) ResolveID(ctx context.Context, ref string, isSlug bool) (id string, found bool, err error) {
	get := b.backing.GetWorkspace
	if isSlug {
		get = b.backing.GetWorkspaceBySlug
	}
	w, lookupErr := get(ctx, ref)
	if lookupErr == nil {
		return w.ID, true, nil
	}
	if lookupErr == ErrNotFound {
		return "", false, nil
	}
	return "", false, lookupErr
}

func (b *membershipBridge) IsMember(ctx context.Context, workspaceID, userID string) (yes bool, err error) {
	_, lookupErr := b.backing.GetMemberByUser(ctx, workspaceID, userID)
	return lookupErr == nil, pickErr(lookupErr)
}

// pickErr скрывает ErrNotFound (это "нет", не сбой) и пропускает остальное.
func pickErr(err error) error {
	if err == ErrNotFound {
		return nil
	}
	return err
}
