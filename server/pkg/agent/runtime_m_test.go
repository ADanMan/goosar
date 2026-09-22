package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNewReturnsRuntimeMBackend(t *testing.T) {
	t.Parallel()
	b, err := New("runtime-m", Config{ExecutablePath: "/nonexistent/opencode"})
	if err != nil {
		t.Fatalf("New(opencode) error: %v", err)
	}
	if _, ok := b.(*runtimeMBackend); !ok {
		t.Fatalf("expected *opencodeBackend, got %T", b)
	}
}

func TestRuntimeMHandleTextEvent(t *testing.T) {
	t.Parallel()

	b := &runtimeMBackend{}
	ch := make(chan Message, 10)
	var output strings.Builder

	event := runtimeMEvent{
		Type:      "text",
		SessionID: "ses_abc",
		Part: runtimeMEventPart{
			Type: "text",
			Text: "Hello from opencode",
		},
	}

	b.handleTextEvent(event, ch, &output)

	if output.String() != "Hello from opencode" {
		t.Errorf("output: got %q, want %q", output.String(), "Hello from opencode")
	}
	сообщение := <-ch
	if сообщение.Type != MessageText {
		t.Errorf("type: got %v, want MessageText", сообщение.Type)
	}
	if сообщение.Content != "Hello from opencode" {
		t.Errorf("content: got %q, want %q", сообщение.Content, "Hello from opencode")
	}
}

func TestRuntimeMHandleTextEventEmpty(t *testing.T) {
	t.Parallel()

	b := &runtimeMBackend{}
	ch := make(chan Message, 10)
	var output strings.Builder

	event := runtimeMEvent{
		Type: "text",
		Part: runtimeMEventPart{Type: "text", Text: ""},
	}

	b.handleTextEvent(event, ch, &output)

	if output.String() != "" {
		t.Errorf("expected empty output, got %q", output.String())
	}
	if len(ch) != 0 {
		t.Errorf("expected no messages, got %d", len(ch))
	}
}

func TestRuntimeMHandleToolUseEventCompleted(t *testing.T) {
	t.Parallel()

	b := &runtimeMBackend{}
	ch := make(chan Message, 10)

	event := runtimeMEvent{
		Type: "tool_use",
		Part: runtimeMEventPart{
			Tool:   "bash",
			CallID: "call_BHA1",
			State: &runtimeMToolState{
				Status: "completed",
				Input:  json.RawMessage(`{"command":"pwd","description":"Prints current working directory path"}`),
				Output: "/tmp/goosar\n",
			},
		},
	}

	b.handleToolUseEvent(event, ch)

	if len(ch) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(ch))
	}

	сообщение := <-ch
	if сообщение.Type != MessageToolUse {
		t.Errorf("type: got %v, want MessageToolUse", сообщение.Type)
	}
	if сообщение.Tool != "bash" {
		t.Errorf("tool: got %q, want %q", сообщение.Tool, "bash")
	}
	if сообщение.CallID != "call_BHA1" {
		t.Errorf("callID: got %q, want %q", сообщение.CallID, "call_BHA1")
	}
	if cmd, ok := сообщение.Input["command"].(string); !ok || cmd != "pwd" {
		t.Errorf("input.command: got %v", сообщение.Input["command"])
	}

	сообщение = <-ch
	if сообщение.Type != MessageToolResult {
		t.Errorf("type: got %v, want MessageToolResult", сообщение.Type)
	}
	if сообщение.CallID != "call_BHA1" {
		t.Errorf("callID: got %q, want %q", сообщение.CallID, "call_BHA1")
	}
	if сообщение.Output != "/tmp/goosar\n" {
		t.Errorf("output: got %q", сообщение.Output)
	}
}

func TestRuntimeMHandleToolUseEventPending(t *testing.T) {
	t.Parallel()

	b := &runtimeMBackend{}
	ch := make(chan Message, 10)

	event := runtimeMEvent{
		Type: "tool_use",
		Part: runtimeMEventPart{
			Tool:   "read",
			CallID: "call_ABC",
			State: &runtimeMToolState{
				Status: "pending",
				Input:  json.RawMessage(`{"filePath":"/tmp/test.go"}`),
			},
		},
	}

	b.handleToolUseEvent(event, ch)

	if len(ch) != 1 {
		t.Fatalf("expected 1 message for pending tool, got %d", len(ch))
	}
	сообщение := <-ch
	if сообщение.Type != MessageToolUse {
		t.Errorf("type: got %v, want MessageToolUse", сообщение.Type)
	}
	if сообщение.Tool != "read" {
		t.Errorf("tool: got %q, want %q", сообщение.Tool, "read")
	}
}

func TestRuntimeMHandleToolUseEventStructuredOutput(t *testing.T) {
	t.Parallel()

	b := &runtimeMBackend{}
	ch := make(chan Message, 10)

	event := runtimeMEvent{
		Type: "tool_use",
		Part: runtimeMEventPart{
			Tool:   "glob",
			CallID: "call_XYZ",
			State: &runtimeMToolState{
				Status: "completed",
				Input:  json.RawMessage(`{"pattern":"*.go"}`),
				Output: map[string]any{"files": []any{"main.go", "main_test.go"}},
			},
		},
	}

	b.handleToolUseEvent(event, ch)

	if len(ch) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(ch))
	}
	<-ch
	сообщение := <-ch
	if сообщение.Type != MessageToolResult {
		t.Errorf("type: got %v, want MessageToolResult", сообщение.Type)
	}
	if !strings.Contains(сообщение.Output, "main.go") {
		t.Errorf("output should contain 'main.go', got %q", сообщение.Output)
	}
}

func TestRuntimeMHandleToolUseEventNilState(t *testing.T) {
	t.Parallel()

	b := &runtimeMBackend{}
	ch := make(chan Message, 10)

	event := runtimeMEvent{
		Type: "tool_use",
		Part: runtimeMEventPart{
			Tool:   "write",
			CallID: "call_NUL",
		},
	}

	b.handleToolUseEvent(event, ch)

	if len(ch) != 1 {
		t.Fatalf("expected 1 message, got %d", len(ch))
	}
	сообщение := <-ch
	if сообщение.Type != MessageToolUse {
		t.Errorf("type: got %v, want MessageToolUse", сообщение.Type)
	}
}

func TestRuntimeMHandleErrorEvent(t *testing.T) {
	t.Parallel()

	b := &runtimeMBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 10)
	status := "completed"
	errMsg := ""

	event := runtimeMEvent{
		Type:      "error",
		SessionID: "ses_abc",
		Error: &runtimeMError{
			Name: "UnknownError",
			Data: &runtimeMErrData{
				Message: "Model not found: definitely/not-a-model.",
			},
		},
	}

	b.handleErrorEvent(event, ch, &status, &errMsg)

	if status != "failed" {
		t.Errorf("status: got %q, want %q", status, "failed")
	}
	if errMsg != "Model not found: definitely/not-a-model." {
		t.Errorf("error: got %q", errMsg)
	}
	сообщение := <-ch
	if сообщение.Type != MessageError {
		t.Errorf("type: got %v, want MessageError", сообщение.Type)
	}
	if сообщение.Content != "Model not found: definitely/not-a-model." {
		t.Errorf("content: got %q", сообщение.Content)
	}
}

func TestRuntimeMHandleErrorEventNameOnly(t *testing.T) {
	t.Parallel()

	b := &runtimeMBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 10)
	status := "completed"
	errMsg := ""

	event := runtimeMEvent{
		Type: "error",
		Error: &runtimeMError{
			Name: "RateLimitError",
		},
	}

	b.handleErrorEvent(event, ch, &status, &errMsg)

	if errMsg != "RateLimitError" {
		t.Errorf("error: got %q, want %q", errMsg, "RateLimitError")
	}
}

func TestRuntimeMHandleErrorEventNilError(t *testing.T) {
	t.Parallel()

	b := &runtimeMBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 10)
	status := "completed"
	errMsg := ""

	event := runtimeMEvent{Type: "error"}

	b.handleErrorEvent(event, ch, &status, &errMsg)

	if errMsg != "unknown opencode error" {
		t.Errorf("error: got %q, want %q", errMsg, "unknown opencode error")
	}
}

func TestRuntimeMEventParsingTextFixture(t *testing.T) {
	t.Parallel()

	line := `{"type":"text","timestamp":1775116675833,"sessionID":"ses_abc","part":{"id":"prt_123","messageID":"msg_456","sessionID":"ses_abc","type":"text","text":"pong","time":{"start":1775116675833,"end":1775116675833}}}`

	var event runtimeMEvent
	if err := json.Unmarshal([]byte(line), &event); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if event.Type != "text" {
		t.Errorf("type: got %q, want %q", event.Type, "text")
	}
	if event.SessionID != "ses_abc" {
		t.Errorf("sessionID: got %q, want %q", event.SessionID, "ses_abc")
	}
	if event.Part.Text != "pong" {
		t.Errorf("part.text: got %q, want %q", event.Part.Text, "pong")
	}
}

func TestRuntimeMEventParsingToolUseFixture(t *testing.T) {
	t.Parallel()

	line := `{"type":"tool_use","timestamp":1775117187163,"sessionID":"ses_abc","part":{"id":"prt_123","messageID":"msg_456","sessionID":"ses_abc","type":"tool","tool":"bash","callID":"call_BHA1","state":{"status":"completed","input":{"command":"pwd","description":"Prints current working directory path"},"output":"/tmp/goosar\n","metadata":{"exit":0},"time":{"start":1775117187092,"end":1775117187162}}}}`

	var event runtimeMEvent
	if err := json.Unmarshal([]byte(line), &event); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if event.Type != "tool_use" {
		t.Errorf("type: got %q, want %q", event.Type, "tool_use")
	}
	if event.Part.Tool != "bash" {
		t.Errorf("part.tool: got %q, want %q", event.Part.Tool, "bash")
	}
	if event.Part.CallID != "call_BHA1" {
		t.Errorf("part.callID: got %q, want %q", event.Part.CallID, "call_BHA1")
	}
	if event.Part.State == nil {
		t.Fatal("part.state is nil")
	}
	if event.Part.State.Status != "completed" {
		t.Errorf("state.status: got %q, want %q", event.Part.State.Status, "completed")
	}

	var input map[string]any
	if err := json.Unmarshal(event.Part.State.Input, &input); err != nil {
		t.Fatalf("unmarshal state.input: %v", err)
	}
	if input["command"] != "pwd" {
		t.Errorf("state.input.command: got %v, want %q", input["command"], "pwd")
	}

	if output, ok := event.Part.State.Output.(string); !ok || output != "/tmp/goosar\n" {
		t.Errorf("state.output: got %v (%T)", event.Part.State.Output, event.Part.State.Output)
	}
}

func TestRuntimeMEventParsingErrorFixture(t *testing.T) {
	t.Parallel()

	line := `{"type":"error","timestamp":1775117233612,"sessionID":"ses_abc","error":{"name":"UnknownError","data":{"message":"Model not found: definitely/not-a-model."}}}`

	var event runtimeMEvent
	if err := json.Unmarshal([]byte(line), &event); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if event.Type != "error" {
		t.Errorf("type: got %q, want %q", event.Type, "error")
	}
	if event.Error == nil {
		t.Fatal("error field is nil")
	}
	if event.Error.Name != "UnknownError" {
		t.Errorf("error.name: got %q", event.Error.Name)
	}
	if got := event.Error.Message(); got != "Model not found: definitely/not-a-model." {
		t.Errorf("error.Message(): got %q", got)
	}
}

func TestRuntimeMEventParsingStepStartFixture(t *testing.T) {
	t.Parallel()

	line := `{"type":"step_start","timestamp":1775116675819,"sessionID":"ses_abc","part":{"id":"prt_123","messageID":"msg_456","sessionID":"ses_abc","snapshot":"abc123","type":"step-start"}}`

	var event runtimeMEvent
	if err := json.Unmarshal([]byte(line), &event); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if event.Type != "step_start" {
		t.Errorf("type: got %q, want %q", event.Type, "step_start")
	}
	if event.SessionID != "ses_abc" {
		t.Errorf("sessionID: got %q", event.SessionID)
	}
}

func TestRuntimeMStepFinishParsing(t *testing.T) {
	t.Parallel()

	line := `{"type":"step_finish","timestamp":1775116676180,"sessionID":"ses_abc","part":{"id":"prt_789","reason":"stop","snapshot":"abc123","messageID":"msg_456","sessionID":"ses_abc","type":"step-finish","tokens":{"total":14674,"input":14585,"output":89,"reasoning":82,"cache":{"write":0,"read":0}},"cost":0}}`

	var event runtimeMEvent
	if err := json.Unmarshal([]byte(line), &event); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if event.Type != "step_finish" {
		t.Errorf("type: got %q, want %q", event.Type, "step_finish")
	}
	if event.SessionID != "ses_abc" {
		t.Errorf("sessionID: got %q", event.SessionID)
	}
}

func TestExtractToolOutputString(t *testing.T) {
	t.Parallel()
	if got := extractToolOutput("hello\n"); got != "hello\n" {
		t.Errorf("got %q, want %q", got, "hello\n")
	}
}

func TestExtractToolOutputNil(t *testing.T) {
	t.Parallel()
	if got := extractToolOutput(nil); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestExtractToolOutputStructured(t *testing.T) {
	t.Parallel()
	obj := map[string]any{"key": "value"}
	got := extractToolOutput(obj)
	if !strings.Contains(got, `"key"`) || !strings.Contains(got, `"value"`) {
		t.Errorf("got %q, expected JSON containing key/value", got)
	}
}

func TestRuntimeMErrorMessage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  *runtimeMError
		want string
	}{
		{
			name: "data message",
			err:  &runtimeMError{Name: "Err", Data: &runtimeMErrData{Message: "details"}},
			want: "details",
		},
		{
			name: "name only",
			err:  &runtimeMError{Name: "RateLimitError"},
			want: "RateLimitError",
		},
		{
			name: "empty",
			err:  &runtimeMError{},
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Message(); got != tt.want {
				t.Errorf("Message() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRuntimeMProcessEventsHappyPath(t *testing.T) {
	t.Parallel()

	b := &runtimeMBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	lines := strings.Join([]string{
		`{"type":"step_start","timestamp":1000,"sessionID":"ses_happy","part":{"type":"step-start"}}`,
		`{"type":"text","timestamp":1001,"sessionID":"ses_happy","part":{"type":"text","text":"Analyzing the issue..."}}`,
		`{"type":"tool_use","timestamp":1002,"sessionID":"ses_happy","part":{"tool":"bash","callID":"call_1","state":{"status":"completed","input":{"command":"ls"},"output":"file1.go\nfile2.go\n"}}}`,
		`{"type":"text","timestamp":1003,"sessionID":"ses_happy","part":{"type":"text","text":" Done."}}`,
		`{"type":"step_finish","timestamp":1004,"sessionID":"ses_happy","part":{"type":"step-finish"}}`,
	}, "\n")

	result := b.processEvents(strings.NewReader(lines), ch)

	if result.status != "completed" {
		t.Errorf("status: got %q, want %q", result.status, "completed")
	}
	if result.sessionID != "ses_happy" {
		t.Errorf("sessionID: got %q, want %q", result.sessionID, "ses_happy")
	}
	if result.output != "Analyzing the issue... Done." {
		t.Errorf("output: got %q, want %q", result.output, "Analyzing the issue... Done.")
	}
	if result.errMsg != "" {
		t.Errorf("errMsg: got %q, want empty", result.errMsg)
	}

	close(ch)
	var msgs []Message
	for m := range ch {
		msgs = append(msgs, m)
	}

	if len(msgs) != 5 {
		t.Fatalf("expected 5 messages, got %d: %+v", len(msgs), msgs)
	}
	if msgs[0].Type != MessageStatus || msgs[0].Status != "running" {
		t.Errorf("msg[0]: got %+v, want status=running", msgs[0])
	}
	if msgs[1].Type != MessageText || msgs[1].Content != "Analyzing the issue..." {
		t.Errorf("msg[1]: got %+v", msgs[1])
	}
	if msgs[2].Type != MessageToolUse || msgs[2].Tool != "bash" {
		t.Errorf("msg[2]: got %+v, want tool-use(bash)", msgs[2])
	}
	if msgs[3].Type != MessageToolResult || msgs[3].Output != "file1.go\nfile2.go\n" {
		t.Errorf("msg[3]: got %+v, want tool-result", msgs[3])
	}
	if msgs[4].Type != MessageText || msgs[4].Content != " Done." {
		t.Errorf("msg[4]: got %+v", msgs[4])
	}
}

func TestRuntimeMProcessEventsErrorCausesFailedStatus(t *testing.T) {
	t.Parallel()

	b := &runtimeMBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	lines := strings.Join([]string{
		`{"type":"step_start","timestamp":1000,"sessionID":"ses_err","part":{"type":"step-start"}}`,
		`{"type":"error","timestamp":1001,"sessionID":"ses_err","error":{"name":"UnknownError","data":{"message":"Model not found: bad/model"}}}`,
		`{"type":"step_finish","timestamp":1002,"sessionID":"ses_err","part":{"type":"step-finish"}}`,
	}, "\n")

	result := b.processEvents(strings.NewReader(lines), ch)

	if result.status != "failed" {
		t.Errorf("status: got %q, want %q", result.status, "failed")
	}
	if result.errMsg != "Model not found: bad/model" {
		t.Errorf("errMsg: got %q", result.errMsg)
	}
	if result.sessionID != "ses_err" {
		t.Errorf("sessionID: got %q, want %q", result.sessionID, "ses_err")
	}

	close(ch)
	var errorMsgs int
	for m := range ch {
		if m.Type == MessageError {
			errorMsgs++
		}
	}
	if errorMsgs != 1 {
		t.Errorf("expected 1 error message, got %d", errorMsgs)
	}
}

func TestRuntimeMProcessEventsSessionIDExtracted(t *testing.T) {
	t.Parallel()

	b := &runtimeMBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	lines := strings.Join([]string{
		`{"type":"step_start","timestamp":1000,"sessionID":"ses_first","part":{"type":"step-start"}}`,
		`{"type":"text","timestamp":1001,"sessionID":"ses_updated","part":{"type":"text","text":"hi"}}`,
	}, "\n")

	result := b.processEvents(strings.NewReader(lines), ch)

	if result.sessionID != "ses_updated" {
		t.Errorf("sessionID: got %q, want %q (should use last seen)", result.sessionID, "ses_updated")
	}

	close(ch)
}

func TestRuntimeMProcessEventsScannerError(t *testing.T) {
	t.Parallel()

	b := &runtimeMBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	result := b.processEvents(&ioErrReader{
		data: `{"type":"text","sessionID":"ses_scan","part":{"text":"before error"}}` + "\n",
	}, ch)

	if result.status != "failed" {
		t.Errorf("status: got %q, want %q", result.status, "failed")
	}
	if !strings.Contains(result.errMsg, "stdout read error") {
		t.Errorf("errMsg: got %q, want it to contain 'stdout read error'", result.errMsg)
	}

	if result.output != "before error" {
		t.Errorf("output: got %q, want %q", result.output, "before error")
	}

	close(ch)
}

type ioErrReader struct {
	data string
	read bool
}

func (r *ioErrReader) Read(p []byte) (int, error) {
	if !r.read {
		r.read = true
		n := copy(p, r.data)
		return n, nil
	}
	return 0, fmt.Errorf("simulated I/O error")
}

func TestRuntimeMProcessEventsEmptyLines(t *testing.T) {
	t.Parallel()

	b := &runtimeMBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	lines := strings.Join([]string{
		"",
		"   ",
		"not json at all",
		`{"type":"text","sessionID":"ses_ok","part":{"text":"valid"}}`,
		"",
	}, "\n")

	result := b.processEvents(strings.NewReader(lines), ch)

	if result.status != "completed" {
		t.Errorf("status: got %q, want %q", result.status, "completed")
	}
	if result.output != "valid" {
		t.Errorf("output: got %q, want %q", result.output, "valid")
	}
	if result.sessionID != "ses_ok" {
		t.Errorf("sessionID: got %q, want %q", result.sessionID, "ses_ok")
	}

	close(ch)
	var msgs []Message
	for m := range ch {
		msgs = append(msgs, m)
	}
	if len(msgs) != 1 || msgs[0].Type != MessageText {
		t.Errorf("expected 1 text message, got %d: %+v", len(msgs), msgs)
	}
}

func TestRuntimeMProcessEventsErrorDoesNotRevertToCompleted(t *testing.T) {
	t.Parallel()

	b := &runtimeMBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	lines := strings.Join([]string{
		`{"type":"error","sessionID":"ses_x","error":{"name":"RateLimitError"}}`,
		`{"type":"text","sessionID":"ses_x","part":{"text":"recovered?"}}`,
	}, "\n")

	result := b.processEvents(strings.NewReader(lines), ch)

	if result.status != "failed" {
		t.Errorf("status: got %q, want %q (error should stick)", result.status, "failed")
	}
	if result.errMsg != "RateLimitError" {
		t.Errorf("errMsg: got %q, want %q", result.errMsg, "RateLimitError")
	}

	close(ch)
}

func TestRuntimeMProcessEventsStreamEndsMidStep(t *testing.T) {
	t.Parallel()

	b := &runtimeMBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	lines := strings.Join([]string{
		`{"type":"step_start","timestamp":1000,"sessionID":"ses_midstep","part":{"type":"step-start"}}`,
		`{"type":"text","timestamp":1001,"sessionID":"ses_midstep","part":{"type":"text","text":"Let me plan the work..."}}`,
	}, "\n")

	result := b.processEvents(strings.NewReader(lines), ch)

	if result.status != "failed" {
		t.Errorf("status: got %q, want %q", result.status, "failed")
	}
	if !strings.Contains(result.errMsg, "terminal signal") {
		t.Errorf("errMsg: got %q, want it to mention the missing terminal signal", result.errMsg)
	}

	if result.output != "Let me plan the work..." {
		t.Errorf("output: got %q", result.output)
	}

	close(ch)
}

func TestRuntimeMProcessEventsStreamEndsAfterToolBeforeStepFinish(t *testing.T) {
	t.Parallel()

	b := &runtimeMBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	lines := strings.Join([]string{
		`{"type":"step_start","timestamp":1000,"sessionID":"ses_tool","part":{"type":"step-start"}}`,
		`{"type":"tool_use","timestamp":1001,"sessionID":"ses_tool","part":{"type":"tool","tool":"bash","callID":"call_1","state":{"status":"completed","input":{"command":"go test ./..."},"output":"ok\n"}}}`,
	}, "\n")

	result := b.processEvents(strings.NewReader(lines), ch)

	if result.status != "failed" {
		t.Errorf("status: got %q, want %q", result.status, "failed")
	}
	if !strings.Contains(result.errMsg, "terminal signal") {
		t.Errorf("errMsg: got %q, want it to mention the missing terminal signal", result.errMsg)
	}

	close(ch)
}

func TestRuntimeMProcessEventsMultiStepHappyPath(t *testing.T) {
	t.Parallel()

	b := &runtimeMBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	lines := strings.Join([]string{
		`{"type":"step_start","timestamp":1000,"sessionID":"ses_multi","part":{"type":"step-start"}}`,
		`{"type":"text","timestamp":1001,"sessionID":"ses_multi","part":{"type":"text","text":"step one"}}`,
		`{"type":"tool_use","timestamp":1002,"sessionID":"ses_multi","part":{"type":"tool","tool":"bash","callID":"call_1","state":{"status":"completed","input":{"command":"echo ok"},"output":"ok\n"}}}`,
		`{"type":"step_finish","timestamp":1003,"sessionID":"ses_multi","part":{"type":"step-finish","reason":"tool-calls"}}`,
		`{"type":"step_start","timestamp":1004,"sessionID":"ses_multi","part":{"type":"step-start"}}`,
		`{"type":"text","timestamp":1005,"sessionID":"ses_multi","part":{"type":"text","text":" step two"}}`,
		`{"type":"step_finish","timestamp":1006,"sessionID":"ses_multi","part":{"type":"step-finish","reason":"stop"}}`,
	}, "\n")

	result := b.processEvents(strings.NewReader(lines), ch)

	if result.status != "completed" {
		t.Errorf("status: got %q, want %q", result.status, "completed")
	}
	if result.output != "step one step two" {
		t.Errorf("output: got %q", result.output)
	}
	if result.errMsg != "" {
		t.Errorf("errMsg: got %q, want empty", result.errMsg)
	}

	close(ch)
}

func TestRuntimeMProcessEventsStreamEndsAfterToolCallsStepFinish(t *testing.T) {
	t.Parallel()

	b := &runtimeMBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	lines := strings.Join([]string{
		`{"type":"step_start","timestamp":1000,"sessionID":"ses_toolcalls","part":{"type":"step-start"}}`,
		`{"type":"tool_use","timestamp":1001,"sessionID":"ses_toolcalls","part":{"type":"tool","tool":"bash","callID":"call_1","state":{"status":"completed","input":{"command":"go test ./..."},"output":"ok\n"}}}`,
		`{"type":"step_finish","timestamp":1002,"sessionID":"ses_toolcalls","part":{"type":"step-finish","reason":"tool-calls"}}`,
	}, "\n")

	result := b.processEvents(strings.NewReader(lines), ch)

	if result.status != "failed" {
		t.Errorf("status: got %q, want %q", result.status, "failed")
	}
	if !strings.Contains(result.errMsg, "terminal signal") {
		t.Errorf("errMsg: got %q, want it to mention the missing terminal signal", result.errMsg)
	}
	if !result.noTerminalSignal {
		t.Error("noTerminalSignal: got false, want true")
	}

	close(ch)
}

func TestRuntimeMProcessEventsStreamEndsAfterToolWithStopFinish(t *testing.T) {
	t.Parallel()

	b := &runtimeMBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	lines := strings.Join([]string{
		`{"type":"step_start","timestamp":1000,"sessionID":"ses_stop_tool","part":{"type":"step-start"}}`,
		`{"type":"tool_use","timestamp":1001,"sessionID":"ses_stop_tool","part":{"type":"tool","tool":"bash","callID":"call_1","state":{"status":"completed","input":{"command":"go test ./..."},"output":"ok\n"}}}`,
		`{"type":"step_finish","timestamp":1002,"sessionID":"ses_stop_tool","part":{"type":"step-finish","reason":"stop"}}`,
	}, "\n")

	result := b.processEvents(strings.NewReader(lines), ch)

	if result.status != "failed" {
		t.Errorf("status: got %q, want %q", result.status, "failed")
	}
	if !strings.Contains(result.errMsg, "terminal signal") {
		t.Errorf("errMsg: got %q, want it to mention the missing terminal signal", result.errMsg)
	}
	if !result.noTerminalSignal {
		t.Error("noTerminalSignal: got false, want true")
	}

	close(ch)
}

func TestRuntimeMProcessEventsToolWithStopFinishThenContinuation(t *testing.T) {
	t.Parallel()

	b := &runtimeMBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	lines := strings.Join([]string{
		`{"type":"step_start","timestamp":1000,"sessionID":"ses_stop_continue","part":{"type":"step-start"}}`,
		`{"type":"tool_use","timestamp":1001,"sessionID":"ses_stop_continue","part":{"type":"tool","tool":"bash","callID":"call_1","state":{"status":"completed","input":{"command":"echo ok"},"output":"ok\n"}}}`,
		`{"type":"step_finish","timestamp":1002,"sessionID":"ses_stop_continue","part":{"type":"step-finish","reason":"stop"}}`,
		`{"type":"step_start","timestamp":1003,"sessionID":"ses_stop_continue","part":{"type":"step-start"}}`,
		`{"type":"text","timestamp":1004,"sessionID":"ses_stop_continue","part":{"type":"text","text":"done"}}`,
		`{"type":"step_finish","timestamp":1005,"sessionID":"ses_stop_continue","part":{"type":"step-finish","reason":"stop"}}`,
	}, "\n")

	result := b.processEvents(strings.NewReader(lines), ch)

	if result.status != "completed" {
		t.Errorf("status: got %q, want %q", result.status, "completed")
	}
	if result.errMsg != "" {
		t.Errorf("errMsg: got %q, want empty", result.errMsg)
	}

	close(ch)
}

func TestRuntimeMProcessEventsProviderExecutedToolWithStopFinish(t *testing.T) {
	t.Parallel()

	b := &runtimeMBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	lines := strings.Join([]string{
		`{"type":"step_start","timestamp":1000,"sessionID":"ses_provider_tool","part":{"type":"step-start"}}`,
		`{"type":"tool_use","timestamp":1001,"sessionID":"ses_provider_tool","part":{"type":"tool","tool":"web_search","callID":"call_1","metadata":{"providerExecuted":true},"state":{"status":"completed","input":{"query":"weather"},"output":"sunny"}}}`,
		`{"type":"step_finish","timestamp":1002,"sessionID":"ses_provider_tool","part":{"type":"step-finish","reason":"stop"}}`,
	}, "\n")

	result := b.processEvents(strings.NewReader(lines), ch)

	if result.status != "completed" {
		t.Errorf("status: got %q, want %q", result.status, "completed")
	}
	if result.errMsg != "" {
		t.Errorf("errMsg: got %q, want empty", result.errMsg)
	}

	close(ch)
}

func TestRuntimeMProcessEventsStepFinishWithoutReasonBackcompat(t *testing.T) {
	t.Parallel()

	b := &runtimeMBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	lines := strings.Join([]string{
		`{"type":"step_start","timestamp":1000,"sessionID":"ses_noreason","part":{"type":"step-start"}}`,
		`{"type":"text","timestamp":1001,"sessionID":"ses_noreason","part":{"type":"text","text":"legacy run"}}`,
		`{"type":"step_finish","timestamp":1002,"sessionID":"ses_noreason","part":{"type":"step-finish"}}`,
	}, "\n")

	result := b.processEvents(strings.NewReader(lines), ch)

	if result.status != "completed" {
		t.Errorf("status: got %q, want %q", result.status, "completed")
	}
	if result.errMsg != "" {
		t.Errorf("errMsg: got %q, want empty", result.errMsg)
	}

	close(ch)
}

func TestRuntimeMProcessEventsToolErrorThenCleanFinish(t *testing.T) {
	t.Parallel()

	b := &runtimeMBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	lines := strings.Join([]string{
		`{"type":"step_start","timestamp":1000,"sessionID":"ses_toolerr","part":{"type":"step-start"}}`,
		`{"type":"tool_use","timestamp":1001,"sessionID":"ses_toolerr","part":{"type":"tool","tool":"read","callID":"functions.read:1","state":{"status":"error","input":{"filePath":"/nonexistent-path-xyz/also-missing.md"},"error":"File not found: /nonexistent-path-xyz/also-missing.md"}}}`,
		`{"type":"tool_use","timestamp":1002,"sessionID":"ses_toolerr","part":{"type":"tool","tool":"bash","callID":"functions.bash:0","state":{"status":"completed","input":{"command":"echo hi"},"output":"hi\n"}}}`,
		`{"type":"step_finish","timestamp":1003,"sessionID":"ses_toolerr","part":{"type":"step-finish"}}`,
	}, "\n")

	result := b.processEvents(strings.NewReader(lines), ch)

	if result.status != "completed" {
		t.Errorf("status: got %q, want %q (recovered tool error must not fail a healthy run)", result.status, "completed")
	}
	if result.errMsg != "" {
		t.Errorf("errMsg: got %q, want empty", result.errMsg)
	}

	close(ch)
}

func fakeStat(present ...string) func(string) (os.FileInfo, error) {
	set := make(map[string]struct{}, len(present))
	for _, p := range present {
		set[p] = struct{}{}
	}
	return func(path string) (os.FileInfo, error) {
		if _, ok := set[path]; ok {
			return nil, nil
		}
		return nil, errors.New("not found")
	}
}

func TestResolveRuntimeMNativeFromShimResolvesNpmShim(t *testing.T) {
	t.Parallel()

	shim := filepath.Join("C:\\nvm4w", "nodejs", "opencode.cmd")
	native := filepath.Join("C:\\nvm4w", "nodejs", "node_modules", "opencode-ai", "node_modules", "opencode-windows-x64", "bin", "opencode.exe")

	got := resolveRuntimeMNativeFromShim(shim, fakeStat(native))
	if got != native {
		t.Errorf("got %q, want %q", got, native)
	}
}

func TestResolveRuntimeMNativeFromShimReturnsEmptyWhenNativeMissing(t *testing.T) {
	t.Parallel()

	shim := filepath.Join("C:\\nvm4w", "nodejs", "opencode.cmd")

	got := resolveRuntimeMNativeFromShim(shim, fakeStat())
	if got != "" {
		t.Errorf("got %q, want empty (missing native binary)", got)
	}
}

func TestResolveRuntimeMNativeFromShimSkipsNonCmdPath(t *testing.T) {
	t.Parallel()

	cases := []string{
		"/usr/local/bin/opencode",
		"C:\\nvm4w\\nodejs\\opencode.exe",
		"",
	}
	for _, p := range cases {
		if got := resolveRuntimeMNativeFromShim(p, fakeStat("anything")); got != "" {
			t.Errorf("path %q: got %q, want empty", p, got)
		}
	}
}

func TestResolveRuntimeMNativeFromShimAcceptsUppercaseExtension(t *testing.T) {
	t.Parallel()

	shim := filepath.Join("C:\\nvm4w", "nodejs", "opencode.CMD")
	native := filepath.Join("C:\\nvm4w", "nodejs", "node_modules", "opencode-ai", "node_modules", "opencode-windows-x64", "bin", "opencode.exe")

	got := resolveRuntimeMNativeFromShim(shim, fakeStat(native))
	if got != native {
		t.Errorf("got %q, want %q", got, native)
	}
}

func TestResolveRuntimeMNativeFromShimFallsBackToBaseline(t *testing.T) {
	t.Parallel()

	shim := filepath.Join("C:\\nvm4w", "nodejs", "opencode.cmd")
	baseline := filepath.Join("C:\\nvm4w", "nodejs", "node_modules", "opencode-ai", "node_modules", "opencode-windows-x64-baseline", "bin", "opencode.exe")

	got := resolveRuntimeMNativeFromShim(shim, fakeStat(baseline))
	if got != baseline {
		t.Errorf("got %q, want %q", got, baseline)
	}
}

func TestRuntimeMWindowsPackageCandidatesArm64(t *testing.T) {
	t.Parallel()

	got := runtimeMWindowsPackageCandidates("arm64")
	want := []string{"opencode-windows-arm64", "opencode-windows-x64", "opencode-windows-x64-baseline"}
	if !equalStringSlice(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestRuntimeMWindowsPackageCandidatesAmd64(t *testing.T) {
	t.Parallel()

	got := runtimeMWindowsPackageCandidates("amd64")
	want := []string{"opencode-windows-x64", "opencode-windows-x64-baseline", "opencode-windows-arm64"}
	if !equalStringSlice(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func fakeRuntimeMScript() string {
	return `#!/bin/sh
if [ -n "$OPENCODE_ARGS_FILE" ]; then
  for arg in "$@"; do
    printf '%s\n' "$arg" >> "$OPENCODE_ARGS_FILE"
  done
fi
if [ -n "$OPENCODE_PWD_FILE" ]; then
  printf '%s\n' "$PWD" > "$OPENCODE_PWD_FILE"
fi
if [ -n "$OPENCODE_PERMISSION_FILE" ]; then
  printf '%s\n' "$OPENCODE_PERMISSION" > "$OPENCODE_PERMISSION_FILE"
fi
if [ -f "$PWD/opencode.json" ] && grep -Eq '"question"[[:space:]]*:[[:space:]]*"allow"' "$PWD/opencode.json"; then
  if [ "$OPENCODE_PERMISSION" = '{"*":"allow","question":"deny"}' ]; then
    printf '{"type":"error","timestamp":1,"sessionID":"ses_fake","error":{"name":"PermissionBypass","data":{"message":"question permission bypassed by env wildcard order"}}}\n'
    exit 0
  fi
fi
printf '{"type":"step_start","timestamp":1,"sessionID":"ses_fake","part":{"type":"step-start"}}\n'
printf '{"type":"text","timestamp":2,"sessionID":"ses_fake","part":{"type":"text","text":"ok"}}\n'
printf '{"type":"step_finish","timestamp":3,"sessionID":"ses_fake","part":{"type":"step-finish"}}\n'
`
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func TestRuntimeMBackendAnchorsDirAndPWD(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	argsFile := filepath.Join(tempDir, "argv.txt")
	pwdFile := filepath.Join(tempDir, "pwd.txt")
	fakePath := filepath.Join(tempDir, "opencode")
	writeTestExecutable(t, fakePath, []byte(fakeRuntimeMScript()))

	workDir := t.TempDir()

	backend, err := New("runtime-m", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Env: map[string]string{
			"OPENCODE_ARGS_FILE": argsFile,
			"OPENCODE_PWD_FILE":  pwdFile,
		},
	})
	if err != nil {
		t.Fatalf("new opencode backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{
		Cwd:     workDir,
		Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	<-session.Result

	raw, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	args := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(args) < 2 || args[0] != "run" {
		t.Fatalf("expected first arg to be 'run', got %q", args)
	}
	dirIdx := -1
	for i, a := range args {
		if a == "--dir" {
			dirIdx = i
			break
		}
	}
	if dirIdx == -1 {
		t.Fatalf("expected --dir flag in argv, got %q", args)
	}
	if dirIdx+1 >= len(args) || args[dirIdx+1] != workDir {
		t.Fatalf("expected --dir %q, got args=%q", workDir, args)
	}

	gotPWD, err := os.ReadFile(pwdFile)
	if err != nil {
		t.Fatalf("read PWD file: %v", err)
	}
	if got := strings.TrimSpace(string(gotPWD)); got != workDir {
		t.Errorf("child PWD = %q, want %q", got, workDir)
	}
}

func TestRuntimeMBackendInjectsThinkingVariant(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	argsFile := filepath.Join(tempDir, "argv.txt")
	fakePath := filepath.Join(tempDir, "opencode")
	writeTestExecutable(t, fakePath, []byte(fakeRuntimeMScript()))

	backend, err := New("runtime-m", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Env: map[string]string{
			"OPENCODE_ARGS_FILE": argsFile,
		},
	})
	if err != nil {
		t.Fatalf("new opencode backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{
		Model:         "opencode/deepseek-v4",
		ThinkingLevel: "max",
		CustomArgs:    []string{"--variant", "low", "--keep-me"},
		Timeout:       5 * time.Second,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	<-session.Result

	raw, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	args := splitNonEmptyLines(string(raw))
	if !containsAdjacent(args, "--model", "opencode/deepseek-v4") {
		t.Fatalf("expected --model opencode/deepseek-v4 in args: %v", args)
	}
	if !containsAdjacent(args, "--variant", "max") {
		t.Fatalf("expected daemon-injected --variant max in args: %v", args)
	}
	if argIndexOf(args, "--model") > argIndexOf(args, "--variant") {
		t.Errorf("expected --variant after --model for readability: %v", args)
	}
	count := 0
	for _, arg := range args {
		if arg == "--variant" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly one --variant, got %d: %v", count, args)
	}
	if argIndexOf(args, "low") >= 0 {
		t.Errorf("filtered user --variant value still appears: %v", args)
	}
	if argIndexOf(args, "--keep-me") < 0 {
		t.Errorf("non-blocked custom arg was dropped: %v", args)
	}
}

func TestRuntimeMBackendDoesNotUsePermissionEnvOverride(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	permissionFile := filepath.Join(tempDir, "permission.json")
	fakePath := filepath.Join(tempDir, "opencode")
	writeTestExecutable(t, fakePath, []byte(fakeRuntimeMScript()))

	backend, err := New("runtime-m", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Env: map[string]string{
			"OPENCODE_PERMISSION_FILE": permissionFile,
		},
	})
	if err != nil {
		t.Fatalf("new opencode backend: %v", err)
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
	<-session.Result

	raw, err := os.ReadFile(permissionFile)
	if err != nil {
		t.Fatalf("read permission file: %v", err)
	}
	if got := strings.TrimSpace(string(raw)); got != "" {
		t.Fatalf("OPENCODE_PERMISSION = %q, want empty env override", got)
	}
}

func TestRuntimeMBackendQuestionDenySurvivesUserConfig(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	argsFile := filepath.Join(tempDir, "argv.txt")
	fakePath := filepath.Join(tempDir, "opencode")
	writeTestExecutable(t, fakePath, []byte(fakeRuntimeMScript()))

	workDir := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(workDir, "opencode.json"),
		[]byte(`{"permission":{"question":"allow"}}`),
		0o644,
	); err != nil {
		t.Fatalf("write opencode config: %v", err)
	}

	backend, err := New("runtime-m", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Env: map[string]string{
			"OPENCODE_ARGS_FILE": argsFile,
		},
	})
	if err != nil {
		t.Fatalf("new opencode backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{
		Cwd:     workDir,
		Timeout: 5 * time.Second,
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
		t.Fatalf("result status = %q, error = %q; want completed", result.Status, result.Error)
	}

	raw, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	args := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if !containsString(args, "--dangerously-skip-permissions") {
		t.Fatalf("expected daemon-mode argv to include --dangerously-skip-permissions, got %q", args)
	}
}

func TestRuntimeMBackendBlocksDirOverride(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	argsFile := filepath.Join(tempDir, "argv.txt")
	fakePath := filepath.Join(tempDir, "opencode")
	writeTestExecutable(t, fakePath, []byte(fakeRuntimeMScript()))

	workDir := t.TempDir()
	bogusDir := t.TempDir()

	backend, err := New("runtime-m", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Env: map[string]string{
			"OPENCODE_ARGS_FILE": argsFile,
		},
	})
	if err != nil {
		t.Fatalf("new opencode backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{
		Cwd:        workDir,
		Timeout:    5 * time.Second,
		CustomArgs: []string{"--dir", bogusDir},
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	<-session.Result

	raw, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	args := strings.Split(strings.TrimSpace(string(raw)), "\n")
	for i, a := range args {
		if a == "--dir" {
			if i+1 >= len(args) || args[i+1] != workDir {
				t.Errorf("--dir was overridden by custom args: got %q", args)
			}
		}
		if a == bogusDir {
			t.Errorf("custom --dir value %q leaked into argv: %q", bogusDir, args)
		}
	}
}

func fakeRuntimeMMidToolScript() string {
	return `#!/bin/sh
printf '{"type":"step_start","timestamp":1,"sessionID":"ses_fake","part":{"type":"step-start"}}\n'
printf '{"type":"tool_use","timestamp":2,"sessionID":"ses_fake","part":{"type":"tool","tool":"read","callID":"functions.read:1","state":{"status":"error","input":{"filePath":"/nope.md"},"error":"File not found"}}}\n'
exit 0
`
}

func TestRuntimeMBackendFailsOnStreamEndingMidTool(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	fakePath := filepath.Join(tempDir, "opencode")
	writeTestExecutable(t, fakePath, []byte(fakeRuntimeMMidToolScript()))

	backend, err := New("runtime-m", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
	})
	if err != nil {
		t.Fatalf("new opencode backend: %v", err)
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
	result := <-session.Result

	if result.Status != "failed" {
		t.Fatalf("result status = %q, error = %q; want failed", result.Status, result.Error)
	}
	if !strings.Contains(result.Error, "terminal signal") {
		t.Errorf("result error = %q, want it to mention the missing terminal signal", result.Error)
	}
}

func fakeRuntimeMStepThenExit1Script() string {
	return `#!/bin/sh
printf '{"type":"step_start","timestamp":1,"sessionID":"ses_fake","part":{"type":"step-start"}}\n'
exit 1
`
}

func TestRuntimeMBackendAppendsExitDetailOnMidStepCrash(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	fakePath := filepath.Join(tempDir, "opencode")
	writeTestExecutable(t, fakePath, []byte(fakeRuntimeMStepThenExit1Script()))

	backend, err := New("runtime-m", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
	})
	if err != nil {
		t.Fatalf("new opencode backend: %v", err)
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
	result := <-session.Result

	if result.Status != "failed" {
		t.Fatalf("result status = %q, error = %q; want failed", result.Status, result.Error)
	}
	if !strings.Contains(result.Error, "terminal signal") {
		t.Errorf("result error = %q, want it to mention the missing terminal signal", result.Error)
	}
	if !strings.Contains(result.Error, "exit status 1") {
		t.Errorf("result error = %q, want it to include the process exit status", result.Error)
	}
}

func equalStringSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
