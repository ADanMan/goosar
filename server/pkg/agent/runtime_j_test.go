package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestNewReturnsRuntimeJBackend(t *testing.T) {
	t.Parallel()
	b, err := New("runtime-j", Config{ExecutablePath: "/nonexistent/hermes"})
	if err != nil {
		t.Fatalf("New(hermes) error: %v", err)
	}
	if _, ok := b.(*runtimeJBackend); !ok {
		t.Fatalf("expected *hermesBackend, got %T", b)
	}
}

func TestExtractACPSessionID(t *testing.T) {
	t.Parallel()
	raw := json.RawMessage(`{"sessionId":"20260410_141145_47260c"}`)
	got := extractACPSessionID(raw)
	if got != "20260410_141145_47260c" {
		t.Errorf("got %q, want %q", got, "20260410_141145_47260c")
	}
}

func TestExtractACPSessionIDEmpty(t *testing.T) {
	t.Parallel()
	raw := json.RawMessage(`{}`)
	got := extractACPSessionID(raw)
	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestExtractACPSessionIDInvalidJSON(t *testing.T) {
	t.Parallel()
	raw := json.RawMessage(`not json`)
	got := extractACPSessionID(raw)
	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestExtractACPCurrentModelID(t *testing.T) {
	t.Parallel()
	raw := json.RawMessage(`{
		"sessionId": "ses_123",
		"models": {
			"currentModelId": "nous:moonshotai/kimi-k2.6"
		}
	}`)
	got := extractACPCurrentModelID(raw)
	if got != "nous:moonshotai/kimi-k2.6" {
		t.Errorf("got %q, want current model", got)
	}
}

func TestExtractACPCurrentModelIDSnakeCase(t *testing.T) {
	t.Parallel()
	raw := json.RawMessage(`{
		"session_id": "ses_123",
		"models": {
			"current_model_id": "openrouter:anthropic/claude-sonnet-4.6"
		}
	}`)
	got := extractACPCurrentModelID(raw)
	if got != "openrouter:anthropic/claude-sonnet-4.6" {
		t.Errorf("got %q, want current model", got)
	}
}

func TestExtractACPCurrentModelIDMissing(t *testing.T) {
	t.Parallel()
	if got := extractACPCurrentModelID(json.RawMessage(`{"sessionId":"ses_123"}`)); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestResolveResumedSessionIDMatching(t *testing.T) {
	t.Parallel()

	got, changed := resolveResumedSessionID(
		"ses_alpha",
		json.RawMessage(`{"sessionId":"ses_alpha"}`),
	)
	if got != "ses_alpha" {
		t.Errorf("got %q, want ses_alpha", got)
	}
	if changed {
		t.Errorf("changed: got true, want false")
	}
}

func TestResolveResumedSessionIDDifferent(t *testing.T) {
	t.Parallel()

	got, changed := resolveResumedSessionID(
		"ses_alpha",
		json.RawMessage(`{"sessionId":"ses_beta_new"}`),
	)
	if got != "ses_beta_new" {
		t.Errorf("got %q, want ses_beta_new", got)
	}
	if !changed {
		t.Errorf("changed: got false, want true")
	}
}

func TestResolveResumedSessionIDEmptyResponse(t *testing.T) {
	t.Parallel()

	for _, body := range []string{
		`{}`,
		`{"sessionId":""}`,
		`not json`,
	} {
		got, changed := resolveResumedSessionID(
			"ses_alpha",
			json.RawMessage(body),
		)
		if got != "ses_alpha" {
			t.Errorf("body=%q: got %q, want ses_alpha", body, got)
		}
		if changed {
			t.Errorf("body=%q: changed: got true, want false", body)
		}
	}
}

func TestBuildRuntimeJSessionParamsIncludesModel(t *testing.T) {
	t.Parallel()
	params := buildRuntimeJSessionParams("/tmp/work", "gpt-4o", nil)
	if params["cwd"] != "/tmp/work" {
		t.Errorf("cwd: got %v, want /tmp/work", params["cwd"])
	}
	if _, ok := params["mcpServers"]; !ok {
		t.Error("mcpServers missing")
	}
	if got, ok := params["model"].(string); !ok || got != "gpt-4o" {
		t.Errorf("model: got %v, want gpt-4o", params["model"])
	}
}

func TestBuildRuntimeJSessionParamsOmitsEmptyModel(t *testing.T) {
	t.Parallel()
	params := buildRuntimeJSessionParams("/tmp/work", "", nil)
	if _, present := params["model"]; present {
		t.Error("expected model key to be omitted when model is empty")
	}
}

func TestBuildRuntimeJSessionParamsPassesThroughMcpServers(t *testing.T) {
	t.Parallel()
	servers := []any{map[string]any{"name": "fetch", "command": "uvx", "args": []string{}, "env": []map[string]any{}}}
	params := buildRuntimeJSessionParams("/tmp/work", "", servers)
	got, ok := params["mcpServers"].([]any)
	if !ok {
		t.Fatalf("mcpServers: got %T, want []any", params["mcpServers"])
	}
	if len(got) != 1 {
		t.Fatalf("len(mcpServers): got %d, want 1", len(got))
	}
}

func TestBuildRuntimeJSessionParamsNilMcpServersBecomesEmptyArray(t *testing.T) {
	t.Parallel()

	params := buildRuntimeJSessionParams("/tmp/work", "", nil)
	got, ok := params["mcpServers"].([]any)
	if !ok {
		t.Fatalf("mcpServers: got %T, want []any", params["mcpServers"])
	}
	if len(got) != 0 {
		t.Errorf("len(mcpServers): got %d, want 0", len(got))
	}
}

func TestBuildACPMcpServersEmptyInputReturnsEmpty(t *testing.T) {
	t.Parallel()
	for _, raw := range []json.RawMessage{nil, {}, json.RawMessage("null"), json.RawMessage(" null "), json.RawMessage("{}"), json.RawMessage(`{"mcpServers":{}}`)} {
		got, err := buildACPMcpServers(raw, slog.Default())
		if err != nil {
			t.Fatalf("raw=%q: unexpected error: %v", string(raw), err)
		}
		if got == nil {
			t.Errorf("raw=%q: got nil, want non-nil empty slice", string(raw))
		}
		if len(got) != 0 {
			t.Errorf("raw=%q: got %d entries, want 0", string(raw), len(got))
		}
	}
}

func TestBuildACPMcpServersTranslatesStdioEntry(t *testing.T) {
	t.Parallel()
	raw := json.RawMessage(`{"mcpServers":{"fetch":{"command":"uvx","args":["mcp-server-fetch"],"env":{"API_KEY":"secret","HOME":"/tmp"}}}}`)
	got, err := buildACPMcpServers(raw, slog.Default())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len: got %d, want 1", len(got))
	}
	entry, ok := got[0].(map[string]any)
	if !ok {
		t.Fatalf("entry type: got %T, want map[string]any", got[0])
	}
	if entry["name"] != "fetch" {
		t.Errorf("name: got %v, want fetch", entry["name"])
	}
	if entry["command"] != "uvx" {
		t.Errorf("command: got %v, want uvx", entry["command"])
	}
	if _, hasType := entry["type"]; hasType {
		t.Errorf("stdio entry should not include type field, got %v", entry["type"])
	}
	args, ok := entry["args"].([]string)
	if !ok || len(args) != 1 || args[0] != "mcp-server-fetch" {
		t.Errorf("args: got %v, want [mcp-server-fetch]", entry["args"])
	}
	envArr, ok := entry["env"].([]map[string]any)
	if !ok {
		t.Fatalf("env type: got %T, want []map[string]any", entry["env"])
	}

	if len(envArr) != 2 {
		t.Fatalf("len(env): got %d, want 2", len(envArr))
	}
	if envArr[0]["name"] != "API_KEY" || envArr[0]["value"] != "secret" {
		t.Errorf("env[0]: got %v, want {name:API_KEY,value:secret}", envArr[0])
	}
	if envArr[1]["name"] != "HOME" || envArr[1]["value"] != "/tmp" {
		t.Errorf("env[1]: got %v, want {name:HOME,value:/tmp}", envArr[1])
	}
}

func TestBuildACPMcpServersStdioWithoutArgsOrEnvUsesEmptyArrays(t *testing.T) {
	t.Parallel()

	raw := json.RawMessage(`{"mcpServers":{"minimal":{"command":"echo"}}}`)
	got, err := buildACPMcpServers(raw, slog.Default())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	entry := got[0].(map[string]any)
	if args, ok := entry["args"].([]string); !ok || len(args) != 0 {
		t.Errorf("args: got %v, want []", entry["args"])
	}
	if env, ok := entry["env"].([]map[string]any); !ok || len(env) != 0 {
		t.Errorf("env: got %v, want []", entry["env"])
	}
}

func TestBuildACPMcpServersTranslatesHttpEntry(t *testing.T) {
	t.Parallel()
	raw := json.RawMessage(`{"mcpServers":{"remote":{"type":"http","url":"https://example.com/mcp","headers":{"Authorization":"Bearer x","X-Trace":"abc"}}}}`)
	got, err := buildACPMcpServers(raw, slog.Default())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len: got %d, want 1", len(got))
	}
	entry := got[0].(map[string]any)
	if entry["type"] != "http" {
		t.Errorf("type: got %v, want http", entry["type"])
	}
	if entry["name"] != "remote" {
		t.Errorf("name: got %v, want remote", entry["name"])
	}
	if entry["url"] != "https://example.com/mcp" {
		t.Errorf("url: got %v, want https://example.com/mcp", entry["url"])
	}
	headers, ok := entry["headers"].([]map[string]any)
	if !ok || len(headers) != 2 {
		t.Fatalf("headers: got %v, want 2 entries", entry["headers"])
	}
	if headers[0]["name"] != "Authorization" {
		t.Errorf("headers[0].name: got %v, want Authorization", headers[0]["name"])
	}
}

func TestBuildACPMcpServersDefaultsRemoteTypeToHttp(t *testing.T) {
	t.Parallel()

	raw := json.RawMessage(`{"mcpServers":{"remote":{"url":"https://example.com/mcp"}}}`)
	got, err := buildACPMcpServers(raw, slog.Default())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	entry := got[0].(map[string]any)
	if entry["type"] != "http" {
		t.Errorf("type: got %v, want http (default)", entry["type"])
	}
}

func TestBuildACPMcpServersSupportsSseTransport(t *testing.T) {
	t.Parallel()
	raw := json.RawMessage(`{"mcpServers":{"remote":{"type":"sse","url":"https://example.com/sse"}}}`)
	got, err := buildACPMcpServers(raw, slog.Default())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	entry := got[0].(map[string]any)
	if entry["type"] != "sse" {
		t.Errorf("type: got %v, want sse", entry["type"])
	}
}

func TestBuildACPMcpServersAcceptsStreamableHttpAlias(t *testing.T) {
	t.Parallel()

	for _, alias := range []string{"streamable-http", "http_streamable", "Streamable-HTTP"} {
		raw := json.RawMessage(`{"mcpServers":{"remote":{"type":"` + alias + `","url":"https://example.com/mcp"}}}`)
		got, err := buildACPMcpServers(raw, slog.Default())
		if err != nil {
			t.Fatalf("alias=%s: unexpected error: %v", alias, err)
		}
		entry := got[0].(map[string]any)
		if entry["type"] != "http" {
			t.Errorf("alias=%s: type got %v, want http", alias, entry["type"])
		}
	}
}

func TestBuildACPMcpServersSortsEntriesByName(t *testing.T) {
	t.Parallel()

	raw := json.RawMessage(`{"mcpServers":{"zeta":{"command":"z"},"alpha":{"command":"a"},"mid":{"command":"m"}}}`)
	got, err := buildACPMcpServers(raw, slog.Default())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"alpha", "mid", "zeta"}
	for i, w := range want {
		if got[i].(map[string]any)["name"] != w {
			t.Errorf("position %d: got %v, want %s", i, got[i].(map[string]any)["name"], w)
		}
	}
}

func TestBuildACPMcpServersSkipsDisabledEntries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		entry string
		kept  bool
	}{
		{"enabled false drops", `{"command":"mcp-atlassian","enabled":false}`, false},
		{"disabled true drops", `{"command":"ewsmcp","disabled":true}`, false},
		{"enabled true keeps", `{"command":"uvx","enabled":true}`, true},
		{"no flag keeps", `{"command":"docker"}`, true},
		{"enabled null keeps", `{"command":"docker","enabled":null}`, true},
		{"malformed enabled keeps", `{"command":"echo","enabled":"nope"}`, true},
		{"malformed disabled keeps", `{"command":"echo","disabled":"maybe"}`, true},

		{"valid enabled false beside malformed disabled drops", `{"command":"echo","enabled":false,"disabled":"maybe"}`, false},
		{"valid disabled true beside malformed enabled drops", `{"command":"echo","enabled":[],"disabled":true}`, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			raw := json.RawMessage(`{"mcpServers":{"probe":` + tc.entry + `}}`)
			got, err := buildACPMcpServers(raw, slog.Default())
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if kept := len(got) == 1; kept != tc.kept {
				t.Fatalf("entry %s: kept=%v, want kept=%v", tc.entry, kept, tc.kept)
			}
		})
	}
}

func TestBuildACPMcpServersSkipsInvalidEntriesAndContinues(t *testing.T) {
	t.Parallel()

	raw := json.RawMessage(`{"mcpServers":{"bad":{"args":["nothing"]},"good":{"command":"uvx"}}}`)
	got, err := buildACPMcpServers(raw, slog.Default())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len: got %d, want 1 (bad entry should be skipped)", len(got))
	}
	if got[0].(map[string]any)["name"] != "good" {
		t.Errorf("kept the wrong entry: %v", got[0])
	}
}

func TestBuildACPMcpServersReturnsErrorOnMalformedJSON(t *testing.T) {
	t.Parallel()
	_, err := buildACPMcpServers(json.RawMessage(`not json`), slog.Default())
	if err == nil {
		t.Fatal("expected error for malformed JSON, got nil")
	}
	if !strings.Contains(err.Error(), "parse mcp_config json") {
		t.Errorf("error message: got %q, want it to mention parsing", err.Error())
	}
}

func TestRuntimeJToolNameFromTitle(t *testing.T) {
	t.Parallel()
	tests := []struct {
		title string
		kind  string
		want  string
	}{
		{"terminal: ls -la", "execute", "terminal"},
		{"read: /tmp/foo.go", "read", "read_file"},
		{"write: /tmp/bar.go", "edit", "write_file"},
		{"patch (replace): /tmp/baz.go", "edit", "patch"},
		{"search: *.go", "search", "search_files"},
		{"web search: golang acp protocol", "fetch", "web_search"},
		{"extract: https://example.com", "fetch", "web_extract"},
		{"delegate: fix the bug", "execute", "delegate_task"},
		{"analyze image: what is this?", "read", "vision_analyze"},
		{"execute code", "execute", "execute_code"},

		{"unknownTool", "read", "read_file"},
		{"unknownTool", "edit", "write_file"},
		{"unknownTool", "execute", "terminal"},
		{"unknownTool", "search", "search_files"},
		{"unknownTool", "fetch", "web_search"},
		{"unknownTool", "think", "thinking"},

		{"Shell", "", "Shell"},
		{"Read file", "", "Read file"},
		{"unknownTool", "other", "unknownTool"},

		{"", "other", "other"},

		{"custom_tool: args", "other", "custom_tool"},
	}
	for _, tt := range tests {
		got := runtimeJToolNameFromTitle(tt.title, tt.kind)
		if got != tt.want {
			t.Errorf("hermesToolNameFromTitle(%q, %q) = %q, want %q", tt.title, tt.kind, got, tt.want)
		}
	}
}

func TestRuntimeJClientHandleLineResponse(t *testing.T) {
	t.Parallel()

	c := &runtimeJClient{
		pending: make(map[int]*pendingRPC),
	}
	pr := &pendingRPC{ch: make(chan rpcResult, 1), method: "session/new"}
	c.pending[1] = pr

	c.handleLine(`{"jsonrpc":"2.0","id":1,"result":{"sessionId":"ses_abc"}}`)

	res := <-pr.ch
	if res.err != nil {
		t.Fatalf("unexpected error: %v", res.err)
	}
	sid := extractACPSessionID(res.result)
	if sid != "ses_abc" {
		t.Errorf("sessionId: got %q, want %q", sid, "ses_abc")
	}
}

func TestRuntimeJClientHandleLineError(t *testing.T) {
	t.Parallel()

	c := &runtimeJClient{
		pending: make(map[int]*pendingRPC),
	}
	pr := &pendingRPC{ch: make(chan rpcResult, 1), method: "initialize"}
	c.pending[0] = pr

	c.handleLine(`{"jsonrpc":"2.0","id":0,"error":{"code":-32600,"message":"bad request"}}`)

	res := <-pr.ch
	if res.err == nil {
		t.Fatal("expected error")
	}
	if got := res.err.Error(); got != "initialize: bad request (code=-32600)" {
		t.Errorf("error: got %q", got)
	}
}

func TestRuntimeJClientHandleLineErrorWithStringData(t *testing.T) {
	t.Parallel()

	c := &runtimeJClient{
		pending: make(map[int]*pendingRPC),
	}
	pr := &pendingRPC{ch: make(chan rpcResult, 1), method: "session/prompt"}
	c.pending[3] = pr

	c.handleLine(`{"jsonrpc":"2.0","id":3,"error":{"code":-32603,"message":"Internal error","data":"No session found with id"}}`)

	res := <-pr.ch
	if res.err == nil {
		t.Fatal("expected error")
	}
	want := "session/prompt: Internal error (code=-32603, data=No session found with id)"
	if got := res.err.Error(); got != want {
		t.Errorf("error: got %q, want %q", got, want)
	}
}

func TestRuntimeJClientHandleLineErrorWithObjectData(t *testing.T) {
	t.Parallel()

	c := &runtimeJClient{
		pending: make(map[int]*pendingRPC),
	}
	pr := &pendingRPC{ch: make(chan rpcResult, 1), method: "session/prompt"}
	c.pending[5] = pr

	c.handleLine(`{"jsonrpc":"2.0","id":5,"error":{"code":-32000,"message":"quota","data":{"reason":"limit","remaining":0}}}`)

	res := <-pr.ch
	if res.err == nil {
		t.Fatal("expected error")
	}
	want := `session/prompt: quota (code=-32000, data={"reason":"limit","remaining":0})`
	if got := res.err.Error(); got != want {
		t.Errorf("error: got %q, want %q", got, want)
	}
}

func TestRuntimeJClientHandleLineErrorWithNullData(t *testing.T) {
	t.Parallel()

	c := &runtimeJClient{
		pending: make(map[int]*pendingRPC),
	}
	pr := &pendingRPC{ch: make(chan rpcResult, 1), method: "initialize"}
	c.pending[7] = pr

	c.handleLine(`{"jsonrpc":"2.0","id":7,"error":{"code":-32600,"message":"bad request","data":null}}`)

	res := <-pr.ch
	if res.err == nil {
		t.Fatal("expected error")
	}
	if got := res.err.Error(); got != "initialize: bad request (code=-32600)" {
		t.Errorf("error: got %q", got)
	}
}

type bufferWriter struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (b *bufferWriter) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.WriteString(string(p))
}

func (b *bufferWriter) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestRuntimeJClientAutoApprovesPermissionRequest(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		options string
		wantErr bool
		wantID  string
	}{
		{

			name:    "hermes edit approval selects allow_once",
			options: `[{"optionId":"allow_once","name":"Allow edit","kind":"allow_once"},{"optionId":"deny","name":"Deny","kind":"reject_once"}]`,
			wantID:  "allow_once",
		},
		{

			name:    "command approval prefers session over permanent",
			options: `[{"optionId":"allow_once","kind":"allow_once"},{"optionId":"allow_session","kind":"allow_always"},{"optionId":"allow_always","kind":"allow_always"},{"optionId":"deny","kind":"reject_once"},{"optionId":"deny_always","kind":"reject_always"}]`,
			wantID:  "allow_session",
		},
		{

			name:    "session-scoped id honoured",
			options: `[{"optionId":"approve","kind":"allow_once"},{"optionId":"approve_for_session","kind":"allow_always"},{"optionId":"reject","kind":"reject_once"}]`,
			wantID:  "approve_for_session",
		},
		{

			name:    "non-standard single-use id selected by kind",
			options: `[{"optionId":"yolo-42","kind":"allow_once"},{"optionId":"nope","kind":"reject_once"}]`,
			wantID:  "yolo-42",
		},
		{

			name:    "permanent grant refused, offered reject_once selected",
			options: `[{"optionId":"allow_always","kind":"allow_always"},{"optionId":"deny","kind":"reject_once"}]`,
			wantID:  "deny",
		},
		{

			name:    "reject-only selects reject_once",
			options: `[{"optionId":"deny","kind":"reject_once"},{"optionId":"deny_always","kind":"reject_always"}]`,
			wantID:  "deny",
		},
		{

			name:    "unknown kind selects offered reject_once",
			options: `[{"optionId":"allow_forever","kind":"allow_super"},{"optionId":"deny","kind":"reject_once"}]`,
			wantID:  "deny",
		},
		{

			name:    "permanent-only without reject_once errors",
			options: `[{"optionId":"allow_always","kind":"allow_always"}]`,
			wantErr: true,
		},
		{

			name:    "reject_always-only errors",
			options: `[{"optionId":"deny_always","kind":"reject_always"}]`,
			wantErr: true,
		},
		{

			name:    "empty options errors",
			options: `[]`,
			wantErr: true,
		},
		{

			name:    "malformed options errors",
			options: `"not-an-array"`,
			wantErr: true,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			w := &bufferWriter{}
			c := &runtimeJClient{
				cfg:     Config{Logger: slog.Default()},
				stdin:   w,
				pending: make(map[int]*pendingRPC),
			}

			c.handleLine(`{"jsonrpc":"2.0","id":42,"method":"session/request_permission","params":{"sessionId":"ses_1","options":` + tc.options + `,"toolCall":{"toolCallId":"tc_1","title":"write: reply.md","content":[]}}}`)

			got := w.String()
			var resp struct {
				JSONRPC string `json:"jsonrpc"`
				ID      int    `json:"id"`
				Result  *struct {
					Outcome struct {
						Outcome  string `json:"outcome"`
						OptionID string `json:"optionId"`
					} `json:"outcome"`
				} `json:"result"`
				Error *struct {
					Code    int    `json:"code"`
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.Unmarshal([]byte(strings.TrimSpace(got)), &resp); err != nil {
				t.Fatalf("reply is not valid JSON: %q err=%v", got, err)
			}
			if resp.JSONRPC != "2.0" {
				t.Errorf("jsonrpc: got %q, want 2.0", resp.JSONRPC)
			}
			if resp.ID != 42 {
				t.Errorf("id: got %d, want 42 (must echo agent's request id)", resp.ID)
			}
			if tc.wantErr {
				if resp.Error == nil {
					t.Errorf("want a JSON-RPC error reply, got result %+v", resp.Result)
				}
				if resp.Result != nil {
					t.Errorf("error reply must not carry a result, got %+v", resp.Result)
				}
				return
			}
			if resp.Error != nil {
				t.Fatalf("unexpected JSON-RPC error reply: %+v", resp.Error)
			}
			if resp.Result == nil {
				t.Fatalf("want a selected outcome, got no result: %q", got)
			}
			if resp.Result.Outcome.Outcome != "selected" {
				t.Errorf("outcome.outcome: got %q, want %q", resp.Result.Outcome.Outcome, "selected")
			}
			if resp.Result.Outcome.OptionID != tc.wantID {
				t.Errorf("outcome.optionId: got %q, want %q", resp.Result.Outcome.OptionID, tc.wantID)
			}
		})
	}
}

func TestRuntimeJClientReplesMethodNotFoundForUnknownAgentRequest(t *testing.T) {
	t.Parallel()

	w := &bufferWriter{}
	c := &runtimeJClient{
		cfg:     Config{Logger: slog.Default()},
		stdin:   w,
		pending: make(map[int]*pendingRPC),
	}
	c.handleLine(`{"jsonrpc":"2.0","id":7,"method":"fs/read_text_file","params":{"path":"/tmp/x"}}`)

	got := w.String()
	var resp struct {
		ID    int `json:"id"`
		Error struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(got)), &resp); err != nil {
		t.Fatalf("reply not valid JSON: %q err=%v", got, err)
	}
	if resp.ID != 7 {
		t.Errorf("id echo: got %d, want 7", resp.ID)
	}
	if resp.Error.Code != -32601 {
		t.Errorf("error code: got %d, want -32601 (method not found)", resp.Error.Code)
	}
	if !strings.Contains(resp.Error.Message, "fs/read_text_file") {
		t.Errorf("error message should name the unhandled method, got %q", resp.Error.Message)
	}
}

func TestRuntimeJClientHandleAgentMessage(t *testing.T) {
	t.Parallel()

	var got Message
	c := &runtimeJClient{
		pending: make(map[int]*pendingRPC),
		onMessage: func(сообщение Message) {
			got = сообщение
		},
	}

	line := `{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_1","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"Hello world"}}}}`
	c.handleLine(line)

	if got.Type != MessageText {
		t.Errorf("type: got %v, want MessageText", got.Type)
	}
	if got.Content != "Hello world" {
		t.Errorf("content: got %q, want %q", got.Content, "Hello world")
	}
}

func TestRuntimeJClientHandleSessionNotificationAgentMessage(t *testing.T) {
	t.Parallel()

	var got Message
	c := &runtimeJClient{
		pending: make(map[int]*pendingRPC),
		onMessage: func(сообщение Message) {
			got = сообщение
		},
	}

	line := `{"jsonrpc":"2.0","method":"session/notification","params":{"sessionId":"ses_1","update":{"type":"AgentMessageChunk","content":{"type":"text","text":"Hello from Kiro"}}}}`
	c.handleLine(line)

	if got.Type != MessageText {
		t.Errorf("type: got %v, want MessageText", got.Type)
	}
	if got.Content != "Hello from Kiro" {
		t.Errorf("content: got %q, want %q", got.Content, "Hello from Kiro")
	}
}

func TestRuntimeJClientAcceptNotificationGate(t *testing.T) {
	t.Parallel()

	var (
		got    []Message
		accept bool
	)
	c := &runtimeJClient{
		pending: make(map[int]*pendingRPC),
		acceptNotification: func(string) bool {
			return accept
		},
		onMessage: func(сообщение Message) {
			got = append(got, сообщение)
		},
	}

	replay := `{"jsonrpc":"2.0","method":"session/notification","params":{"sessionId":"ses_1","update":{"type":"AgentMessageChunk","content":{"type":"text","text":"history should be ignored"}}}}`
	c.handleLine(replay)
	if len(got) != 0 {
		t.Fatalf("expected gate to drop replay before turn starts, got %+v", got)
	}

	accept = true
	live := `{"jsonrpc":"2.0","method":"session/notification","params":{"sessionId":"ses_1","update":{"type":"AgentMessageChunk","content":{"type":"text","text":"current"}}}}`
	c.handleLine(live)
	if len(got) != 1 {
		t.Fatalf("expected current-turn update to pass the gate, got %+v", got)
	}
	if got[0].Content != "current" {
		t.Fatalf("got content %q, want \"current\"", got[0].Content)
	}
}

func TestRuntimeJBackendDrainsLateFinalNotificationAfterPromptResponse(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	fakePath := filepath.Join(t.TempDir(), "hermes")
	script := `#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{}}}\n' "$id"
      ;;
    *'"method":"session/resume"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"ses_resumed"}}\n' "$id"
      ;;
    *'"method":"session/prompt"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn"}}\n' "$id"
      sleep 0.05
      printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_resumed","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"Current answer"}}}}\n'
      ;;
  esac
done
`
	writeTestExecutable(t, fakePath, []byte(script))

	backend, err := New("runtime-j", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new hermes backend: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, err := backend.Execute(ctx, "prompt", ExecOptions{
		ResumeSessionID: "ses_resumed",
		Timeout:         5 * time.Second,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()

	select {
	case result, ok := <-session.Result:
		if !ok {
			t.Fatal("result channel closed without a value")
		}
		if result.Status != "completed" {
			t.Fatalf("expected completed, got status=%q error=%q", result.Status, result.Error)
		}
		if result.Output != "Current answer" {
			t.Fatalf("expected late current-turn output, got %q", result.Output)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
	}
}

func TestRuntimeJBackendEmitsSessionStatusForPinning(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	cases := []struct {
		name     string
		resumeID string
		wantSID  string
	}{
		{name: "session_new", resumeID: "", wantSID: "ses_fresh"},
		{name: "session_resume", resumeID: "ses_prior", wantSID: "ses_prior"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fakePath := filepath.Join(t.TempDir(), "hermes")
			script := `#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{}}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"ses_fresh"}}\n' "$id"
      ;;
    *'"method":"session/resume"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"ses_prior"}}\n' "$id"
      ;;
    *'"method":"session/prompt"'*)
      printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"s","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"hi"}}}}\n'
      printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn"}}\n' "$id"
      ;;
  esac
done
`
			writeTestExecutable(t, fakePath, []byte(script))

			backend, err := New("runtime-j", Config{ExecutablePath: fakePath, Logger: slog.Default()})
			if err != nil {
				t.Fatalf("new hermes backend: %v", err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			session, err := backend.Execute(ctx, "prompt", ExecOptions{
				ResumeSessionID: tc.resumeID,
				Timeout:         5 * time.Second,
			})
			if err != nil {
				t.Fatalf("execute: %v", err)
			}

			statusSID := make(chan string, 1)
			go func() {
				for сообщение := range session.Messages {
					if сообщение.Type == MessageStatus && сообщение.SessionID != "" {
						select {
						case statusSID <- сообщение.SessionID:
						default:
						}
					}
				}
			}()

			select {
			case result, ok := <-session.Result:
				if !ok {
					t.Fatal("result channel closed without a value")
				}
				if result.Status != "completed" {
					t.Fatalf("expected completed, got status=%q error=%q", result.Status, result.Error)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("timeout waiting for result")
			}

			select {
			case sid := <-statusSID:
				if sid != tc.wantSID {
					t.Fatalf("expected MessageStatus session id %q, got %q", tc.wantSID, sid)
				}
			default:
				t.Fatal("no MessageStatus with a session id was emitted")
			}
		})
	}
}

func TestRuntimeJBackendStatusPinPrecedesStop(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	fakePath := filepath.Join(t.TempDir(), "hermes")

	script := `#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{}}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"ses_stopped"}}\n' "$id"
      ;;
    *'"method":"session/prompt"'*)
      sleep 60
      ;;
  esac
done
`
	writeTestExecutable(t, fakePath, []byte(script))

	backend, err := New("runtime-j", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new hermes backend: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	session, err := backend.Execute(ctx, "prompt", ExecOptions{Timeout: 30 * time.Second})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	statusSeen := make(chan string, 1)
	go func() {
		for сообщение := range session.Messages {
			if сообщение.Type == MessageStatus && сообщение.SessionID != "" {
				select {
				case statusSeen <- сообщение.SessionID:
				default:
				}
			}
		}
	}()

	select {
	case sid := <-statusSeen:
		if sid != "ses_stopped" {
			t.Fatalf("expected session id %q on the status message, got %q", "ses_stopped", sid)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("no MessageStatus with a session id before the stop")
	}
	cancel()

	select {
	case result, ok := <-session.Result:
		if !ok {
			t.Fatal("result channel closed without a value")
		}
		if result.Status != "aborted" {
			t.Fatalf("expected aborted after stop, got status=%q error=%q", result.Status, result.Error)
		}
		if result.SessionID != "ses_stopped" {
			t.Fatalf("aborted result must keep the session id; got %q", result.SessionID)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for aborted result")
	}
}

func TestRuntimeJBackendCancelsBeforeWaitingForLingeringProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	fakePath := filepath.Join(t.TempDir(), "hermes")
	script := `#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{}}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"ses_lingering"}}\n' "$id"
      ;;
    *'"method":"session/prompt"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn"}}\n' "$id"
      exec 1>&-
      exec 2>&-
      while :; do :; done
      ;;
  esac
done
`
	writeTestExecutable(t, fakePath, []byte(script))

	backend, err := New("runtime-j", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new hermes backend: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, err := backend.Execute(ctx, "prompt", ExecOptions{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()

	select {
	case result, ok := <-session.Result:
		if !ok {
			t.Fatal("result channel closed without a value")
		}
		if result.Status != "completed" {
			t.Fatalf("expected completed, got status=%q error=%q", result.Status, result.Error)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for result")
	}

	select {
	case _, ok := <-session.Result:
		if ok {
			t.Fatal("result channel produced an unexpected second value")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("result channel did not close; cmd.Wait likely ran before cancel")
	}
}

func TestRuntimeJClientHandleAgentThought(t *testing.T) {
	t.Parallel()

	var got Message
	c := &runtimeJClient{
		pending: make(map[int]*pendingRPC),
		onMessage: func(сообщение Message) {
			got = сообщение
		},
	}

	line := `{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_1","update":{"sessionUpdate":"agent_thought_chunk","content":{"type":"text","text":"Let me think..."}}}}`
	c.handleLine(line)

	if got.Type != MessageThinking {
		t.Errorf("type: got %v, want MessageThinking", got.Type)
	}
	if got.Content != "Let me think..." {
		t.Errorf("content: got %q, want %q", got.Content, "Let me think...")
	}
}

func TestRuntimeJClientHandleToolCallStart(t *testing.T) {
	t.Parallel()

	var got Message
	c := &runtimeJClient{
		pending: make(map[int]*pendingRPC),
		onMessage: func(сообщение Message) {
			got = сообщение
		},
	}

	line := `{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_1","update":{"sessionUpdate":"tool_call","toolCallId":"tc-abc123","title":"terminal: ls -la","kind":"execute","status":"pending","rawInput":{"command":"ls -la"}}}}`
	c.handleLine(line)

	if got.Type != MessageToolUse {
		t.Errorf("type: got %v, want MessageToolUse", got.Type)
	}
	if got.Tool != "terminal" {
		t.Errorf("tool: got %q, want %q", got.Tool, "terminal")
	}
	if got.CallID != "tc-abc123" {
		t.Errorf("callID: got %q, want %q", got.CallID, "tc-abc123")
	}
	if cmd, ok := got.Input["command"].(string); !ok || cmd != "ls -la" {
		t.Errorf("input.command: got %v", got.Input["command"])
	}
}

func TestRuntimeJClientHandleSessionNotificationToolCall(t *testing.T) {
	t.Parallel()

	var got []Message
	c := &runtimeJClient{
		pending: make(map[int]*pendingRPC),
		onMessage: func(сообщение Message) {
			got = append(got, сообщение)
		},
	}

	c.handleLine(`{"jsonrpc":"2.0","method":"session/notification","params":{"sessionId":"ses_1","update":{"type":"ToolCall","toolCallId":"tc-kiro","name":"Shell","status":"pending","parameters":{"command":"pwd"}}}}`)
	c.handleLine(`{"jsonrpc":"2.0","method":"session/notification","params":{"sessionId":"ses_1","update":{"type":"ToolCallUpdate","toolCallId":"tc-kiro","status":"completed","name":"Shell","output":"/tmp/project\n"}}}`)

	if len(got) != 2 {
		t.Fatalf("expected [ToolUse, ToolResult], got %+v", got)
	}
	if got[0].Type != MessageToolUse {
		t.Errorf("first message: got %v, want MessageToolUse", got[0].Type)
	}
	if got[0].Tool != "Shell" {
		t.Errorf("first tool: got %q, want Shell", got[0].Tool)
	}
	if cmd, _ := got[0].Input["command"].(string); cmd != "pwd" {
		t.Errorf("first input.command: got %v, want pwd", got[0].Input["command"])
	}
	if got[1].Type != MessageToolResult {
		t.Errorf("second message: got %v, want MessageToolResult", got[1].Type)
	}
	if got[1].Output != "/tmp/project\n" {
		t.Errorf("second output: got %q", got[1].Output)
	}
	if got[1].Status != "completed" {
		t.Errorf("second status: got %q, want completed", got[1].Status)
	}
}

func TestRuntimeJClientHandleSessionNotificationTurnEnd(t *testing.T) {
	t.Parallel()

	var got runtimeJPromptResult
	c := &runtimeJClient{
		pending: make(map[int]*pendingRPC),
		onPromptDone: func(result runtimeJPromptResult) {
			got = result
		},
	}

	line := `{"jsonrpc":"2.0","method":"session/notification","params":{"sessionId":"ses_1","update":{"type":"TurnEnd","stopReason":"end_turn","usage":{"inputTokens":3,"outputTokens":4,"cachedReadTokens":1}}}}`
	c.handleLine(line)

	if got.stopReason != "end_turn" {
		t.Errorf("stopReason: got %q, want end_turn", got.stopReason)
	}
	if got.usage.InputTokens != 3 || got.usage.OutputTokens != 4 || got.usage.CacheReadTokens != 1 {
		t.Errorf("usage: got %+v", got.usage)
	}
}

func TestParseACPTokenUsageAliases(t *testing.T) {
	t.Parallel()

	usage := parseACPTokenUsage(json.RawMessage(`{
		"input_tokens": 11,
		"output_tokens": "7",
		"cacheReadTokens": 5,
		"cache_creation_input_tokens": 3
	}`))

	if usage.InputTokens != 11 {
		t.Errorf("InputTokens: got %d, want 11", usage.InputTokens)
	}
	if usage.OutputTokens != 7 {
		t.Errorf("OutputTokens: got %d, want 7", usage.OutputTokens)
	}
	if usage.CacheReadTokens != 5 {
		t.Errorf("CacheReadTokens: got %d, want 5", usage.CacheReadTokens)
	}
	if usage.CacheWriteTokens != 3 {
		t.Errorf("CacheWriteTokens: got %d, want 3", usage.CacheWriteTokens)
	}
}

func TestParseACPTokenUsageCachedInputBucketing(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want TokenUsage
	}{
		{

			name: "inclusive input is reduced by cached reads",
			raw:  `{"inputTokens":12929,"outputTokens":29,"totalTokens":12958,"cachedReadTokens":10880}`,
			want: TokenUsage{InputTokens: 2049, OutputTokens: 29, CacheReadTokens: 10880},
		},
		{

			name: "exclusive buckets are left alone",
			raw:  `{"inputTokens":100,"outputTokens":20,"totalTokens":150,"cachedReadTokens":30}`,
			want: TokenUsage{InputTokens: 100, OutputTokens: 20, CacheReadTokens: 30},
		},
		{

			name: "missing totalTokens keeps counters as reported",
			raw:  `{"inputTokens":100,"outputTokens":20,"cachedReadTokens":30}`,
			want: TokenUsage{InputTokens: 100, OutputTokens: 20, CacheReadTokens: 30},
		},
		{

			name: "cached larger than input keeps counters as reported",
			raw:  `{"inputTokens":10,"outputTokens":20,"totalTokens":30,"cachedReadTokens":40}`,
			want: TokenUsage{InputTokens: 10, OutputTokens: 20, CacheReadTokens: 40},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := parseACPTokenUsage(json.RawMessage(tt.raw)); got != tt.want {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestRuntimeJClientHandleToolCallComplete(t *testing.T) {
	t.Parallel()

	var got Message
	c := &runtimeJClient{
		pending: make(map[int]*pendingRPC),
		onMessage: func(сообщение Message) {
			got = сообщение
		},
	}

	line := `{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_1","update":{"sessionUpdate":"tool_call_update","toolCallId":"tc-abc123","status":"completed","kind":"execute","rawOutput":"file1.go\nfile2.go\n"}}}`
	c.handleLine(line)

	if got.Type != MessageToolResult {
		t.Errorf("type: got %v, want MessageToolResult", got.Type)
	}
	if got.CallID != "tc-abc123" {
		t.Errorf("callID: got %q, want %q", got.CallID, "tc-abc123")
	}
	if got.Output != "file1.go\nfile2.go\n" {
		t.Errorf("output: got %q", got.Output)
	}
}

func TestRuntimeJClientRuntimeKStreamingToolCall(t *testing.T) {
	t.Parallel()

	var got []Message
	c := &runtimeJClient{
		pending: make(map[int]*pendingRPC),
		onMessage: func(сообщение Message) {
			got = append(got, сообщение)
		},
	}

	c.handleLine(`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_1","update":{"sessionUpdate":"tool_call","toolCallId":"tc-kimi-1","title":"Shell","status":"in_progress","content":[{"type":"content","content":{"type":"text","text":""}}]}}}`)
	if len(got) != 0 {
		t.Fatalf("expected nothing emitted yet (args empty), got %+v", got)
	}

	partials := []string{
		`{"`,
		`{"command`,
		`{"command":`,
		`{"command":"echo `,
		`{"command":"echo hi"}`,
	}
	for _, args := range partials {

		argsJSON, _ := json.Marshal(args)
		line := `{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_1","update":{"sessionUpdate":"tool_call_update","toolCallId":"tc-kimi-1","status":"in_progress","content":[{"type":"content","content":{"type":"text","text":` + string(argsJSON) + `}}]}}}`
		c.handleLine(line)
	}
	if len(got) != 0 {
		t.Fatalf("expected nothing emitted mid-stream, got %+v", got)
	}

	c.handleLine(`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_1","update":{"sessionUpdate":"tool_call_update","toolCallId":"tc-kimi-1","status":"completed","content":[{"type":"content","content":{"type":"text","text":"hi\n"}}]}}}`)

	if len(got) != 2 {
		t.Fatalf("expected [MessageToolUse, MessageToolResult], got %d: %+v", len(got), got)
	}
	if got[0].Type != MessageToolUse {
		t.Errorf("first message: got %v, want MessageToolUse", got[0].Type)
	}
	if got[0].CallID != "tc-kimi-1" {
		t.Errorf("first.callID: got %q", got[0].CallID)
	}
	if cmd, _ := got[0].Input["command"].(string); cmd != "echo hi" {
		t.Errorf("first.Input.command: got %v, want %q", got[0].Input["command"], "echo hi")
	}
	if got[1].Type != MessageToolResult {
		t.Errorf("second message: got %v, want MessageToolResult", got[1].Type)
	}
	if got[1].Output != "hi\n" {
		t.Errorf("second.output: got %q, want %q", got[1].Output, "hi\n")
	}
}

func TestRuntimeJClientRuntimeKMalformedArgsFallback(t *testing.T) {
	t.Parallel()

	var got []Message
	c := &runtimeJClient{
		pending: make(map[int]*pendingRPC),
		onMessage: func(сообщение Message) {
			got = append(got, сообщение)
		},
	}

	c.handleLine(`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_1","update":{"sessionUpdate":"tool_call","toolCallId":"tc","title":"Shell","status":"in_progress","content":[{"type":"content","content":{"type":"text","text":"not-json"}}]}}}`)
	c.handleLine(`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_1","update":{"sessionUpdate":"tool_call_update","toolCallId":"tc","status":"completed","content":[{"type":"content","content":{"type":"text","text":"output"}}]}}}`)

	if len(got) < 1 {
		t.Fatalf("expected ToolUse+ToolResult, got %+v", got)
	}
	if text, _ := got[0].Input["text"].(string); text != "not-json" {
		t.Errorf("fallback Input.text: got %v", got[0].Input["text"])
	}
}

func TestRuntimeJClientHandleToolCallCompleteOrphan(t *testing.T) {
	t.Parallel()

	var got []Message
	c := &runtimeJClient{
		pending: make(map[int]*pendingRPC),
		onMessage: func(сообщение Message) {
			got = append(got, сообщение)
		},
	}

	c.handleLine(`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_1","update":{"sessionUpdate":"tool_call_update","toolCallId":"tc","status":"completed","title":"terminal: ls","kind":"execute","rawInput":{"command":"ls"},"content":[{"type":"content","content":{"type":"text","text":"file.go\n"}}]}}}`)

	if len(got) != 2 || got[0].Type != MessageToolUse || got[1].Type != MessageToolResult {
		t.Fatalf("expected [ToolUse, ToolResult], got %+v", got)
	}
	if got[0].Tool != "terminal" {
		t.Errorf("orphan ToolUse tool: got %q", got[0].Tool)
	}
	if cmd, _ := got[0].Input["command"].(string); cmd != "ls" {
		t.Errorf("orphan ToolUse input.command: got %v", got[0].Input["command"])
	}
	if got[1].Output != "file.go\n" {
		t.Errorf("ToolResult output: got %q", got[1].Output)
	}
}

func TestRuntimeJClientHandleToolCallRawOutputTakesPrecedence(t *testing.T) {
	t.Parallel()

	var got Message
	c := &runtimeJClient{
		pending: make(map[int]*pendingRPC),
		onMessage: func(сообщение Message) {
			got = сообщение
		},
	}

	line := `{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_1","update":{"sessionUpdate":"tool_call_update","toolCallId":"tc","status":"completed","rawOutput":"raw wins","content":[{"type":"content","content":{"type":"text","text":"ignored"}}]}}}`
	c.handleLine(line)

	if got.Output != "raw wins" {
		t.Errorf("output: got %q, want %q", got.Output, "raw wins")
	}
}

func TestExtractACPToolCallText(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		json string
		want string
	}{
		{
			name: "single text block",
			json: `[{"type":"content","content":{"type":"text","text":"hello"}}]`,
			want: "hello",
		},
		{
			name: "multiple text blocks join with newline",
			json: `[{"type":"content","content":{"type":"text","text":"a"}},{"type":"content","content":{"type":"text","text":"b"}}]`,
			want: "a\nb",
		},
		{
			name: "terminal blocks skipped",
			json: `[{"type":"terminal","terminalId":"t1"},{"type":"content","content":{"type":"text","text":"shell out"}}]`,
			want: "shell out",
		},
		{
			name: "diff block renders as mini header",
			json: `[{"type":"diff","path":"foo.go","oldText":"abc","newText":"abcdef"}]`,
			want: "--- foo.go\n+++ foo.go\n(edited: 3 → 6 bytes)",
		},
		{
			name: "new-file diff (no oldText)",
			json: `[{"type":"diff","path":"new.go","oldText":"","newText":"hi"}]`,
			want: "--- new.go\n+++ new.go\n(new file, 2 bytes)",
		},
		{
			name: "empty array returns empty",
			json: `[]`,
			want: "",
		},
		{
			name: "no text content",
			json: `[{"type":"terminal","terminalId":"t1"}]`,
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var blocks []json.RawMessage
			if err := json.Unmarshal([]byte(tt.json), &blocks); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if got := extractACPToolCallText(blocks); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRuntimeJClientHandleToolCallInProgressIgnored(t *testing.T) {
	t.Parallel()

	called := false
	c := &runtimeJClient{
		pending: make(map[int]*pendingRPC),
		onMessage: func(сообщение Message) {
			called = true
		},
	}

	line := `{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_1","update":{"sessionUpdate":"tool_call_update","toolCallId":"tc-abc123","status":"in_progress"}}}`
	c.handleLine(line)

	if called {
		t.Error("expected in_progress tool_call_update to be ignored")
	}
}

func TestRuntimeJClientHandleUsageUpdate(t *testing.T) {
	t.Parallel()

	c := &runtimeJClient{
		pending: make(map[int]*pendingRPC),
	}

	line := `{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_1","update":{"sessionUpdate":"usage_update","usage":{"inputTokens":500,"outputTokens":200,"cachedReadTokens":100}}}}`
	c.handleLine(line)

	c.usageMu.Lock()
	defer c.usageMu.Unlock()

	if c.usage.InputTokens != 500 {
		t.Errorf("inputTokens: got %d, want 500", c.usage.InputTokens)
	}
	if c.usage.OutputTokens != 200 {
		t.Errorf("outputTokens: got %d, want 200", c.usage.OutputTokens)
	}
	if c.usage.CacheReadTokens != 100 {
		t.Errorf("cacheReadTokens: got %d, want 100", c.usage.CacheReadTokens)
	}
}

func TestRuntimeJClientHandleUsageUpdateCumulative(t *testing.T) {
	t.Parallel()

	c := &runtimeJClient{
		pending: make(map[int]*pendingRPC),
	}

	c.handleLine(`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_1","update":{"sessionUpdate":"usage_update","usage":{"inputTokens":100,"outputTokens":50}}}}`)

	c.handleLine(`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_1","update":{"sessionUpdate":"usage_update","usage":{"inputTokens":300,"outputTokens":120}}}}`)

	c.usageMu.Lock()
	defer c.usageMu.Unlock()

	if c.usage.InputTokens != 300 {
		t.Errorf("inputTokens: got %d, want 300", c.usage.InputTokens)
	}
	if c.usage.OutputTokens != 120 {
		t.Errorf("outputTokens: got %d, want 120", c.usage.OutputTokens)
	}
}

func TestRuntimeJClientExtractPromptResult(t *testing.T) {
	t.Parallel()

	var got runtimeJPromptResult
	c := &runtimeJClient{
		pending: make(map[int]*pendingRPC),
		onPromptDone: func(result runtimeJPromptResult) {
			got = result
		},
	}

	data := json.RawMessage(`{"stopReason":"end_turn","usage":{"inputTokens":1000,"outputTokens":200,"cachedReadTokens":50}}`)
	c.extractPromptResult(data)

	if got.stopReason != "end_turn" {
		t.Errorf("stopReason: got %q, want %q", got.stopReason, "end_turn")
	}
	if got.usage.InputTokens != 1000 {
		t.Errorf("inputTokens: got %d, want 1000", got.usage.InputTokens)
	}
	if got.usage.OutputTokens != 200 {
		t.Errorf("outputTokens: got %d, want 200", got.usage.OutputTokens)
	}
	if got.usage.CacheReadTokens != 50 {
		t.Errorf("cacheReadTokens: got %d, want 50", got.usage.CacheReadTokens)
	}
}

func TestRuntimeJClientExtractPromptResultNoUsage(t *testing.T) {
	t.Parallel()

	var got runtimeJPromptResult
	c := &runtimeJClient{
		pending: make(map[int]*pendingRPC),
		onPromptDone: func(result runtimeJPromptResult) {
			got = result
		},
	}

	data := json.RawMessage(`{"stopReason":"cancelled"}`)
	c.extractPromptResult(data)

	if got.stopReason != "cancelled" {
		t.Errorf("stopReason: got %q, want %q", got.stopReason, "cancelled")
	}
	if got.usage.InputTokens != 0 {
		t.Errorf("inputTokens: got %d, want 0", got.usage.InputTokens)
	}
}

func TestRuntimeJClientExtractPromptResultMetaUsage(t *testing.T) {
	t.Parallel()

	var got runtimeJPromptResult
	c := &runtimeJClient{
		pending: make(map[int]*pendingRPC),
		onPromptDone: func(result runtimeJPromptResult) {
			got = result
		},
	}

	data := json.RawMessage(`{
		"stopReason": "end_turn",
		"_meta": {
			"sessionId": "019f8516-4fee-7c23-9766-ec748929e01a",
			"modelId": "grok-4.5",
			"inputTokens": 12929,
			"outputTokens": 29,
			"cachedReadTokens": 10880,
			"reasoningTokens": 24,
			"usage": {
				"inputTokens": 12929,
				"outputTokens": 29,
				"totalTokens": 12958,
				"cachedReadTokens": 10880,
				"reasoningTokens": 24,
				"modelCalls": 1,
				"costUsdTicks": 75360000
			}
		}
	}`)
	c.extractPromptResult(data)

	if got.stopReason != "end_turn" {
		t.Errorf("stopReason: got %q, want %q", got.stopReason, "end_turn")
	}

	if got.usage.InputTokens != 2049 {
		t.Errorf("inputTokens: got %d, want 2049", got.usage.InputTokens)
	}
	if got.usage.OutputTokens != 29 {
		t.Errorf("outputTokens: got %d, want 29", got.usage.OutputTokens)
	}
	if got.usage.CacheReadTokens != 10880 {
		t.Errorf("cacheReadTokens: got %d, want 10880", got.usage.CacheReadTokens)
	}

	if got.modelID != "grok-4.5" {
		t.Errorf("modelID: got %q, want %q", got.modelID, "grok-4.5")
	}

	if got.usage.CostUSDTicks != 75360000 {
		t.Errorf("costUsdTicks: got %d, want 75360000", got.usage.CostUSDTicks)
	}
	if usd := float64(got.usage.CostUSDTicks) / CostUSDTicksPerUSD; math.Abs(usd-0.007536) > 1e-12 {
		t.Errorf("cost in USD: got %v, want 0.007536", usd)
	}
}

func TestParseACPTokenUsageCostIsOptional(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		raw  string
		want int64
	}{
		{name: "camelCase", raw: `{"inputTokens":10,"costUsdTicks":4200}`, want: 4200},
		{name: "snake_case", raw: `{"input_tokens":10,"cost_usd_ticks":4200}`, want: 4200},
		{name: "absent", raw: `{"inputTokens":10,"outputTokens":2}`, want: 0},
		{name: "null", raw: `{"inputTokens":10,"costUsdTicks":null}`, want: 0},
		{name: "string number", raw: `{"inputTokens":10,"costUsdTicks":"4200"}`, want: 4200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := parseACPTokenUsage(json.RawMessage(tc.raw)).CostUSDTicks; got != tc.want {
				t.Errorf("CostUSDTicks: got %d, want %d", got, tc.want)
			}
		})
	}
}

func TestParseACPModelIDFromMeta(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		raw  string
		want string
	}{
		{name: "camelCase", raw: `{"modelId":"grok-4.5"}`, want: "grok-4.5"},
		{name: "snake_case", raw: `{"model_id":"grok-4.3"}`, want: "grok-4.3"},
		{name: "camelCase wins over snake_case", raw: `{"modelId":"grok-4.5","model_id":"grok-4.3"}`, want: "grok-4.5"},
		{name: "whitespace trimmed", raw: `{"modelId":"  grok-4.5  "}`, want: "grok-4.5"},
		{name: "absent", raw: `{"sessionId":"ses_1"}`, want: ""},
		{name: "empty string", raw: `{"modelId":""}`, want: ""},
		{name: "null meta", raw: `null`, want: ""},
		{name: "malformed", raw: `{"modelId":`, want: ""},
		{name: "wrong type degrades to empty", raw: `{"modelId":42}`, want: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := parseACPModelIDFromMeta(json.RawMessage(tc.raw)); got != tc.want {
				t.Errorf("parseACPModelIDFromMeta(%s): got %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

func TestRuntimeJClientExtractPromptResultMetaFlatOnly(t *testing.T) {
	t.Parallel()

	var got runtimeJPromptResult
	c := &runtimeJClient{
		pending: make(map[int]*pendingRPC),
		onPromptDone: func(result runtimeJPromptResult) {
			got = result
		},
	}

	data := json.RawMessage(`{
		"stopReason": "end_turn",
		"_meta": {
			"inputTokens": 50,
			"outputTokens": 10,
			"cachedReadTokens": 5
		}
	}`)
	c.extractPromptResult(data)

	if got.usage.InputTokens != 50 || got.usage.OutputTokens != 10 || got.usage.CacheReadTokens != 5 {
		t.Fatalf("unexpected usage from flat _meta: %+v", got.usage)
	}
}

func TestRuntimeJClientExtractPromptResultTopLevelBeatsMeta(t *testing.T) {
	t.Parallel()

	var got runtimeJPromptResult
	c := &runtimeJClient{
		pending: make(map[int]*pendingRPC),
		onPromptDone: func(result runtimeJPromptResult) {
			got = result
		},
	}

	data := json.RawMessage(`{
		"stopReason": "end_turn",
		"usage": {"inputTokens": 1, "outputTokens": 2},
		"_meta": {"usage": {"inputTokens": 999, "outputTokens": 999}}
	}`)
	c.extractPromptResult(data)

	if got.usage.InputTokens != 1 || got.usage.OutputTokens != 2 {
		t.Fatalf("top-level usage should win: %+v", got.usage)
	}
}

func TestRuntimeJClientExtractPromptResultTopLevelZeroFallsBackToMeta(t *testing.T) {
	t.Parallel()

	var got runtimeJPromptResult
	c := &runtimeJClient{
		pending: make(map[int]*pendingRPC),
		onPromptDone: func(result runtimeJPromptResult) {
			got = result
		},
	}

	data := json.RawMessage(`{
		"stopReason": "end_turn",
		"usage": {
			"inputTokens": 0,
			"outputTokens": 0,
			"cacheReadTokens": 0,
			"cacheWriteTokens": 0
		},
		"_meta": {
			"usage": {
				"inputTokens": 100,
				"outputTokens": 20,
				"cachedReadTokens": 5,
				"cachedWriteTokens": 2
			}
		}
	}`)
	c.extractPromptResult(data)

	want := TokenUsage{InputTokens: 100, OutputTokens: 20, CacheReadTokens: 5, CacheWriteTokens: 2}
	if got.usage != want {
		t.Fatalf("zero top-level usage should fall back to _meta: got %+v, want %+v", got.usage, want)
	}
}

func TestRuntimeJClientIgnoresUnknownNotification(t *testing.T) {
	t.Parallel()

	called := false
	c := &runtimeJClient{
		pending: make(map[int]*pendingRPC),
		onMessage: func(сообщение Message) {
			called = true
		},
	}

	c.handleLine(`{"jsonrpc":"2.0","method":"unknown/event","params":{}}`)

	if called {
		t.Error("expected unknown notification to be ignored")
	}
}

func TestRuntimeJClientIgnoresInvalidJSON(t *testing.T) {
	t.Parallel()

	c := &runtimeJClient{
		pending: make(map[int]*pendingRPC),
	}

	c.handleLine("not json at all")
	c.handleLine("")
	c.handleLine("{}")
}

func TestRuntimeJProviderErrorSniffer(t *testing.T) {
	t.Parallel()

	s := newACPProviderErrorSniffer("hermes")
	lines := []string{
		"2026-04-20 23:41:47 [INFO] acp_adapter.server: Prompt on session abc",
		`⚠️  API call failed (attempt 1/3): BadRequestError [HTTP 400]`,
		`   🔌 Provider: openai-codex  Model: gpt-5.1-codex-mini`,
		`   📝 Error: HTTP 400: Error code: 400 - {'detail': "The 'gpt-5.1-codex-mini' model is not supported when using Codex with a ChatGPT account."}`,
		`⏱️  Elapsed: 1.17s`,
	}
	for _, line := range lines {
		if _, err := s.Write([]byte(line + "\n")); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}
	сообщение := s.сообщение()
	if сообщение == "" {
		t.Fatal("expected a non-empty error message")
	}
	if !strings.Contains(сообщение, "model is not supported") {
		t.Errorf("expected detail about model support, got %q", сообщение)
	}
}

func TestRuntimeJProviderErrorSnifferIgnoresInfoLines(t *testing.T) {
	t.Parallel()

	s := newACPProviderErrorSniffer("hermes")
	s.Write([]byte("2026-04-20 23:41:45 [INFO] acp_adapter.entry: Loaded env\n"))
	s.Write([]byte("2026-04-20 23:41:47 [INFO] agent.auxiliary_client: Vision auto-detect...\n"))
	if сообщение := s.сообщение(); сообщение != "" {
		t.Errorf("info lines should produce no error, got %q", сообщение)
	}
}

func TestRuntimeJProviderErrorSnifferHandlesPartialLines(t *testing.T) {
	t.Parallel()

	s := newACPProviderErrorSniffer("hermes")
	s.Write([]byte(`⚠️  API call failed (attempt 1/3):`))
	s.Write([]byte(` BadRequestError [HTTP 400]` + "\n"))
	s.Write([]byte(`   📝 Error: something went wrong` + "\n"))
	сообщение := s.сообщение()
	if !strings.Contains(сообщение, "something went wrong") {
		t.Errorf("expected buffered line to be captured, got %q", сообщение)
	}
}

func TestRuntimeJProviderErrorSnifferBoundedBuffer(t *testing.T) {
	t.Parallel()

	s := newACPProviderErrorSniffer("hermes")
	for i := 0; i < 20; i++ {

		s.Write([]byte(`⚠️  API call failed (HTTP 400) attempt ` + string(rune('a'+i%26)) + `: Non-retryable error` + "\n"))
	}
	if len(s.lines) > acpMaxErrorLines {
		t.Errorf("sniffer kept %d lines, limit is %d", len(s.lines), acpMaxErrorLines)
	}
}

func TestRuntimeJProviderErrorSnifferIgnoresEchoedInfoRecords(t *testing.T) {
	t.Parallel()

	oversizedDoc := `2026-07-24 10:08:59 [INFO] root: {"message":{"role":"tool","content":"` +
		"# ❌ Deprecated approach: do not use this query. " +
		strings.Repeat("reference documentation body ... ", 500) +
		`The parser may return KeyError: 'series'; inspect the response shape first."}}`

	tests := []struct {
		name string
		line string
	}{
		{
			name: "oversized documentation payload",
			line: oversizedDoc,
		},
		{
			name: "short tool result payload",
			line: `2026-07-24 10:09:00 [INFO] root: {"message":{"role":"tool","content":"# ❌ Deprecated example. KeyError: 'series' means the optional field is absent."}}`,
		},
		{
			name: "short llm call payload",
			line: `2026-07-24 10:09:01 [INFO] root: {"type":"llm_call","finish_reason":"tool_calls","arguments":"print(f\"Error: {err}\")","reasoning_content":"❌ This sample is intentionally invalid."}`,
		},
		{
			name: "provider-looking text inside info payload",
			line: `2026-07-24 10:09:02 [INFO] root: {"message":{"role":"tool","content":"Troubleshooting example: ❌ API call failed after 3 retries: HTTP 429. This is quoted documentation, not the current run."}}`,
		},
	}

	if len(oversizedDoc) < acpMaxErrorLineLen {
		t.Fatalf("oversized fixture is %d bytes, below the cap %d", len(oversizedDoc), acpMaxErrorLineLen)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newACPProviderErrorSniffer("hermes")
			if _, err := s.Write([]byte(tt.line + "\n")); err != nil {
				t.Fatalf("Write: %v", err)
			}
			if сообщение := s.сообщение(); сообщение != "" {
				t.Errorf("echoed INFO record must not be captured, got %d bytes: %q", len(сообщение), firstNRunes(сообщение, 100))
			}
			if сообщение := s.terminalMessage(); сообщение != "" {
				t.Errorf("echoed INFO record must not set terminal failure, got %d bytes: %q", len(сообщение), firstNRunes(сообщение, 100))
			}

			successfulFinalReply := "Done. The requested work completed successfully."
			status, errStr := promoteACPResultOnProviderError("completed", "", successfulFinalReply, s)
			if status != "completed" {
				t.Errorf("pollution: successful final reply was present, but status flipped to %q (error=%q)", status, firstNRunes(errStr, 100))
			}
		})
	}
}

func TestRuntimeJProviderErrorSnifferStillCapturesErrorRootRecords(t *testing.T) {
	t.Parallel()

	s := newACPProviderErrorSniffer("hermes")
	line := `2026-07-24 10:09:03 [ERROR] root: ❌ API call failed after 3 retries: HTTP 429 rate limit exceeded`
	if _, err := s.Write([]byte(line + "\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	сообщение := s.terminalMessage()
	if !strings.Contains(сообщение, "HTTP 429") {
		t.Fatalf("real error: expected genuine ERROR root record to remain terminal, got %q", сообщение)
	}
}

func TestRuntimeJProviderErrorSnifferIgnoresMultiLineInfoRecords(t *testing.T) {
	t.Parallel()

	record := "2026-07-24 10:09:00 [INFO] root: {\n" +
		"  \"message\": {\"role\": \"tool\", \"content\": \"❌ Error: KeyError 'series' is quoted documentation, not a provider failure\"}\n" +
		"}\n"

	s := newACPProviderErrorSniffer("hermes")
	for _, physicalLine := range strings.SplitAfter(record, "\n") {
		if physicalLine == "" {
			continue
		}
		if _, err := s.Write([]byte(physicalLine)); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}
	if сообщение := s.сообщение(); сообщение != "" {
		t.Errorf("multi-line INFO echo must not be captured, got %q", firstNRunes(сообщение, 120))
	}
	if сообщение := s.terminalMessage(); сообщение != "" {
		t.Errorf("multi-line INFO echo must not be terminal, got %q", firstNRunes(сообщение, 120))
	}
	status, errStr := promoteACPResultOnProviderError("completed", "", "Done. The requested work completed successfully.", s)
	if status != "completed" {
		t.Errorf("multi-line INFO echo flipped a completed run to %q (error=%q)", status, firstNRunes(errStr, 120))
	}
}

func TestRuntimeJProviderErrorSnifferErrorRecordAfterInfoRecord(t *testing.T) {
	t.Parallel()

	stream := "2026-07-24 10:09:00 [INFO] root: {\n" +
		"  \"content\": \"❌ Error: harmless echoed documentation\"\n" +
		"}\n" +
		"2026-07-24 10:09:05 [ERROR] root: ❌ API call failed after 3 retries: HTTP 429 rate limit exceeded\n"

	s := newACPProviderErrorSniffer("hermes")
	if _, err := s.Write([]byte(stream)); err != nil {
		t.Fatalf("Write: %v", err)
	}
	сообщение := s.terminalMessage()
	if !strings.Contains(сообщение, "HTTP 429") {
		t.Fatalf("real ERROR record after an INFO echo must stay terminal, got %q", сообщение)
	}
}

func TestRuntimeJProviderErrorSnifferRealErrorsAfterSingleLineInfo(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		stream string
	}{
		{
			name: "bare provider error",
			stream: "2026-07-24 10:09:00 [INFO] root: {\"message\":\"ordinary echo with an unmatched { in quoted text\"}\n" +
				"❌ API call failed after 3 retries: RateLimitError [HTTP 429]\n" +
				"📝 Error: HTTP 429 rate limit exceeded\n",
		},
		{
			name: "non-root error logger",
			stream: "2026-07-24 10:09:00 [INFO] root: {\"message\":\"ordinary echo\"}\n" +
				"2026-07-24 10:09:01 [ERROR] acp_adapter.server: ❌ API call failed after 3 retries: HTTP 429 rate limit exceeded\n",
		},
		{
			name: "bare provider error after echo truncated inside string",
			stream: "2026-07-24 10:09:00 [INFO] root: {\"message\":\"truncated echo\n" +
				"❌ API call failed after 3 retries: RateLimitError [HTTP 429]\n" +
				"📝 Error: HTTP 429 rate limit exceeded\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newACPProviderErrorSniffer("hermes")
			if _, err := s.Write([]byte(tt.stream)); err != nil {
				t.Fatalf("Write: %v", err)
			}
			if сообщение := s.terminalMessage(); !strings.Contains(сообщение, "HTTP 429") {
				t.Fatalf("real provider error after a single-line INFO record was swallowed, got %q", сообщение)
			}
		})
	}
}

func TestRuntimeJProviderErrorSnifferLongRealErrorStillFails(t *testing.T) {
	t.Parallel()

	longReal := "❌ API call failed after 3 retries: BadRequestError [HTTP 400] " +
		"Error: HTTP 400: Error code: 400 - {'detail': \"" +
		strings.Repeat("the requested model is not supported for this account; ", 200) +
		"\"}"
	if len(longReal) <= acpMaxErrorLineLen {
		t.Fatalf("fixture is %d bytes, must exceed the cap %d to exercise the path", len(longReal), acpMaxErrorLineLen)
	}

	for _, provider := range []string{"hermes", "kimi", "kiro", "qoder", "grok", "traecli"} {
		t.Run(provider, func(t *testing.T) {
			s := newACPProviderErrorSniffer(provider)
			if _, err := s.Write([]byte(longReal + "\n")); err != nil {
				t.Fatalf("Write: %v", err)
			}

			status, errStr := promoteACPResultOnProviderError("completed", "", "", s)
			if status != "failed" {
				t.Fatalf("long real provider error was dropped; run stayed %q instead of failed", status)
			}
			if errStr == "" {
				t.Fatal("failed run must carry a non-empty provider-error message")
			}
			if len(errStr) > acpMaxErrorLineLen+len("…(truncated)") {
				t.Errorf("persisted error not bounded: %d bytes", len(errStr))
			}
		})
	}
}

func firstNRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

func fakeRuntimeJACPUsageWithDefaultModelScript() string {
	return `#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{}}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"ses_model","models":{"currentModelId":"nous:moonshotai/kimi-k2.6","availableModels":[{"modelId":"nous:moonshotai/kimi-k2.6","name":"moonshotai/kimi-k2.6"}]}}}\n' "$id"
      ;;
    *'"method":"session/prompt"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn","usage":{"inputTokens":17,"outputTokens":5,"cachedReadTokens":3}}}\n' "$id"
      exit 0
      ;;
  esac
done
`
}

func TestRuntimeJBackendAttributesUsageToACPDefaultModel(t *testing.T) {
	t.Parallel()

	fakePath := filepath.Join(t.TempDir(), "hermes")
	writeTestExecutable(t, fakePath, []byte(fakeRuntimeJACPUsageWithDefaultModelScript()))

	backend, err := New("runtime-j", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new hermes backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{
		Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()

	select {
	case result, ok := <-session.Result:
		if !ok {
			t.Fatal("result channel closed without a value")
		}
		if result.Status != "completed" {
			t.Fatalf("expected completed result, got %q: %s", result.Status, result.Error)
		}
		if _, ok := result.Usage["unknown"]; ok {
			t.Fatalf("usage should not be attributed to unknown: %+v", result.Usage)
		}
		usage, ok := result.Usage["nous:moonshotai/kimi-k2.6"]
		if !ok {
			t.Fatalf("expected usage under Hermes current model, got %+v", result.Usage)
		}
		if usage.InputTokens != 17 || usage.OutputTokens != 5 || usage.CacheReadTokens != 3 {
			t.Fatalf("usage = %+v, want input=17 output=5 cache_read=3", usage)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
	}
}

func fakeRuntimeJACPRateLimitScript() string {
	return `#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{}}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"ses_429"}}\n' "$id"
      ;;
    *'"method":"session/prompt"'*)
      # Mimic hermes' real-world stderr block on a 429.
      printf '%s\n' '⚠️  API call failed (attempt 3/3): RateLimitError [HTTP 429]' >&2
      printf '%s\n' '   📝 Error: HTTP 429: The usage limit has been reached' >&2
      # Mimic hermes injecting the failure as a synthetic agent turn so
      # the chat shows *something*; this puts text in output and used to
      # mask the failure from the daemon.
      printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_429","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"API call failed after 3 retries: HTTP 429: The usage limit has been reached"}}}}\n'
      printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn"}}\n' "$id"
      exit 0
      ;;
  esac
done
`
}

func TestRuntimeJProviderErrorSnifferTerminalVsTransient(t *testing.T) {
	t.Parallel()

	s := newACPProviderErrorSniffer("hermes")
	s.Write([]byte("⚠️  API call failed (attempt 1/3): retryable upstream blip\n"))
	if сообщение := s.сообщение(); сообщение == "" {
		t.Fatalf("sniffer should still capture transient warnings for diagnostics")
	}
	if сообщение := s.terminalMessage(); сообщение != "" {
		t.Fatalf("transient attempt should NOT be a terminal failure, got %q", сообщение)
	}

	s.Write([]byte("❌  API call failed after 3 retries: usage limit reached\n"))
	if сообщение := s.terminalMessage(); сообщение == "" {
		t.Fatalf("after-N-retries / ❌ should switch terminalMessage on")
	}
}

func TestRuntimeJProviderErrorSnifferTerminalNonRetryable(t *testing.T) {
	t.Parallel()

	for _, line := range []string{
		`⚠️  API call failed (attempt 1/3): BadRequestError [HTTP 400]`,
		`⚠️  API call failed (attempt 1/3): AuthenticationError [HTTP 401]`,
		`⚠️  API call failed (HTTP 400) attempt a: Non-retryable error`,
		`❌ API call failed after 3 retries: RateLimitError [HTTP 429]`,
		`[ERROR] API call failed: upstream returned HTTP 500`,
	} {
		s := newACPProviderErrorSniffer("hermes")
		s.Write([]byte(line + "\n"))
		if сообщение := s.terminalMessage(); сообщение == "" {
			t.Errorf("expected %q to be classified as terminal", line)
		}
	}
}

func TestRuntimeJBackendPromotesProviderErrorWithNonEmptyOutput(t *testing.T) {
	t.Parallel()

	fakePath := filepath.Join(t.TempDir(), "hermes")
	writeTestExecutable(t, fakePath, []byte(fakeRuntimeJACPRateLimitScript()))

	backend, err := New("runtime-j", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new hermes backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{
		Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()

	select {
	case result, ok := <-session.Result:
		if !ok {
			t.Fatal("result channel closed without a value")
		}
		if result.Status != "failed" {
			t.Fatalf("expected status=failed (sniffer should promote on 429 even with non-empty output), got %q (error=%q output=%q)", result.Status, result.Error, result.Output)
		}
		if !strings.Contains(result.Error, "429") && !strings.Contains(result.Error, "usage limit") {
			t.Errorf("expected error to surface the 429 / usage-limit message, got %q", result.Error)
		}
		if result.SessionID != "ses_429" {
			t.Errorf("expected session id to be preserved on failure, got %q", result.SessionID)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
	}
}

func TestIsACPSessionNotFound(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "hermes session not found in message",
			err:  &acpRPCError{Method: "session/prompt", Code: -32603, Message: "Session not found"},
			want: true,
		},
		{
			name: "kiro no session found in data",
			err:  &acpRPCError{Method: "session/prompt", Code: -32603, Message: "Internal error", Data: "No session found with id ses_abc"},
			want: true,
		},
		{
			name: "internal error without session wording",
			err:  &acpRPCError{Method: "session/prompt", Code: -32603, Message: "Internal error", Data: "upstream provider returned HTTP 429"},
			want: false,
		},
		{
			name: "kimi session not found as invalid_params data",

			err:  &acpRPCError{Method: "session/set_model", Code: -32602, Message: "Invalid params", Data: `{"session_id": "Session not found"}`},
			want: true,
		},
		{
			name: "invalid params without session wording",
			err:  &acpRPCError{Method: "session/set_model", Code: -32602, Message: "model not available: bogus-model"},
			want: false,
		},
		{
			name: "session wording under an unrelated code",
			err:  &acpRPCError{Method: "session/prompt", Code: -32601, Message: "Session not found"},
			want: false,
		},
		{
			name: "plain error",
			err:  fmt.Errorf("session/prompt: Session not found (code=-32603)"),
			want: false,
		},
		{
			name: "wrapped rpc error",
			err:  fmt.Errorf("request failed: %w", &acpRPCError{Method: "session/prompt", Code: -32603, Message: "Session not found"}),
			want: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := isACPSessionNotFound(tc.err); got != tc.want {
				t.Errorf("isACPSessionNotFound(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func fakeRuntimeJACPStaleResumeScript() string {
	return `#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{}}}\n' "$id"
      ;;
    *'"method":"session/resume"'*)
      sid=$(printf '%s' "$line" | sed -n 's/.*"sessionId":"\([^"]*\)".*/\1/p')
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"%s"}}\n' "$id" "$sid"
      ;;
    *'"method":"session/prompt"'*)
      printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32603,"message":"Session not found"}}\n' "$id"
      exit 0
      ;;
  esac
done
`
}

func TestRuntimeJBackendClearsSessionIDWhenResumedSessionNotFound(t *testing.T) {
	t.Parallel()

	fakePath := filepath.Join(t.TempDir(), "hermes")
	writeTestExecutable(t, fakePath, []byte(fakeRuntimeJACPStaleResumeScript()))

	backend, err := New("runtime-j", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new hermes backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{
		Timeout:         5 * time.Second,
		ResumeSessionID: "ses_stale",
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()

	select {
	case result, ok := <-session.Result:
		if !ok {
			t.Fatal("result channel closed without a value")
		}
		if result.Status != "failed" {
			t.Fatalf("expected status=failed, got %q (error=%q)", result.Status, result.Error)
		}
		if !strings.Contains(result.Error, "Session not found") {
			t.Errorf("expected error to surface the session-not-found message, got %q", result.Error)
		}
		if result.SessionID != "" {
			t.Errorf("expected empty session id so the daemon's fresh-session retry fires, got %q", result.SessionID)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
	}
}

func TestIsACPProviderBadRequest(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
		want bool
	}{
		{

			name: "hermes provider 400 on prompt",
			err:  &acpRPCError{Method: "session/prompt", Code: -32603, Message: "Internal error", Data: "BadRequestError"},
			want: true,
		},
		{
			name: "wrapped rpc error still matches",
			err:  fmt.Errorf("request failed: %w", &acpRPCError{Method: "session/prompt", Code: -32603, Message: "Internal error", Data: "BadRequestError"}),
			want: true,
		},
		{

			name: "context window exceeded is not poisoning",
			err:  &acpRPCError{Method: "session/prompt", Code: -32603, Message: "Internal error", Data: "ContextWindowExceededError"},
			want: false,
		},
		{
			name: "unrelated exception class",
			err:  &acpRPCError{Method: "session/prompt", Code: -32603, Message: "Internal error", Data: "TimeoutError"},
			want: false,
		},
		{

			name: "llm_not_configured is environmental",
			err:  &acpRPCError{Method: "session/prompt", Code: -32603, Message: "config.user.yaml llm.api_key is not set", Data: "llm_not_configured"},
			want: false,
		},
		{

			name: "session not found is the other detector's job",
			err:  &acpRPCError{Method: "session/prompt", Code: -32603, Message: "Session not found", Data: "Session not found"},
			want: false,
		},
		{

			name: "wrong method",
			err:  &acpRPCError{Method: "session/new", Code: -32603, Message: "Internal error", Data: "BadRequestError"},
			want: false,
		},
		{
			name: "wrong code",
			err:  &acpRPCError{Method: "session/prompt", Code: -32602, Message: "Invalid params", Data: "BadRequestError"},
			want: false,
		},
		{

			name: "prefix-similar class name",
			err:  &acpRPCError{Method: "session/prompt", Code: -32603, Message: "Internal error", Data: "BadRequestErrorRetryable"},
			want: false,
		},
		{
			name: "plain error with the same text",
			err:  fmt.Errorf("session/prompt: Internal error (code=-32603, data=BadRequestError)"),
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := isACPProviderBadRequest(tc.err); got != tc.want {
				t.Errorf("isACPProviderBadRequest(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func fakeRuntimeBACPPoisonedResumeScript(data string) string {
	return `#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{}}}\n' "$id"
      ;;
    *'"method":"session/resume"'*)
      sid=$(printf '%s' "$line" | sed -n 's/.*"sessionId":"\([^"]*\)".*/\1/p')
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"%s"}}\n' "$id" "$sid"
      ;;
    *'"method":"session/prompt"'*)
      printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32603,"message":"Internal error","data":"` + data + `"}}\n' "$id"
      exit 0
      ;;
  esac
done
`
}

func TestRuntimeBBackendClearsResumedSessionOnProviderBadRequest(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	cases := []struct {
		name           string
		data           string
		wantSessionID  string
		wantRejected   bool
		wantErrMarkers []string
	}{
		{
			name:          "provider 400 clears the session for a fresh retry",
			data:          "BadRequestError",
			wantSessionID: "",
			wantRejected:  true,

			wantErrMarkers: []string{"session/prompt", "(code=-32603, data=BadRequestError)"},
		},
		{
			name:           "unknown exception class keeps the session",
			data:           "TimeoutError",
			wantSessionID:  "ses_poisoned",
			wantRejected:   false,
			wantErrMarkers: []string{"session/prompt", "(code=-32603, data=TimeoutError)"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fakePath := filepath.Join(t.TempDir(), "hermes")
			writeTestExecutable(t, fakePath, []byte(fakeRuntimeBACPPoisonedResumeScript(tc.data)))

			backend, err := New("runtime-j", Config{ExecutablePath: fakePath, Logger: slog.Default()})
			if err != nil {
				t.Fatalf("new hermes backend: %v", err)
			}

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{
				Timeout:         20 * time.Second,
				ResumeSessionID: "ses_poisoned",
			})
			if err != nil {
				t.Fatalf("execute: %v", err)
			}
			go func() {
				for range session.Messages {
				}
			}()

			select {
			case result, ok := <-session.Result:
				if !ok {
					t.Fatal("result channel closed without a value")
				}
				if result.Status != "failed" {
					t.Fatalf("expected status=failed, got %q (error=%q)", result.Status, result.Error)
				}
				for _, marker := range tc.wantErrMarkers {
					if !strings.Contains(result.Error, marker) {
						t.Errorf("expected error to contain %q, got %q", marker, result.Error)
					}
				}
				if result.SessionID != tc.wantSessionID {
					t.Errorf("expected session id %q, got %q", tc.wantSessionID, result.SessionID)
				}
				if result.ResumeRejected != tc.wantRejected {
					t.Errorf("expected ResumeRejected=%v, got %v", tc.wantRejected, result.ResumeRejected)
				}
			case <-time.After(30 * time.Second):
				t.Fatal("timeout waiting for result")
			}
		})
	}
}

func fakeRuntimeJACPStaleResumeSetModelScript() string {
	return `#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{}}}\n' "$id"
      ;;
    *'"method":"session/resume"'*)
      sid=$(printf '%s' "$line" | sed -n 's/.*"sessionId":"\([^"]*\)".*/\1/p')
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"%s"}}\n' "$id" "$sid"
      ;;
    *'"method":"session/set_model"'*)
      printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32603,"message":"Session not found"}}\n' "$id"
      exit 0
      ;;
  esac
done
`
}

func TestRuntimeJBackendClearsSessionIDWhenSetModelSessionNotFound(t *testing.T) {
	t.Parallel()

	fakePath := filepath.Join(t.TempDir(), "hermes")
	writeTestExecutable(t, fakePath, []byte(fakeRuntimeJACPStaleResumeSetModelScript()))

	backend, err := New("runtime-j", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new hermes backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{
		Timeout:         5 * time.Second,
		ResumeSessionID: "ses_stale",
		Model:           "some-model",
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()

	select {
	case result, ok := <-session.Result:
		if !ok {
			t.Fatal("result channel closed without a value")
		}
		if result.Status != "failed" {
			t.Fatalf("expected status=failed, got %q (error=%q)", result.Status, result.Error)
		}
		if !strings.Contains(result.Error, `could not switch to model "some-model"`) {
			t.Errorf("expected error to name the requested model, got %q", result.Error)
		}
		if result.SessionID != "" {
			t.Errorf("expected empty session id so the daemon's fresh-session retry fires, got %q", result.SessionID)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
	}
}

func fakeRuntimeJACPTransientRetryScript() string {
	return `#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{}}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"ses_ok"}}\n' "$id"
      ;;
    *'"method":"session/prompt"'*)
      # Per-attempt rate-limit warning that hermes routinely logs on
      # transient blips — the request DOES retry and succeed below.
      printf '%s\n' '⚠️  API call failed (attempt 1/3): RateLimitError [HTTP 429]' >&2
      # Real agent answer streamed back as a normal text turn.
      printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_ok","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"Here is the answer you asked for."}}}}\n'
      printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn"}}\n' "$id"
      exit 0
      ;;
  esac
done
`
}

func TestRuntimeJBackendDoesNotPromoteOnTransientRetry(t *testing.T) {
	t.Parallel()

	fakePath := filepath.Join(t.TempDir(), "hermes")
	writeTestExecutable(t, fakePath, []byte(fakeRuntimeJACPTransientRetryScript()))

	backend, err := New("runtime-j", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new hermes backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{
		Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()

	select {
	case result, ok := <-session.Result:
		if !ok {
			t.Fatal("result channel closed without a value")
		}
		if result.Status != "completed" {
			t.Fatalf("transient retry that ultimately succeeded must stay status=completed, got %q (error=%q output=%q)", result.Status, result.Error, result.Output)
		}
		if !strings.Contains(result.Output, "Here is the answer") {
			t.Errorf("expected the successful agent turn to be in output, got %q", result.Output)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
	}
}

func TestExtractACPMcpCapabilities(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		raw      string
		wantHTTP bool
		wantSSE  bool
	}{
		{
			name:     "both true",
			raw:      `{"protocolVersion":1,"agentCapabilities":{"mcpCapabilities":{"http":true,"sse":true}}}`,
			wantHTTP: true,
			wantSSE:  true,
		},
		{
			name:     "http only",
			raw:      `{"agentCapabilities":{"mcpCapabilities":{"http":true}}}`,
			wantHTTP: true,
			wantSSE:  false,
		},
		{
			name:     "sse only",
			raw:      `{"agentCapabilities":{"mcpCapabilities":{"sse":true}}}`,
			wantHTTP: false,
			wantSSE:  true,
		},
		{
			name:     "block missing",
			raw:      `{"agentCapabilities":{}}`,
			wantHTTP: false,
			wantSSE:  false,
		},
		{
			name:     "agentCapabilities missing",
			raw:      `{"protocolVersion":1}`,
			wantHTTP: false,
			wantSSE:  false,
		},
		{
			name:     "malformed json",
			raw:      `not json`,
			wantHTTP: false,
			wantSSE:  false,
		},
	}
	for _, tc := range tests {
		got := extractACPMcpCapabilities(json.RawMessage(tc.raw))
		if got.HTTP != tc.wantHTTP || got.SSE != tc.wantSSE {
			t.Errorf("%s: got {HTTP:%v SSE:%v}, want {HTTP:%v SSE:%v}", tc.name, got.HTTP, got.SSE, tc.wantHTTP, tc.wantSSE)
		}
	}
}

func TestFilterACPMcpServersByCapabilityStdioAlwaysPassesThrough(t *testing.T) {
	t.Parallel()

	servers := []any{
		map[string]any{"name": "fetch", "command": "uvx"},
	}
	got := filterACPMcpServersByCapability(servers, acpMcpTransportCapabilities{}, "hermes", slog.Default())
	if len(got) != 1 {
		t.Fatalf("len: got %d, want 1", len(got))
	}
}

func TestFilterACPMcpServersByCapabilityDropsUnsupportedHttp(t *testing.T) {
	t.Parallel()
	servers := []any{
		map[string]any{"name": "stdio-ok", "command": "uvx"},
		map[string]any{"type": "http", "name": "http-drop", "url": "https://x/mcp"},
		map[string]any{"type": "sse", "name": "sse-keep", "url": "https://x/sse"},
	}
	got := filterACPMcpServersByCapability(servers, acpMcpTransportCapabilities{SSE: true}, "hermes", slog.Default())
	if len(got) != 2 {
		t.Fatalf("len: got %d, want 2 (http should be dropped, sse kept)", len(got))
	}
	names := []string{got[0].(map[string]any)["name"].(string), got[1].(map[string]any)["name"].(string)}
	wantNames := map[string]bool{"stdio-ok": true, "sse-keep": true}
	for _, n := range names {
		if !wantNames[n] {
			t.Errorf("unexpected entry kept: %q", n)
		}
	}
}

func TestFilterACPMcpServersByCapabilityDropsUnsupportedSse(t *testing.T) {
	t.Parallel()
	servers := []any{
		map[string]any{"type": "sse", "name": "sse-drop", "url": "https://x/sse"},
		map[string]any{"type": "http", "name": "http-keep", "url": "https://x/mcp"},
	}
	got := filterACPMcpServersByCapability(servers, acpMcpTransportCapabilities{HTTP: true}, "kimi", slog.Default())
	if len(got) != 1 {
		t.Fatalf("len: got %d, want 1", len(got))
	}
	if got[0].(map[string]any)["name"] != "http-keep" {
		t.Errorf("kept wrong entry: %v", got[0])
	}
}

func TestFilterACPMcpServersByCapabilityKeepsAllWhenBothSupported(t *testing.T) {
	t.Parallel()
	servers := []any{
		map[string]any{"name": "stdio", "command": "uvx"},
		map[string]any{"type": "http", "name": "http", "url": "https://x/mcp"},
		map[string]any{"type": "sse", "name": "sse", "url": "https://x/sse"},
	}
	got := filterACPMcpServersByCapability(servers, acpMcpTransportCapabilities{HTTP: true, SSE: true}, "kiro", slog.Default())
	if len(got) != 3 {
		t.Fatalf("len: got %d, want 3", len(got))
	}
}

func TestFilterACPMcpServersByCapabilityEmptyInputReturnsEmpty(t *testing.T) {
	t.Parallel()
	got := filterACPMcpServersByCapability(nil, acpMcpTransportCapabilities{HTTP: true, SSE: true}, "hermes", slog.Default())
	if len(got) != 0 {
		t.Errorf("len: got %d, want 0", len(got))
	}
}

func TestRuntimeJExecuteFailsClosedOnMalformedMcpConfig(t *testing.T) {
	t.Parallel()

	fakePath := filepath.Join(t.TempDir(), "hermes")
	writeTestExecutable(t, fakePath, []byte("#!/bin/sh\nexit 0\n"))

	backend, err := New("runtime-j", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new hermes backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = backend.Execute(ctx, "prompt", ExecOptions{
		Timeout:   2 * time.Second,
		McpConfig: json.RawMessage(`not json`),
	})
	if err == nil {
		t.Fatal("expected Execute to fail closed on malformed mcp_config, got nil error")
	}
	if !strings.Contains(err.Error(), "mcp_config") {
		t.Fatalf("expected error to mention mcp_config, got %q", err)
	}
}

func fakeACPRecordingScript(recordPath, sessionID, caps string) string {
	return `#!/bin/sh
RECORD_PATH=` + recordPath + `
while IFS= read -r line; do
  printf '%s\n' "$line" >> "$RECORD_PATH"
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":` + caps + `}}\n' "$id"
      ;;
    *'"method":"session/new"'*|*'"method":"session/resume"'*|*'"method":"session/load"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"` + sessionID + `"}}\n' "$id"
      ;;
    *'"method":"session/set_model"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{}}\n' "$id"
      ;;
    *'"method":"session/prompt"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn"}}\n' "$id"
      exit 0
      ;;
  esac
done
`
}

func fakeACPRecordingScriptWithCurrentModel(recordPath, sessionID, currentModelID string) string {
	return `#!/bin/sh
RECORD_PATH=` + recordPath + `
while IFS= read -r line; do
  printf '%s\n' "$line" >> "$RECORD_PATH"
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{}}}\n' "$id"
      ;;
    *'"method":"session/new"'*|*'"method":"session/resume"'*|*'"method":"session/load"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"` + sessionID + `","models":{"currentModelId":"` + currentModelID + `"}}}\n' "$id"
      ;;
    *'"method":"session/set_model"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{}}\n' "$id"
      ;;
    *'"method":"session/prompt"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn"}}\n' "$id"
      exit 0
      ;;
  esac
done
`
}

func assertNoRecordedFrame(t *testing.T, recordPath, method string) {
	t.Helper()
	data, err := os.ReadFile(recordPath)
	if err != nil {
		t.Fatalf("read record file: %v", err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var frame map[string]any
		if err := json.Unmarshal([]byte(line), &frame); err != nil {
			continue
		}
		if frame["method"] == method {
			t.Fatalf("unexpected recorded frame for method %q in %s", method, string(data))
		}
	}
}

func findRecordedFrame(t *testing.T, recordPath, method string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(recordPath)
	if err != nil {
		t.Fatalf("read record file: %v", err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var frame map[string]any
		if err := json.Unmarshal([]byte(line), &frame); err != nil {
			continue
		}
		if frame["method"] == method {
			return frame
		}
	}
	t.Fatalf("no recorded frame for method %q in %s", method, string(data))
	return nil
}

func TestRuntimeJSetModelPreservesCustomModelIDWithColon(t *testing.T) {
	t.Parallel()

	recordPath := filepath.Join(t.TempDir(), "frames.jsonl")
	fakePath := filepath.Join(t.TempDir(), "hermes")
	writeTestExecutable(t, fakePath, []byte(fakeACPRecordingScript(recordPath, "ses_new", `{}`)))

	backend, err := New("runtime-j", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new hermes backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{
		Timeout: 5 * time.Second,
		Model:   "custom:lfm2.5:8b",
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	select {
	case result := <-session.Result:
		if result.Status != "completed" {
			t.Fatalf("expected completed result, got %q: %s", result.Status, result.Error)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
	}

	frame := findRecordedFrame(t, recordPath, "session/set_model")
	params, ok := frame["params"].(map[string]any)
	if !ok {
		t.Fatalf("session/set_model params: got %T, want map", frame["params"])
	}
	if params["sessionId"] != "ses_new" {
		t.Errorf("session/set_model.sessionId = %v, want ses_new", params["sessionId"])
	}
	if params["modelId"] != "custom:lfm2.5:8b" {
		t.Errorf("session/set_model.modelId must be passed verbatim, got %v", params["modelId"])
	}
}

func TestRuntimeJSkipsRedundantSetModelWhenAlreadyCurrent(t *testing.T) {
	t.Parallel()

	recordPath := filepath.Join(t.TempDir(), "frames.jsonl")
	fakePath := filepath.Join(t.TempDir(), "hermes")
	writeTestExecutable(t, fakePath, []byte(fakeACPRecordingScriptWithCurrentModel(recordPath, "ses_new", "custom:deepseek-v4-pro")))

	backend, err := New("runtime-j", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new hermes backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{
		Timeout: 5 * time.Second,
		Model:   "custom:deepseek-v4-pro",
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	select {
	case result := <-session.Result:
		if result.Status != "completed" {
			t.Fatalf("expected completed result, got %q: %s", result.Status, result.Error)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
	}

	assertNoRecordedFrame(t, recordPath, "session/set_model")
}

func TestRuntimeJSendsSetModelWhenModelDiffersFromCurrent(t *testing.T) {
	t.Parallel()

	recordPath := filepath.Join(t.TempDir(), "frames.jsonl")
	fakePath := filepath.Join(t.TempDir(), "hermes")
	writeTestExecutable(t, fakePath, []byte(fakeACPRecordingScriptWithCurrentModel(recordPath, "ses_new", "custom:some-default-model")))

	backend, err := New("runtime-j", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new hermes backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{
		Timeout: 5 * time.Second,
		Model:   "custom:deepseek-v4-pro",
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	select {
	case result := <-session.Result:
		if result.Status != "completed" {
			t.Fatalf("expected completed result, got %q: %s", result.Status, result.Error)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
	}

	frame := findRecordedFrame(t, recordPath, "session/set_model")
	params, ok := frame["params"].(map[string]any)
	if !ok {
		t.Fatalf("session/set_model params: got %T, want map", frame["params"])
	}
	if params["modelId"] != "custom:deepseek-v4-pro" {
		t.Errorf("session/set_model.modelId = %v, want custom:deepseek-v4-pro", params["modelId"])
	}
}

func TestRuntimeJResumeIncludesMcpServers(t *testing.T) {
	t.Parallel()

	recordPath := filepath.Join(t.TempDir(), "frames.jsonl")
	fakePath := filepath.Join(t.TempDir(), "hermes")
	writeTestExecutable(t, fakePath, []byte(fakeACPRecordingScript(recordPath, "ses_resume", `{}`)))

	backend, err := New("runtime-j", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new hermes backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{
		Timeout:         30 * time.Second,
		ResumeSessionID: "ses_resume",
		McpConfig:       json.RawMessage(`{"mcpServers":{"fetch":{"command":"uvx"}}}`),
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	select {
	case <-session.Result:
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
	}

	frame := findRecordedFrame(t, recordPath, "session/resume")
	params, ok := frame["params"].(map[string]any)
	if !ok {
		t.Fatalf("session/resume params: got %T, want map", frame["params"])
	}
	servers, ok := params["mcpServers"].([]any)
	if !ok {
		t.Fatalf("session/resume.mcpServers: got %T, want []any", params["mcpServers"])
	}
	if len(servers) != 1 {
		t.Fatalf("session/resume.mcpServers: got %d entries, want 1", len(servers))
	}
	entry := servers[0].(map[string]any)
	if entry["name"] != "fetch" || entry["command"] != "uvx" {
		t.Errorf("session/resume.mcpServers[0]: got %v, want {name:fetch,command:uvx,...}", entry)
	}
}

func TestRuntimeJDropsRemoteMcpWhenCapabilityNotAdvertised(t *testing.T) {
	t.Parallel()

	recordPath := filepath.Join(t.TempDir(), "frames.jsonl")
	fakePath := filepath.Join(t.TempDir(), "hermes")

	writeTestExecutable(t, fakePath, []byte(fakeACPRecordingScript(recordPath, "ses_new", `{}`)))

	backend, err := New("runtime-j", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new hermes backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{
		Timeout: 30 * time.Second,
		McpConfig: json.RawMessage(`{"mcpServers":{
			"local":{"command":"uvx"},
			"remote-http":{"type":"http","url":"https://x/mcp"},
			"remote-sse":{"type":"sse","url":"https://x/sse"}
		}}`),
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	select {
	case <-session.Result:
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
	}

	frame := findRecordedFrame(t, recordPath, "session/new")
	params := frame["params"].(map[string]any)
	servers, ok := params["mcpServers"].([]any)
	if !ok {
		t.Fatalf("session/new.mcpServers: got %T, want []any", params["mcpServers"])
	}
	if len(servers) != 1 {
		t.Fatalf("session/new.mcpServers: got %d entries, want 1 (only stdio should remain)", len(servers))
	}
	if servers[0].(map[string]any)["name"] != "local" {
		t.Errorf("kept the wrong entry: %v", servers[0])
	}
}

func TestRuntimeJKeepsRemoteMcpWhenCapabilityAdvertised(t *testing.T) {
	t.Parallel()

	recordPath := filepath.Join(t.TempDir(), "frames.jsonl")
	fakePath := filepath.Join(t.TempDir(), "hermes")
	writeTestExecutable(t, fakePath, []byte(fakeACPRecordingScript(recordPath, "ses_new", `{"mcpCapabilities":{"http":true,"sse":true}}`)))

	backend, err := New("runtime-j", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new hermes backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{
		Timeout: 30 * time.Second,
		McpConfig: json.RawMessage(`{"mcpServers":{
			"local":{"command":"uvx"},
			"remote-http":{"type":"http","url":"https://x/mcp"},
			"remote-sse":{"type":"sse","url":"https://x/sse"}
		}}`),
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	select {
	case <-session.Result:
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
	}

	frame := findRecordedFrame(t, recordPath, "session/new")
	params := frame["params"].(map[string]any)
	servers := params["mcpServers"].([]any)
	if len(servers) != 3 {
		t.Fatalf("session/new.mcpServers: got %d entries, want 3", len(servers))
	}
}

func TestParseRuntimeJProfileArgs(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		args     []string
		wantName string
		found    bool
		inline   bool
		from     int
		length   int
	}{
		{"none", []string{"--yolo"}, "", false, false, -1, 0},
		{"short flag", []string{"-p", "research"}, "research", true, false, 0, 2},
		{"long flag", []string{"--profile", "research"}, "research", true, false, 0, 2},
		{"inline", []string{"--profile=research"}, "research", true, true, 0, 1},
		{"inline empty value", []string{"--profile="}, "", true, true, 0, 1},
		{"trailing short flag with no value", []string{"--yolo", "-p"}, "", false, false, -1, 0},
		{"amid other args", []string{"--yolo", "--profile", "coder", "-x"}, "coder", true, false, 1, 2},
		{"space-form invalid value ignored", []string{"-p", "no:xdist"}, "", false, false, -1, 0},
		{"value-flag hides a following -p", []string{"-m", "-p", "research"}, "", false, false, -1, 0},
		{"double-dash sentinel stops scan", []string{"--", "-p", "research"}, "", false, false, -1, 0},
		{"mcp add --args passthrough stops scan", []string{"mcp", "add", "srv", "--args", "-p", "research"}, "", false, false, -1, 0},
		{"only the first occurrence is selected", []string{"-p", "research", "--profile", "coder"}, "research", true, false, 0, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			sel := ParseRuntimeJProfileArgs(tc.args)
			if sel.Name != tc.wantName || sel.Found != tc.found || sel.Inline != tc.inline ||
				sel.ArgFrom != tc.from || sel.ArgLen != tc.length {
				t.Errorf("ParseHermesProfileArgs(%v) = %+v, want name=%q found=%v inline=%v from=%d len=%d",
					tc.args, sel, tc.wantName, tc.found, tc.inline, tc.from, tc.length)
			}
		})
	}
}

func TestStripRuntimeJProfileArgs(t *testing.T) {
	t.Parallel()
	args := []string{"-p", "research", "--yolo", "--profile", "coder"}
	sel := ParseRuntimeJProfileArgs(args)
	got := StripRuntimeJProfileArgs(args, sel)
	want := []string{"--yolo", "--profile", "coder"}
	if len(got) != len(want) {
		t.Fatalf("StripHermesProfileArgs = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("StripHermesProfileArgs = %v, want %v", got, want)
		}
	}
	if none := StripRuntimeJProfileArgs([]string{"--yolo"}, ParseRuntimeJProfileArgs([]string{"--yolo"})); len(none) != 1 || none[0] != "--yolo" {
		t.Fatalf("no selection should leave args unchanged, got %v", none)
	}
	if _, ok := runtimeJBlockedArgs["-p"]; ok {
		t.Error("hermesBlockedArgs must not unconditionally strip -p")
	}
	if _, ok := runtimeJBlockedArgs["--profile"]; ok {
		t.Error("hermesBlockedArgs must not unconditionally strip --profile")
	}
}

func TestACPRawText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"empty", ``, ""},
		{"json null", `null`, ""},
		{"plain string", `"created comment"`, "created comment"},
		{"object (gpt-5.6-sol shape)", `{"items":[{"Json":{"stdout":"ok\n"}}]}`, `{"items":[{"Json":{"stdout":"ok\n"}}]}`},
		{"array", `["a","b"]`, `["a","b"]`},
	}
	for _, tt := range tests {
		got := acpRawText(json.RawMessage(tt.raw))
		if got != tt.want {
			t.Errorf("%s: acpRawText(%q) = %q, want %q", tt.name, tt.raw, got, tt.want)
		}
	}
}

func TestRuntimeJProviderErrorSnifferIgnoresToolServerDiagnostics(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name string
		line string
	}{
		{
			name: "ews connection test",
			line: `2026-08-06 11:42:48,925 [ERROR] ewsmcp.gateway.client: connection test failed: ` +
				`TransportError: HTTPSConnectionPool(host='mail.example.test', port=443): Max retries ` +
				`exceeded with url: /EWS/Exchange.asmx (Caused by SSLError(SSLEOFError(8, ` +
				`'[SSL: UNEXPECTED_EOF_WHILE_READING] EOF occurred in violation of protocol')))`,
		},
		{
			name: "ews background cache sync",
			line: `2026-08-05 20:05:19,919 [WARNING] ewsmcp.cache.sync: cache sync cycle failed: ` +
				`TransportError: HTTPSConnectionPool(host='mail.example.test', port=443): Max retries exceeded`,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s := newACPProviderErrorSniffer("hermes")
			if _, err := s.Write([]byte(tt.line + "\n")); err != nil {
				t.Fatalf("Write: %v", err)
			}
			if сообщение := s.terminalMessage(); сообщение != "" {
				t.Errorf("tool-server diagnostic must not be terminal, got %q", firstNRunes(сообщение, 120))
			}

			answer := "Готово: КП собрано, таблица цен приложена."
			status, errStr := promoteACPResultOnProviderError("completed", "", answer, s)
			if status != "completed" {
				t.Errorf("completed run was discarded on a tool-server diagnostic: status=%q error=%q",
					status, firstNRunes(errStr, 120))
			}
		})
	}
}

func TestRuntimeJBackendEmitsSessionStatusBeforeCompletion(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	tests := []struct {
		name            string
		resumeSessionID string
		effectiveID     string
	}{
		{name: "fresh", effectiveID: "ses_new"},
		{name: "resume uses returned id", resumeSessionID: "ses_old", effectiveID: "ses_resumed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempDir := t.TempDir()
			fakePath := filepath.Join(tempDir, "hermes")
			gatePath := filepath.Join(tempDir, "release-prompt")
			script := fmt.Sprintf(`#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%%s,"result":{"protocolVersion":1,"agentCapabilities":{}}}\n' "$id"
      ;;
    *'"method":"session/new"'*|*'"method":"session/resume"'*)
      printf '{"jsonrpc":"2.0","id":%%s,"result":{"sessionId":"%s"}}\n' "$id"
      ;;
    *'"method":"session/prompt"'*)
      while [ ! -f "$HERMES_TEST_GATE" ]; do sleep 0.01; done
      printf '{"jsonrpc":"2.0","id":%%s,"result":{"stopReason":"end_turn"}}\n' "$id"
      exit 0
      ;;
  esac
done
`, tt.effectiveID)
			writeTestExecutable(t, fakePath, []byte(script))

			backend, err := New("runtime-j", Config{
				ExecutablePath: fakePath,
				Logger:         slog.Default(),
				Env:            map[string]string{"HERMES_TEST_GATE": gatePath},
			})
			if err != nil {
				t.Fatalf("new hermes backend: %v", err)
			}

			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			session, err := backend.Execute(ctx, "prompt", ExecOptions{
				ResumeSessionID: tt.resumeSessionID,
				Timeout:         10 * time.Second,
			})
			if err != nil {
				t.Fatalf("execute: %v", err)
			}

			select {
			case сообщение, ok := <-session.Messages:
				if !ok {
					t.Fatal("message channel closed before session status")
				}
				if сообщение.Type != MessageStatus || сообщение.Status != "running" || сообщение.SessionID != tt.effectiveID {
					t.Fatalf("session status = %+v, want running with session ID %q", сообщение, tt.effectiveID)
				}
			case result := <-session.Result:
				t.Fatalf("terminal result arrived before session status: %+v", result)
			case <-time.After(5 * time.Second):
				t.Fatal("timeout waiting for mid-flight session status")
			}

			if err := os.WriteFile(gatePath, []byte("release"), 0o600); err != nil {
				t.Fatalf("release prompt: %v", err)
			}
			select {
			case result := <-session.Result:
				if result.Status != "completed" || result.SessionID != tt.effectiveID {
					t.Fatalf("result = %+v, want completed with session ID %q", result, tt.effectiveID)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("timeout waiting for terminal result")
			}
		})
	}
}

func TestRuntimeJResumeSessionLost(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		resumeID   string
		stopReason string
		activity   int64
		want       bool
	}{
		{name: "resumed refusal with no activity", resumeID: "ses_dead", stopReason: "refusal", want: true},
		{name: "resumed refusal after activity", resumeID: "ses_dead", stopReason: "refusal", activity: 3},
		{name: "fresh session refusal", stopReason: "refusal"},
		{name: "resumed end_turn", resumeID: "ses_ok", stopReason: "end_turn"},
		{name: "resumed cancelled", resumeID: "ses_ok", stopReason: "cancelled"},
		{name: "resumed empty stop reason", resumeID: "ses_ok"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := runtimeJResumeSessionLost(tc.resumeID, tc.stopReason, tc.activity); got != tc.want {
				t.Errorf("hermesResumeSessionLost(%q, %q, %d) = %v, want %v",
					tc.resumeID, tc.stopReason, tc.activity, got, tc.want)
			}
		})
	}
}

func fakeRuntimeJACPRefusedResumeScript() string {
	return `#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{"loadSession":true}}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"ses_fresh"}}\n' "$id"
      ;;
    *'"method":"session/resume"'*)
      if [ "$ACP_PROVIDER_ERR" = "1" ]; then
        echo '2026-07-30 13:46:51 [WARNING] acp_adapter.session: Failed to recreate agent for ACP session ses_dead' >&2
        echo 'RuntimeError: No LLM provider configured. Run hermes model to select a provider.' >&2
      fi
      printf '{"jsonrpc":"2.0","id":%s,"result":{"_meta":{},"models":null,"modes":null}}\n' "$id"
      ;;
    *'"method":"session/prompt"'*)
      sid=$(printf '%s' "$line" | sed -n 's/.*"sessionId":"\([^"]*\)".*/\1/p')
      if [ "$ACP_ACTIVITY" = "1" ]; then
        printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"%s","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"I will not do that."}}}}\n' "$sid"
      fi
      if [ "$ACP_PROVIDER_ERR" = "1" ]; then
        echo '2026-07-30 13:46:51 [ERROR] acp_adapter.server: prompt: session ses_dead not found' >&2
      fi
      printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"refusal"}}\n' "$id"
      if [ "$ACP_LATE_ACTIVITY" = "1" ]; then
        sleep 0.6
        printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"%s","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"Sorry, I will not do that."}}}}\n' "$sid"
      fi
      exit 0
      ;;
  esac
done
`
}

func runRuntimeJRefusedResume(t *testing.T, opts ExecOptions, env map[string]string) Result {
	t.Helper()

	fakePath := filepath.Join(t.TempDir(), "hermes")
	writeTestExecutable(t, fakePath, []byte(fakeRuntimeJACPRefusedResumeScript()))

	backend, err := New("runtime-j", Config{ExecutablePath: fakePath, Logger: slog.Default(), Env: env})
	if err != nil {
		t.Fatalf("new hermes backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if opts.Timeout == 0 {
		opts.Timeout = 20 * time.Second
	}
	session, err := backend.Execute(ctx, "continue", opts)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()

	select {
	case result, ok := <-session.Result:
		if !ok {
			t.Fatal("result channel closed without a value")
		}
		return result
	case <-time.After(30 * time.Second):
		t.Fatal("timeout waiting for result")
	}
	return Result{}
}

func TestRuntimeJBackendRecoversFromRefusedResume(t *testing.T) {
	t.Parallel()

	result := runRuntimeJRefusedResume(t, ExecOptions{ResumeSessionID: "ses_dead"}, nil)

	if result.Status != "failed" {
		t.Errorf("status = %q, want failed (a turn that ran nothing must not report success)", result.Status)
	}
	if result.Error != runtimeJResumeLostError {
		t.Errorf("error = %q, want %q", result.Error, runtimeJResumeLostError)
	}
	if result.SessionID != "" {
		t.Errorf("session id = %q, want empty so the daemon retries fresh", result.SessionID)
	}
	if !result.ResumeRejected {
		t.Error("ResumeRejected = false, want true so shouldRetryWithFreshSession fires")
	}
}

func TestRuntimeJBackendKeepsProviderErrorOnRefusedResume(t *testing.T) {
	t.Parallel()

	result := runRuntimeJRefusedResume(t,
		ExecOptions{ResumeSessionID: "ses_dead"},
		map[string]string{"ACP_PROVIDER_ERR": "1"},
	)

	if result.Status != "failed" {
		t.Errorf("status = %q, want failed", result.Status)
	}
	if !strings.Contains(result.Error, "No LLM provider configured") {
		t.Errorf("error = %q, want the captured provider error rather than the generic fallback", result.Error)
	}
	if result.SessionID != "" {
		t.Errorf("session id = %q, want empty so the daemon retries fresh", result.SessionID)
	}
	if !result.ResumeRejected {
		t.Error("ResumeRejected = false, want true so shouldRetryWithFreshSession fires")
	}
}

func TestRuntimeJBackendKeepsSessionOnRefusalAfterActivity(t *testing.T) {
	t.Parallel()

	result := runRuntimeJRefusedResume(t,
		ExecOptions{ResumeSessionID: "ses_dead"},
		map[string]string{"ACP_ACTIVITY": "1"},
	)

	if result.SessionID != "ses_dead" {
		t.Errorf("session id = %q, want the resumed id preserved", result.SessionID)
	}
	if result.ResumeRejected {
		t.Error("ResumeRejected = true, want false — the agent ran, so a fresh session is not the cure")
	}
}

func TestRuntimeJBackendKeepsSessionOnRefusalWithLateActivity(t *testing.T) {
	t.Parallel()

	result := runRuntimeJRefusedResume(t,
		ExecOptions{ResumeSessionID: "ses_dead"},
		map[string]string{"ACP_LATE_ACTIVITY": "1"},
	)

	if result.Output == "" {
		t.Fatal("late chunk never reached the result; the fixture is not exercising the drain gap")
	}
	if result.SessionID != "ses_dead" {
		t.Errorf("session id = %q, want the resumed id preserved", result.SessionID)
	}
	if result.ResumeRejected {
		t.Error("ResumeRejected = true, want false — the agent answered, only late")
	}
}

func TestRuntimeJBackendIgnoresRefusalOnFreshSession(t *testing.T) {
	t.Parallel()

	result := runRuntimeJRefusedResume(t, ExecOptions{}, nil)

	if result.SessionID != "ses_fresh" {
		t.Errorf("session id = %q, want ses_fresh", result.SessionID)
	}
	if result.ResumeRejected {
		t.Error("ResumeRejected = true, want false on a fresh session")
	}
}
