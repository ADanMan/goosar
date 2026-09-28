// ws.go — /api/daemon/ws (contract §2.2): апгрейд, daemon:heartbeat по
// каналу (тот же обработчик, что HTTP-heartbeat), daemon:rpc_request с
// единственным поддерживаемым методом tasks.claim, и push-сообщения
// сервер→демон (daemon:task_available и т.п.).
//
// Пробел спецификации (см. decisions.md): push daemon:task_available при
// постановке новой задачи в очередь другим доменом (task/chat/note через
// dispatch.Deps.Enqueue) не подключён — эти домены публикуют только
// task:queued в realtime.Hub (комната клиентского /ws), а не в реестр этого
// пакета. Референсный демон и без push продолжает работать: он периодически
// сам вызывает claim/heartbeat. notifyTaskAvailable вызывается там, где у
// этого пакета и так есть подходящий момент (после register/heartbeat), не
// на каждую постановку в очередь.
package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/adanman/goosar/server2/internal/httpapi"
)

const (
	wsMaxFrameBytes = 64 * 1024
	wsRPCConcurrent = 8
)

type wsFrame struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

type daemonConn struct {
	c          *websocket.Conn
	runtimeIDs map[string]struct{}
}

// wsRegistry — индекс живых /api/daemon/ws подключений по runtime_id, чтобы
// сервер мог адресно послать push-сообщение (daemon:task_available и т.п.)
// демону, обслуживающему конкретный runtime.
type wsRegistry struct {
	mu    sync.Mutex
	byRun map[string]map[*daemonConn]struct{}
}

func newWSRegistry() *wsRegistry {
	return &wsRegistry{byRun: make(map[string]map[*daemonConn]struct{})}
}

func (reg *wsRegistry) add(conn *daemonConn) {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	for id := range conn.runtimeIDs {
		if reg.byRun[id] == nil {
			reg.byRun[id] = make(map[*daemonConn]struct{})
		}
		reg.byRun[id][conn] = struct{}{}
	}
}

func (reg *wsRegistry) remove(conn *daemonConn) {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	for id := range conn.runtimeIDs {
		delete(reg.byRun[id], conn)
		if len(reg.byRun[id]) == 0 {
			delete(reg.byRun, id)
		}
	}
}

func (reg *wsRegistry) push(runtimeID string, frame wsFrame) {
	reg.mu.Lock()
	conns := make([]*daemonConn, 0, len(reg.byRun[runtimeID]))
	for c := range reg.byRun[runtimeID] {
		conns = append(conns, c)
	}
	reg.mu.Unlock()
	body, err := json.Marshal(frame)
	if err != nil {
		return
	}
	for _, c := range conns {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = c.c.Write(ctx, websocket.MessageText, body)
		cancel()
	}
}

// notifyTaskAvailable — daemon:task_available (см. пробел спецификации выше).
func (d *Deps) notifyTaskAvailable(runtimeID, taskID string) {
	payload := map[string]any{"runtime_id": runtimeID}
	if taskID != "" {
		payload["task_id"] = taskID
	}
	d.ws.push(runtimeID, wsFrame{Type: "daemon:task_available", Payload: mustJSON(payload)})
}

func mustJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

// handleWS — GET /api/daemon/ws.
func (d *Deps) handleWS(w http.ResponseWriter, r *http.Request) {
	actor, ok := httpapi.RequireActor(w, r)
	if !ok {
		return
	}
	runtimeIDs := parseRuntimeIDs(r)
	if len(runtimeIDs) == 0 && !actor.IsHuman {
		httpapi.BadRequest(w, "no runtime_ids and no authenticated user")
		return
	}
	verified := make(map[string]struct{}, len(runtimeIDs))
	workspaceIDs := map[string]struct{}{}
	for _, id := range runtimeIDs {
		ex, err := d.Runtime.Store.Get(r.Context(), id)
		if err != nil {
			httpapi.NotFound(w, "a given runtime does not exist")
			return
		}
		allowed, err := d.hasWorkspaceAccess(r.Context(), actor, ex.WorkspaceID)
		if err != nil || !allowed {
			httpapi.NotFound(w, "a given runtime does not exist, or belongs to a different daemon")
			return
		}
		if actor.Source == httpapi.SourceDaemon && actor.DaemonID != "" && (ex.DaemonID == nil || *ex.DaemonID != actor.DaemonID) {
			httpapi.NotFound(w, "a given runtime does not exist, or belongs to a different daemon")
			return
		}
		verified[id] = struct{}{}
		workspaceIDs[ex.WorkspaceID] = struct{}{}
	}

	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: []string{"*"}})
	if err != nil {
		return
	}
	c.SetReadLimit(wsMaxFrameBytes)
	conn := &daemonConn{c: c, runtimeIDs: verified}
	d.ws.add(conn)
	defer d.ws.remove(conn)
	defer c.Close(websocket.StatusNormalClosure, "")

	sem := make(chan struct{}, wsRPCConcurrent)
	ctx := r.Context()
	for {
		_, data, err := c.Read(ctx)
		if err != nil {
			return
		}
		var frame wsFrame
		if err := json.Unmarshal(data, &frame); err != nil {
			continue // невалидный фрейм молча игнорируется
		}
		switch frame.Type {
		case "daemon:heartbeat":
			d.handleWSHeartbeat(ctx, c, frame.Payload)
		case "daemon:rpc_request":
			select {
			case sem <- struct{}{}:
				go func(p json.RawMessage) {
					defer func() { <-sem }()
					d.handleWSRPC(ctx, actor, c, p)
				}(frame.Payload)
			default:
				d.respondRPCOverLimit(ctx, c, frame.Payload)
			}
		}
	}
}

func parseRuntimeIDs(r *http.Request) []string {
	var out []string
	q := r.URL.Query()
	out = append(out, q["runtime_id"]...)
	if joined := q.Get("runtime_ids"); joined != "" {
		out = append(out, strings.Split(joined, ",")...)
	}
	return out
}

func (d *Deps) handleWSHeartbeat(ctx context.Context, c *websocket.Conn, payload json.RawMessage) {
	var req struct {
		RuntimeID string `json:"runtime_id"`
	}
	if err := json.Unmarshal(payload, &req); err != nil || req.RuntimeID == "" {
		return
	}
	ack, err := d.heartbeatAck(ctx, req.RuntimeID)
	if err != nil {
		return
	}
	body := mustJSON(wsFrame{Type: "daemon:heartbeat_ack", Payload: mustJSON(ack)})
	writeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = c.Write(writeCtx, websocket.MessageText, body)
}

type rpcRequest struct {
	RequestID string          `json:"request_id"`
	Method    string          `json:"method"`
	Body      json.RawMessage `json:"body"`
}

func (d *Deps) handleWSRPC(ctx context.Context, actor *httpapi.Actor, c *websocket.Conn, payload json.RawMessage) {
	var req rpcRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		return
	}
	var status int
	var body any
	var rpcErr string
	switch req.Method {
	case "tasks.claim":
		var claimReq batchClaimRequest
		if err := json.Unmarshal(req.Body, &claimReq); err != nil {
			status, rpcErr = http.StatusBadRequest, "invalid body"
		} else {
			tasks, st, msg := d.claimBatch(ctx, actor, claimReq)
			status = st
			if st == http.StatusOK {
				body = map[string]any{"tasks": tasks}
			} else {
				rpcErr = msg
			}
		}
	default:
		status, rpcErr = http.StatusNotFound, "unknown rpc method \""+req.Method+"\""
	}
	resp := map[string]any{"request_id": req.RequestID, "status": status}
	if body != nil {
		resp["body"] = body
	}
	if rpcErr != "" {
		resp["error"] = rpcErr
	}
	frame := mustJSON(wsFrame{Type: "daemon:rpc_response", Payload: mustJSON(resp)})
	writeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = c.Write(writeCtx, websocket.MessageText, frame)
}

func (d *Deps) respondRPCOverLimit(ctx context.Context, c *websocket.Conn, payload json.RawMessage) {
	var req rpcRequest
	_ = json.Unmarshal(payload, &req)
	resp := map[string]any{"request_id": req.RequestID, "status": http.StatusTooManyRequests}
	frame := mustJSON(wsFrame{Type: "daemon:rpc_response", Payload: mustJSON(resp)})
	writeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = c.Write(writeCtx, websocket.MessageText, frame)
}
