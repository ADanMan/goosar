// Package contract runs black-box contract tests for the Goosar HTTP API
// against docs/50-api-contract.yaml. It never imports server code: it only
// speaks HTTP to BASE_URL, exactly like any other client of the API would.
package contract

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

// specPath resolves docs/50-api-contract.yaml relative to this source file,
// so `go test` works from any working directory.
func specPath() string {
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "docs", "50-api-contract.yaml")
}

var (
	specOnce sync.Once
	spec     *openapi3.T
	specErr  error

	coverageMu   sync.Mutex
	coveredOps   = map[string]bool{}
	allOpIDs     = map[string]bool{}
	allOpIDsOnce sync.Once
)

func loadSpec(t testing.TB) *openapi3.T {
	t.Helper()
	specOnce.Do(func() {
		loader := openapi3.NewLoader()
		spec, specErr = loader.LoadFromFile(specPath())
	})
	if specErr != nil {
		t.Fatalf("failed to load OpenAPI contract at %s: %v", specPath(), specErr)
	}
	allOpIDsOnce.Do(func() {
		for _, item := range spec.Paths.Map() {
			for _, op := range operationsOf(item) {
				if op.op.OperationID != "" {
					allOpIDs[op.op.OperationID] = true
				}
			}
		}
	})
	return spec
}

type methodOp struct {
	method string
	op     *openapi3.Operation
}

func operationsOf(item *openapi3.PathItem) []methodOp {
	var out []methodOp
	add := func(m string, op *openapi3.Operation) {
		if op != nil {
			out = append(out, methodOp{m, op})
		}
	}
	add("GET", item.Get)
	add("POST", item.Post)
	add("PUT", item.Put)
	add("PATCH", item.Patch)
	add("DELETE", item.Delete)
	add("HEAD", item.Head)
	add("OPTIONS", item.Options)
	return out
}

// validateResponse checks that `status` is a documented response for
// method+pathTemplate in the OpenAPI contract, and, when that response
// declares an application/json schema, that `body` decodes into JSON that
// satisfies it. It records the operation as covered regardless of pass/fail,
// since the point of coverage is "this operation was exercised by the
// suite", not "it passed".
func validateResponse(t testing.TB, doc *openapi3.T, method, pathTemplate string, status int, body []byte) {
	t.Helper()

	item := doc.Paths.Find(pathTemplate)
	if item == nil {
		t.Fatalf("OpenAPI contract has no path %q (used by %s %s)", pathTemplate, method, pathTemplate)
		return
	}

	var op *openapi3.Operation
	for _, mo := range operationsOf(item) {
		if mo.method == method {
			op = mo.op
			break
		}
	}
	if op == nil {
		t.Fatalf("OpenAPI contract has no %s operation on %q", method, pathTemplate)
		return
	}

	if op.OperationID != "" {
		coverageMu.Lock()
		coveredOps[op.OperationID] = true
		coverageMu.Unlock()
	}

	resp := op.Responses.Value(strconv.Itoa(status))
	if resp == nil {
		resp = op.Responses.Default()
	}
	if resp == nil {
		t.Errorf("%s %s: server returned status %d, which is not documented (operationId=%s)",
			method, pathTemplate, status, op.OperationID)
		return
	}
	if resp.Value == nil {
		return
	}

	mt := resp.Value.Content.Get("application/json")
	if mt == nil || mt.Schema == nil || mt.Schema.Value == nil {
		// No JSON body documented for this status (e.g. 204, or a redirect).
		return
	}

	if len(body) == 0 {
		// Some documented-JSON responses are legitimately empty in edge
		// cases the schema still models as optional; only flag it when the
		// schema requires at least something.
		if mt.Schema.Value.Type != nil && mt.Schema.Value.Type.Includes("object") && len(mt.Schema.Value.Required) > 0 {
			t.Errorf("%s %s: status %d documented a JSON body but the response body was empty",
				method, pathTemplate, status)
		}
		return
	}

	var decoded any
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Errorf("%s %s: status %d body is not valid JSON: %v\nbody: %s", method, pathTemplate, status, err, truncate(body))
		return
	}

	if err := mt.Schema.Value.VisitJSON(decoded); err != nil {
		t.Errorf("%s %s: status %d body does not match its OpenAPI schema (operationId=%s): %v\nbody: %s",
			method, pathTemplate, status, op.OperationID, err, truncate(body))
	}
}

func truncate(b []byte) string {
	const max = 800
	if len(b) <= max {
		return string(b)
	}
	return string(b[:max]) + "...(truncated)"
}

// printCoverage is called once, after all requested subtests have run
// (regardless of -run filtering), to report how much of the contract this
// invocation actually exercised.
func printCoverage() {
	loadedSpec := spec
	if loadedSpec == nil {
		return
	}
	coverageMu.Lock()
	defer coverageMu.Unlock()
	fmt.Printf("\n=== Contract coverage: %d/%d documented operations exercised ===\n",
		len(coveredOps), len(allOpIDs))
	var missing []string
	for id := range allOpIDs {
		if !coveredOps[id] {
			missing = append(missing, id)
		}
	}
	if len(missing) > 0 {
		fmt.Printf("(%d operations not exercised this run; this is expected for a partial -run, "+
			"or for operations that need infrastructure this suite intentionally does not stand up)\n",
			len(missing))
	}
}
