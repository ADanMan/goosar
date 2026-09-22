package daemon

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"
)

func TestDefaultAgentCommandNamesMatchesRegistry(t *testing.T) {
	want := make([]string, 0, 18)
	for _, code := range runtimeRegistry.Codes() {
		d, ok := runtimeRegistry.ByCode(code)
		if !ok || d.CLIName == "" {
			t.Fatalf("runtime %s has no CLI name; the probe loop would skip it", code)
		}
		want = append(want, d.CLIName)
	}
	sort.Strings(want)

	got := append([]string(nil), defaultAgentCommandNames...)
	sort.Strings(got)

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("defaultAgentCommandNames = %v, want the registry's CLI names %v", got, want)
	}
}

func TestProbeCallsTakeNoLiteralCommandName(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "config.go", nil, 0)
	if err != nil {
		t.Fatalf("parse config.go: %v", err)
	}

	var literals []string
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		ident, ok := call.Fun.(*ast.Ident)
		if !ok || ident.Name != "probe" {
			return true
		}

		if len(call.Args) < 3 {
			t.Errorf("probe() called with %d arguments at %s; the command name is expected at index 2",
				len(call.Args), fset.Position(call.Pos()))
			return true
		}
		lit, ok := call.Args[2].(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		literals = append(literals, lit.Value+" at "+fset.Position(lit.Pos()).String())
		return true
	})

	if len(literals) > 0 {
		sort.Strings(literals)
		t.Fatalf("probe() called with a hardcoded command name: %v; take it from the runtime "+
			"registry instead, so defaultAgentCommandNames and the "+
			"scripts/agent-cli-command-names.txt guard keep covering every command the daemon can execute",
			literals)
	}
}

func TestAgentCLIGuardCoversDefaultCommands(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "scripts", "agent-cli-command-names.txt"))
	if err != nil {
		t.Fatalf("read agent CLI guard names: %v", err)
	}
	guarded := map[string]bool{}
	for lineNumber, line := range strings.Split(string(data), "\n") {
		if line != strings.TrimSpace(line) {
			t.Fatalf("agent CLI guard name on line %d has surrounding whitespace", lineNumber+1)
		}
		if line != "" && !strings.HasPrefix(line, "#") {
			if !isSafeAgentCLICommandName(line) {
				t.Fatalf("agent CLI guard name on line %d contains unsafe characters: %q", lineNumber+1, line)
			}
			guarded[line] = true
		}
	}
	for _, name := range defaultAgentCommandNames {
		if !guarded[name] {
			t.Errorf("default agent command %q is not covered by the test guard", name)
		}
	}
	if !guarded["qodercli"] {
		t.Error("default qoder command \"qodercli\" is not covered by the test guard")
	}
}

func isSafeAgentCLICommandName(name string) bool {
	for _, char := range name {
		if (char >= 'a' && char <= 'z') ||
			(char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') ||
			char == '.' || char == '_' || char == '-' {
			continue
		}
		return false
	}
	return name != ""
}

func TestAgentCLIGuardDetectsSwallowedFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the full guarded backend suite runs on Linux/macOS")
	}
	script := filepath.Join("..", "..", "..", "scripts", "go-test-with-agent-cli-guard.sh")
	cmd := exec.Command(script, "--", "/bin/sh", "-c", "claude --version --token super-secret >/dev/null 2>&1 || true")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("guard succeeded after a swallowed agent CLI failure: %s", out)
	}
	if !strings.Contains(string(out), "unexpected agent CLI invocation: claude [arguments redacted]") {
		t.Fatalf("guard diagnostic missing invocation: %s", out)
	}
	if strings.Contains(string(out), "super-secret") {
		t.Fatalf("guard diagnostic exposed command arguments: %s", out)
	}
}

func TestAgentCLIGuardFailsClosedWhenSetupFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the full guarded backend suite runs on Linux/macOS")
	}
	invalidTempDir := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(invalidTempDir, []byte("fixture"), 0o600); err != nil {
		t.Fatalf("write invalid temp directory fixture: %v", err)
	}
	executedMarker := filepath.Join(t.TempDir(), "executed")
	script := filepath.Join("..", "..", "..", "scripts", "go-test-with-agent-cli-guard.sh")
	cmd := exec.Command(script, "--", "/bin/sh", "-c", "printf ran >\"$1\"", "sh", executedMarker)
	cmd.Env = append(os.Environ(), "TMPDIR="+invalidTempDir)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("guard succeeded after setup failure: %s", out)
	}
	if _, statErr := os.Stat(executedMarker); !os.IsNotExist(statErr) {
		t.Fatalf("wrapped command ran after guard setup failure: %v", statErr)
	}
}
