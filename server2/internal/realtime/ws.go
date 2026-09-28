package realtime

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/coder/websocket"

	"github.com/adanman/goosar/server2/internal/httpapi"
)

const writeTimeout = 5 * time.Second
const firstFrameTimeout = 10 * time.Second

// Authenticator аутентифицирует запрос на апгрейд (cookie, опционально — см.
// контракт: "cookie auth optional at handshake") и, отдельно, произвольный
// токен, присланный первым текстовым фреймом, когда cookie не было.
type Authenticator interface {
	AuthenticateRequest(r *http.Request) (*httpapi.Actor, bool)
	AuthenticateToken(ctx context.Context, token string) (*httpapi.Actor, bool)
}

// WorkspaceMembership резолвит воркспейс по slug/id и проверяет членство —
// реализуется пакетом workspace; realtime намеренно не знает о таблице spaces.
type WorkspaceMembership interface {
	ResolveID(ctx context.Context, ref string, isSlug bool) (workspaceID string, found bool, err error)
	IsMember(ctx context.Context, workspaceID, userID string) (bool, error)
}

// Register регистрирует GET /ws.
func Register(router *httpapi.Router, hub *Hub, auth Authenticator, wsMembers WorkspaceMembership, logger *slog.Logger) {
	router.Handle(http.MethodGet, "/ws", handleConnect(hub, auth, wsMembers, logger))
}

func handleConnect(hub *Hub, auth Authenticator, wsMembers WorkspaceMembership, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		wsID := q.Get("workspace_id")
		wsSlug := q.Get("workspace_slug")
		if wsID == "" && wsSlug == "" {
			httpapi.BadRequest(w, "workspace_id or workspace_slug is required")
			return
		}

		actor, authenticated := auth.AuthenticateRequest(r)

		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			OriginPatterns: []string{"*"},
		})
		if err != nil {
			// websocket.Accept уже написал HTTP-ответ при ошибке.
			return
		}
		ctx := r.Context()

		if !authenticated {
			frameCtx, cancel := context.WithTimeout(ctx, firstFrameTimeout)
			_, data, err := c.Read(frameCtx)
			cancel()
			if err != nil {
				_ = c.Close(websocket.StatusPolicyViolation, "auth timeout")
				return
			}
			actor, authenticated = auth.AuthenticateToken(ctx, string(data))
			if !authenticated {
				_ = c.Close(websocket.StatusPolicyViolation, "invalid token")
				return
			}
		}

		var (
			workspaceID string
			found       bool
		)
		if wsID != "" {
			workspaceID, found, err = wsMembers.ResolveID(ctx, wsID, false)
		} else {
			workspaceID, found, err = wsMembers.ResolveID(ctx, wsSlug, true)
		}
		if err != nil || !found {
			_ = c.Close(websocket.StatusPolicyViolation, "workspace not found")
			return
		}
		isMember, err := wsMembers.IsMember(ctx, workspaceID, actor.UserID)
		if err != nil || !isMember {
			_ = c.Close(websocket.StatusPolicyViolation, "not a member")
			return
		}

		cn := &socket{ws: c}
		hub.join(workspaceID, cn)
		defer hub.leave(workspaceID, cn)

		logger.Info("realtime: подключение", "workspace_id", workspaceID, "user_id", actor.UserID)

		// Читающий цикл: держит соединение живым и замечает его закрытие
		// клиентом. Разбор протокола подписок (см. A.md) — вне объёма T-026,
		// зарегистрировано в decisions.md как пробел спецификации.
		for {
			if _, _, err := c.Read(ctx); err != nil {
				return
			}
		}
	}
}
