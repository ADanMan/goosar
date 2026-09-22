package realtime

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/websocket"

	"github.com/adanman/goosar/server/internal/auth"
)

type MembershipChecker interface {
	IsMember(ctx context.Context, userID, workspaceID string, claimedVersion *int32) bool
}

type SlugResolver func(ctx context.Context, slug string) (workspaceID string, err error)

type PATResolver interface {
	ResolveToken(ctx context.Context, token string) (userID string, ok bool)
}

type ScopeAuthorizer interface {
	AuthorizeScope(ctx context.Context, userID, workspaceID, scopeType, scopeID string) (bool, error)
}

var (
	allowedWSOrigins atomic.Value
	trustedProxies   atomic.Value
)

func init() {
	allowedWSOrigins.Store(loadAllowedOrigins())
	trustedProxies.Store(loadTrustedProxies())
}

func loadAllowedOrigins() []string {
	raw := strings.TrimSpace(os.Getenv("ALLOWED_ORIGINS"))
	if raw == "" {
		raw = strings.TrimSpace(os.Getenv("CORS_ALLOWED_ORIGINS"))
	}
	if raw == "" {
		raw = strings.TrimSpace(os.Getenv("FRONTEND_ORIGIN"))
	}
	if raw == "" {
		return []string{
			"http://localhost:3000",
			"http://localhost:5173",
			"http://localhost:5174",
		}
	}

	parts := strings.Split(raw, ",")
	origins := make([]string, 0, len(parts))
	for _, part := range parts {
		origin := strings.TrimSpace(part)
		if origin != "" {
			origins = append(origins, origin)
		}
	}
	return origins
}

func loadTrustedProxies() []netip.Prefix {
	raw := strings.TrimSpace(os.Getenv("GOOSAR_TRUSTED_PROXIES"))
	if raw == "" {
		return nil
	}
	var prefixes []netip.Prefix
	for _, part := range strings.Split(raw, ",") {
		s := strings.TrimSpace(part)
		if s == "" {
			continue
		}
		p, err := netip.ParsePrefix(s)
		if err != nil {
			slog.Warn("ws: ignoring invalid trusted proxy CIDR", "value", s, "error", err)
			continue
		}
		prefixes = append(prefixes, p)
	}
	return prefixes
}

func SetAllowedOrigins(origins []string) {
	allowedWSOrigins.Store(origins)
}

func SetTrustedProxies(proxies []netip.Prefix) {
	trustedProxies.Store(proxies)
}

func isTrustedProxy(remoteAddr string) bool {
	proxies := trustedProxies.Load().([]netip.Prefix)
	if len(proxies) == 0 {
		return false
	}
	addr, err := netip.ParseAddr(remoteHost(remoteAddr))
	if err != nil {
		return false
	}
	addr = addr.Unmap()
	for _, p := range proxies {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

func remoteHost(remoteAddr string) string {
	if host, _, err := net.SplitHostPort(remoteAddr); err == nil {
		return host
	}
	return strings.Trim(remoteAddr, "[]")
}

func firstForwardedHost(h string) string {
	if i := strings.IndexByte(h, ','); i >= 0 {
		h = h[:i]
	}
	return strings.TrimSpace(h)
}

func checkOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}

	if u, err := url.Parse(origin); err == nil && strings.EqualFold(u.Host, r.Host) {
		return true
	}

	if fwdHost := firstForwardedHost(r.Header.Get("X-Forwarded-Host")); fwdHost != "" && isTrustedProxy(r.RemoteAddr) {
		if u, err := url.Parse(origin); err == nil && strings.EqualFold(u.Host, fwdHost) {
			return true
		}
	}
	origins := allowedWSOrigins.Load().([]string)
	for _, allowed := range origins {
		if origin == allowed {
			return true
		}
	}
	slog.Warn("ws: rejected origin", "origin", origin, "remote_addr", r.RemoteAddr)
	return false
}

const (
	writeWait  = 10 * time.Second
	pongWait   = 60 * time.Second
	pingPeriod = (pongWait * 9) / 10

	inboundReadLimit = 64 * 1024
)

var upgrader = websocket.Upgrader{
	CheckOrigin: checkOrigin,
}

type scopeKey struct {
	Type string
	ID   string
}

func sk(t, id string) scopeKey { return scopeKey{Type: t, ID: id} }

type Client struct {
	hub         *Hub
	conn        *websocket.Conn
	send        chan []byte
	userID      string
	workspaceID string

	subscriptions map[scopeKey]bool

	dedupMu  sync.Mutex
	seenIDs  map[string]struct{}
	seenList []string
}

const dedupCapacity = 128

func (c *Client) markSeen(eventID string) bool {
	if eventID == "" {
		return true
	}
	c.dedupMu.Lock()
	defer c.dedupMu.Unlock()
	if c.seenIDs == nil {
		c.seenIDs = make(map[string]struct{}, dedupCapacity)
	}
	if _, ok := c.seenIDs[eventID]; ok {
		return false
	}
	c.seenIDs[eventID] = struct{}{}
	c.seenList = append(c.seenList, eventID)
	if len(c.seenList) > dedupCapacity {
		drop := c.seenList[0]
		c.seenList = c.seenList[1:]
		delete(c.seenIDs, drop)
	}
	return true
}

type SubscriptionCallback func(scopeType, scopeID string)

type Hub struct {
	rooms      map[scopeKey]map[*Client]bool
	clients    map[*Client]bool
	broadcast  chan []byte
	register   chan *Client
	unregister chan *Client
	mu         sync.RWMutex

	authorizer ScopeAuthorizer

	onFirstSubscriber SubscriptionCallback
	onLastSubscriber  SubscriptionCallback
}

func NewHub() *Hub {
	return &Hub{
		rooms:      make(map[scopeKey]map[*Client]bool),
		clients:    make(map[*Client]bool),
		broadcast:  make(chan []byte),
		register:   make(chan *Client),
		unregister: make(chan *Client),
	}
}

func (h *Hub) SetAuthorizer(a ScopeAuthorizer) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.authorizer = a
}

func (h *Hub) SetSubscriptionCallbacks(onFirst, onLast SubscriptionCallback) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.onFirstSubscriber = onFirst
	h.onLastSubscriber = onLast
}

func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			total := len(h.clients)
			h.mu.Unlock()
			M.ConnectsTotal.Add(1)
			M.ActiveConnections.Add(1)

			h.subscribe(client, ScopeWorkspace, client.workspaceID)
			if client.userID != "" {
				h.subscribe(client, ScopeUser, client.userID)
			}
			slog.Info("ws client connected", "workspace_id", client.workspaceID, "user_id", client.userID, "total_clients", total)

		case client := <-h.unregister:
			h.removeClient(client)

		case message := <-h.broadcast:
			h.fanoutAll(message, "")
		}
	}
}

func (h *Hub) removeClient(client *Client) {
	h.mu.Lock()
	if !h.clients[client] {
		h.mu.Unlock()
		return
	}
	delete(h.clients, client)
	subs := client.subscriptions
	client.subscriptions = nil
	emptied := make([]scopeKey, 0, len(subs))
	for key := range subs {
		if room, ok := h.rooms[key]; ok {
			delete(room, client)
			if len(room) == 0 {
				delete(h.rooms, key)
				emptied = append(emptied, key)
			}
		}
	}
	close(client.send)
	cb := h.onLastSubscriber
	total := len(h.clients)
	h.mu.Unlock()

	M.DisconnectsTotal.Add(1)
	M.ActiveConnections.Add(-1)
	if cb != nil {
		for _, key := range emptied {
			cb(key.Type, key.ID)
		}
	}
	for _, key := range emptied {
		M.DecRoom(key.Type)
	}
	slog.Info("ws client disconnected", "workspace_id", client.workspaceID, "user_id", client.userID, "total_clients", total)
}

func (h *Hub) subscribe(client *Client, scopeType, scopeID string) bool {
	if scopeType == "" || scopeID == "" {
		return false
	}
	key := sk(scopeType, scopeID)

	h.mu.Lock()
	if !h.clients[client] {
		h.mu.Unlock()
		return false
	}
	if client.subscriptions == nil {
		client.subscriptions = map[scopeKey]bool{}
	}
	if client.subscriptions[key] {
		h.mu.Unlock()
		return false
	}
	client.subscriptions[key] = true
	room, ok := h.rooms[key]
	first := false
	if !ok {
		room = make(map[*Client]bool)
		h.rooms[key] = room
		first = true
	}
	room[client] = true
	cb := h.onFirstSubscriber
	h.mu.Unlock()

	M.SubscribesTotal(scopeType).Add(1)
	if first {
		M.IncRoom(scopeType)
		if cb != nil {
			cb(scopeType, scopeID)
		}
	}
	return true
}

func (h *Hub) unsubscribe(client *Client, scopeType, scopeID string) bool {
	if scopeType == "" || scopeID == "" {
		return false
	}
	key := sk(scopeType, scopeID)

	h.mu.Lock()
	if !h.clients[client] {
		h.mu.Unlock()
		return false
	}
	if client.subscriptions == nil || !client.subscriptions[key] {
		h.mu.Unlock()
		return false
	}
	delete(client.subscriptions, key)
	emptied := false
	if room, ok := h.rooms[key]; ok {
		delete(room, client)
		if len(room) == 0 {
			delete(h.rooms, key)
			emptied = true
		}
	}
	cb := h.onLastSubscriber
	h.mu.Unlock()

	M.UnsubscribesTotal(scopeType).Add(1)
	if emptied {
		M.DecRoom(scopeType)
		if cb != nil {
			cb(scopeType, scopeID)
		}
	}
	return true
}

func (h *Hub) HasLocalSubscribers(scopeType, scopeID string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	_, ok := h.rooms[sk(scopeType, scopeID)]
	return ok
}

func (h *Hub) LocalScopes() []scopeKey {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make([]scopeKey, 0, len(h.rooms))
	for k := range h.rooms {
		out = append(out, k)
	}
	return out
}

func (h *Hub) BroadcastToScope(scopeType, scopeID string, message []byte) {
	h.BroadcastToScopeDedup(scopeType, scopeID, message, "")
}

func (h *Hub) BroadcastToScopeDedup(scopeType, scopeID string, message []byte, eventID string) {
	if scopeType == "" || scopeID == "" {
		return
	}
	key := sk(scopeType, scopeID)

	h.mu.RLock()
	clients := h.rooms[key]
	var slow []*Client
	var sent int64
	for client := range clients {
		if !client.markSeen(eventID) {
			continue
		}
		select {
		case client.send <- message:
			sent++
		default:
			slow = append(slow, client)
		}
	}
	h.mu.RUnlock()

	if sent > 0 {
		M.MessagesSentTotal.Add(sent)
	}
	if len(slow) > 0 {
		h.evictSlow(slow)
	}
}

func (h *Hub) fanoutAll(message []byte, excludeWorkspace string) {
	h.fanoutAllDedup(message, excludeWorkspace, "")
}

func (h *Hub) fanoutAllDedup(message []byte, excludeWorkspace, eventID string) {
	h.mu.RLock()
	var slow []*Client
	var sent int64
	for client := range h.clients {
		if excludeWorkspace != "" && client.workspaceID == excludeWorkspace {
			continue
		}
		if !client.markSeen(eventID) {
			continue
		}
		select {
		case client.send <- message:
			sent++
		default:
			slow = append(slow, client)
		}
	}
	h.mu.RUnlock()

	if sent > 0 {
		M.MessagesSentTotal.Add(sent)
	}
	if len(slow) > 0 {
		h.evictSlow(slow)
	}
}

func (h *Hub) BroadcastToWorkspace(workspaceID string, message []byte) {
	h.BroadcastToScope(ScopeWorkspace, workspaceID, message)
}

func (h *Hub) SendToUser(userID string, message []byte, excludeWorkspace ...string) {
	exclude := ""
	if len(excludeWorkspace) > 0 {
		exclude = excludeWorkspace[0]
	}
	h.fanoutUser(userID, message, exclude, "")
}

func (h *Hub) Broadcast(message []byte) {
	h.broadcast <- message
}

func (h *Hub) fanoutUser(userID string, message []byte, excludeWorkspace, eventID string) {
	key := sk(ScopeUser, userID)
	h.mu.RLock()
	clients := h.rooms[key]
	var slow []*Client
	var sent int64
	for client := range clients {
		if excludeWorkspace != "" && client.workspaceID == excludeWorkspace {
			continue
		}
		if !client.markSeen(eventID) {
			continue
		}
		select {
		case client.send <- message:
			sent++
		default:
			slow = append(slow, client)
		}
	}
	h.mu.RUnlock()
	if sent > 0 {
		M.MessagesSentTotal.Add(sent)
	}
	if len(slow) > 0 {
		h.evictSlow(slow)
	}
}

func (h *Hub) evictSlow(slow []*Client) {
	M.MessagesDroppedTotal.Add(int64(len(slow)))
	M.SlowEvictionsTotal.Add(int64(len(slow)))

	h.mu.Lock()
	evicted := 0
	type emptied struct {
		Type, ID string
	}
	var drainedRooms []emptied
	for _, c := range slow {
		if !h.clients[c] {
			continue
		}
		delete(h.clients, c)
		for key := range c.subscriptions {
			if room, ok := h.rooms[key]; ok {
				delete(room, c)
				if len(room) == 0 {
					delete(h.rooms, key)
					drainedRooms = append(drainedRooms, emptied{key.Type, key.ID})
				}
			}
		}
		c.subscriptions = nil
		close(c.send)
		evicted++
	}
	cb := h.onLastSubscriber
	h.mu.Unlock()

	if evicted > 0 {
		M.ActiveConnections.Add(int64(-evicted))
		M.DisconnectsTotal.Add(int64(evicted))
	}
	for _, r := range drainedRooms {
		M.DecRoom(r.Type)
	}
	if cb != nil {
		for _, r := range drainedRooms {
			cb(r.Type, r.ID)
		}
	}
}

func (h *Hub) Snapshot() map[string]any {
	h.mu.RLock()
	defer h.mu.RUnlock()
	rooms := map[string]int{}
	for key := range h.rooms {
		rooms[key.Type]++
	}
	return map[string]any{
		"connections": len(h.clients),
		"rooms":       rooms,
	}
}

func authenticateToken(tokenStr string, pr PATResolver, ctx context.Context) (string, *int32, string) {
	if strings.HasPrefix(tokenStr, auth.PATPrefix) {
		if pr == nil {
			return "", nil, `{"error":"invalid token"}`
		}
		uid, ok := pr.ResolveToken(ctx, tokenStr)
		if !ok {
			return "", nil, `{"error":"invalid token"}`
		}
		return uid, nil, ""
	}

	token, err := auth.ParseSessionHS256(tokenStr)
	if err != nil || !token.Valid {
		return "", nil, `{"error":"invalid token"}`
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return "", nil, `{"error":"invalid claims"}`
	}

	uid, ok := claims["sub"].(string)
	if !ok || strings.TrimSpace(uid) == "" {
		return "", nil, `{"error":"invalid claims"}`
	}

	claimedVersion := int32(0)
	if raw, ok := claims["tv"].(float64); ok {
		claimedVersion = int32(raw)
	}
	return uid, &claimedVersion, ""
}

func firstMessageAuth(conn *websocket.Conn) (token, errMsg string, closed bool) {
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	defer conn.SetReadDeadline(time.Time{})

	_, raw, err := conn.ReadMessage()
	if err != nil {
		if errors.Is(err, websocket.ErrReadLimit) {

			M.InboundTooLargeTotal.Add(1)
			slog.Warn("ws: pre-auth frame exceeded read limit", "limit_bytes", inboundReadLimit)
			conn.Close()
			return "", "", true
		}
		return "", `{"error":"auth timeout or read error"}`, false
	}

	var msg struct {
		Type    string `json:"type"`
		Payload struct {
			Token string `json:"token"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(raw, &msg); err != nil || msg.Type != "auth" || msg.Payload.Token == "" {
		return "", `{"error":"expected auth message as first frame"}`, false
	}

	return msg.Payload.Token, "", false
}

type wsMessageWriter interface {
	WriteMessage(messageType int, data []byte) error
}

func writeWSAuthFrame(conn wsMessageWriter, payload []byte, frame string, attrs ...any) bool {
	if err := conn.WriteMessage(websocket.TextMessage, payload); err != nil {
		logAttrs := append([]any{"frame", frame, "error", err}, attrs...)
		slog.Warn("ws: failed to send auth frame", logAttrs...)
		return false
	}
	return true
}

func writeWSAuthErrorAndClose(conn *websocket.Conn, payload []byte, attrs ...any) {
	writeWSAuthFrame(conn, payload, "auth_error", attrs...)
	conn.Close()
}

func HandleWebSocket(hub *Hub, mc MembershipChecker, pr PATResolver, resolveSlug SlugResolver, w http.ResponseWriter, r *http.Request) {
	workspaceID := r.URL.Query().Get("workspace_id")
	if workspaceID == "" {
		if slug := r.URL.Query().Get("workspace_slug"); slug != "" && resolveSlug != nil {
			resolved, err := resolveSlug(r.Context(), slug)
			if err != nil {
				http.Error(w, `{"error":"workspace not found"}`, http.StatusNotFound)
				return
			}
			workspaceID = resolved
		}
	}
	if workspaceID == "" {
		http.Error(w, `{"error":"workspace_id or workspace_slug required"}`, http.StatusBadRequest)
		return
	}

	var userID string
	if cookie, err := r.Cookie(auth.AuthCookieName); err == nil && cookie.Value != "" {
		uid, claimedVersion, errMsg := authenticateToken(cookie.Value, pr, r.Context())
		if errMsg != "" {
			http.Error(w, errMsg, http.StatusUnauthorized)
			return
		}
		if !mc.IsMember(r.Context(), uid, workspaceID, claimedVersion) {
			http.Error(w, `{"error":"not a member of this workspace"}`, http.StatusForbidden)
			return
		}
		userID = uid
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Error("websocket upgrade failed", "error", err)
		return
	}

	conn.SetReadLimit(inboundReadLimit)

	if userID == "" {
		tokenStr, errMsg, closed := firstMessageAuth(conn)
		if closed {
			return
		}
		if errMsg != "" {
			writeWSAuthErrorAndClose(conn, []byte(errMsg), "workspace_id", workspaceID)
			return
		}
		uid, claimedVersion, errMsg := authenticateToken(tokenStr, pr, r.Context())
		if errMsg != "" {
			writeWSAuthErrorAndClose(conn, []byte(errMsg), "workspace_id", workspaceID)
			return
		}
		if !mc.IsMember(r.Context(), uid, workspaceID, claimedVersion) {
			writeWSAuthErrorAndClose(
				conn,
				[]byte(`{"error":"not a member of this workspace"}`),
				"workspace_id", workspaceID,
				"user_id", uid,
			)
			return
		}
		userID = uid

		if !writeWSAuthFrame(
			conn,
			[]byte(`{"type":"auth_ack"}`),
			"auth_ack",
			"workspace_id", workspaceID,
			"user_id", userID,
		) {
			conn.Close()
			return
		}
	}

	clientPlatform := r.URL.Query().Get("client_platform")
	clientVersion := r.URL.Query().Get("client_version")
	clientOS := r.URL.Query().Get("client_os")
	slog.Info("websocket connected",
		"user_id", userID,
		"workspace_id", workspaceID,
		"client_platform", clientPlatform,
		"client_version", clientVersion,
		"client_os", clientOS,
	)

	client := &Client{
		hub:         hub,
		conn:        conn,
		send:        make(chan []byte, 256),
		userID:      userID,
		workspaceID: workspaceID,
	}
	hub.register <- client

	go client.writePump()
	go client.readPump()
}

type inboundFrame struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

type subPayload struct {
	Scope string `json:"scope"`
	ID    string `json:"id"`
}

func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()

	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, raw, err := c.conn.ReadMessage()
		if err != nil {
			switch {
			case errors.Is(err, websocket.ErrReadLimit):

				M.InboundTooLargeTotal.Add(1)
				slog.Warn("ws: inbound frame exceeded read limit",
					"limit_bytes", inboundReadLimit,
					"user_id", c.userID,
					"workspace_id", c.workspaceID,
				)
			case websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure):
				slog.Debug("websocket read error", "error", err, "user_id", c.userID, "workspace_id", c.workspaceID)
			}
			break
		}
		c.handleFrame(raw)
	}
}

func (c *Client) handleFrame(raw []byte) {
	var f inboundFrame
	if err := json.Unmarshal(raw, &f); err != nil {
		slog.Debug("ws inbound: invalid json", "error", err, "user_id", c.userID)
		return
	}
	switch f.Type {
	case "subscribe", "unsubscribe":
		var p subPayload
		if err := json.Unmarshal(f.Payload, &p); err != nil || p.Scope == "" || p.ID == "" {
			c.sendJSON(map[string]any{
				"type": f.Type + "_error",
				"payload": map[string]string{
					"scope": p.Scope,
					"id":    p.ID,
					"error": "invalid payload",
				},
			})
			return
		}
		if f.Type == "subscribe" {
			c.handleSubscribe(p.Scope, p.ID)
		} else {
			c.handleUnsubscribe(p.Scope, p.ID)
		}
	case "ping":
		c.sendJSON(map[string]string{"type": "pong"})
	default:

		slog.Debug("ws inbound: unknown frame", "type", f.Type, "user_id", c.userID)
	}
}

func (c *Client) handleSubscribe(scope, id string) {
	switch scope {
	case ScopeWorkspace, ScopeUser:

		if (scope == ScopeWorkspace && id != c.workspaceID) || (scope == ScopeUser && id != c.userID) {
			M.SubscribeDeniedTotal(scope).Add(1)
			c.sendJSON(map[string]any{
				"type": "subscribe_error",
				"payload": map[string]string{
					"scope": scope,
					"id":    id,
					"error": "forbidden",
				},
			})
			return
		}

		c.hub.subscribe(c, scope, id)
	case ScopeTask, ScopeChat:
		auth := c.hub.authorizer
		if auth != nil {
			ok, err := auth.AuthorizeScope(context.Background(), c.userID, c.workspaceID, scope, id)
			if err != nil || !ok {
				M.SubscribeDeniedTotal(scope).Add(1)
				reason := "forbidden"
				if err != nil {
					reason = "lookup_failed"
				}
				c.sendJSON(map[string]any{
					"type": "subscribe_error",
					"payload": map[string]string{
						"scope": scope,
						"id":    id,
						"error": reason,
					},
				})
				return
			}
		}
		c.hub.subscribe(c, scope, id)
	default:
		M.SubscribeDeniedTotal(scope).Add(1)
		c.sendJSON(map[string]any{
			"type": "subscribe_error",
			"payload": map[string]string{
				"scope": scope,
				"id":    id,
				"error": "unknown_scope",
			},
		})
		return
	}
	c.sendJSON(map[string]any{
		"type":    "subscribe_ack",
		"payload": map[string]string{"scope": scope, "id": id},
	})
}

func (c *Client) handleUnsubscribe(scope, id string) {
	c.hub.unsubscribe(c, scope, id)
	c.sendJSON(map[string]any{
		"type":    "unsubscribe_ack",
		"payload": map[string]string{"scope": scope, "id": id},
	})
}

func (c *Client) sendJSON(v any) {
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	select {
	case c.send <- data:
	default:
	}
}

func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
				slog.Warn("websocket write error", "error", err, "user_id", c.userID, "workspace_id", c.workspaceID)
				return
			}
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (h *Hub) DisconnectUser(userID, workspaceID string) int {
	if userID == "" {
		return 0
	}
	h.mu.RLock()
	victims := make([]*Client, 0, 2)
	for client := range h.clients {
		if client.userID != userID {
			continue
		}
		if workspaceID != "" && client.workspaceID != workspaceID {
			continue
		}
		victims = append(victims, client)
	}
	h.mu.RUnlock()

	for _, client := range victims {

		h.removeClient(client)
		if client.conn != nil {
			_ = client.conn.Close()
		}
	}
	if len(victims) > 0 {
		slog.Info("ws connections closed by offboarding", "user_id", userID, "workspace_id", workspaceID, "closed", len(victims))
	}
	return len(victims)
}
