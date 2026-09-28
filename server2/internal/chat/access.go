package chat

import "context"

// ChatAccessBridge адаптирует *Store к realtime.ChatAccess (см. его
// комментарий в internal/realtime/ws.go) — тем же приёмом, что и
// workspace.Store.HTTPAPIMembership для httpapi.WorkspaceMembership:
// realtime не знает о convos, только об этой узкой щели.
type ChatAccessBridge struct{ store *Store }

// NewChatAccessBridge — конструктор для internal/app (см. routes.go: подключает
// realtime.Register к домену chat).
func NewChatAccessBridge(s *Store) *ChatAccessBridge { return &ChatAccessBridge{store: s} }

// CanAccessChat — found=false, если сессия не существует в этом воркспейсе;
// allowed — только для создателя сессии (тот же контракт доступа, что и
// requireSession в handlers.go: чат виден только своему создателю).
func (b *ChatAccessBridge) CanAccessChat(ctx context.Context, workspaceID, userID, chatID string) (found, allowed bool, err error) {
	sess, err := b.store.GetSession(ctx, workspaceID, chatID)
	if err == ErrNotFound {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	return true, sess.CreatorID == userID, nil
}
