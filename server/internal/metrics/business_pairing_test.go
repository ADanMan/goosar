package metrics_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/adanman/goosar/server/internal/analytics"
	"github.com/adanman/goosar/server/internal/metrics"
)

var frontendOnlyEvents = map[string]bool{
	analytics.EventOnboardingStarted: true,
}

func TestEveryAnalyticsEventHasPrometheusCounter(t *testing.T) {
	t.Parallel()

	declared := analyticsEventNames(t)

	m := metrics.NewBusinessMetrics()
	for name := range declared {

		ev := analytics.Event{
			Name:       name,
			DistinctID: "test",
			Properties: defaultPropsForEvent(name),
		}
		ok := dispatchIncrementsCounter(m, ev)
		if !ok {
			t.Errorf("analytics.%s (%q) is not paired with a Prometheus counter via metrics.IncForEvent — add a case in business_events.go", constantNameForEvent(name), name)
		}
	}
}

func TestNoNakedAnalyticsCaptureInHandlersOrServices(t *testing.T) {
	t.Parallel()

	roots := []string{
		filepath.Join(repoRoot(t), "internal", "handler"),
		filepath.Join(repoRoot(t), "internal", "service"),
		filepath.Join(repoRoot(t), "cmd", "server"),
	}

	allowedFunctions := map[string]map[string]struct{}{}

	var offenders []string
	fset := token.NewFileSet()
	for _, root := range roots {
		matches, err := filepath.Glob(filepath.Join(root, "*.go"))
		if err != nil {
			t.Fatalf("glob %s: %v", root, err)
		}
		for _, file := range matches {
			if strings.HasSuffix(file, "_test.go") {
				continue
			}
			f, err := parser.ParseFile(fset, file, nil, parser.SkipObjectResolution)
			if err != nil {
				t.Fatalf("parse %s: %v", file, err)
			}
			fileAllowedFns := allowedFunctions[file]
			for _, decl := range f.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok {
					continue
				}
				if _, allowed := fileAllowedFns[fn.Name.Name]; allowed {
					continue
				}
				if fn.Body == nil {
					continue
				}
				ast.Inspect(fn.Body, func(n ast.Node) bool {
					call, ok := n.(*ast.CallExpr)
					if !ok {
						return true
					}
					if !isAnalyticsCapture(call) {
						return true
					}
					offenders = append(offenders, fset.Position(call.Pos()).String()+" (in "+fn.Name.Name+")")
					return true
				})
			}
		}
	}

	if len(offenders) > 0 {
		sort.Strings(offenders)
		t.Errorf("found %d naked Analytics.Capture(...) calls — wrap them in metrics.RecordEvent so the Prometheus and PostHog sides cannot drift:\n  %s", len(offenders), strings.Join(offenders, "\n  "))
	}
}

func TestEveryAnalyticsRecordEventTakesAnalyticsHelper(t *testing.T) {
	t.Parallel()

	roots := []string{
		filepath.Join(repoRoot(t), "internal", "handler"),
		filepath.Join(repoRoot(t), "internal", "service"),
		filepath.Join(repoRoot(t), "cmd", "server"),
	}

	var offenders []string
	fset := token.NewFileSet()
	for _, root := range roots {
		matches, err := filepath.Glob(filepath.Join(root, "*.go"))
		if err != nil {
			t.Fatalf("glob %s: %v", root, err)
		}
		for _, file := range matches {
			if strings.HasSuffix(file, "_test.go") {
				continue
			}
			f, err := parser.ParseFile(fset, file, nil, parser.SkipObjectResolution)
			if err != nil {
				t.Fatalf("parse %s: %v", file, err)
			}
			for _, decl := range f.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Body == nil {
					continue
				}
				analyticsLocals := analyticsBackedIdents(fn.Body)
				ast.Inspect(fn.Body, func(n ast.Node) bool {
					call, ok := n.(*ast.CallExpr)
					if !ok {
						return true
					}
					if !isMetricsRecordEvent(call) {
						return true
					}
					if len(call.Args) < 3 {
						offenders = append(offenders, fset.Position(call.Pos()).String()+" (RecordEvent must be called with 3 args: client, metrics, event)")
						return true
					}
					ev := call.Args[2]
					if analyticsHelperCall(ev) {
						return true
					}
					if id, ok := ev.(*ast.Ident); ok {
						if _, traced := analyticsLocals[id.Name]; traced {
							return true
						}
						offenders = append(offenders, fset.Position(call.Pos()).String()+" (third arg "+id.Name+" was not assigned from an analytics.* helper in this function)")
						return true
					}
					offenders = append(offenders, fset.Position(call.Pos()).String()+" (third arg must be an analytics.* helper call or a local assigned from one)")
					return true
				})
			}
		}
	}

	if len(offenders) > 0 {
		sort.Strings(offenders)
		t.Errorf("metrics.RecordEvent call sites must take an analytics.* event:\n  %s", strings.Join(offenders, "\n  "))
	}
}

func analyticsBackedIdents(body *ast.BlockStmt) map[string]struct{} {
	out := map[string]struct{}{}
	if body == nil {
		return out
	}
	ast.Inspect(body, func(n ast.Node) bool {
		switch stmt := n.(type) {
		case *ast.AssignStmt:

			if len(stmt.Rhs) == 0 {
				return true
			}
			for i, lhs := range stmt.Lhs {
				if i >= len(stmt.Rhs) {

					if len(stmt.Rhs) == 1 && analyticsHelperCall(stmt.Rhs[0]) {
						if id, ok := lhs.(*ast.Ident); ok {
							out[id.Name] = struct{}{}
						}
					}
					continue
				}
				if id, ok := lhs.(*ast.Ident); ok && analyticsHelperCall(stmt.Rhs[i]) {
					out[id.Name] = struct{}{}
				}
			}
		case *ast.ValueSpec:

			for i, name := range stmt.Names {
				if i < len(stmt.Values) && analyticsHelperCall(stmt.Values[i]) {
					out[name.Name] = struct{}{}
				}
			}
		}
		return true
	})
	return out
}

func repoRoot(t *testing.T) string {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("runtime.Caller failed")
	}

	return filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", ".."))
}

func analyticsEventNames(t *testing.T) map[string]struct{} {
	t.Helper()

	path := filepath.Join(repoRoot(t), "internal", "analytics", "events.go")
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}

	out := map[string]struct{}{}
	for _, decl := range f.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok || len(vs.Values) == 0 {
				continue
			}
			for i, name := range vs.Names {
				if !strings.HasPrefix(name.Name, "Event") {
					continue
				}
				lit, ok := vs.Values[i].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				out[strings.Trim(lit.Value, "\"")] = struct{}{}
			}
		}
	}
	if len(out) == 0 {
		t.Fatalf("no Event* constants found in %s", path)
	}
	return out
}

func constantNameForEvent(name string) string {
	parts := strings.Split(name, "_")
	for i, p := range parts {
		if len(p) == 0 {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return "Event" + strings.Join(parts, "")
}

func dispatchIncrementsCounter(m *metrics.BusinessMetrics, ev analytics.Event) bool {
	before := metrics.SumAllCounters(m)
	metrics.RecordEvent(analytics.NoopClient{}, m, ev)
	after := metrics.SumAllCounters(m)
	return after > before
}

func defaultPropsForEvent(name string) map[string]any {
	switch name {
	case analytics.EventSignup:
		return map[string]any{"signup_source": "test"}
	case analytics.EventWorkspaceCreated:
		return map[string]any{"source": "manual"}
	case analytics.EventOnboardingStarted:
		return map[string]any{"platform": "web"}
	case analytics.EventOnboardingCompleted:
		return map[string]any{"completion_path": "full"}
	case analytics.EventIssueCreated:
		return map[string]any{"source": "manual", "platform": "web"}
	case analytics.EventChatMessageSent:
		return map[string]any{"platform": "web"}
	case analytics.EventAgentCreated:
		return map[string]any{"runtime_mode": "local", "source": "manual"}
	case analytics.EventAutopilotCreated:
		return map[string]any{"cadence": "manual"}
	case analytics.EventIssueExecuted:
		return map[string]any{"source": "manual"}
	case analytics.EventRuntimeRegistered, analytics.EventRuntimeReady, analytics.EventRuntimeOffline:
		return map[string]any{"runtime_mode": "local", "provider": "runtime-c"}
	case analytics.EventRuntimeFailed:
		return map[string]any{"runtime_mode": "local", "provider": "runtime-c", "failure_reason": "unknown", "recoverable": false}
	case analytics.EventAutopilotRunStarted, analytics.EventAutopilotRunCompleted, analytics.EventAutopilotRunFailed:
		return map[string]any{"cadence": "manual", "trigger_kind": "manual"}
	case analytics.EventFeedbackSubmitted:
		return map[string]any{"kind": "general", "platform": "web"}
	case analytics.EventContactSalesSubmitted:
		return map[string]any{"form_source": "page"}
	}
	return map[string]any{}
}

func isAnalyticsCapture(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	if sel.Sel == nil || sel.Sel.Name != "Capture" {
		return false
	}

	rec, ok := sel.X.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	if rec.Sel == nil || rec.Sel.Name != "Analytics" {
		return false
	}

	return true
}

func isMetricsRecordEvent(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	if sel.Sel == nil || sel.Sel.Name != "RecordEvent" {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	return pkg.Name == "obsmetrics" || pkg.Name == "metrics"
}

func analyticsHelperCall(expr ast.Expr) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	return pkg.Name == "analytics" && sel.Sel != nil && len(sel.Sel.Name) > 0
}
