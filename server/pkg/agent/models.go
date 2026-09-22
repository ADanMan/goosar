package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/adanman/goosar/server/pkg/agent/runtimeregistry"
)

type Model struct {
	ID           string             `json:"id"`
	Label        string             `json:"label"`
	Provider     string             `json:"provider,omitempty"`
	Default      bool               `json:"default,omitempty"`
	ServiceTiers []ModelServiceTier `json:"service_tiers,omitempty"`

	Thinking *ModelThinking `json:"thinking,omitempty"`
}

type ModelServiceTier struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type ModelThinking struct {
	SupportedLevels []ThinkingLevel `json:"supported_levels"`

	DefaultLevel string `json:"default_level,omitempty"`
}

type ThinkingLevel struct {
	Value       string `json:"value"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

type modelCacheEntry struct {
	models    []Model
	expiresAt time.Time
}

var (
	modelCacheMu sync.Mutex
	modelCache   = map[string]modelCacheEntry{}
)

const modelCacheTTL = 60 * time.Second

func ListModels(ctx context.Context, runtimeCode, executablePath string) ([]Model, error) {
	switch runtimeCode {
	case "runtime-c":
		models := staticModelsForRuntime(runtimeCode)
		annotateRuntimeCThinking(ctx, models, executablePath)
		return models, nil
	case "runtime-e":
		return cachedDiscovery(discoveryCacheKey(runtimeCode, executablePath), func() ([]Model, error) {
			return discoverRuntimeEModels(ctx, executablePath), nil
		})
	case "runtime-a":

		return cachedDiscovery(runtimeCode, func() ([]Model, error) {
			return discoverRuntimeAModels(ctx, executablePath)
		})
	case "runtime-r":

		return cachedDiscovery(runtimeCode, func() ([]Model, error) {
			return discoverRuntimeRModels(ctx, executablePath)
		})
	case "runtime-g":
		return cachedDiscovery(runtimeCode, func() ([]Model, error) {
			return discoverRuntimeGModels(ctx, executablePath)
		})
	case "runtime-f":
		return cachedDiscovery(runtimeCode, func() ([]Model, error) {
			return discoverRuntimeFModels(ctx, executablePath)
		})
	case "runtime-j":
		return cachedDiscovery(runtimeCode, func() ([]Model, error) {
			return discoverRuntimeJModels(ctx, executablePath)
		})
	case "runtime-k":
		return cachedDiscovery(runtimeCode, func() ([]Model, error) {
			return discoverRuntimeKModels(ctx, executablePath)
		})
	case "runtime-l":
		return cachedDiscovery(runtimeCode, func() ([]Model, error) {
			return discoverRuntimeLModels(ctx, executablePath)
		})
	case "runtime-p":
		return cachedDiscovery(runtimeCode, func() ([]Model, error) {
			return discoverRuntimePModels(ctx, executablePath)
		})
	case "runtime-m":
		return cachedDiscovery(discoveryCacheKey(runtimeCode, executablePath), func() ([]Model, error) {
			return discoverRuntimeMModels(ctx, executablePath)
		})
	case "runtime-h":
		return cachedDiscovery(discoveryCacheKey(runtimeCode, executablePath), func() ([]Model, error) {
			return discoverRuntimeHModels(ctx, executablePath)
		})
	case "runtime-o":
		return cachedDiscovery(runtimeCode, func() ([]Model, error) {
			return discoverRuntimeOModels(ctx, executablePath)
		})
	case "runtime-n":
		return cachedDiscovery(runtimeCode, func() ([]Model, error) {
			return discoverRuntimeNAgents(ctx, executablePath)
		})
	case "runtime-d":
		return cachedDiscovery(runtimeCode, func() ([]Model, error) {
			models, err := discoverRuntimeDModels(ctx, executablePath)
			if err != nil {
				return nil, err
			}
			annotateRuntimeDThinking(ctx, models, executablePath)
			return models, nil
		})
	case "runtime-q":

		return []Model{}, nil
	case "runtime-i":

		return cachedDiscovery(runtimeCode, func() ([]Model, error) {
			return discoverRuntimeIModels(ctx, executablePath)
		})
	default:
		return nil, fmt.Errorf("unknown runtime code: %q", runtimeCode)
	}
}

func ModelSelectionSupported(runtimeCode string) bool {
	return true
}

func ModelKnownIncompatibleWithProvider(runtimeCode, model string) bool {
	model = strings.TrimSpace(model)
	if model == "" {
		return false
	}

	accepted, ok := acceptedModelIDsForRuntime(runtimeCode)
	if !ok {
		return false
	}
	if accepted[model] {
		return false
	}
	return isRuntimeSpecificModelID(model)
}

var exactCatalogRuntimes = map[string]bool{
	"runtime-c": true,
	"runtime-e": true,
}

func acceptedModelIDsForRuntime(runtimeCode string) (map[string]bool, bool) {
	if !exactCatalogRuntimes[runtimeCode] {
		return nil, false
	}
	return modelIDSet(staticModelsForRuntime(runtimeCode)), true
}

func modelIDSet(models []Model) map[string]bool {
	out := make(map[string]bool, len(models))
	for _, m := range models {
		out[m.ID] = true
	}
	return out
}

func isRuntimeSpecificModelID(model string) bool {
	if strings.Contains(model, "/") {
		return true
	}
	if modelHasKnownPrefix(model) {
		return true
	}
	for code := range exactCatalogRuntimes {
		if modelIDSet(staticModelsForRuntime(code))[model] {
			return true
		}
	}
	return false
}

func modelHasKnownPrefix(model string) bool {
	return strings.HasPrefix(model, "claude-") ||
		strings.HasPrefix(model, "gpt-") ||
		strings.HasPrefix(model, "gemini-") ||
		strings.HasPrefix(model, "auto-gemini-") ||
		isOpenAIReasoningSeriesID(model)
}

func cachedDiscovery(key string, fn func() ([]Model, error)) ([]Model, error) {
	modelCacheMu.Lock()
	if entry, ok := modelCache[key]; ok && time.Now().Before(entry.expiresAt) {
		out := entry.models
		modelCacheMu.Unlock()
		return out, nil
	}
	modelCacheMu.Unlock()

	models, err := fn()
	if err != nil {
		return nil, err
	}

	if len(models) == 0 {
		return models, nil
	}

	modelCacheMu.Lock()
	modelCache[key] = modelCacheEntry{models: models, expiresAt: time.Now().Add(modelCacheTTL)}
	modelCacheMu.Unlock()
	return models, nil
}

func discoveryCacheKey(providerType, executablePath string) string {
	if executablePath == "" {
		return providerType
	}
	return providerType + ":" + executablePath
}

func staticModelsForRuntime(runtimeCode string) []Model {
	d, ok := runtimeRegistry.ByCode(runtimeCode)
	if !ok {
		return []Model{}
	}
	models := make([]Model, 0, len(d.Models))
	for _, entry := range d.Models {
		models = append(models, Model{
			ID:       entry.RealID,
			Label:    entry.Label,
			Provider: modelVendorForEntry(entry.RealID, d.CLIName),
			Default:  entry.Default,
			Thinking: thinkingFromRegistry(entry.Thinking),
		})
	}
	return models
}

func modelVendorForEntry(modelID, cliName string) string {
	if v := inferModelVendorFromID(modelID); v != "" {
		return v
	}
	if v := inferModelVendorFromExtendedID(modelID); v != "" {
		return v
	}
	return cliName
}

func thinkingFromRegistry(spec *runtimeregistry.ThinkingSpec) *ModelThinking {
	if spec == nil {
		return nil
	}
	levels := make([]ThinkingLevel, 0, len(spec.SupportedLevels))
	for _, l := range spec.SupportedLevels {
		levels = append(levels, ThinkingLevel{Value: l.Value, Label: l.Label, Description: l.Description})
	}
	return &ModelThinking{SupportedLevels: levels, DefaultLevel: spec.DefaultLevel}
}

func discoverRuntimeRModels(ctx context.Context, executablePath string) ([]Model, error) {
	return discoverACPModels(ctx, executablePath, acpDiscoveryProvider{
		defaultBin:   runtimeCLIName("runtime-r"),
		clientName:   "goosar-model-discovery",
		tmpdirPrefix: "goosar-traecli-discovery-",
		acpArgs:      []string{"acp", "serve", "--yolo"},
	})
}

func inferModelVendorFromID(modelID string) string {
	switch {
	case strings.HasPrefix(modelID, "gpt-") || isOpenAIReasoningSeriesID(modelID):
		return "openai"
	case strings.HasPrefix(modelID, "claude-"):
		return "anthropic"
	case strings.HasPrefix(modelID, "gemini-"):
		return "google"
	case strings.HasPrefix(modelID, "grok-"):
		return "xai"
	default:
		return ""
	}
}

func isOpenAIReasoningSeriesID(id string) bool {
	if len(id) < 2 || id[0] != 'o' {
		return false
	}
	i := 1
	for i < len(id) && id[i] >= '0' && id[i] <= '9' {
		i++
	}
	if i == 1 {
		return false
	}
	return i == len(id) || id[i] == '-'
}

func discoverRuntimeMModels(ctx context.Context, executablePath string) ([]Model, error) {
	if executablePath == "" {
		executablePath = runtimeCLIName("runtime-m")
	}
	if _, err := exec.LookPath(executablePath); err != nil {
		return []Model{}, nil
	}

	runCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := newRuntimeCmd(exec.CommandContext(runCtx, executablePath, "models", "--verbose"))
	hideAgentWindow(cmd)

	out, _ := cmd.Output()
	models := parseRuntimeMModels(string(out))
	if len(models) == 0 {

		cmd = newRuntimeCmd(exec.CommandContext(runCtx, executablePath, "models"))
		hideAgentWindow(cmd)
		out, _ = cmd.Output()
		models = parseRuntimeMModels(string(out))
	}
	if len(models) == 0 {
		return []Model{}, nil
	}
	return models, nil
}

func parseRuntimeMModels(output string) []Model {
	lines := strings.Split(output, "\n")
	var models []Model
	indexByID := map[string]int{}
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		id := parseRuntimeMModelIDLine(line)
		if id == "" {
			continue
		}
		idx, seen := indexByID[id]
		if !seen {
			provider := ""
			if slash := strings.Index(id, "/"); slash > 0 {
				provider = id[:slash]
			}
			idx = len(models)
			indexByID[id] = idx
			models = append(models, Model{ID: id, Label: id, Provider: provider})
		}

		next := i + 1
		for next < len(lines) && strings.TrimSpace(lines[next]) == "" {
			next++
		}
		if next >= len(lines) || !strings.HasPrefix(strings.TrimSpace(lines[next]), "{") {
			continue
		}
		raw, resumeAt := collectRuntimeMModelJSON(lines, next)
		if json.Valid(raw) {
			annotateRuntimeMModelMetadata(&models[idx], raw)
		}
		i = resumeAt - 1
	}
	return models
}

func parseRuntimeMModelIDLine(line string) string {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return ""
	}
	id := fields[0]
	if strings.HasPrefix(id, `"`) || strings.HasPrefix(id, "{") || strings.HasPrefix(id, "[") {
		return ""
	}
	if !strings.Contains(id, "/") {
		return ""
	}

	if id == strings.ToUpper(id) {
		return ""
	}
	return id
}

func collectRuntimeMModelJSON(lines []string, start int) ([]byte, int) {
	var b strings.Builder
	for i := start; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if i > start && parseRuntimeMModelIDLine(line) != "" {
			return []byte(b.String()), i
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(lines[i])
		if json.Valid([]byte(b.String())) {
			return []byte(b.String()), i + 1
		}
	}
	return []byte(b.String()), len(lines)
}

type runtimeMModelMetadata struct {
	Reasoning bool                            `json:"reasoning"`
	Variants  map[string]runtimeMModelVariant `json:"variants"`
}

type runtimeMModelVariant struct {
	Disabled        bool            `json:"disabled"`
	ReasoningEffort string          `json:"reasoningEffort"`
	Thinking        json.RawMessage `json:"thinking"`
}

var runtimeMVariantLabel = map[string]string{
	"none":    "None",
	"minimal": "Minimal",
	"low":     "Low",
	"medium":  "Medium",
	"high":    "High",
	"xhigh":   "Extra high",
	"max":     "Max",
}

var runtimeMVariantOrder = map[string]int{
	"none":    0,
	"minimal": 1,
	"low":     2,
	"medium":  3,
	"high":    4,
	"xhigh":   5,
	"max":     6,
}

func annotateRuntimeMModelMetadata(model *Model, raw []byte) {
	var meta runtimeMModelMetadata
	if err := json.Unmarshal(raw, &meta); err != nil {
		return
	}
	if !meta.Reasoning && !runtimeMVariantsLookReasoning(meta.Variants) {
		return
	}
	levels := runtimeMThinkingLevelsFromVariants(meta.Variants)
	if len(levels) == 0 {
		return
	}
	model.Thinking = &ModelThinking{SupportedLevels: levels}
}

func runtimeMVariantsLookReasoning(variants map[string]runtimeMModelVariant) bool {
	for name, variant := range variants {
		if _, known := runtimeMVariantOrder[name]; known {
			return true
		}
		if variant.ReasoningEffort != "" || len(variant.Thinking) > 0 {
			return true
		}
	}
	return false
}

func runtimeMThinkingLevelsFromVariants(variants map[string]runtimeMModelVariant) []ThinkingLevel {
	if len(variants) == 0 {
		return nil
	}
	values := make([]string, 0, len(variants))
	for value, variant := range variants {
		if value == "" || variant.Disabled {
			continue
		}
		values = append(values, value)
	}
	sort.Slice(values, func(i, j int) bool {
		left, leftKnown := runtimeMVariantOrder[values[i]]
		right, rightKnown := runtimeMVariantOrder[values[j]]
		if leftKnown && rightKnown {
			return left < right
		}
		if leftKnown != rightKnown {
			return leftKnown
		}
		return values[i] < values[j]
	})
	levels := make([]ThinkingLevel, 0, len(values))
	for _, value := range values {
		label, ok := runtimeMVariantLabel[value]
		if !ok {
			label = strings.Title(strings.ReplaceAll(value, "-", " ")) //nolint:staticcheck
		}
		levels = append(levels, ThinkingLevel{Value: value, Label: label})
	}
	return levels
}

func discoverRuntimeOModels(ctx context.Context, executablePath string) ([]Model, error) {
	if executablePath == "" {
		executablePath = runtimeCLIName("runtime-o")
	}
	if _, err := exec.LookPath(executablePath); err != nil {
		return []Model{}, nil
	}

	runCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := newRuntimeCmd(exec.CommandContext(runCtx, executablePath, "--list-models"))
	hideAgentWindow(cmd)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	stdout, err := cmd.Output()
	if err != nil && len(stdout) == 0 && stderr.Len() == 0 {
		return []Model{}, nil
	}
	text := string(stdout)
	if strings.TrimSpace(text) == "" {
		text = stderr.String()
	}
	return parseRuntimeOModels(text), nil
}

func parseRuntimeOModels(output string) []Model {
	scanner := bufio.NewScanner(strings.NewReader(output))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var models []Model
	seen := map[string]bool{}
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		if isRuntimeODiscoveryNoise(line) {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		first := fields[0]
		if strings.EqualFold(first, "provider") {
			continue
		}
		var id string
		if strings.ContainsAny(first, ":/") {

			id = strings.Replace(first, ":", "/", 1)
		} else if len(fields) >= 2 {
			id = first + "/" + fields[1]
		} else {
			continue
		}

		if slash := strings.Index(id, "/"); slash <= 0 || slash == len(id)-1 {
			continue
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		provider := ""
		if i := strings.Index(id, "/"); i > 0 {
			provider = id[:i]
		}
		models = append(models, Model{ID: id, Label: id, Provider: provider})
	}
	return models
}

func isRuntimeODiscoveryNoise(line string) bool {
	lower := strings.ToLower(line)
	if strings.Contains(lower, "no models match pattern") {
		return true
	}
	return strings.HasPrefix(lower, "warning:") ||
		strings.HasPrefix(lower, "error:") ||
		strings.HasPrefix(lower, "info:")
}

func discoverRuntimeJModels(ctx context.Context, executablePath string) ([]Model, error) {
	return discoverACPModels(ctx, executablePath, acpDiscoveryProvider{
		defaultBin:   runtimeCLIName("runtime-j"),
		clientName:   "goosar-model-discovery",
		extraEnv:     []string{"HERMES_YOLO_MODE=1"},
		tmpdirPrefix: "goosar-hermes-discovery-",
	})
}

func discoverRuntimeKModels(ctx context.Context, executablePath string) ([]Model, error) {
	return discoverACPModels(ctx, executablePath, acpDiscoveryProvider{
		defaultBin:   runtimeCLIName("runtime-k"),
		clientName:   "goosar-model-discovery",
		tmpdirPrefix: "goosar-kimi-discovery-",
	})
}

func discoverRuntimeLModels(ctx context.Context, executablePath string) ([]Model, error) {
	return discoverACPModels(ctx, executablePath, acpDiscoveryProvider{
		defaultBin:   runtimeCLIName("runtime-l"),
		clientName:   "goosar-model-discovery",
		tmpdirPrefix: "goosar-kiro-discovery-",
	})
}

func discoverRuntimeFModels(ctx context.Context, executablePath string) ([]Model, error) {
	models, err := discoverACPModels(ctx, executablePath, acpDiscoveryProvider{
		defaultBin:   runtimeCLIName("runtime-f"),
		clientName:   "goosar-model-discovery",
		tmpdirPrefix: "goosar-copilot-discovery-",
		acpArgs:      []string{"--acp"},
	})
	if err != nil || len(models) == 0 {
		return staticModelsForRuntime("runtime-f"), nil
	}
	for i := range models {
		if models[i].Provider == "" {
			models[i].Provider = inferModelVendorFromID(models[i].ID)
		}
	}
	return models, nil
}

func discoverRuntimePModels(ctx context.Context, executablePath string) ([]Model, error) {
	return discoverACPModels(ctx, executablePath, acpDiscoveryProvider{
		defaultBin:   runtimeCLIName("runtime-p"),
		clientName:   "goosar-model-discovery",
		acpArgs:      []string{"--yolo", "--acp"},
		tmpdirPrefix: "goosar-qoder-discovery-",
	})
}

type acpDiscoveryProvider struct {
	defaultBin   string
	clientName   string
	extraEnv     []string
	tmpdirPrefix string

	acpArgs []string

	selectAuthMethod func(json.RawMessage, []string) (string, error)

	strictErrors bool
}

func discoverACPModels(ctx context.Context, executablePath string, p acpDiscoveryProvider) ([]Model, error) {
	fail := func(stage string, err error) ([]Model, error) {
		if p.strictErrors {
			return nil, fmt.Errorf("ACP model discovery %s failed: %w", stage, err)
		}
		return []Model{}, nil
	}
	if executablePath == "" {
		executablePath = p.defaultBin
	}
	if _, err := exec.LookPath(executablePath); err != nil {
		return fail("executable lookup", err)
	}
	runCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	cmdArgs := p.acpArgs
	if len(cmdArgs) == 0 {
		cmdArgs = []string{"acp"}
	}
	cmd := newRuntimeCmd(exec.CommandContext(runCtx, executablePath, cmdArgs...))
	hideAgentWindow(cmd)
	childEnv := append(os.Environ(), p.extraEnv...)
	cmd.Env = childEnv
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fail("stdin setup", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return fail("stdout setup", err)
	}

	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return fail("process start", err)
	}

	defer func() {
		_ = stdin.Close()
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}()

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 1024*1024), 4*1024*1024)
	nextID := 1
	requestACP := func(method string, params map[string]any) (json.RawMessage, error) {
		id := nextID
		nextID++
		сообщение := map[string]any{
			"jsonrpc": "2.0",
			"id":      id,
			"method":  method,
			"params":  params,
		}
		data, err := json.Marshal(сообщение)
		if err != nil {
			return nil, err
		}
		data = append(data, '\n')
		if _, err = stdin.Write(data); err != nil {
			return nil, err
		}

		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			var env struct {
				ID     json.RawMessage `json:"id"`
				Result json.RawMessage `json:"result"`
				Error  json.RawMessage `json:"error"`
			}
			if err := json.Unmarshal([]byte(line), &env); err != nil || string(env.ID) != fmt.Sprint(id) {
				continue
			}
			if len(env.Error) > 0 && string(env.Error) != "null" {
				var rpcErr struct {
					Code    int             `json:"code"`
					Message string          `json:"message"`
					Data    json.RawMessage `json:"data"`
				}
				_ = json.Unmarshal(env.Error, &rpcErr)
				detail := ""
				if len(rpcErr.Data) > 0 && string(rpcErr.Data) != "null" {
					if err := json.Unmarshal(rpcErr.Data, &detail); err != nil {
						detail = strings.TrimSpace(string(rpcErr.Data))
					}
				}
				return nil, &acpRPCError{Method: method, Code: rpcErr.Code, Message: rpcErr.Message, Data: detail}
			}
			if len(env.Result) == 0 {
				return nil, fmt.Errorf("response contained neither result nor error")
			}
			return env.Result, nil
		}
		if err := scanner.Err(); err != nil {
			return nil, err
		}
		if err := runCtx.Err(); err != nil {
			return nil, err
		}
		return nil, io.ErrUnexpectedEOF
	}

	initResult, err := requestACP("initialize", map[string]any{
		"protocolVersion":    1,
		"clientInfo":         map[string]any{"name": p.clientName, "version": "0.1.0"},
		"clientCapabilities": map[string]any{},
	})
	if err != nil {
		return fail("initialize", err)
	}

	tmp, err := os.MkdirTemp("", p.tmpdirPrefix)
	if err != nil {
		return fail("temporary cwd", err)
	}
	defer os.RemoveAll(tmp)

	if p.selectAuthMethod != nil {
		methodID, err := p.selectAuthMethod(initResult, childEnv)
		if err != nil {
			return fail("auth method selection", err)
		}
		if _, err := requestACP("authenticate", map[string]any{
			"methodId": methodID,
			"_meta":    map[string]any{"headless": true},
		}); err != nil {
			return fail(fmt.Sprintf("authenticate (%s)", methodID), err)
		}
	}

	sessionResult, err := requestACP("session/new", map[string]any{
		"cwd":        tmp,
		"mcpServers": []any{},
	})
	if err != nil {
		return fail("session/new", err)
	}
	models := parseACPSessionNewModels(sessionResult)
	if len(models) == 0 {

		slog.Debug("ACP model discovery found no models in session/new response",
			"binary", executablePath,
			"result_keys", strings.Join(acpResultTopLevelKeys(sessionResult), ","),
		)
	}
	if models == nil {
		return fail("session/new model parsing", fmt.Errorf("response contained no model catalog"))
	}
	if err := runCtx.Err(); err != nil {
		return fail("completion", err)
	}
	return models, nil
}

func parseACPSessionNewModels(raw json.RawMessage) []Model {
	type acpModelInfo struct {
		ModelID      string `json:"modelId"`
		ModelIDSnake string `json:"model_id"`
		Name         string `json:"name"`
		Description  string `json:"description"`
	}
	var resp struct {
		Models struct {
			AvailableModels      []acpModelInfo `json:"availableModels"`
			AvailableModelsSnake []acpModelInfo `json:"available_models"`
			CurrentModelID       string         `json:"currentModelId"`
			CurrentModelIDSnake  string         `json:"current_model_id"`
		} `json:"models"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil
	}
	availableModels := resp.Models.AvailableModels
	if len(availableModels) == 0 && resp.Models.AvailableModelsSnake != nil {
		availableModels = resp.Models.AvailableModelsSnake
	}
	currentModelID := strings.TrimSpace(resp.Models.CurrentModelID)
	if currentModelID == "" {
		currentModelID = strings.TrimSpace(resp.Models.CurrentModelIDSnake)
	}
	models := make([]Model, 0, len(availableModels))
	seen := map[string]bool{}
	for _, m := range availableModels {
		modelID := strings.TrimSpace(m.ModelID)
		if modelID == "" {
			modelID = strings.TrimSpace(m.ModelIDSnake)
		}
		if modelID == "" || seen[modelID] {
			continue
		}
		seen[modelID] = true
		models = append(models, acpModelEntry(modelID, m.Name, currentModelID))
	}
	if len(models) > 0 {
		return models
	}
	if fromConfig := parseACPConfigOptionModels(raw); len(fromConfig) > 0 {
		return fromConfig
	}
	return models
}

func parseACPConfigOptionModels(raw json.RawMessage) []Model {
	type acpConfigChoice struct {
		Value string `json:"value"`
		Name  string `json:"name"`
	}
	type acpConfigOption struct {
		ID                string            `json:"id"`
		Category          string            `json:"category"`
		CurrentValue      string            `json:"currentValue"`
		CurrentValueSnake string            `json:"current_value"`
		Options           []acpConfigChoice `json:"options"`
	}
	var resp struct {
		ConfigOptions      []acpConfigOption `json:"configOptions"`
		ConfigOptionsSnake []acpConfigOption `json:"config_options"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil
	}
	configOptions := resp.ConfigOptions
	if len(configOptions) == 0 {
		configOptions = resp.ConfigOptionsSnake
	}
	for _, opt := range configOptions {
		if !strings.EqualFold(strings.TrimSpace(opt.ID), "model") &&
			!strings.EqualFold(strings.TrimSpace(opt.Category), "model") {
			continue
		}
		currentValue := strings.TrimSpace(opt.CurrentValue)
		if currentValue == "" {
			currentValue = strings.TrimSpace(opt.CurrentValueSnake)
		}
		models := make([]Model, 0, len(opt.Options))
		seen := map[string]bool{}
		for _, choice := range opt.Options {
			modelID := strings.TrimSpace(choice.Value)
			if modelID == "" || seen[modelID] {
				continue
			}
			seen[modelID] = true
			models = append(models, acpModelEntry(modelID, choice.Name, currentValue))
		}
		if len(models) > 0 {
			return models
		}
	}
	return nil
}

func acpModelEntry(modelID, name, currentModelID string) Model {
	provider := ""
	if idx := strings.Index(modelID, ":"); idx > 0 {
		provider = modelID[:idx]
	}
	return Model{
		ID:       modelID,
		Label:    acpModelLabel(name, modelID),
		Provider: provider,
		Default:  modelID == currentModelID,
	}
}

func acpResultTopLevelKeys(raw json.RawMessage) []string {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil
	}
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func acpModelLabel(name, modelID string) string {
	label := strings.TrimSpace(name)
	if label == "" || strings.EqualFold(label, "unknown") {
		return modelID
	}
	return label
}

func discoverRuntimeAModels(ctx context.Context, executablePath string) ([]Model, error) {
	if executablePath == "" {
		executablePath = runtimeCLIName("runtime-a")
	}
	if _, err := exec.LookPath(executablePath); err != nil {
		return nil, nil
	}

	runCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := newRuntimeCmd(exec.CommandContext(runCtx, executablePath, "models"))
	hideAgentWindow(cmd)
	out, err := cmd.Output()
	if err != nil && len(out) == 0 {
		return nil, nil
	}
	return parseRuntimeAModels(string(out)), nil
}

func parseRuntimeAModels(output string) []Model {
	scanner := bufio.NewScanner(strings.NewReader(output))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var models []Model
	seen := map[string]bool{}
	for scanner.Scan() {
		name := strings.TrimSpace(scanner.Text())
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		models = append(models, Model{
			ID:       name,
			Label:    name,
			Provider: "antigravity",
		})
	}
	return models
}

func discoverRuntimeIModels(ctx context.Context, executablePath string) ([]Model, error) {

	models, err := discoverACPModels(ctx, executablePath, acpDiscoveryProvider{
		defaultBin:   runtimeCLIName("runtime-i"),
		clientName:   "goosar-model-discovery",
		tmpdirPrefix: "goosar-grok-discovery-",
		acpArgs:      []string{"--no-auto-update", "agent", "--always-approve", "stdio"},
		selectAuthMethod: func(initResult json.RawMessage, childEnv []string) (string, error) {
			return selectRuntimeIAuthMethod(extractACPAuthMethods(initResult), envHasNonEmpty(childEnv, "XAI_API_KEY"))
		},
		strictErrors: true,
	})
	if err != nil || len(models) == 0 {
		if err != nil {
			slog.Debug("runtime-i model discovery fell back to the registry catalog", "error", err)
		}
		return staticModelsForRuntime("runtime-i"), nil
	}
	for i := range models {
		if models[i].Provider == "" {
			models[i].Provider = "xai"
		}
	}
	annotateRuntimeIThinking(models)
	return models, nil
}

func annotateRuntimeIThinking(models []Model) {
	for i := range models {
		if models[i].ID != "grok-4.5" {
			continue
		}
		models[i].Thinking = &ModelThinking{SupportedLevels: []ThinkingLevel{
			{Value: "low", Label: "Low"},
			{Value: "medium", Label: "Medium"},
			{Value: "high", Label: "High"},
		}}
	}
}

func discoverRuntimeGModels(ctx context.Context, executablePath string) ([]Model, error) {
	if executablePath == "" {
		executablePath = runtimeCLIName("runtime-g")
	}
	if _, err := exec.LookPath(executablePath); err != nil {
		return staticModelsForRuntime("runtime-g"), nil
	}

	runCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := newRuntimeCmd(exec.CommandContext(runCtx, executablePath, "--list-models"))
	hideAgentWindow(cmd)
	out, err := cmd.Output()
	if err != nil && len(out) == 0 {
		return staticModelsForRuntime("runtime-g"), nil
	}
	models := parseRuntimeGModels(string(out))
	if len(models) == 0 {
		return staticModelsForRuntime("runtime-g"), nil
	}
	return models, nil
}

func parseRuntimeGModels(output string) []Model {
	scanner := bufio.NewScanner(strings.NewReader(output))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var models []Model
	seen := map[string]bool{}
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		idx := strings.Index(line, " - ")
		if idx <= 0 {
			continue
		}
		id := strings.TrimSpace(line[:idx])
		label := strings.TrimSpace(line[idx+3:])
		if !isRuntimeNIdentifier(id) {

			continue
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		isDefault := strings.Contains(label, "default")

		if paren := strings.Index(label, "("); paren > 0 {
			label = strings.TrimSpace(label[:paren])
		}
		if label == "" {
			label = id
		}
		models = append(models, Model{
			ID:       id,
			Label:    label,
			Provider: "cursor",
			Default:  isDefault,
		})
	}
	return models
}

func discoverRuntimeNAgents(ctx context.Context, executablePath string) ([]Model, error) {
	if executablePath == "" {
		executablePath = runtimeCLIName("runtime-n")
	}
	if _, err := exec.LookPath(executablePath); err != nil {
		return []Model{}, nil
	}
	runCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	for _, jsonArgs := range [][]string{
		{"agents", "list", "--json"},
		{"agents", "list", "--output", "json"},
		{"agents", "list", "-o", "json"},
	} {
		cmd := newRuntimeCmd(exec.CommandContext(runCtx, executablePath, jsonArgs...))
		hideAgentWindow(cmd)
		out, err := cmd.Output()
		if err != nil && len(out) == 0 {
			continue
		}
		if models, ok := parseRuntimeNAgentsJSON(out); ok {
			return models, nil
		}
	}

	cmd := newRuntimeCmd(exec.CommandContext(runCtx, executablePath, "agents", "list"))
	hideAgentWindow(cmd)
	out, err := cmd.Output()
	if err != nil && len(out) == 0 {
		return []Model{}, nil
	}
	return parseRuntimeNAgents(string(out)), nil
}

type runtimeNAgentEntry struct {
	Name  string `json:"name"`
	ID    string `json:"id"`
	Model string `json:"model"`
}

func parseRuntimeNAgentsJSON(raw []byte) ([]Model, bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return nil, false
	}

	var flat []runtimeNAgentEntry
	if err := json.Unmarshal(raw, &flat); err == nil {
		return runtimeNEntriesToModels(flat), true
	}

	var wrapped struct {
		Agents []runtimeNAgentEntry `json:"agents"`
	}
	if err := json.Unmarshal(raw, &wrapped); err == nil && wrapped.Agents != nil {
		return runtimeNEntriesToModels(wrapped.Agents), true
	}

	return nil, false
}

func runtimeNEntriesToModels(entries []runtimeNAgentEntry) []Model {
	models := make([]Model, 0, len(entries))
	seen := map[string]bool{}
	for _, e := range entries {

		id := e.ID
		if id == "" {
			id = e.Name
		}
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		displayName := e.Name
		if displayName == "" {
			displayName = id
		}
		label := displayName
		if e.Model != "" {
			label = displayName + " (" + e.Model + ")"
		}
		models = append(models, Model{ID: id, Label: label, Provider: "openclaw"})
	}
	return models
}

func parseRuntimeNAgents(output string) []Model {
	scanner := bufio.NewScanner(strings.NewReader(output))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var models []Model
	seen := map[string]bool{}
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		name, model := fields[0], fields[1]
		if !isRuntimeNIdentifier(name) || !isRuntimeNIdentifier(model) {
			continue
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		models = append(models, Model{
			ID:       name,
			Label:    name + " (" + model + ")",
			Provider: "openclaw",
		})
	}
	return models
}

func isRuntimeNIdentifier(s string) bool {
	if s == "" || strings.HasSuffix(s, ":") {
		return false
	}
	first := s[0]
	if !((first >= 'a' && first <= 'z') || (first >= 'A' && first <= 'Z')) {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '-' || r == '_' || r == '.' || r == '/':
		default:
			return false
		}
	}
	return true
}

var runtimeDModelRe = regexp.MustCompile(`--model\s*<[^>]+>\s*.*?Currently supported:\s*\(([^)]+)\)`)

func discoverRuntimeDModels(ctx context.Context, executablePath string) ([]Model, error) {
	if executablePath == "" {
		executablePath = runtimeCLIName("runtime-d")
	}
	if _, err := exec.LookPath(executablePath); err != nil {
		return staticModelsForRuntime("runtime-d"), nil
	}
	helpOut := runtimeDHelpOutput(ctx, executablePath)
	if helpOut == "" {
		return staticModelsForRuntime("runtime-d"), nil
	}
	models := parseRuntimeDModels(helpOut)
	if len(models) == 0 {
		return staticModelsForRuntime("runtime-d"), nil
	}
	return models, nil
}

func parseRuntimeDModels(helpOutput string) []Model {
	match := runtimeDModelRe.FindStringSubmatch(helpOutput)
	if len(match) < 2 {
		return nil
	}
	raw := strings.Split(match[1], ",")
	var models []Model
	for _, s := range raw {
		id := strings.TrimSpace(s)
		if id == "" {
			continue
		}
		models = append(models, Model{
			ID:       id,
			Label:    runtimeDModelLabel(id),
			Provider: inferModelVendorFromExtendedID(id),
			Default:  len(models) == 0,
		})
	}
	return models
}

func inferModelVendorFromExtendedID(id string) string {
	switch {
	case strings.HasPrefix(id, "claude-"):
		return "anthropic"
	case strings.HasPrefix(id, "gemini-"):
		return "google"
	case strings.HasPrefix(id, "gpt-"):
		return "openai"
	case strings.HasPrefix(id, "glm-"):
		return "zhipu"
	case strings.HasPrefix(id, "minimax-"):
		return "minimax"
	case strings.HasPrefix(id, "kimi-"):
		return "kimi"
	case len(id) >= 3 && id[0] == 'h' && id[1] == 'y' && id[2] >= '0' && id[2] <= '9':
		return "hunyuan"
	case strings.HasPrefix(id, "deepseek-"):
		return "deepseek"
	default:
		return ""
	}
}

func runtimeDModelLabel(id string) string {
	parts := strings.Split(id, "-")
	for i, p := range parts {
		if strings.EqualFold(p, "gpt") || strings.EqualFold(p, "glm") {
			parts[i] = strings.ToUpper(p)
		} else if strings.EqualFold(p, "ioa") {
			parts[i] = "IOA"
		} else if len(p) > 0 {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return strings.Join(parts, " ")
}
