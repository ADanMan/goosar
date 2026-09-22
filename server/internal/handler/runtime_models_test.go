package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestModelListStore_RunningRequestTimesOut(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryModelListStore()
	req, err := store.Create(ctx, "runtime-xyz")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	claimed, err := store.PopPending(ctx, "runtime-xyz")
	if err != nil {
		t.Fatalf("pop: %v", err)
	}
	if claimed == nil {
		t.Fatal("expected PopPending to claim the pending request")
	}
	if claimed.Status != ModelListRunning {
		t.Fatalf("expected Running after PopPending, got %s", claimed.Status)
	}
	if claimed.RunStartedAt == nil {
		t.Fatal("expected RunStartedAt to be set on PopPending")
	}

	aged := time.Now().Add(-(modelListRunningTimeout + time.Second))
	claimed.RunStartedAt = &aged
	got, err := store.Get(ctx, req.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got == nil {
		t.Fatal("expected stored request")
	}
	if got.Status != ModelListTimeout {
		t.Fatalf("expected Timeout after running threshold, got %s", got.Status)
	}
	if got.Error == "" {
		t.Fatal("expected timeout error message")
	}
}

func TestReportModelListResult_PreservesDefault(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryModelListStore()
	req, err := store.Create(ctx, "runtime-xyz")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	body := map[string]any{
		"status":    "completed",
		"supported": true,
		"models": []map[string]any{
			{"id": "foo-default", "label": "Foo", "default": true},
			{"id": "bar", "label": "Bar"},
		},
	}
	raw, _ := json.Marshal(body)

	var parsed struct {
		Models []ModelEntry `json:"models"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("unmarshal report body: %v", err)
	}
	if err := store.Complete(ctx, req.ID, parsed.Models, true); err != nil {
		t.Fatalf("complete: %v", err)
	}

	got, err := store.Get(ctx, req.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got == nil {
		t.Fatal("expected stored result")
	}
	if len(got.Models) != 2 {
		t.Fatalf("expected 2 models, got %d: %+v", len(got.Models), got.Models)
	}
	if !got.Models[0].Default {
		t.Errorf("first model should carry Default=true, got %+v", got.Models[0])
	}
	if got.Models[1].Default {
		t.Errorf("second model should carry Default=false, got %+v", got.Models[1])
	}

	out, _ := json.Marshal(got)
	if !bytes.Contains(out, []byte(`"default":true`)) {
		t.Errorf(`expected "default":true in JSON response, got: %s`, out)
	}
}

func TestReportModelListResult_DecodesJSONBodyDefault(t *testing.T) {

	payload := `{"status":"completed","supported":true,"models":[{"id":"a","label":"A","default":true},{"id":"b","label":"B"}]}`
	r := httptest.NewRequest(http.MethodPost, "/api/daemon/runtimes/rt/models/req/result", bytes.NewBufferString(payload))

	var body struct {
		Status    string       `json:"status"`
		Models    []ModelEntry `json:"models"`
		Supported *bool        `json:"supported"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Models) != 2 {
		t.Fatalf("want 2 models, got %d", len(body.Models))
	}
	if !body.Models[0].Default {
		t.Errorf("default flag lost on model[0]: %+v", body.Models[0])
	}
}

func TestInMemoryModelListStore_HasPending(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryModelListStore()

	if has, err := store.HasPending(ctx, "rt-1"); err != nil || has {
		t.Fatalf("empty store should not report pending: has=%v err=%v", has, err)
	}

	if _, err := store.Create(ctx, "rt-1"); err != nil {
		t.Fatalf("create: %v", err)
	}
	if has, err := store.HasPending(ctx, "rt-1"); err != nil || !has {
		t.Fatalf("expected pending=true after Create: has=%v err=%v", has, err)
	}

	if has, err := store.HasPending(ctx, "rt-2"); err != nil || has {
		t.Fatalf("expected pending=false for unrelated runtime: has=%v err=%v", has, err)
	}

	if _, err := store.PopPending(ctx, "rt-1"); err != nil {
		t.Fatalf("pop: %v", err)
	}
	if has, err := store.HasPending(ctx, "rt-1"); err != nil || has {
		t.Fatalf("expected pending=false after PopPending: has=%v err=%v", has, err)
	}
}

func TestInMemoryModelListStore_PopPendingPicksOldest(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryModelListStore()

	first, _ := store.Create(ctx, "rt-1")

	time.Sleep(2 * time.Millisecond)
	second, _ := store.Create(ctx, "rt-1")

	got, err := store.PopPending(ctx, "rt-1")
	if err != nil {
		t.Fatalf("pop: %v", err)
	}
	if got == nil || got.ID != first.ID {
		t.Fatalf("expected first request, got %+v (second was %s)", got, second.ID)
	}
}
