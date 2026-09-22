package agent

import (
	"context"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func TestStaticModelCatalogsHaveValidEntries(t *testing.T) {
	t.Parallel()
	catalogs := map[string][]Model{
		"claude": staticModelsForRuntime("runtime-c"),
		"codex":  staticModelsForRuntime("runtime-e"),
		"cursor": staticModelsForRuntime("runtime-g"),
	}
	for provider, models := range catalogs {
		if len(models) == 0 {
			t.Errorf("%s static catalog returned no models", provider)
		}
		for i, model := range models {
			if model.ID == "" {
				t.Errorf("%s static catalog[%d] has empty ID", provider, i)
			}
			if model.Label == "" {
				t.Errorf("%s static catalog[%d] has empty Label", provider, i)
			}
		}
	}
}

func TestListModelsRuntimeQUsesRuntimeDefaultAndManualEntry(t *testing.T) {

	got, err := ListModels(context.Background(), "runtime-q", "")
	if err != nil {
		t.Fatalf("ListModels(qwen) error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("ListModels(qwen) = %+v, want no account-specific static catalog", got)
	}
}

func TestListModelsRuntimeFFallsBackToStatic(t *testing.T) {

	ctx := context.Background()
	modelCacheMu.Lock()
	delete(modelCache, "runtime-f")
	modelCacheMu.Unlock()

	got, err := ListModels(ctx, "runtime-f", missingAgentExecutable(t, "copilot"))
	if err != nil {
		t.Fatalf("ListModels(copilot) error: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("expected static fallback models, got empty list")
	}
	ids := map[string]bool{}
	for _, m := range got {
		ids[m.ID] = true
	}
	if !ids["gpt-5.4"] || !ids["claude-sonnet-4.6"] {
		t.Errorf("static fallback missing expected models: %+v", got)
	}
}

func TestRuntimeCStaticModelsExposesFable5(t *testing.T) {
	models := staticModelsForRuntime("runtime-c")
	ids := map[string]Model{}
	defaults := 0
	for _, m := range models {
		ids[m.ID] = m
		if m.Default {
			defaults++
		}
	}

	fable, ok := ids["claude-fable-5"]
	if !ok {
		t.Fatalf("missing Claude Fable 5 in: %+v", models)
	}
	if fable.Label != "Claude Fable 5" || fable.Provider != "anthropic" || fable.Default {
		t.Errorf("unexpected Fable entry: %+v", fable)
	}
	if defaults != 1 || !ids["claude-sonnet-4-6"].Default {
		t.Errorf("expected Sonnet 4.6 to remain the sole default, got defaults=%d models=%+v", defaults, models)
	}
}

func TestRuntimeCStaticModelsExposesSonnet5(t *testing.T) {
	models := staticModelsForRuntime("runtime-c")
	ids := map[string]Model{}
	defaults := 0
	for _, m := range models {
		ids[m.ID] = m
		if m.Default {
			defaults++
		}
	}

	sonnet, ok := ids["claude-sonnet-5"]
	if !ok {
		t.Fatalf("missing Claude Sonnet 5 in: %+v", models)
	}
	if sonnet.Label != "Claude Sonnet 5" || sonnet.Provider != "anthropic" || sonnet.Default {
		t.Errorf("unexpected Sonnet 5 entry: %+v", sonnet)
	}
	if defaults != 1 || !ids["claude-sonnet-4-6"].Default {
		t.Errorf("expected Sonnet 4.6 to remain the sole default, got defaults=%d models=%+v", defaults, models)
	}
}

func TestRuntimeCStaticModelsExposesOpus5(t *testing.T) {
	models := staticModelsForRuntime("runtime-c")
	ids := map[string]Model{}
	defaults := 0
	for _, m := range models {
		ids[m.ID] = m
		if m.Default {
			defaults++
		}
	}

	opus, ok := ids["claude-opus-5"]
	if !ok {
		t.Fatalf("missing Claude Opus 5 in: %+v", models)
	}
	if opus.Label != "Claude Opus 5" || opus.Provider != "anthropic" || opus.Default {
		t.Errorf("unexpected Opus 5 entry: %+v", opus)
	}

	if defaults != 1 || !ids["claude-sonnet-4-6"].Default {
		t.Errorf("expected Sonnet 4.6 to remain the sole default, got defaults=%d models=%+v", defaults, models)
	}
}

func TestRuntimeCOpus5AcceptedByProviderCompatibilityGate(t *testing.T) {
	t.Parallel()
	if ModelKnownIncompatibleWithProvider("runtime-c", "claude-opus-5") {
		t.Error("claude-opus-5 must be accepted by the claude provider gate")
	}
	if !ModelKnownIncompatibleWithProvider("runtime-e", "claude-opus-5") {
		t.Error("claude-opus-5 must still be rejected for the codex provider")
	}
}

func TestRuntimeEStaticModelsMatchVerifiedFallbackCatalog(t *testing.T) {

	models := staticModelsForRuntime("runtime-e")
	ids := map[string]Model{}
	for _, m := range models {
		ids[m.ID] = m
	}
	for _, want := range []string{
		"gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.6-luna",
		"gpt-5.5", "gpt-5.4", "gpt-5.4-mini",
		"gpt-5.3-codex", "gpt-5.2",
	} {
		if _, ok := ids[want]; !ok {
			t.Errorf("missing expected Codex model %q in: %+v", want, models)
		}
	}
	for _, unwanted := range []string{"gpt-5.5-mini", "gpt-5", "o3", "o3-mini"} {
		if _, ok := ids[unwanted]; ok {
			t.Errorf("unexpected stale/invalid Codex model %q in fallback: %+v", unwanted, models)
		}
	}
	latest, ok := ids["gpt-5.6-sol"]
	if !ok || !latest.Default {
		t.Errorf("expected `gpt-5.6-sol` to be the default Codex entry, got %+v", latest)
	}
	defaults := 0
	for _, m := range models {
		if m.Default {
			defaults++
		}
		if m.Provider != "openai" {
			t.Errorf("all Codex entries must carry Provider=openai, got %+v", m)
		}
	}
	if defaults != 1 {
		t.Errorf("expected exactly one default Codex entry, got %d", defaults)
	}
	if got := ids["gpt-5.6-sol"].Thinking; got == nil || got.DefaultLevel != "low" || !hasThinkingLevel(got, "max") || !hasThinkingLevel(got, "ultra") {
		t.Errorf("unexpected gpt-5.6-sol thinking catalog: %+v", got)
	}
	if got := ids["gpt-5.6-luna"].Thinking; got == nil || !hasThinkingLevel(got, "max") || hasThinkingLevel(got, "ultra") {
		t.Errorf("unexpected gpt-5.6-luna thinking catalog: %+v", got)
	}
	if got := ids["gpt-5.3-codex"].Thinking; got == nil || !hasThinkingLevel(got, "xhigh") || hasThinkingLevel(got, "max") || hasThinkingLevel(got, "ultra") {
		t.Errorf("unexpected gpt-5.3-codex thinking catalog: %+v", got)
	}
}

func TestModelKnownIncompatibleWithProvider(t *testing.T) {
	cases := []struct {
		name     string
		provider string
		model    string
		want     bool
	}{
		{
			name:     "claude model is incompatible with codex",
			provider: "runtime-e",
			model:    "claude-sonnet-4-6",
			want:     true,
		},
		{
			name:     "codex model is compatible with codex",
			provider: "runtime-e",
			model:    "gpt-5.5",
			want:     false,
		},
		{
			name:     "codex model is incompatible with claude",
			provider: "runtime-c",
			model:    "o3",
			want:     true,
		},
		{
			name:     "exact claude model is compatible with claude",
			provider: "runtime-c",
			model:    "claude-opus-4-7",
			want:     false,
		},
		{
			name:     "provider-prefixed openai model is incompatible with codex",
			provider: "runtime-e",
			model:    "openai/gpt-4o",
			want:     true,
		},
		{
			name:     "provider-prefixed anthropic model is incompatible with claude",
			provider: "runtime-c",
			model:    "anthropic/claude-opus-4.7",
			want:     true,
		},
		{
			name:     "known openai-looking model outside codex catalog is incompatible",
			provider: "runtime-e",
			model:    "gpt-99",
			want:     true,
		},
		{
			name:     "unknown custom model is not classified",
			provider: "runtime-e",
			model:    "private-lab-model",
			want:     false,
		},
		{
			name:     "unknown target provider does not clear",
			provider: "runtime-m",
			model:    "claude-sonnet-4-6",
			want:     false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ModelKnownIncompatibleWithProvider(tc.provider, tc.model); got != tc.want {
				t.Fatalf("ModelKnownIncompatibleWithProvider(%q, %q) = %v, want %v", tc.provider, tc.model, got, tc.want)
			}
		})
	}
}

func TestInferModelVendorFromID(t *testing.T) {
	cases := map[string]string{
		"gpt-5.5":           "openai",
		"gpt-5.4-mini":      "openai",
		"gpt-5.3-codex":     "openai",
		"gpt-4.1":           "openai",
		"o1":                "openai",
		"o3":                "openai",
		"o3-mini":           "openai",
		"o4-mini":           "openai",
		"o5":                "openai",
		"o6-mini-high":      "openai",
		"claude-opus-4.7":   "anthropic",
		"claude-sonnet-4.6": "anthropic",
		"claude-haiku-4.5":  "anthropic",
		"gemini-3-pro":      "google",
		"grok-code-fast-1":  "xai",
		"auto":              "",
		"raptor-mini":       "",

		"opus-fake": "",
		"omni":      "",
		"o":         "",
	}
	for id, want := range cases {
		if got := inferModelVendorFromID(id); got != want {
			t.Errorf("inferCopilotProvider(%q) = %q, want %q", id, got, want)
		}
	}
}

func TestRuntimeFStaticModelsExposesFullCatalog(t *testing.T) {

	models := staticModelsForRuntime("runtime-f")
	ids := map[string]Model{}
	for _, m := range models {
		ids[m.ID] = m
	}
	for _, want := range []string{
		"gpt-5.5", "gpt-5.4", "gpt-5.4-mini",
		"gpt-5.3-codex", "gpt-5.2-codex", "gpt-5.2",
		"gpt-5-mini", "gpt-4.1",
		"claude-opus-4.7", "claude-sonnet-4.6",
		"claude-sonnet-4.5", "claude-haiku-4.5",
	} {
		if _, ok := ids[want]; !ok {
			t.Errorf("missing expected Copilot model %q in: %+v", want, models)
		}
	}

	for _, banned := range []string{"claude-sonnet-4-6", "claude-sonnet-4-5"} {
		if _, ok := ids[banned]; ok {
			t.Errorf("Copilot catalog must not use dashed model id %q; use dotted form", banned)
		}
	}
	for _, m := range models {
		switch m.Provider {
		case "openai", "anthropic":
		default:
			t.Errorf("Copilot entry %q has unexpected Provider %q", m.ID, m.Provider)
		}
		if m.Default {
			t.Errorf("Copilot entries should not set Default; account routing decides. got %+v", m)
		}
	}
}

func TestListModelsRuntimeJWithoutBinary(t *testing.T) {

	ctx := context.Background()

	modelCacheMu.Lock()
	delete(modelCache, "runtime-j")
	modelCacheMu.Unlock()

	got, err := ListModels(ctx, "runtime-j", missingAgentExecutable(t, "hermes"))
	if err != nil {
		t.Fatalf("ListModels(hermes) error: %v", err)
	}
	if got == nil {
		t.Error("expected non-nil slice even when binary is missing")
	}
}

func TestListModelsRuntimeLWithoutBinary(t *testing.T) {
	ctx := context.Background()
	modelCacheMu.Lock()
	delete(modelCache, "runtime-l")
	modelCacheMu.Unlock()

	got, err := ListModels(ctx, "runtime-l", missingAgentExecutable(t, "kiro-cli"))
	if err != nil {
		t.Fatalf("ListModels(kiro) error: %v", err)
	}
	if got == nil {
		t.Error("expected non-nil slice even when binary is missing")
	}
}

func TestListModelsRuntimePWithoutBinary(t *testing.T) {
	ctx := context.Background()
	modelCacheMu.Lock()
	delete(modelCache, "runtime-p")
	modelCacheMu.Unlock()

	got, err := ListModels(ctx, "runtime-p", missingAgentExecutable(t, "qodercli"))
	if err != nil {
		t.Fatalf("ListModels(qoder) error: %v", err)
	}
	if got == nil {
		t.Error("expected non-nil slice even when binary is missing")
	}
}

func TestListModelsUnknownProvider(t *testing.T) {
	ctx := context.Background()
	_, err := ListModels(ctx, "nonexistent", "")
	if err == nil {
		t.Fatal("ListModels(unknown) expected error")
	}
}

func TestStaticCatalogsHaveAtMostOneDefault(t *testing.T) {

	catalogs := map[string][]Model{
		"claude":  staticModelsForRuntime("runtime-c"),
		"codex":   staticModelsForRuntime("runtime-e"),
		"cursor":  staticModelsForRuntime("runtime-g"),
		"copilot": staticModelsForRuntime("runtime-f"),
	}
	for provider, models := range catalogs {
		count := 0
		for _, m := range models {
			if m.Default {
				count++
			}
		}
		if count > 1 {
			t.Errorf("%s: %d models marked Default, want 0 or 1", provider, count)
		}
	}
}

func TestParseRuntimeMModels(t *testing.T) {
	input := `PROVIDER/MODEL                     CONTEXT  MAX_OUT
openai/gpt-4o                      128000   16384
anthropic/claude-sonnet-4-6        200000   8192
openai/gpt-4o                      128000   16384
nonprefixed-line
`
	models := parseRuntimeMModels(input)
	if len(models) != 2 {
		t.Fatalf("expected 2 models (header skipped, duplicate deduped, non-slash skipped), got %d: %+v", len(models), models)
	}
	if models[0].ID != "openai/gpt-4o" || models[0].Provider != "openai" {
		t.Errorf("unexpected first model: %+v", models[0])
	}
	if models[1].ID != "anthropic/claude-sonnet-4-6" || models[1].Provider != "anthropic" {
		t.Errorf("unexpected second model: %+v", models[1])
	}
}

func TestParseRuntimeMModelsVerboseVariants(t *testing.T) {
	input := `openai/gpt-5
{
  "id": "gpt-5",
  "name": "GPT-5",
  "reasoning": true,
  "variants": {
    "high": { "reasoningEffort": "high" },
    "low": { "reasoningEffort": "low" },
    "xhigh": { "reasoningEffort": "xhigh" },
    "fast-mode": { "reasoningEffort": "low" },
    "disabled": { "disabled": true }
  }
}
anthropic/claude-sonnet-4-6
{
  "id": "claude-sonnet-4-6",
  "reasoning": true,
  "variants": {
    "max": { "thinking": { "type": "enabled", "budgetTokens": 32000 } },
    "high": { "thinking": { "type": "enabled", "budgetTokens": 16000 } }
  }
}
`
	models := parseRuntimeMModels(input)
	if len(models) != 2 {
		t.Fatalf("expected 2 models, got %d: %+v", len(models), models)
	}
	if models[0].Thinking == nil {
		t.Fatalf("expected first model to expose thinking variants")
	}
	got := make([]string, 0, len(models[0].Thinking.SupportedLevels))
	for _, lvl := range models[0].Thinking.SupportedLevels {
		got = append(got, lvl.Value)
		if lvl.Value == "xhigh" && lvl.Label != "Extra high" {
			t.Errorf("xhigh label: got %q, want Extra high", lvl.Label)
		}
		if lvl.Value == "fast-mode" && lvl.Label != "Fast Mode" {
			t.Errorf("custom variant label: got %q, want Fast Mode", lvl.Label)
		}
	}
	want := []string{"low", "high", "xhigh", "fast-mode"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("variant order/values: got %v, want %v", got, want)
	}
	if models[1].Thinking == nil || len(models[1].Thinking.SupportedLevels) != 2 {
		t.Fatalf("expected second model variants, got %+v", models[1].Thinking)
	}
}

func TestParseRuntimeMModelsMalformedVerboseBlockKeepsFollowingModels(t *testing.T) {
	input := `openai/gpt-5
{
  "id": "gpt-5",
  "reasoning": true,
  "variants": {
    "high": {}
  }
anthropic/claude-sonnet-4-6
{
  "id": "claude-sonnet-4-6",
  "reasoning": true,
  "variants": {
    "high": {},
    "max": {}
  }
}
`
	models := parseRuntimeMModels(input)
	if len(models) != 2 {
		t.Fatalf("expected both model rows to survive malformed JSON, got %d: %+v", len(models), models)
	}
	if models[0].ID != "openai/gpt-5" {
		t.Fatalf("unexpected first model: %+v", models[0])
	}
	if models[0].Thinking != nil {
		t.Fatalf("malformed first JSON block should not annotate thinking: %+v", models[0].Thinking)
	}
	if models[1].ID != "anthropic/claude-sonnet-4-6" {
		t.Fatalf("unexpected second model: %+v", models[1])
	}
	if models[1].Thinking == nil || len(models[1].Thinking.SupportedLevels) != 2 {
		t.Fatalf("valid following JSON block should still annotate thinking: %+v", models[1].Thinking)
	}
}

func TestDiscoverRuntimeMModelsFallsBackWhenVerboseFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake binary requires a POSIX shell")
	}

	dir := t.TempDir()
	fake := filepath.Join(dir, "opencode")
	script := `#!/bin/sh
if [ "$1" = "models" ] && [ "$2" = "--verbose" ]; then
  exit 2
fi
if [ "$1" = "models" ]; then
  cat <<'EOF'
PROVIDER/MODEL                     CONTEXT  MAX_OUT
openai/gpt-4o                      128000   16384
EOF
  exit 0
fi
exit 1
`
	writeTestExecutable(t, fake, []byte(script))

	models, err := discoverRuntimeMModels(context.Background(), fake)
	if err != nil {
		t.Fatalf("discoverOpenCodeModels: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("expected fallback non-verbose model, got %d: %+v", len(models), models)
	}
	if models[0].ID != "openai/gpt-4o" || models[0].Thinking != nil {
		t.Fatalf("unexpected fallback model: %+v", models[0])
	}
}

func TestCachedDiscoveryDoesNotCacheEmpty(t *testing.T) {
	const emptyKey, nonEmptyKey = "test-cache-empty", "test-cache-nonempty"

	resetCache := func() {
		modelCacheMu.Lock()
		delete(modelCache, emptyKey)
		delete(modelCache, nonEmptyKey)
		modelCacheMu.Unlock()
	}
	resetCache()
	t.Cleanup(resetCache)

	emptyCalls := 0
	empty := func() ([]Model, error) {
		emptyCalls++
		return []Model{}, nil
	}
	for i := 0; i < 2; i++ {
		got, err := cachedDiscovery(emptyKey, empty)
		if err != nil {
			t.Fatalf("cachedDiscovery: %v", err)
		}
		if len(got) != 0 {
			t.Fatalf("expected empty result, got %+v", got)
		}
	}
	if emptyCalls != 2 {
		t.Fatalf("empty result must not be cached: expected fn called 2x, got %d", emptyCalls)
	}

	nonEmptyCalls := 0
	nonEmpty := func() ([]Model, error) {
		nonEmptyCalls++
		return []Model{{ID: "provider/model"}}, nil
	}
	for i := 0; i < 2; i++ {
		if _, err := cachedDiscovery(nonEmptyKey, nonEmpty); err != nil {
			t.Fatalf("cachedDiscovery: %v", err)
		}
	}
	if nonEmptyCalls != 1 {
		t.Fatalf("non-empty result must be cached: expected fn called 1x, got %d", nonEmptyCalls)
	}
}

func TestParseRuntimeOModels(t *testing.T) {
	input := `openai:gpt-4o
anthropic:claude-opus-4-7
openai:gpt-4o
bareword
`
	models := parseRuntimeOModels(input)
	if len(models) != 2 {
		t.Fatalf("expected 2 models, got %d: %+v", len(models), models)
	}
	if models[0].ID != "openai/gpt-4o" {
		t.Errorf("expected colon normalized to slash: %+v", models[0])
	}
}

func TestParseRuntimeOModelsTableFormat(t *testing.T) {
	input := `provider             model                   context  max-out  thinking  images
bailian-coding-plan  glm-4.7                 202.8K   16.4K    no        no
bailian-coding-plan  qwen3.6-plus            1M       65.5K    no        yes
opencode             claude-sonnet-4-6       1M       64K      yes       yes
opencode             claude-sonnet-4-6:exp   1M       64K      yes       yes
opencode             claude-sonnet-4-6       1M       64K      yes       yes
bareword-only-line
`
	models := parseRuntimeOModels(input)
	if len(models) != 4 {
		t.Fatalf("expected 4 models (header skipped, duplicate deduped, bareword skipped), got %d: %+v", len(models), models)
	}
	if models[0].ID != "bailian-coding-plan/glm-4.7" || models[0].Provider != "bailian-coding-plan" {
		t.Errorf("unexpected first model: %+v", models[0])
	}
	if models[1].ID != "bailian-coding-plan/qwen3.6-plus" || models[1].Provider != "bailian-coding-plan" {
		t.Errorf("unexpected second model: %+v", models[1])
	}
	if models[2].ID != "opencode/claude-sonnet-4-6" || models[2].Provider != "opencode" {
		t.Errorf("unexpected third model: %+v", models[2])
	}

	if models[3].ID != "opencode/claude-sonnet-4-6:exp" || models[3].Provider != "opencode" {
		t.Errorf("expected ':' inside table-format model name to be preserved: %+v", models[3])
	}
}

func TestDiscoverRuntimeOModelsNonZeroExit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake pi binary is a /bin/sh script")
	}

	const table = "provider         model        context  max-out  thinking  images\n" +
		"glm-coding-plan  glm-4.7      202.8K   16.4K    no        no"

	const prefixed = `Warning: No models match pattern "opencode-go/mimo-v2-omni"`
	const bare = `No models match pattern "opencode-go/mimo-v2-pro"`

	cases := []struct {
		name   string
		script string
	}{
		{

			name: "catalog on stdout",
			script: "#!/bin/sh\n" +
				"cat <<'EOF'\n" + table + "\nEOF\n" +
				"echo " + strconv.Quote(prefixed) + " >&2\n" +
				"exit 1\n",
		},
		{

			name: "catalog and prefixed warning on stderr",
			script: "#!/bin/sh\n" +
				"cat >&2 <<'EOF'\n" + table + "\n" + prefixed + "\nEOF\n" +
				"exit 1\n",
		},
		{

			name: "catalog and bare warning on stderr",
			script: "#!/bin/sh\n" +
				"cat >&2 <<'EOF'\n" + table + "\n" + bare + "\nEOF\n" +
				"exit 1\n",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fakePath := filepath.Join(t.TempDir(), "pi")
			writeTestExecutable(t, fakePath, []byte(tc.script))

			models, err := discoverRuntimeOModels(context.Background(), fakePath)
			if err != nil {
				t.Fatalf("discoverPiModels: %v", err)
			}

			if len(models) != 1 || models[0].ID != "glm-coding-plan/glm-4.7" {
				t.Fatalf("expected exactly [glm-coding-plan/glm-4.7] despite non-zero exit, got %+v", models)
			}
		})
	}
}

func TestDiscoverRuntimeMModelsFallsBackOnVerboseNoise(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake opencode binary is a /bin/sh script")
	}

	script := "#!/bin/sh\n" +
		"if [ \"$2\" = \"--verbose\" ]; then\n" +
		"  echo 'panic: catalog sync failed'\n" +
		"  exit 1\n" +
		"fi\n" +
		"echo 'openai/gpt-4o'\n"

	fakePath := filepath.Join(t.TempDir(), "opencode")
	writeTestExecutable(t, fakePath, []byte(script))

	models, err := discoverRuntimeMModels(context.Background(), fakePath)
	if err != nil {
		t.Fatalf("discoverOpenCodeModels: %v", err)
	}
	if len(models) != 1 || models[0].ID != "openai/gpt-4o" {
		t.Fatalf("expected fallback to plain `opencode models` to yield [openai/gpt-4o], got %+v", models)
	}
}

func TestParseRuntimeNAgents(t *testing.T) {
	input := `deepseek-v4   deepseek-v4
claude-sonnet claude-sonnet-4-6
deepseek-v4   deepseek-v4
`
	models := parseRuntimeNAgents(input)

	if len(models) != 2 {
		t.Fatalf("expected 2 agents, got %d: %+v", len(models), models)
	}
	if models[0].ID != "deepseek-v4" {
		t.Errorf("unexpected first agent: %+v", models[0])
	}
	if models[0].Label != "deepseek-v4 (deepseek-v4)" {
		t.Errorf("unexpected label: %+v", models[0])
	}
	if models[0].Provider != "openclaw" {
		t.Errorf("expected provider openclaw, got %q", models[0].Provider)
	}
}

func TestParseRuntimeNAgentsRejectsDecoratedTUI(t *testing.T) {

	input := `╭───────────────────────────────╮
│                               │
│  ◇  Agents:                   │
│  │                            │
│  │    Identity:               │
│  │    Workspace:              │
│  │    Agent                   │
│  │                            │
╰───────────────────────────────╯
deepseek-v4   deepseek-v4
claude-sonnet claude-sonnet-4-6
`
	models := parseRuntimeNAgents(input)
	if len(models) != 2 {
		t.Fatalf("expected 2 agents (decoration skipped), got %d: %+v", len(models), models)
	}
	for _, m := range models {
		if strings.HasSuffix(m.ID, ":") {
			t.Errorf("section header leaked into result: %+v", m)
		}
	}
	if models[0].ID != "deepseek-v4" || models[1].ID != "claude-sonnet" {
		t.Errorf("unexpected agents: %+v", models)
	}
}

func TestParseRuntimeNAgentsJSONArray(t *testing.T) {
	input := []byte(`[
    {"name": "deepseek-v4", "model": "deepseek-v4"},
    {"name": "claude-sonnet", "model": "claude-sonnet-4-6"}
]`)
	models, ok := parseRuntimeNAgentsJSON(input)
	if !ok {
		t.Fatal("expected parseOpenclawAgentsJSON to accept an array")
	}
	if len(models) != 2 {
		t.Fatalf("got %d, want 2: %+v", len(models), models)
	}
	if models[0].ID != "deepseek-v4" || models[0].Label != "deepseek-v4 (deepseek-v4)" {
		t.Errorf("unexpected first entry: %+v", models[0])
	}
}

func TestParseRuntimeNAgentsJSONWrapped(t *testing.T) {
	input := []byte(`{"agents": [{"name": "foo", "model": "bar"}]}`)
	models, ok := parseRuntimeNAgentsJSON(input)
	if !ok {
		t.Fatal("expected parseOpenclawAgentsJSON to accept wrapped object")
	}
	if len(models) != 1 || models[0].ID != "foo" {
		t.Errorf("unexpected: %+v", models)
	}
}

func TestRuntimeNEntriesToModelsUsesIDOverName(t *testing.T) {

	input := []byte(`[{"id": "sub2api", "name": "Sub2API OPS", "model": "gpt-4o"}]`)
	models, ok := parseRuntimeNAgentsJSON(input)
	if !ok {
		t.Fatal("expected parseOpenclawAgentsJSON to accept array")
	}
	if len(models) != 1 {
		t.Fatalf("got %d models, want 1", len(models))
	}
	if models[0].ID != "sub2api" {
		t.Errorf("Model.ID = %q, want %q (should use id, not name)", models[0].ID, "sub2api")
	}
	if models[0].Label != "Sub2API OPS (gpt-4o)" {
		t.Errorf("Model.Label = %q, want %q (should use name for display)", models[0].Label, "Sub2API OPS (gpt-4o)")
	}
}

func TestParseRuntimeNAgentsJSONRejectsGarbage(t *testing.T) {
	if _, ok := parseRuntimeNAgentsJSON([]byte("not json")); ok {
		t.Error("expected ok=false for non-JSON")
	}
}

func TestParseRuntimeGModels(t *testing.T) {
	input := `Available models

auto - Auto
composer-2-fast - Composer 2 Fast (current, default)
composer-2 - Composer 2
claude-4.6-sonnet-medium - Sonnet 4.6 1M
claude-opus-4-7-high - Opus 4.7 1M
gemini-3.1-pro - Gemini 3.1 Pro
`
	models := parseRuntimeGModels(input)
	if len(models) != 6 {
		t.Fatalf("expected 6 models, got %d: %+v", len(models), models)
	}
	ids := map[string]Model{}
	for _, m := range models {
		ids[m.ID] = m
	}
	for _, want := range []string{"auto", "composer-2-fast", "composer-2", "claude-4.6-sonnet-medium", "claude-opus-4-7-high", "gemini-3.1-pro"} {
		if _, ok := ids[want]; !ok {
			t.Errorf("missing expected model %q in: %+v", want, models)
		}
	}
	if def := ids["composer-2-fast"]; !def.Default {
		t.Errorf("composer-2-fast should be marked default, got %+v", def)
	}
	if def := ids["composer-2-fast"]; def.Label != "Composer 2 Fast" {
		t.Errorf("default label should be stripped of parenthetical, got %q", def.Label)
	}

	if auto := ids["auto"]; auto.Default {
		t.Errorf("non-default entry should not be flagged default: %+v", auto)
	}
}

func TestParseRuntimeGModelsSkipsHeaderAndBlankLines(t *testing.T) {
	input := `Available models

composer-2 - Composer 2
`
	models := parseRuntimeGModels(input)
	if len(models) != 1 || models[0].ID != "composer-2" {
		t.Fatalf("unexpected: %+v", models)
	}
}

func TestParseRuntimeJSessionNewModels(t *testing.T) {

	raw := []byte(`{
      "sessionId": "ses_123",
      "models": {
        "availableModels": [
          {"modelId": "nous:moonshotai/kimi-k2.5", "name": "moonshotai/kimi-k2.5", "description": "Provider: Nous"},
          {"modelId": "nous:anthropic/claude-opus-4.7", "name": "anthropic/claude-opus-4.7", "description": "Provider: Nous • current"},
          {"modelId": "nous:moonshotai/kimi-k2.5", "name": "duplicate", "description": "dup"}
        ],
        "currentModelId": "nous:anthropic/claude-opus-4.7"
      }
    }`)
	models := parseACPSessionNewModels(raw)
	if len(models) != 2 {
		t.Fatalf("expected 2 models (duplicate deduped), got %d: %+v", len(models), models)
	}
	if models[0].ID != "nous:moonshotai/kimi-k2.5" || models[0].Provider != "nous" {
		t.Errorf("unexpected first model: %+v", models[0])
	}
	if models[0].Default {
		t.Errorf("non-current entry must not be marked default: %+v", models[0])
	}
	if !models[1].Default {
		t.Errorf("current entry must be marked default: %+v", models[1])
	}
	if models[1].ID != "nous:anthropic/claude-opus-4.7" {
		t.Errorf("expected current model second: %+v", models[1])
	}
}

func TestParseRuntimeJSessionNewModelsPreservesCustomModelIDsWithColons(t *testing.T) {
	raw := []byte(`{
      "sessionId": "ses_123",
      "models": {
        "availableModels": [
          {"modelId": "custom:lfm2.5:8b", "name": "lfm2.5:8b", "description": "Provider: Custom"}
        ],
        "currentModelId": "custom:lfm2.5:8b"
      }
    }`)
	models := parseACPSessionNewModels(raw)
	if len(models) != 1 {
		t.Fatalf("expected 1 model, got %d: %+v", len(models), models)
	}
	if models[0].ID != "custom:lfm2.5:8b" {
		t.Errorf("model id must be preserved verbatim, got %+v", models[0])
	}
	if models[0].Provider != "custom" {
		t.Errorf("provider should be derived from the first colon only, got %+v", models[0])
	}
	if !models[0].Default {
		t.Errorf("current custom model should be marked default: %+v", models[0])
	}
}

func TestParseRuntimeJSessionNewModelsSnakeCaseAndUnknownNames(t *testing.T) {
	raw := []byte(`{
      "session_id": "ses_123",
      "models": {
        "available_models": [
          {"model_id": "nous:moonshotai/kimi-k2.6", "name": "Unknown", "description": "Provider: Nous"},
          {"model_id": "nous:anthropic/claude-sonnet-4.6", "name": "unknown", "description": "Provider: Nous"}
        ],
        "current_model_id": "nous:moonshotai/kimi-k2.6"
      }
    }`)
	models := parseACPSessionNewModels(raw)
	if len(models) != 2 {
		t.Fatalf("expected 2 models, got %d: %+v", len(models), models)
	}
	if models[0].Label != "nous:moonshotai/kimi-k2.6" {
		t.Errorf("Unknown label should fall back to model id, got %+v", models[0])
	}
	if !models[0].Default {
		t.Errorf("snake_case current_model_id should mark default: %+v", models[0])
	}
	if models[1].Label != "nous:anthropic/claude-sonnet-4.6" {
		t.Errorf("lowercase unknown label should fall back to model id, got %+v", models[1])
	}
}

func TestParseRuntimeJSessionNewModelsMissingField(t *testing.T) {

	raw := []byte(`{"sessionId": "ses_123"}`)
	if got := parseACPSessionNewModels(raw); got != nil && len(got) != 0 {
		t.Errorf("expected nil/empty, got %+v", got)
	}
}

func TestParseRuntimeJSessionNewModelsGarbage(t *testing.T) {
	if got := parseACPSessionNewModels([]byte("not json")); got != nil {
		t.Errorf("expected nil for non-JSON, got %+v", got)
	}
}

func TestParseACPSessionNewModelsFromConfigOptions(t *testing.T) {

	raw := []byte(`{
      "sessionId": "session_abc",
      "configOptions": [
        {
          "type": "select",
          "id": "model",
          "name": "Model",
          "category": "model",
          "currentValue": "kimi-code/k3",
          "options": [
            {"value": "kimi-code/kimi-for-coding", "name": "K2.7 Coding"},
            {"value": "kimi-code/kimi-for-coding-highspeed", "name": "K2.7 Coding Highspeed"},
            {"value": "kimi-code/k3", "name": "K3"}
          ]
        },
        {
          "type": "select",
          "id": "thinking",
          "category": "thought_level",
          "currentValue": "high",
          "options": [
            {"value": "low", "name": "Low"},
            {"value": "high", "name": "High"},
            {"value": "max", "name": "Max"}
          ]
        }
      ]
    }`)
	models := parseACPSessionNewModels(raw)
	if len(models) != 3 {
		t.Fatalf("expected 3 models from configOptions, got %d: %+v", len(models), models)
	}
	if models[0].ID != "kimi-code/kimi-for-coding" || models[0].Label != "K2.7 Coding" {
		t.Errorf("unexpected first model: %+v", models[0])
	}

	if models[2].Provider != "" {
		t.Errorf("slash-form model id must not derive a provider: %+v", models[2])
	}
	if !models[2].Default {
		t.Errorf("currentValue entry must be marked default: %+v", models[2])
	}
	for _, m := range models {
		if m.ID == "low" || m.ID == "high" || m.ID == "max" {
			t.Errorf("thinking-level option leaked into the model catalog: %+v", m)
		}
	}
}

func TestParseACPSessionNewModelsPrefersModelsBlockOverConfigOptions(t *testing.T) {
	raw := []byte(`{
      "sessionId": "session_abc",
      "models": {
        "availableModels": [{"modelId": "nous:anthropic/claude-opus-4.7", "name": "Opus"}],
        "currentModelId": "nous:anthropic/claude-opus-4.7"
      },
      "configOptions": [
        {"id": "model", "category": "model", "currentValue": "other/one",
         "options": [{"value": "other/one", "name": "Other"}]}
      ]
    }`)
	models := parseACPSessionNewModels(raw)
	if len(models) != 1 || models[0].ID != "nous:anthropic/claude-opus-4.7" {
		t.Fatalf("models block must win over configOptions, got %+v", models)
	}
}

func TestParseACPSessionNewModelsConfigOptionsSnakeCaseAndCategoryOnly(t *testing.T) {

	raw := []byte(`{
      "session_id": "session_abc",
      "config_options": [
        {
          "id": "primary_model",
          "category": "MODEL",
          "current_value": "kimi-code/k3",
          "options": [
            {"value": "kimi-code/k3", "name": "K3"},
            {"value": "kimi-code/k3", "name": "duplicate"},
            {"value": "  ", "name": "blank"},
            {"value": "kimi-code/kimi-for-coding", "name": ""}
          ]
        }
      ]
    }`)
	models := parseACPSessionNewModels(raw)
	if len(models) != 2 {
		t.Fatalf("expected 2 models (duplicate and blank dropped), got %d: %+v", len(models), models)
	}
	if !models[0].Default {
		t.Errorf("snake_case current_value should mark default: %+v", models[0])
	}
	if models[1].Label != "kimi-code/kimi-for-coding" {
		t.Errorf("missing name should fall back to the model id, got %+v", models[1])
	}
}

func TestParseACPSessionNewModelsIgnoresNonModelConfigOptions(t *testing.T) {

	raw := []byte(`{
      "sessionId": "session_abc",
      "configOptions": [
        {"id": "thinking", "category": "thought_level", "currentValue": "high",
         "options": [{"value": "low", "name": "Low"}, {"value": "high", "name": "High"}]}
      ]
    }`)
	if got := parseACPSessionNewModels(raw); len(got) != 0 {
		t.Errorf("expected empty catalog, got %+v", got)
	}
}

func TestACPResultTopLevelKeys(t *testing.T) {

	keys := acpResultTopLevelKeys([]byte(`{"sessionId":"session_secret","configOptions":[],"modes":{}}`))
	got := strings.Join(keys, ",")
	if got != "configOptions,modes,sessionId" {
		t.Errorf("unexpected keys: %q", got)
	}
	if acpResultTopLevelKeys([]byte("not json")) != nil {
		t.Error("expected nil for non-JSON result")
	}
}

func TestRuntimeJModelSelectionSupported(t *testing.T) {

	if !ModelSelectionSupported("runtime-j") {
		t.Error("hermes should be model-selection-supported now that set_session_model is wired")
	}
}

func TestRuntimeAModelSelectionSupported(t *testing.T) {
	if !ModelSelectionSupported("runtime-a") {
		t.Error("antigravity should be model-selection-supported now that agy 1.0.6 has --model")
	}
}

func TestParseRuntimeAModels(t *testing.T) {
	t.Parallel()

	out := strings.Join([]string{
		"Gemini 3.5 Flash (Medium)",
		"Claude Opus 4.6 (Thinking)",
		"",
		"GPT-OSS 120B (Medium)",
		"Claude Opus 4.6 (Thinking)",
	}, "\n")

	got := parseRuntimeAModels(out)
	want := []Model{
		{ID: "Gemini 3.5 Flash (Medium)", Label: "Gemini 3.5 Flash (Medium)", Provider: "antigravity"},
		{ID: "Claude Opus 4.6 (Thinking)", Label: "Claude Opus 4.6 (Thinking)", Provider: "antigravity"},
		{ID: "GPT-OSS 120B (Medium)", Label: "GPT-OSS 120B (Medium)", Provider: "antigravity"},
	}
	if len(got) != len(want) {
		t.Fatalf("parseAntigravityModels len = %d, want %d (%+v)", len(got), len(want), got)
	}
	for i := range want {
		if !reflect.DeepEqual(got[i], want[i]) {
			t.Errorf("model[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestParseRuntimeAModelsEmpty(t *testing.T) {
	t.Parallel()
	if got := parseRuntimeAModels("   \n\t\n"); len(got) != 0 {
		t.Errorf("expected no models for blank output, got %+v", got)
	}
}

func TestCachedDiscovery(t *testing.T) {
	calls := 0
	fn := func() ([]Model, error) {
		calls++
		return []Model{{ID: "x", Label: "x"}}, nil
	}

	modelCacheMu.Lock()
	delete(modelCache, "testkey")
	modelCacheMu.Unlock()

	if _, err := cachedDiscovery("testkey", fn); err != nil {
		t.Fatal(err)
	}
	if _, err := cachedDiscovery("testkey", fn); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Errorf("expected 1 underlying call due to cache, got %d", calls)
	}
}
