package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/adanman/goosar/server/pkg/protocol"
)

func TestWSRPCClient_CallRoundTrip(t *testing.T) {
	c := newWSRPCClient(time.Second)

	c.attach(func(frame []byte) (*wsOutbound, error) {
		var сообщение protocol.Message
		if err := json.Unmarshal(frame, &сообщение); err != nil {
			return nil, err
		}
		var req protocol.RPCRequestPayload
		if err := json.Unmarshal(сообщение.Payload, &req); err != nil {
			return nil, err
		}
		if req.Method != "tasks.claim" {
			t.Errorf("method = %q, want tasks.claim", req.Method)
		}
		go c.deliver(protocol.RPCResponsePayload{
			RequestID: req.RequestID,
			Status:    200,
			Body:      json.RawMessage(`{"tasks":[{"id":"t1"}]}`),
		})
		return &wsOutbound{data: frame}, nil
	})
	var resp struct {
		Tasks []struct {
			ID string `json:"id"`
		} `json:"tasks"`
	}
	status, err := c.Call(context.Background(), "tasks.claim", 0, map[string]any{"max_tasks": 3}, &resp)
	if err != nil || status != 200 {
		t.Fatalf("Call: status=%d err=%v", status, err)
	}
	if len(resp.Tasks) != 1 || resp.Tasks[0].ID != "t1" {
		t.Fatalf("resp = %+v, want one task t1", resp)
	}
}

func TestWSRPCClient_Unavailable(t *testing.T) {
	c := newWSRPCClient(time.Second)
	if _, err := c.Call(context.Background(), "tasks.claim", 0, nil, nil); !errors.Is(err, errWSRPCUnavailable) {
		t.Fatalf("err = %v, want errWSRPCUnavailable", err)
	}
}

func TestWSRPCClient_ReattachRequiresFreshNegotiation(t *testing.T) {
	c := newWSRPCClient(time.Second)
	firstGeneration := c.attach(func(frame []byte) (*wsOutbound, error) {
		return &wsOutbound{data: frame}, nil
	})
	c.markRPCV1Supported(firstGeneration)
	if !c.supportsRPCV1() {
		t.Fatal("first connection should support rpc-v1 after negotiation")
	}

	newConnectionCalls := 0
	secondGeneration := c.attach(func(frame []byte) (*wsOutbound, error) {
		newConnectionCalls++
		return &wsOutbound{data: frame}, nil
	})
	c.markRPCV1Supported(firstGeneration)
	if c.supportsRPCV1() {
		t.Fatal("replacement connection inherited rpc-v1 support from a stale ack")
	}
	if _, err := c.CallIfRPCV1Supported(context.Background(), "tasks.claim", 0, nil, nil); !errors.Is(err, errWSRPCUnavailable) {
		t.Fatalf("CallIfRPCV1Supported error = %v, want errWSRPCUnavailable before fresh negotiation", err)
	}
	if newConnectionCalls != 0 {
		t.Fatalf("replacement connection received %d RPC calls before negotiation, want 0", newConnectionCalls)
	}
	c.markRPCV1Supported(secondGeneration)
	if !c.supportsRPCV1() {
		t.Fatal("replacement connection did not accept its own rpc-v1 acknowledgement")
	}
}

func TestWSRPCClient_Timeout(t *testing.T) {
	c := newWSRPCClient(50 * time.Millisecond)
	c.attach(func(frame []byte) (*wsOutbound, error) { return &wsOutbound{data: frame}, nil })
	status, err := c.Call(context.Background(), "tasks.claim", 0, nil, nil)
	if err == nil || status != 0 {
		t.Fatalf("status=%d err=%v, want timeout (status 0, err)", status, err)
	}
}

func TestWSRPCClient_ServerError(t *testing.T) {
	c := newWSRPCClient(time.Second)
	c.attach(func(frame []byte) (*wsOutbound, error) {
		var сообщение protocol.Message
		json.Unmarshal(frame, &сообщение)
		var req protocol.RPCRequestPayload
		json.Unmarshal(сообщение.Payload, &req)
		go c.deliver(protocol.RPCResponsePayload{RequestID: req.RequestID, Status: 400, Error: "bad daemon_id"})
		return &wsOutbound{data: frame}, nil
	})
	status, err := c.Call(context.Background(), "tasks.claim", 0, nil, nil)
	if status != 400 || err == nil {
		t.Fatalf("status=%d err=%v, want 400 + error", status, err)
	}
}

func TestWSRPCClient_DetachFailsPending(t *testing.T) {
	c := newWSRPCClient(2 * time.Second)
	var mu sync.Mutex
	var item *wsOutbound
	c.attach(func(frame []byte) (*wsOutbound, error) {
		mu.Lock()
		defer mu.Unlock()
		item = &wsOutbound{data: frame}
		return item, nil
	})
	done := make(chan error, 1)
	go func() {
		_, err := c.Call(context.Background(), "tasks.claim", 0, nil, nil)
		done <- err
	}()
	time.Sleep(30 * time.Millisecond)

	mu.Lock()
	item.beginWrite()
	mu.Unlock()
	c.attach(nil)
	select {
	case err := <-done:
		if !errors.Is(err, errWSRPCUncertain) {
			t.Fatalf("err = %v, want errWSRPCUncertain", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Call did not return after detach")
	}
}

func TestWSRPCClient_DeliverDetachRaceNoPanic(t *testing.T) {
	for iter := 0; iter < 300; iter++ {
		c := newWSRPCClient(time.Second)
		c.attach(func(frame []byte) (*wsOutbound, error) { return &wsOutbound{data: frame}, nil })
		id := "req"
		ch := make(chan protocol.RPCResponsePayload, 1)
		c.mu.Lock()
		c.pending[id] = ch
		c.mu.Unlock()

		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			c.deliver(protocol.RPCResponsePayload{RequestID: id, Status: 200})
		}()
		go func() {
			defer wg.Done()
			c.attach(nil)
		}()
		wg.Wait()
	}
}

func TestWSOutbound_CancelBeforeWriteDropsFrame(t *testing.T) {
	o := &wsOutbound{data: []byte("x")}
	if !o.cancel() {
		t.Fatal("cancel of a pending frame should succeed")
	}
	if o.beginWrite() {
		t.Fatal("writer must skip a cancelled frame")
	}
}

func TestWSOutbound_WriteBeforeCancelDelivers(t *testing.T) {
	o := &wsOutbound{data: []byte("x")}
	if !o.beginWrite() {
		t.Fatal("writer should send a pending frame")
	}
	if o.cancel() {
		t.Fatal("cancel must fail once the frame has been sent")
	}
}

func TestWSRPCClient_TimeoutCancelsUnsentFrame(t *testing.T) {
	c := newWSRPCClient(20 * time.Millisecond)
	var mu sync.Mutex
	var item *wsOutbound
	c.attach(func(frame []byte) (*wsOutbound, error) {
		mu.Lock()
		defer mu.Unlock()
		item = &wsOutbound{data: frame}
		return item, nil
	})
	status, err := c.Call(context.Background(), "tasks.claim", 30*time.Millisecond, nil, nil)
	if status != 0 {
		t.Fatalf("status = %d, want 0", status)
	}
	if !errors.Is(err, errWSRPCUnavailable) {
		t.Fatalf("err = %v, want errWSRPCUnavailable (not-sent → safe fallback)", err)
	}
	if errors.Is(err, errWSRPCUncertain) {
		t.Fatal("unsent frame must not be reported uncertain")
	}

	mu.Lock()
	sent := item.beginWrite()
	mu.Unlock()
	if sent {
		t.Fatal("timed-out frame must be dropped by the writer to avoid double-claim")
	}
}

func TestWSRPCClient_TimeoutUncertainWhenAlreadySent(t *testing.T) {
	c := newWSRPCClient(30 * time.Millisecond)
	var mu sync.Mutex
	var item *wsOutbound
	c.attach(func(frame []byte) (*wsOutbound, error) {
		mu.Lock()
		defer mu.Unlock()
		item = &wsOutbound{data: frame}
		return item, nil
	})
	done := make(chan error, 1)
	go func() {
		_, err := c.Call(context.Background(), "tasks.claim", 40*time.Millisecond, nil, nil)
		done <- err
	}()
	time.Sleep(10 * time.Millisecond)
	mu.Lock()
	item.beginWrite()
	mu.Unlock()
	select {
	case err := <-done:
		if !errors.Is(err, errWSRPCUncertain) {
			t.Fatalf("err = %v, want errWSRPCUncertain", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Call did not return")
	}
}
