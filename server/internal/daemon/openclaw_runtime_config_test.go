package daemon

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/adanman/goosar/server/internal/daemon/execenv"
)

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestDecodeOpenclawRuntimeConfigEmpty(t *testing.T) {
	t.Parallel()

	mode, gw := decodeOpenclawRuntimeConfig(nil, quietLogger())
	if mode != "" {
		t.Errorf("mode for nil payload: got %q, want \"\"", mode)
	}
	if !gw.IsZero() {
		t.Errorf("gateway for nil payload: got %+v, want zero", gw)
	}
}

func TestDecodeOpenclawRuntimeConfigGatewayMode(t *testing.T) {
	t.Parallel()

	raw := json.RawMessage(`{
		"mode": "gateway",
		"gateway": {
			"host": "gw.internal",
			"port": 18789,
			"token": "secret",
			"tls": true
		}
	}`)
	mode, gw := decodeOpenclawRuntimeConfig(raw, quietLogger())
	if mode != "gateway" {
		t.Errorf("mode: got %q, want %q", mode, "gateway")
	}
	want := execenv.OpenclawGatewayPin{
		Host:  "gw.internal",
		Port:  18789,
		Token: "secret",
		TLS:   true,
	}
	if gw != want {
		t.Errorf("gateway: got %+v, want %+v", gw, want)
	}
}

func TestDecodeOpenclawRuntimeConfigMalformedFailsSoftToLocal(t *testing.T) {
	t.Parallel()

	mode, gw := decodeOpenclawRuntimeConfig(json.RawMessage(`{"mode": "gateway"`), quietLogger())
	if mode != "" {
		t.Errorf("mode for malformed payload: got %q, want \"\"", mode)
	}
	if !gw.IsZero() {
		t.Errorf("gateway for malformed payload: got %+v, want zero", gw)
	}
}

func TestDecodeOpenclawRuntimeConfigModeOnly(t *testing.T) {
	t.Parallel()

	mode, gw := decodeOpenclawRuntimeConfig(json.RawMessage(`{"mode": "gateway"}`), quietLogger())
	if mode != "gateway" {
		t.Errorf("mode: got %q, want %q", mode, "gateway")
	}
	if !gw.IsZero() {
		t.Errorf("gateway: got %+v, want zero", gw)
	}
}

func TestOpenclawGatewayPinDefaultFormattingMasksToken(t *testing.T) {
	t.Parallel()

	pin := execenv.OpenclawGatewayPin{
		Host:  "gw.internal",
		Port:  18789,
		Token: "real-secret",
		TLS:   true,
	}

	if got := pin.String(); strings.Contains(got, "real-secret") {
		t.Errorf("String() leaks token: %q", got)
	}
	if got := fmt.Sprintf("%+v", pin); strings.Contains(got, "real-secret") {
		t.Errorf("%%+v leaks token: %q", got)
	}
	raw, err := json.Marshal(pin)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(raw), "real-secret") {
		t.Errorf("MarshalJSON leaks token: %s", raw)
	}

	if !strings.Contains(string(raw), "gw.internal") {
		t.Errorf("MarshalJSON dropped host along with token: %s", raw)
	}
}

func TestDecodeOpenclawRuntimeConfigLocalModeDropsGatewayPin(t *testing.T) {
	t.Parallel()

	raw := json.RawMessage(`{
		"mode": "local",
		"gateway": {"host": "gw.internal", "port": 18789, "token": "secret", "tls": true}
	}`)
	mode, gw := decodeOpenclawRuntimeConfig(raw, quietLogger())
	if mode != "local" {
		t.Errorf("mode: got %q, want %q", mode, "local")
	}
	if !gw.IsZero() {
		t.Errorf("gateway for local mode: got %+v, want zero", gw)
	}
}

func TestDecodeOpenclawRuntimeConfigUnknownModeWarnsAndDropsPin(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	raw := json.RawMessage(`{
		"mode": "gatway",
		"gateway": {"host": "gw.internal", "port": 18789, "token": "secret"}
	}`)
	mode, gw := decodeOpenclawRuntimeConfig(raw, logger)
	if mode != "gatway" {
		t.Errorf("mode: got %q, want %q", mode, "gatway")
	}
	if !gw.IsZero() {
		t.Errorf("gateway for unknown mode: got %+v, want zero", gw)
	}
	if !strings.Contains(buf.String(), "unrecognized mode") {
		t.Errorf("expected WARN about unrecognized mode, got: %q", buf.String())
	}
}
