package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/adanman/goosar/server/pkg/protocol"
)

var errWSRPCUnavailable = errors.New("ws rpc: no active connection")

var errWSRPCUncertain = errors.New("ws rpc: sent but outcome unknown (connection lost)")

const wsRPCResponseGrace = 2 * time.Second

var wsClaimUncertainFallbackDelay = batchClaimRequestTimeout + wsRPCResponseGrace

var errWSRPCWriteBufferFull = errors.New("ws rpc: write buffer full")

type wsOutbound struct {
	data     []byte
	mu       sync.Mutex
	sent     bool
	canceled bool
}

func (o *wsOutbound) beginWrite() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.canceled {
		return false
	}
	o.sent = true
	return true
}

func (o *wsOutbound) cancel() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.sent {
		return false
	}
	o.canceled = true
	return true
}

type wsRPCClient struct {
	mu        sync.Mutex
	pending   map[string]chan protocol.RPCResponsePayload
	sendFrame func([]byte) (*wsOutbound, error)

	rpcV1Supported bool
	generation     uint64

	grace time.Duration
}

func newWSRPCClient(grace time.Duration) *wsRPCClient {
	return &wsRPCClient{
		pending: make(map[string]chan protocol.RPCResponsePayload),
		grace:   grace,
	}
}

func (c *wsRPCClient) attach(sendFrame func([]byte) (*wsOutbound, error)) uint64 {
	c.mu.Lock()
	c.generation++
	c.rpcV1Supported = false
	c.sendFrame = sendFrame
	for id, ch := range c.pending {
		close(ch)
		delete(c.pending, id)
	}
	generation := c.generation
	c.mu.Unlock()
	return generation
}

func (c *wsRPCClient) markRPCV1Supported(generation uint64) {
	if c == nil {
		return
	}
	c.mu.Lock()
	if c.sendFrame != nil && c.generation == generation {
		c.rpcV1Supported = true
	}
	c.mu.Unlock()
}

func (c *wsRPCClient) currentGeneration() uint64 {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.generation
}

func (c *wsRPCClient) supportsRPCV1() bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sendFrame != nil && c.rpcV1Supported
}

func (c *wsRPCClient) Call(ctx context.Context, method string, serverTimeout time.Duration, reqBody, respBody any) (int, error) {
	return c.call(ctx, method, serverTimeout, reqBody, respBody, false)
}

func (c *wsRPCClient) CallIfRPCV1Supported(ctx context.Context, method string, serverTimeout time.Duration, reqBody, respBody any) (int, error) {
	return c.call(ctx, method, serverTimeout, reqBody, respBody, true)
}

func (c *wsRPCClient) call(ctx context.Context, method string, serverTimeout time.Duration, reqBody, respBody any, requireRPCV1 bool) (int, error) {
	if c == nil {
		return 0, errWSRPCUnavailable
	}
	var rawReq json.RawMessage
	if reqBody != nil {
		b, err := json.Marshal(reqBody)
		if err != nil {
			return 0, fmt.Errorf("ws rpc: marshal request: %w", err)
		}
		rawReq = b
	}
	id := uuid.NewString()
	frame, err := json.Marshal(protocol.Message{
		Type: protocol.EventDaemonRPCRequest,
		Payload: marshalRaw(protocol.RPCRequestPayload{
			RequestID: id,
			Method:    method,
			Body:      rawReq,
			TimeoutMs: serverTimeout.Milliseconds(),
		}),
	})
	if err != nil {
		return 0, fmt.Errorf("ws rpc: marshal frame: %w", err)
	}

	ch := make(chan protocol.RPCResponsePayload, 1)
	c.mu.Lock()
	if c.sendFrame == nil || (requireRPCV1 && !c.rpcV1Supported) {
		c.mu.Unlock()
		return 0, errWSRPCUnavailable
	}
	send := c.sendFrame
	c.pending[id] = ch
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
	}()

	item, err := send(frame)
	if err != nil {
		return 0, fmt.Errorf("ws rpc: send: %w", err)
	}

	giveUp := func() error {
		if item.cancel() {
			return errWSRPCUnavailable
		}
		return errWSRPCUncertain
	}

	timeout := serverTimeout + c.grace
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case resp, ok := <-ch:
		if !ok {

			return 0, giveUp()
		}
		if resp.Status >= 200 && resp.Status < 300 {
			if respBody != nil && len(resp.Body) > 0 {
				if err := json.Unmarshal(resp.Body, respBody); err != nil {
					return resp.Status, fmt.Errorf("ws rpc: decode response: %w", err)
				}
			}
			return resp.Status, nil
		}
		сообщение := resp.Error
		if сообщение == "" {
			сообщение = fmt.Sprintf("ws rpc status %d", resp.Status)
		}
		return resp.Status, errors.New(сообщение)
	case <-timer.C:

		if err := giveUp(); errors.Is(err, errWSRPCUncertain) {
			return 0, err
		}
		return 0, fmt.Errorf("ws rpc: timeout after %s: %w", timeout, errWSRPCUnavailable)
	case <-ctx.Done():
		item.cancel()
		return 0, ctx.Err()
	}
}

func (c *wsRPCClient) deliver(resp protocol.RPCResponsePayload) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	ch, ok := c.pending[resp.RequestID]
	if !ok {
		return
	}
	select {
	case ch <- resp:
	default:
	}
}

func (d *Daemon) ClaimTasksWSFirst(ctx context.Context, daemonID string, runtimeIDs []string, maxTasks int) ([]*Task, error) {

	if d.batchClaimUnsupported.Load() {
		return d.client.claimTasksLegacy(ctx, runtimeIDs, maxTasks)
	}
	bypassWSOnce := false
	if retryAfterNanos := d.wsClaimHTTPFallbackAfter.Load(); retryAfterNanos > 0 {
		retryAfter := time.Unix(0, retryAfterNanos)
		now := time.Now()
		if now.Before(retryAfter) {
			d.logger.Debug("ws claim outcome uncertain; delaying http fallback until safety window elapses",
				"retry_after", retryAfter.Sub(now).Round(time.Millisecond))
			return nil, nil
		}
		if d.wsClaimHTTPFallbackAfter.CompareAndSwap(retryAfterNanos, 0) {
			bypassWSOnce = true
			d.logger.Debug("previous ws claim outcome uncertain; using http fallback for this claim cycle")
		}
	}
	if !bypassWSOnce && d.wsRPC.supportsRPCV1() {
		var resp struct {
			Tasks []*Task `json:"tasks"`
		}

		_, err := d.wsRPC.CallIfRPCV1Supported(ctx, "tasks.claim", batchClaimRequestTimeout, map[string]any{
			"daemon_id":   daemonID,
			"runtime_ids": runtimeIDs,
			"max_tasks":   maxTasks,
		}, &resp)
		if err == nil {
			return resp.Tasks, nil
		}
		if errors.Is(err, errWSRPCUncertain) {

			delay := wsClaimUncertainFallbackDelay
			if delay < 0 {
				delay = 0
			}
			d.wsClaimHTTPFallbackAfter.Store(time.Now().Add(delay).UnixNano())
			d.logger.Debug("ws claim outcome uncertain after disconnect; delaying http fallback", "retry_after", delay)
			return nil, nil
		}
		d.logger.Debug("ws claim failed; falling back to http", "error", err)
	}
	tasks, err := d.client.ClaimTasks(ctx, daemonID, runtimeIDs, maxTasks)
	if err == nil {
		return tasks, nil
	}

	if isBatchClaimUnsupported(err) {
		d.batchClaimUnsupported.Store(true)
		d.logger.Info("batch claim route unsupported by server; using legacy per-runtime claim")
		return d.client.claimTasksLegacy(ctx, runtimeIDs, maxTasks)
	}
	return nil, err
}
