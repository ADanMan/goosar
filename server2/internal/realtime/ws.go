package realtime

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/coder/websocket"

	"github.com/adanman/goosar/server2/internal/httpapi"
)

const writeTimeout = 5 * time.Second
const firstFrameTimeout = 10 * time.Second
const maxFrameBytes = 64 * 1024 // contract §2.1: "лимит размера фрейма 64 KiB"

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

// TaskAccess проверяет доступ к комнате task:<id> при subscribe-фрейме
// (contract §2.1: "task/chat проходят отдельную проверку доступа"). found=false
// без ошибки — задача не существует ("lookup_failed"), а не "forbidden".
// Реализуется пакетом task; nil здесь означает, что домен ещё не подключён
// (subscribe на scope=task тогда всегда отвечает lookup_failed).
type TaskAccess interface {
	CanAccessTask(ctx context.Context, workspaceID, userID, taskID string) (found, allowed bool, err error)
}

// ChatAccess — то же самое для комнаты chat:<id>. Реализуется пакетом chat
// (за пределами этой сессии T-027 — см. server2/docs/decisions.md); nil
// означает "lookup_failed" для scope=chat до тех пор, пока домен chat не
// подключит свою реализацию.
type ChatAccess interface {
	CanAccessChat(ctx context.Context, workspaceID, userID, chatID string) (found, allowed bool, err error)
}

// Register регистрирует GET /ws. taskAccess/chatAccess могут быть nil — тогда
// соответствующий scope subscribe-фрейма всегда отвечает lookup_failed
// (домен ещё не готов), не ломая остальной протокол.
func Register(router *httpapi.Router, hub *Hub, auth Authenticator, wsMembers WorkspaceMembership, taskAccess TaskAccess, chatAccess ChatAccess, logger *slog.Logger) {
	router.Handle(http.MethodGet, "/ws", handleConnect(hub, auth, wsMembers, taskAccess, chatAccess, logger))
}

func handleConnect(hub *Hub, auth Authenticator, wsMembers WorkspaceMembership, taskAccess TaskAccess, chatAccess ChatAccess, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		wsID := q.Get("workspace_id")
		wsSlug := q.Get("workspace_slug")
		if wsID == "" && wsSlug == "" {
			httpapi.BadRequest(w, "workspace_id or workspace_slug is required")
			return
		}

		// Slug/id резолвится до апгрейда: неизвестный workspace — 404,
		// ещё обычным HTTP-ответом (contract §2.1).
		var (
			workspaceID string
			found       bool
			err         error
		)
		if wsID != "" {
			workspaceID, found, err = wsMembers.ResolveID(r.Context(), wsID, false)
		} else {
			workspaceID, found, err = wsMembers.ResolveID(r.Context(), wsSlug, true)
		}
		if err != nil {
			httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
			return
		}
		if !found {
			httpapi.NotFound(w, "workspace not found")
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
		c.SetReadLimit(maxFrameBytes)
		ctx := r.Context()

		if !authenticated {
			frameCtx, cancel := context.WithTimeout(ctx, firstFrameTimeout)
			_, data, readErr := c.Read(frameCtx)
			cancel()
			if readErr != nil {
				_ = c.Close(websocket.StatusPolicyViolation, "auth timeout")
				return
			}
			var frame inFrame
			var token string
			if json.Unmarshal(data, &frame) == nil && frame.Type == "auth" {
				var p struct {
					Token string `json:"token"`
				}
				_ = json.Unmarshal(frame.Payload, &p)
				token = p.Token
			}
			if token == "" {
				writeFrame(ctx, c, outFrame{Type: "auth_error", Payload: map[string]any{"error": "missing token"}})
				_ = c.Close(websocket.StatusPolicyViolation, "auth required")
				return
			}
			actor, authenticated = auth.AuthenticateToken(ctx, token)
			if !authenticated {
				writeFrame(ctx, c, outFrame{Type: "auth_error", Payload: map[string]any{"error": "invalid token"}})
				_ = c.Close(websocket.StatusPolicyViolation, "invalid token")
				return
			}
			writeFrame(ctx, c, outFrame{Type: "auth_ack"})
		}

		isMember, err := wsMembers.IsMember(ctx, workspaceID, actor.UserID)
		if err != nil || !isMember {
			_ = c.Close(websocket.StatusPolicyViolation, "not a member")
			return
		}

		sock := newSocket(c)
		hub.registerConn(sock)
		defer hub.unregisterConn(sock)

		// После апгрейда клиент автоматически подписан на workspace:<id> и user:<id>.
		hub.join(roomKey("workspace", workspaceID), sock)
		hub.join(roomKey("user", actor.UserID), sock)
		defer hub.leaveAll(sock)

		logger.Info("realtime: подключение", "workspace_id", workspaceID, "user_id", actor.UserID)

		conn := &clientConn{
			hub: hub, sock: sock, logger: logger,
			workspaceID: workspaceID, userID: actor.UserID,
			taskAccess: taskAccess, chatAccess: chatAccess,
		}
		conn.readLoop(ctx)
	}
}

// inFrame/outFrame — форма кадров протокола §2.1: {"type":"...","payload":{...}}.
type inFrame struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

type outFrame struct {
	Type    string `json:"type"`
	Payload any    `json:"payload,omitempty"`
}

func writeFrame(ctx context.Context, c *websocket.Conn, f outFrame) {
	b, err := json.Marshal(f)
	if err != nil {
		return
	}
	writeCtx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	_ = c.Write(writeCtx, websocket.MessageText, b)
}

// clientConn держит per-connection состояние читающего цикла /ws, отдельно
// от Hub (который остаётся process-wide реестром комнат) — subscribe/
// unsubscribe только этому соединению нужны workspaceID/userID и ссылки на
// TaskAccess/ChatAccess.
type clientConn struct {
	hub    *Hub
	sock   *socket
	logger *slog.Logger

	workspaceID string
	userID      string

	taskAccess TaskAccess
	chatAccess ChatAccess
}

// readLoop читает фреймы клиента, пока соединение живо. Любой
// невалидный/неизвестный фрейм молча игнорируется (contract §2.1);
// протокол-уровневые ping/pong — забота coder/websocket (см. AcceptOptions
// по умолчанию), не JSON-фреймы этого цикла.
func (c *clientConn) readLoop(ctx context.Context) {
	for {
		_, data, err := c.sock.ws.Read(ctx)
		if err != nil {
			return
		}
		var frame inFrame
		if json.Unmarshal(data, &frame) != nil {
			continue
		}
		switch frame.Type {
		case "ping":
			writeFrame(ctx, c.sock.ws, outFrame{Type: "pong"})
		case "subscribe":
			c.handleSubscribe(ctx, frame.Payload)
		case "unsubscribe":
			c.handleUnsubscribe(ctx, frame.Payload)
		default:
			// неизвестный/невалидный фрейм — молча игнорируется (debug-лог на сервере).
			c.logger.Debug("realtime: неизвестный входящий фрейм", "type", frame.Type)
		}
	}
}

type scopePayload struct {
	Scope string `json:"scope"`
	ID    string `json:"id"`
}

func (c *clientConn) handleSubscribe(ctx context.Context, raw json.RawMessage) {
	var p scopePayload
	_ = json.Unmarshal(raw, &p)

	switch p.Scope {
	case "workspace":
		if p.ID != c.workspaceID {
			c.subscribeError(ctx, p, "forbidden")
			return
		}
		c.hub.join(roomKey("workspace", p.ID), c.sock)
	case "user":
		if p.ID != c.userID {
			c.subscribeError(ctx, p, "forbidden")
			return
		}
		c.hub.join(roomKey("user", p.ID), c.sock)
	case "task":
		if c.taskAccess == nil {
			c.subscribeError(ctx, p, "lookup_failed")
			return
		}
		found, allowed, err := c.taskAccess.CanAccessTask(ctx, c.workspaceID, c.userID, p.ID)
		if err != nil || !found {
			c.subscribeError(ctx, p, "lookup_failed")
			return
		}
		if !allowed {
			c.subscribeError(ctx, p, "forbidden")
			return
		}
		c.hub.join(roomKey("task", p.ID), c.sock)
	case "chat":
		if c.chatAccess == nil {
			c.subscribeError(ctx, p, "lookup_failed")
			return
		}
		found, allowed, err := c.chatAccess.CanAccessChat(ctx, c.workspaceID, c.userID, p.ID)
		if err != nil || !found {
			c.subscribeError(ctx, p, "lookup_failed")
			return
		}
		if !allowed {
			c.subscribeError(ctx, p, "forbidden")
			return
		}
		c.hub.join(roomKey("chat", p.ID), c.sock)
	default:
		c.subscribeError(ctx, p, "unknown_scope")
		return
	}
	writeFrame(ctx, c.sock.ws, outFrame{Type: "subscribe_ack", Payload: map[string]any{"scope": p.Scope, "id": p.ID}})
}

func (c *clientConn) subscribeError(ctx context.Context, p scopePayload, reason string) {
	writeFrame(ctx, c.sock.ws, outFrame{Type: "subscribe_error", Payload: map[string]any{
		"scope": p.Scope, "id": p.ID, "error": reason,
	}})
}

// handleUnsubscribe всегда отвечает unsubscribe_ack (contract §2.1), даже
// для scope/id, которым сокет не был подписан — leave на пустую комнату
// уже безопасный no-op.
func (c *clientConn) handleUnsubscribe(ctx context.Context, raw json.RawMessage) {
	var p scopePayload
	_ = json.Unmarshal(raw, &p)
	if p.Scope != "" && p.ID != "" {
		c.hub.leave(roomKey(p.Scope, p.ID), c.sock)
	}
	writeFrame(ctx, c.sock.ws, outFrame{Type: "unsubscribe_ack", Payload: map[string]any{"scope": p.Scope, "id": p.ID}})
}
