package agent

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBuildRuntimeMMCPConfigContent_Empty(t *testing.T) {
	t.Parallel()
	for _, raw := range []json.RawMessage{nil, json.RawMessage(""), json.RawMessage("null")} {
		got, err := buildRuntimeMMCPConfigContent(raw)
		if err != nil {
			t.Fatalf("err for %q: %v", string(raw), err)
		}
		if got != "" {
			t.Fatalf("expected empty content for %q, got %q", string(raw), got)
		}
	}
}

func TestBuildRuntimeMMCPConfigContent_Remote(t *testing.T) {
	t.Parallel()
	raw := json.RawMessage(`{
	  "mcpServers": {
	    "mcpbase": {
	      "url": "https://mcpbase.example/adanman/mcp",
	      "headers": {"Authorization": "Bearer test-token"}
	    }
	  }
	}`)
	content, err := buildRuntimeMMCPConfigContent(raw)
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	cfg := parseJSONString(t, content)
	mcpbase := cfg["mcp"].(map[string]any)["mcpbase"].(map[string]any)
	if got := mcpbase["type"]; got != "remote" {
		t.Fatalf("type = %v, want remote", got)
	}
	if got := mcpbase["url"]; got != "https://mcpbase.example/adanman/mcp" {
		t.Fatalf("url = %v", got)
	}
	if _, present := mcpbase["enabled"]; present {
		t.Fatalf("enabled should not be injected when not in source, got %v", mcpbase["enabled"])
	}
	if got := mcpbase["headers"].(map[string]any)["Authorization"]; got != "Bearer test-token" {
		t.Fatalf("Authorization header = %v", got)
	}
}

func TestBuildRuntimeMMCPConfigContent_Local(t *testing.T) {
	t.Parallel()
	raw := json.RawMessage(`{"mcpServers":{"local":{"command":"node","args":["server.js"],"env":{"TOKEN":"x"}}}}`)
	content, err := buildRuntimeMMCPConfigContent(raw)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	cfg := parseJSONString(t, content)
	local := cfg["mcp"].(map[string]any)["local"].(map[string]any)
	if got := local["type"]; got != "local" {
		t.Fatalf("type = %v, want local", got)
	}
	command, ok := local["command"].([]any)
	if !ok || len(command) != 2 || command[0] != "node" || command[1] != "server.js" {
		t.Fatalf("command = %#v, want [node server.js]", local["command"])
	}
	env, ok := local["environment"].(map[string]any)
	if !ok || env["TOKEN"] != "x" {
		t.Fatalf("environment = %#v, want {TOKEN:x}", local["environment"])
	}
	if _, present := local["env"]; present {
		t.Fatal("legacy `env` key should have been renamed to `environment`")
	}
}

func TestBuildRuntimeMMCPConfigContent_Native(t *testing.T) {
	t.Parallel()
	raw := json.RawMessage(`{
	  "mcp": {
	    "native": {
	      "type": "remote",
	      "url": "https://native.example/mcp",
	      "enabled": false
	    }
	  }
	}`)
	content, err := buildRuntimeMMCPConfigContent(raw)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	cfg := parseJSONString(t, content)
	native := cfg["mcp"].(map[string]any)["native"].(map[string]any)
	if got := native["enabled"]; got != false {
		t.Fatalf("enabled = %v, want false", got)
	}
	if got := native["url"]; got != "https://native.example/mcp" {
		t.Fatalf("url = %v", got)
	}
}

func TestBuildRuntimeMMCPConfigContent_NativeAcceptsAllSchemaFields(t *testing.T) {
	t.Parallel()

	t.Run("local with all optional fields", func(t *testing.T) {
		t.Parallel()
		raw := json.RawMessage(`{"mcp":{"x":{
			"type":"local",
			"command":["python","-m","my_mcp"],
			"environment":{"API_KEY":"secret","REGION":"us"},
			"enabled":true,
			"timeout":30000
		}}}`)
		content, err := buildRuntimeMMCPConfigContent(raw)
		if err != nil {
			t.Fatalf("build: %v", err)
		}
		x := parseJSONString(t, content)["mcp"].(map[string]any)["x"].(map[string]any)
		if x["type"] != "local" || x["timeout"].(float64) != 30000 || x["enabled"].(bool) != true {
			t.Fatalf("local fields lost in round-trip: %#v", x)
		}
		cmd := x["command"].([]any)
		if len(cmd) != 3 || cmd[0] != "python" {
			t.Fatalf("command lost: %#v", cmd)
		}
		env := x["environment"].(map[string]any)
		if env["API_KEY"] != "secret" {
			t.Fatalf("environment lost: %#v", env)
		}
	})

	t.Run("remote with oauth object", func(t *testing.T) {
		t.Parallel()
		raw := json.RawMessage(`{"mcp":{"x":{
			"type":"remote",
			"url":"https://e.example/mcp",
			"headers":{"Authorization":"Bearer T","X-Trace":"abc"},
			"oauth":{"clientId":"cid","scope":"read","callbackPort":3000},
			"enabled":true,
			"timeout":5000
		}}}`)
		content, err := buildRuntimeMMCPConfigContent(raw)
		if err != nil {
			t.Fatalf("build: %v", err)
		}
		x := parseJSONString(t, content)["mcp"].(map[string]any)["x"].(map[string]any)
		oauth := x["oauth"].(map[string]any)
		if oauth["clientId"] != "cid" || oauth["callbackPort"].(float64) != 3000 {
			t.Fatalf("oauth fields lost: %#v", oauth)
		}
	})

	t.Run("remote with oauth false", func(t *testing.T) {
		t.Parallel()
		raw := json.RawMessage(`{"mcp":{"x":{"type":"remote","url":"https://e/","oauth":false}}}`)
		content, err := buildRuntimeMMCPConfigContent(raw)
		if err != nil {
			t.Fatalf("build: %v", err)
		}
		x := parseJSONString(t, content)["mcp"].(map[string]any)["x"].(map[string]any)

		if v, ok := x["oauth"].(bool); !ok || v {
			t.Fatalf("oauth literal `false` not preserved: %#v", x["oauth"])
		}
	})

	t.Run("bare enabled override", func(t *testing.T) {
		t.Parallel()

		raw := json.RawMessage(`{"mcp":{"inherited":{"enabled":false}}}`)
		content, err := buildRuntimeMMCPConfigContent(raw)
		if err != nil {
			t.Fatalf("build: %v", err)
		}
		x := parseJSONString(t, content)["mcp"].(map[string]any)["inherited"].(map[string]any)
		if v, ok := x["enabled"].(bool); !ok || v {
			t.Fatalf("override `enabled:false` lost: %#v", x)
		}
		if _, hasType := x["type"]; hasType {
			t.Fatalf("override should not have a type field: %#v", x)
		}
	})

	t.Run("bare enabled override rejects extra fields", func(t *testing.T) {
		t.Parallel()

		raw := json.RawMessage(`{"mcp":{"x":{"enabled":true,"foo":"bar"}}}`)
		_, err := buildRuntimeMMCPConfigContent(raw)
		if err == nil {
			t.Fatal("expected validation failure for extra fields without type")
		}
		if !strings.Contains(err.Error(), "missing required field `type`") {
			t.Fatalf("expected friendly missing-type error, got %q", err.Error())
		}
	})
}

func TestBuildRuntimeMMCPConfigContent_RejectsMalformedNative(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		raw  string
		want string
	}{

		{"missing type", `{"mcp":{"x":{"url":"https://e.example/mcp"}}}`, "missing required field `type`"},
		{"invalid type", `{"mcp":{"x":{"type":"bogus","url":"https://e.example/mcp"}}}`, "invalid type"},
		{"remote missing url", `{"mcp":{"x":{"type":"remote"}}}`, "remote server missing required field `url`"},
		{"local missing command", `{"mcp":{"x":{"type":"local"}}}`, "local server missing required field `command`"},
		{"entry not an object (string)", `{"mcp":{"x":"not-an-object"}}`, "entry must be a JSON object"},
		{"entry not an object (number)", `{"mcp":{"x":42}}`, "entry must be a JSON object"},
		{"entry not an object (array)", `{"mcp":{"x":["a","b"]}}`, "entry must be a JSON object"},
		{"entry not an object (null)", `{"mcp":{"x":null}}`, "entry must be a JSON object"},
		{"type field is not a string", `{"mcp":{"x":{"type":42}}}`, "`type` must be a string"},

		{"local command is string", `{"mcp":{"x":{"type":"local","command":"node"}}}`, "json: cannot unmarshal string"},
		{"local command has non-string element", `{"mcp":{"x":{"type":"local","command":["node",5]}}}`, "json: cannot unmarshal number"},
		{"local command is object", `{"mcp":{"x":{"type":"local","command":{"foo":"bar"}}}}`, "json: cannot unmarshal object"},

		{"local env value is number", `{"mcp":{"x":{"type":"local","command":["node"],"environment":{"PORT":3000}}}}`, "json: cannot unmarshal number"},
		{"local env value is array", `{"mcp":{"x":{"type":"local","command":["node"],"environment":{"FOO":["a"]}}}}`, "json: cannot unmarshal array"},
		{"remote header value is number", `{"mcp":{"x":{"type":"remote","url":"https://e/","headers":{"X-Limit":10}}}}`, "json: cannot unmarshal number"},
		{"remote header value is bool", `{"mcp":{"x":{"type":"remote","url":"https://e/","headers":{"X-Auth":true}}}}`, "json: cannot unmarshal bool"},

		{"oauth is true", `{"mcp":{"x":{"type":"remote","url":"https://e/","oauth":true}}}`, "must be an object or `false`"},
		{"oauth is string", `{"mcp":{"x":{"type":"remote","url":"https://e/","oauth":"yes"}}}`, "must be an object or `false`"},
		{"oauth is number", `{"mcp":{"x":{"type":"remote","url":"https://e/","oauth":1}}}`, "must be an object or `false`"},
		{"oauth has unknown field", `{"mcp":{"x":{"type":"remote","url":"https://e/","oauth":{"foo":"bar"}}}}`, `json: unknown field "foo"`},
		{"oauth callbackPort out of range (high)", `{"mcp":{"x":{"type":"remote","url":"https://e/","oauth":{"callbackPort":70000}}}}`, "`callbackPort` must be in 1..65535"},
		{"oauth callbackPort out of range (negative)", `{"mcp":{"x":{"type":"remote","url":"https://e/","oauth":{"callbackPort":-1}}}}`, "`callbackPort` must be in 1..65535"},

		{"oauth callbackPort explicit zero", `{"mcp":{"x":{"type":"remote","url":"https://e/","oauth":{"callbackPort":0}}}}`, "`callbackPort` must be in 1..65535"},

		{"timeout zero", `{"mcp":{"x":{"type":"local","command":["node"],"timeout":0}}}`, "`timeout` must be a positive integer"},
		{"timeout negative", `{"mcp":{"x":{"type":"remote","url":"https://e/","timeout":-1}}}`, "`timeout` must be a positive integer"},
		{"timeout fractional", `{"mcp":{"x":{"type":"local","command":["node"],"timeout":60.5}}}`, "json: cannot unmarshal number"},
		{"timeout string", `{"mcp":{"x":{"type":"remote","url":"https://e/","timeout":"60"}}}`, "json: cannot unmarshal string"},

		{"local has unknown field", `{"mcp":{"x":{"type":"local","command":["node"],"unknown":"x"}}}`, `json: unknown field "unknown"`},
		{"local has remote-only field", `{"mcp":{"x":{"type":"local","command":["node"],"url":"https://e/"}}}`, `json: unknown field "url"`},
		{"remote has local-only field", `{"mcp":{"x":{"type":"remote","url":"https://e/","command":["node"]}}}`, `json: unknown field "command"`},
		{"remote has unknown field", `{"mcp":{"x":{"type":"remote","url":"https://e/","extra":1}}}`, `json: unknown field "extra"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			content, err := buildRuntimeMMCPConfigContent(json.RawMessage(tc.raw))
			if err == nil {
				t.Fatalf("expected error containing %q, got nil (content=%q)", tc.want, content)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected error containing %q, got %q", tc.want, err.Error())
			}
			if content != "" {
				t.Fatalf("content should be empty on validation failure, got %q", content)
			}
		})
	}
}

func TestBuildRuntimeMMCPConfigContent_RuntimeCStyleOAuthRoundTrip(t *testing.T) {
	t.Parallel()
	raw := json.RawMessage(`{"mcpServers":{"x":{
		"url":"https://oauth.example/mcp",
		"headers":{"Authorization":"Bearer T"},
		"oauth":{"clientId":"cid","clientSecret":"sec","scope":"read write","callbackPort":3000,"redirectUri":"https://example/cb"},
		"timeout":5000
	}}}`)
	content, err := buildRuntimeMMCPConfigContent(raw)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	x := parseJSONString(t, content)["mcp"].(map[string]any)["x"].(map[string]any)
	if x["type"] != "remote" {
		t.Fatalf("type = %v, want remote", x["type"])
	}
	if x["timeout"].(float64) != 5000 {
		t.Fatalf("timeout = %v, want 5000", x["timeout"])
	}
	oauth, ok := x["oauth"].(map[string]any)
	if !ok {
		t.Fatalf("oauth not preserved as object: %#v", x["oauth"])
	}
	if oauth["clientId"] != "cid" || oauth["clientSecret"] != "sec" || oauth["scope"] != "read write" {
		t.Fatalf("oauth string fields lost: %#v", oauth)
	}
	if oauth["callbackPort"].(float64) != 3000 {
		t.Fatalf("oauth.callbackPort = %v, want 3000", oauth["callbackPort"])
	}
	if oauth["redirectUri"] != "https://example/cb" {
		t.Fatalf("oauth.redirectUri = %v", oauth["redirectUri"])
	}
}

func TestBuildRuntimeMMCPConfigContent_RejectsMalformedRuntimeCStyle(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		raw  string
		want string
	}{

		{"remote header value is number", `{"mcpServers":{"x":{"url":"https://e/","headers":{"Authorization":123}}}}`, "json: cannot unmarshal number"},
		{"remote header value is bool", `{"mcpServers":{"x":{"url":"https://e/","headers":{"X-Auth":true}}}}`, "json: cannot unmarshal bool"},

		{"local env value is number", `{"mcpServers":{"x":{"command":"node","env":{"FOO":42}}}}`, "json: cannot unmarshal number"},
		{"local env value is array", `{"mcpServers":{"x":{"command":"node","env":{"FOO":["a"]}}}}`, "json: cannot unmarshal array"},

		{"oauth is true", `{"mcpServers":{"x":{"url":"https://e/","oauth":true}}}`, "must be an object or `false`"},

		{"timeout negative", `{"mcpServers":{"x":{"command":"node","timeout":-1}}}`, "`timeout` must be a positive integer"},
		{"timeout zero", `{"mcpServers":{"x":{"command":"node","timeout":0}}}`, "`timeout` must be a positive integer"},

		{"oauth callbackPort explicit zero (claude-style)", `{"mcpServers":{"x":{"url":"https://e/","oauth":{"callbackPort":0}}}}`, "`callbackPort` must be in 1..65535"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			content, err := buildRuntimeMMCPConfigContent(json.RawMessage(tc.raw))
			if err == nil {
				t.Fatalf("expected error containing %q, got nil (content=%q)", tc.want, content)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected error containing %q, got %q", tc.want, err.Error())
			}
			if content != "" {
				t.Fatalf("content should be empty on validation failure, got %q", content)
			}
		})
	}
}

func TestRuntimeMBackendInjectsMCPConfigViaEnv(t *testing.T) {
	t.Parallel()
	tempDir := t.TempDir()
	fakePath := filepath.Join(tempDir, "opencode")
	captureFile := filepath.Join(tempDir, "env-capture.txt")
	writeTestExecutable(t, fakePath, []byte(fakeRuntimeMScriptCapturingEnv()))

	workDir := t.TempDir()
	backend, err := New("runtime-m", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Env: map[string]string{
			"OPENCODE_CAPTURE_FILE": captureFile,
		},
	})
	if err != nil {
		t.Fatalf("new backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{
		Cwd:       workDir,
		Timeout:   5 * time.Second,
		McpConfig: json.RawMessage(`{"mcpServers":{"mcpbase":{"url":"https://mcpbase.example/mcp"}}}`),
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	result := <-session.Result
	if result.Status != "completed" {
		t.Fatalf("status = %q, error = %q; want completed", result.Status, result.Error)
	}

	captured := readCapturedEnv(t, captureFile)
	got := captured["OPENCODE_CONFIG_CONTENT"]
	if !strings.Contains(got, "https://mcpbase.example/mcp") {
		t.Fatalf("OPENCODE_CONFIG_CONTENT did not include managed url:\n%s", got)
	}
	if !strings.Contains(got, `"type":"remote"`) {
		t.Fatalf("OPENCODE_CONFIG_CONTENT missing translated type=remote:\n%s", got)
	}

	if _, statErr := os.Stat(filepath.Join(workDir, "opencode.json")); !os.IsNotExist(statErr) {
		body, _ := os.ReadFile(filepath.Join(workDir, "opencode.json"))
		t.Fatalf("daemon must not write <workdir>/opencode.json; found:\n%s", string(body))
	}
}

func TestRuntimeMBackendOmitsMCPEnvWhenEmpty(t *testing.T) {
	t.Parallel()
	tempDir := t.TempDir()
	fakePath := filepath.Join(tempDir, "opencode")
	captureFile := filepath.Join(tempDir, "env-capture.txt")
	writeTestExecutable(t, fakePath, []byte(fakeRuntimeMScriptCapturingEnv()))

	const userContent = `{"mcp":{"user_only":{"type":"remote","url":"https://user.example/mcp"}}}`
	workDir := t.TempDir()
	backend, err := New("runtime-m", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Env: map[string]string{
			"OPENCODE_CAPTURE_FILE":   captureFile,
			"OPENCODE_CONFIG_CONTENT": userContent,
		},
	})
	if err != nil {
		t.Fatalf("new backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{
		Cwd:       workDir,
		Timeout:   5 * time.Second,
		McpConfig: nil,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	if r := <-session.Result; r.Status != "completed" {
		t.Fatalf("status = %q, error = %q; want completed", r.Status, r.Error)
	}

	captured := readCapturedEnv(t, captureFile)
	if got := captured["OPENCODE_CONFIG_CONTENT"]; got != userContent {
		t.Fatalf("user OPENCODE_CONFIG_CONTENT was not preserved:\n  want %q\n  got  %q", userContent, got)
	}
}

func TestRuntimeMBackendOverridesUserRuntimeMConfigContent(t *testing.T) {
	t.Parallel()
	tempDir := t.TempDir()
	fakePath := filepath.Join(tempDir, "opencode")
	captureFile := filepath.Join(tempDir, "env-capture.txt")
	writeTestExecutable(t, fakePath, []byte(fakeRuntimeMScriptCapturingEnv()))

	const userBogus = `{"this-should-not-survive":true}`
	workDir := t.TempDir()
	backend, err := New("runtime-m", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Env: map[string]string{
			"OPENCODE_CAPTURE_FILE":   captureFile,
			"OPENCODE_CONFIG_CONTENT": userBogus,
		},
	})
	if err != nil {
		t.Fatalf("new backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{
		Cwd:       workDir,
		Timeout:   5 * time.Second,
		McpConfig: json.RawMessage(`{"mcpServers":{"daemon":{"url":"https://daemon.example/mcp"}}}`),
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	if r := <-session.Result; r.Status != "completed" {
		t.Fatalf("status = %q, error = %q; want completed", r.Status, r.Error)
	}

	captured := readCapturedEnv(t, captureFile)
	got := captured["OPENCODE_CONFIG_CONTENT"]
	if strings.Contains(got, "this-should-not-survive") {
		t.Fatalf("user-set OPENCODE_CONFIG_CONTENT survived dedup; daemon mcp_config did not win:\n%s", got)
	}
	if !strings.Contains(got, "https://daemon.example/mcp") {
		t.Fatalf("daemon mcp_config did not reach the child process:\n%s", got)
	}
}

func fakeRuntimeMScriptCapturingEnv() string {
	return `#!/bin/sh
if [ -n "$OPENCODE_CAPTURE_FILE" ]; then
  {
    printf 'OPENCODE_CONFIG_CONTENT=%s\n' "${OPENCODE_CONFIG_CONTENT-<unset>}"
  } > "$OPENCODE_CAPTURE_FILE"
fi
printf '{"type":"step_start","timestamp":1,"sessionID":"ses_fake","part":{"type":"step-start"}}\n'
printf '{"type":"text","timestamp":2,"sessionID":"ses_fake","part":{"type":"text","text":"ok"}}\n'
printf '{"type":"step_finish","timestamp":3,"sessionID":"ses_fake","part":{"type":"step-finish"}}\n'
`
}

func readCapturedEnv(t *testing.T, path string) map[string]string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read capture %s: %v", path, err)
	}
	out := make(map[string]string)
	for _, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		if v == "<unset>" {
			continue
		}
		out[k] = v
	}
	return out
}

func parseJSONString(t *testing.T, s string) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		t.Fatalf("parse content: %v\n%s", err, s)
	}
	return out
}
