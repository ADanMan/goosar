package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"

	"github.com/adanman/goosar/server/pkg/protocol"
)

var errRuntimeSetChanged = errors.New("runtime set changed")

const (
	taskWakeupMaxBackoff = 30 * time.Second

	taskWakeupReadLimit int64 = 64 << 20
)

type taskWakeupTimings struct {
	pongWait          time.Duration
	writeWait         time.Duration
	backoffResetAfter time.Duration
}

var defaultTaskWakeupTimings = taskWakeupTimings{
	pongWait:          60 * time.Second,
	writeWait:         10 * time.Second,
	backoffResetAfter: 10 * time.Second,
}

var taskWakeupTimingsHook atomic.Pointer[taskWakeupTimings]

func currentTaskWakeupTimings() taskWakeupTimings {
	if override := taskWakeupTimingsHook.Load(); override != nil {
		return *override
	}
	return defaultTaskWakeupTimings
}

type taskWakeup struct {
	runtimeID string
}

func taskWakeupDialer() websocket.Dialer {
	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
		Proxy:            http.ProxyFromEnvironment,
	}
	if transport, ok := http.DefaultTransport.(*http.Transport); ok && transport.TLSClientConfig != nil {
		dialer.TLSClientConfig = transport.TLSClientConfig.Clone()
	}
	return dialer
}

func (d *Daemon) taskWakeupLoop(ctx context.Context, taskWakeups chan<- taskWakeup) {
	backoff := time.Second
	runtimeSetCh, unsub := d.runtimeSet.Subscribe()
	defer unsub()

	for {
		runtimeIDs := d.allRuntimeIDs()
		connectedFor, err := d.runTaskWakeupConnection(ctx, runtimeIDs, taskWakeups, runtimeSetCh)
		if ctx.Err() != nil {
			return
		}
		if errors.Is(err, errRuntimeSetChanged) {
			backoff = time.Second
			continue
		}
		if shouldResetTaskWakeupBackoff(connectedFor) {
			backoff = time.Second
		}
		if err != nil {
			d.logger.Debug("task wakeup websocket unavailable; polling fallback remains active", "error", err, "retry_in", backoff)
		}

		if err := sleepWithContextOrRuntimeChange(ctx, jitterDuration(backoff), runtimeSetCh); err != nil {
			return
		}
		if backoff < taskWakeupMaxBackoff {
			backoff *= 2
			if backoff > taskWakeupMaxBackoff {
				backoff = taskWakeupMaxBackoff
			}
		}
	}
}

func shouldResetTaskWakeupBackoff(connectedFor time.Duration) bool {
	if connectedFor <= 0 {
		return false
	}
	resetAfter := currentTaskWakeupTimings().backoffResetAfter
	return resetAfter <= 0 || connectedFor >= resetAfter
}

func jitterDuration(d time.Duration) time.Duration {
	if d <= 0 {
		return d
	}
	spread := d / 5
	if spread <= 0 {
		return d
	}
	delta := time.Duration(rand.Int63n(int64(spread)*2+1)) - spread
	return d + delta
}

func (d *Daemon) runTaskWakeupConnection(ctx context.Context, runtimeIDs []string, taskWakeups chan<- taskWakeup, runtimeSetCh <-chan struct{}) (time.Duration, error) {
	wsURL, err := taskWakeupURL(d.cfg.ServerBaseURL, runtimeIDs)
	if err != nil {
		return 0, err
	}

	headers := http.Header{}
	if token := d.client.Token(); token != "" {
		headers.Set("Authorization", "Bearer "+token)
	}
	if d.client.platform != "" {
		headers.Set("X-Client-Platform", d.client.platform)
	}
	if d.client.version != "" {
		headers.Set("X-Client-Version", d.client.version)
	}
	if d.client.os != "" {
		headers.Set("X-Client-OS", d.client.os)
	}

	headers.Set("X-Client-Capabilities", daemonClientCapabilities())

	dialer := taskWakeupDialer()
	conn, _, err := dialer.DialContext(ctx, wsURL, headers)
	if err != nil {
		return 0, err
	}
	connectedAt := time.Now()
	uptime := func() time.Duration { return time.Since(connectedAt) }
	defer conn.Close()

	defer d.clearWSHeartbeatAcks()

	d.logger.Info("task wakeup websocket connected", "runtimes", len(runtimeIDs))
	signalTaskWakeup(taskWakeups, "")

	if d.reconcile != nil {
		d.reconcile.broadcast()
	}

	writeBufSize := 16
	if 2*len(runtimeIDs) > writeBufSize {
		writeBufSize = 2 * len(runtimeIDs)
	}
	writes := make(chan *wsOutbound, writeBufSize)
	writerDone := make(chan struct{})
	go d.runWSWriter(conn, writes, writerDone)

	var sendMu sync.Mutex
	sendClosed := false
	wsRPCGeneration := d.wsRPC.attach(func(frame []byte) (*wsOutbound, error) {
		sendMu.Lock()
		defer sendMu.Unlock()
		if sendClosed {
			return nil, errWSRPCUnavailable
		}
		item := &wsOutbound{data: frame}
		select {
		case writes <- item:
			return item, nil
		default:
			return nil, errWSRPCWriteBufferFull
		}
	})

	d.batchClaimUnsupported.Store(false)

	heartbeatCtx, cancelHeartbeat := context.WithCancel(ctx)
	hbDone := make(chan struct{})
	go func() {
		defer close(hbDone)
		d.runWSHeartbeatSender(heartbeatCtx, runtimeIDs, writes)
	}()

	errCh := make(chan error, 1)
	go func() {
		errCh <- d.readTaskWakeupMessagesForConnection(conn, taskWakeups, wsRPCGeneration)
	}()

	defer func() {

		conn.Close()

		d.wsRPC.attach(nil)
		sendMu.Lock()
		sendClosed = true
		sendMu.Unlock()
		cancelHeartbeat()
		<-hbDone
		close(writes)
		<-writerDone
	}()

	select {
	case <-ctx.Done():
		return uptime(), ctx.Err()
	case <-runtimeSetCh:
		return uptime(), errRuntimeSetChanged
	case err := <-errCh:
		return uptime(), err
	}
}

func (d *Daemon) runWSWriter(conn *websocket.Conn, writes <-chan *wsOutbound, done chan<- struct{}) {
	defer close(done)
	for item := range writes {

		if !item.beginWrite() {
			continue
		}
		conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		if err := conn.WriteMessage(websocket.TextMessage, item.data); err != nil {
			d.logger.Debug("task wakeup websocket write failed", "error", err)
			conn.Close()

			for range writes {
			}
			return
		}
	}
}

func (d *Daemon) runWSHeartbeatSender(ctx context.Context, runtimeIDs []string, writes chan<- *wsOutbound) {
	d.sendWSHeartbeats(ctx, runtimeIDs, writes)
	interval := d.cfg.HeartbeatInterval
	if interval <= 0 {
		interval = 15 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.sendWSHeartbeats(ctx, runtimeIDs, writes)
		}
	}
}

func (d *Daemon) sendWSHeartbeats(ctx context.Context, runtimeIDs []string, writes chan<- *wsOutbound) {
	for _, rid := range runtimeIDs {
		if ctx.Err() != nil {
			return
		}
		frame, err := json.Marshal(protocol.Message{
			Type:    protocol.EventDaemonHeartbeat,
			Payload: marshalRaw(protocol.DaemonHeartbeatRequestPayload{RuntimeID: rid, SupportsBatchImport: true}),
		})
		if err != nil {
			d.logger.Debug("ws heartbeat marshal failed", "error", err, "runtime_id", rid)
			continue
		}
		select {
		case writes <- &wsOutbound{data: frame}:
		case <-ctx.Done():
			return
		default:

			d.logger.Debug("ws heartbeat dropped: writer backlog", "runtime_id", rid)
		}
	}
}

func marshalRaw(v any) json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return data
}

func (d *Daemon) handleWSHeartbeatAck(ctx context.Context, ack *HeartbeatResponse) {
	d.handleWSHeartbeatAckForConnection(ctx, ack, d.wsRPC.currentGeneration())
}

func (d *Daemon) handleWSHeartbeatAckForConnection(ctx context.Context, ack *HeartbeatResponse, wsRPCGeneration uint64) {
	if ack == nil || ack.RuntimeID == "" {
		return
	}
	if ack.RuntimeGone {
		go d.handleRuntimeGone(ack.RuntimeID)
		return
	}
	for _, capability := range ack.ServerCapabilities {
		if capability == protocol.DaemonCapabilityRPCV1 {
			d.wsRPC.markRPCV1Supported(wsRPCGeneration)
			break
		}
	}
	d.recordWSHeartbeatAck(ack.RuntimeID)
	d.handleHeartbeatActions(ctx, ack.RuntimeID, ack)
}

func (d *Daemon) readTaskWakeupMessages(conn *websocket.Conn, taskWakeups chan<- taskWakeup) error {
	return d.readTaskWakeupMessagesForConnection(conn, taskWakeups, d.wsRPC.currentGeneration())
}

func (d *Daemon) readTaskWakeupMessagesForConnection(conn *websocket.Conn, taskWakeups chan<- taskWakeup, wsRPCGeneration uint64) error {
	d.configureTaskWakeupReadLiveness(conn)
	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			return err
		}
		if err := d.extendTaskWakeupReadDeadline(conn); err != nil {
			return err
		}
		var сообщение protocol.Message
		if err := json.Unmarshal(raw, &сообщение); err != nil {
			d.logger.Debug("task wakeup websocket invalid message", "error", err)
			continue
		}
		switch сообщение.Type {
		case protocol.EventDaemonTaskAvailable:
			var payload protocol.TaskAvailablePayload
			if len(сообщение.Payload) > 0 {
				if err := json.Unmarshal(сообщение.Payload, &payload); err != nil {
					d.logger.Debug("task wakeup websocket invalid payload", "error", err)
					continue
				}
			}
			if payload.RuntimeID != "" {
				d.logger.Debug("task wakeup received", "runtime_id", payload.RuntimeID, "task_id", payload.TaskID)
			}
			signalTaskWakeup(taskWakeups, payload.RuntimeID)
		case protocol.EventDaemonRuntimeProfilesChanged:
			var payload protocol.RuntimeProfilesChangedPayload
			if err := json.Unmarshal(сообщение.Payload, &payload); err != nil {
				d.logger.Debug("runtime profile refresh websocket invalid payload", "error", err)
				continue
			}
			if payload.WorkspaceID == "" {
				d.logger.Debug("runtime profile refresh websocket missing workspace_id")
				continue
			}
			go d.handleRuntimeProfilesChanged(payload)
		case protocol.EventDaemonWorkspacesChanged:
			if d.workspaceChanges != nil {
				d.workspaceChanges.broadcast()
			}
		case protocol.EventDaemonHeartbeatAck:
			var ack HeartbeatResponse
			if err := json.Unmarshal(сообщение.Payload, &ack); err != nil {
				d.logger.Debug("ws heartbeat ack invalid payload", "error", err)
				continue
			}
			d.handleWSHeartbeatAckForConnection(context.Background(), &ack, wsRPCGeneration)
		case protocol.EventDaemonRPCResponse:
			var resp protocol.RPCResponsePayload
			if err := json.Unmarshal(сообщение.Payload, &resp); err != nil {
				d.logger.Debug("ws rpc response invalid payload", "error", err)
				continue
			}
			d.wsRPC.deliver(resp)
		}
	}
}

func (d *Daemon) configureTaskWakeupReadLiveness(conn *websocket.Conn) {
	conn.SetReadLimit(taskWakeupReadLimit)
	if err := d.extendTaskWakeupReadDeadline(conn); err != nil {
		d.logger.Debug("task wakeup websocket read deadline failed", "error", err)
	}
	conn.SetPongHandler(func(string) error {
		return d.extendTaskWakeupReadDeadline(conn)
	})
	conn.SetPingHandler(func(appData string) error {
		if err := d.extendTaskWakeupReadDeadline(conn); err != nil {
			return err
		}
		return conn.WriteControl(websocket.PongMessage, []byte(appData), time.Now().Add(currentTaskWakeupTimings().writeWait))
	})
}

func (d *Daemon) extendTaskWakeupReadDeadline(conn *websocket.Conn) error {
	return conn.SetReadDeadline(time.Now().Add(currentTaskWakeupTimings().pongWait))
}

func (d *Daemon) handleRuntimeProfilesChanged(payload protocol.RuntimeProfilesChangedPayload) {
	if payload.WorkspaceID == "" {
		return
	}
	if err := d.refreshWorkspaceRuntimeProfiles(d.recoveryContext(), payload.WorkspaceID); err != nil {
		d.logger.Debug("runtime profile refresh websocket hint failed",
			"workspace_id", payload.WorkspaceID,
			"runtime_profile_id", payload.RuntimeProfileID,
			"error", err)
	}
}

func signalTaskWakeup(taskWakeups chan<- taskWakeup, runtimeID string) {
	select {
	case taskWakeups <- taskWakeup{runtimeID: runtimeID}:
	default:
	}
}

func taskWakeupURL(baseURL string, runtimeIDs []string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return "", fmt.Errorf("invalid daemon server URL: %w", err)
	}
	switch u.Scheme {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	case "ws", "wss":
	default:
		return "", fmt.Errorf("daemon server URL must use http, https, ws, or wss")
	}

	u.Path = strings.TrimRight(u.Path, "/") + "/api/daemon/ws"
	u.RawPath = ""
	q := u.Query()
	ids := append([]string(nil), runtimeIDs...)
	sort.Strings(ids)
	if len(ids) > 0 {
		q.Set("runtime_ids", strings.Join(ids, ","))
	}
	u.RawQuery = q.Encode()
	u.Fragment = ""
	return u.String(), nil
}

func sleepWithContextOrRuntimeChange(ctx context.Context, d time.Duration, runtimeSetCh <-chan struct{}) error {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-runtimeSetCh:
		return nil
	case <-timer.C:
		return nil
	}
}
