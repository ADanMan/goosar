package handler

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestWriteErrorCarriesRequestID(t *testing.T) {
	w := httptest.NewRecorder()
	w.Header().Set("X-Request-ID", "req-abc123")
	writeError(w, 400, "bad request")

	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["error"] != "bad request" {
		t.Fatalf("error = %q", body["error"])
	}
	if body["request_id"] != "req-abc123" {
		t.Fatalf("request_id = %q, want req-abc123 (body %s)", body["request_id"], w.Body.String())
	}
}

func TestWriteErrorWithoutRequestIDStaysClean(t *testing.T) {
	w := httptest.NewRecorder()
	writeError(w, 400, "bad request")

	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := body["request_id"]; ok {
		t.Fatalf("no request id available, key must be absent: %s", w.Body.String())
	}
}
