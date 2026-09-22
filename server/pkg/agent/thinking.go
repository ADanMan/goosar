package agent

import (
	"context"
	"encoding/json"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
)

type thinkingCacheKey struct {
	provider       string
	executablePath string
	cliVersion     string
}

type thinkingCacheEntry struct {
	value     map[string]*ModelThinking
	expiresAt time.Time
}

const thinkingDiscoveryTTL = 10 * time.Minute

var (
	thinkingCacheMu sync.Mutex
	thinkingCache   = map[thinkingCacheKey]thinkingCacheEntry{}
)

func thinkingCacheGet(key thinkingCacheKey) (map[string]*ModelThinking, bool) {
	thinkingCacheMu.Lock()
	defer thinkingCacheMu.Unlock()
	entry, ok := thinkingCache[key]
	if !ok || time.Now().After(entry.expiresAt) {
		return nil, false
	}
	return entry.value, true
}

func thinkingCachePut(key thinkingCacheKey, value map[string]*ModelThinking) {
	thinkingCacheMu.Lock()
	defer thinkingCacheMu.Unlock()
	thinkingCache[key] = thinkingCacheEntry{value: value, expiresAt: time.Now().Add(thinkingDiscoveryTTL)}
}

func resetThinkingCacheForTests() {
	thinkingCacheMu.Lock()
	thinkingCache = map[thinkingCacheKey]thinkingCacheEntry{}
	thinkingCacheMu.Unlock()
}

var runtimeCEffortRe = regexp.MustCompile(`--effort\s*(?:<[^>]+>)?\s*(?:Effort level[^(]*)?\(([^)]+)\)`)

var runtimeCEffortLabel = map[string]string{
	"low":    "Low",
	"medium": "Medium",
	"high":   "High",
	"xhigh":  "Extra high",
	"max":    "Max",
}

var runtimeCModelEffortAllow = map[string]map[string]bool{

	"claude-opus-5":             {"low": true, "medium": true, "high": true, "xhigh": true, "max": true},
	"claude-opus-4-8":           {"low": true, "medium": true, "high": true, "xhigh": true, "max": true},
	"claude-opus-4-7":           {"low": true, "medium": true, "high": true, "xhigh": true, "max": true},
	"claude-opus-4-6":           {"low": true, "medium": true, "high": true, "xhigh": true, "max": true},
	"claude-sonnet-4-6":         {"low": true, "medium": true, "high": true, "max": true},
	"claude-sonnet-4-5":         {"low": true, "medium": true, "high": true, "max": true},
	"claude-haiku-4-5-20251001": {"low": true, "medium": true, "high": true},
}

var runtimeCStaticEffortFallback = []string{"low", "medium", "high"}

var runtimeCStaticEffortFullSuperset = []string{"low", "medium", "high", "xhigh", "max"}

func annotateRuntimeCThinking(ctx context.Context, models []Model, executablePath string) {
	mapping := loadRuntimeCThinkingByModel(ctx, executablePath)
	for i := range models {
		if t, ok := mapping[models[i].ID]; ok && t != nil {
			models[i].Thinking = t
		}
	}
}

func loadRuntimeCThinkingByModel(ctx context.Context, executablePath string) map[string]*ModelThinking {
	if executablePath == "" {
		executablePath = runtimeCLIName("runtime-c")
	}
	version, _ := DetectVersion(ctx, executablePath)
	key := thinkingCacheKey{provider: "claude", executablePath: executablePath, cliVersion: version}
	if cached, ok := thinkingCacheGet(key); ok {
		return cached
	}

	superset := runtimeCEffortSuperset(ctx, executablePath)
	result := map[string]*ModelThinking{}
	for _, m := range staticModelsForRuntime("runtime-c") {
		allow := runtimeCModelEffortAllow[m.ID]
		levels := projectRuntimeCLevels(superset, allow)
		if len(levels) == 0 {
			continue
		}
		result[m.ID] = &ModelThinking{
			SupportedLevels: levels,
			DefaultLevel:    "medium",
		}
	}
	thinkingCachePut(key, result)
	return result
}

func runtimeCEffortSuperset(ctx context.Context, executablePath string) []string {
	cmd := newRuntimeCmd(exec.CommandContext(ctx, executablePath, "--help"))
	hideAgentWindow(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return append([]string(nil), runtimeCStaticEffortFallback...)
	}
	return runtimeCEffortLevelsFromHelp(string(out))
}

func runtimeCEffortLevelsFromHelp(helpText string) []string {
	parsed := parseRuntimeCEffortHelp(helpText)
	if len(parsed) > 0 {
		return parsed
	}
	if strings.Contains(helpText, "--effort") {
		return append([]string(nil), runtimeCStaticEffortFullSuperset...)
	}
	return nil
}

func parseRuntimeCEffortHelp(helpText string) []string {
	match := runtimeCEffortRe.FindStringSubmatch(helpText)
	if len(match) < 2 {
		return nil
	}
	var out []string
	for _, raw := range strings.Split(match[1], ",") {
		token := strings.TrimSpace(raw)
		if token == "" {
			continue
		}
		out = append(out, token)
	}
	return out
}

func projectRuntimeCLevels(superset []string, allow map[string]bool) []ThinkingLevel {
	out := make([]ThinkingLevel, 0, len(superset))
	for _, value := range superset {
		if allow != nil && !allow[value] {
			continue
		}
		label, ok := runtimeCEffortLabel[value]
		if !ok {

			label = strings.Title(value) //nolint:staticcheck
		}
		out = append(out, ThinkingLevel{Value: value, Label: label})
	}
	return out
}

var runtimeEEffortLabel = map[string]string{
	"none":    "None",
	"minimal": "Minimal",
	"low":     "Low",
	"medium":  "Medium",
	"high":    "High",
	"xhigh":   "Extra high",
	"max":     "Max",
	"ultra":   "Ultra",
}

const minRuntimeEDebugModelsVersion = "0.122.0"

type runtimeEDebugModelsResponse struct {
	Models []runtimeEDebugModel `json:"models"`
}

type runtimeEDebugModel struct {
	Slug                    string                        `json:"slug"`
	DisplayName             string                        `json:"display_name"`
	Visibility              string                        `json:"visibility"`
	DefaultReasoningLevel   string                        `json:"default_reasoning_level"`
	SupportedReasoningLevel []runtimeEDebugReasoningLevel `json:"supported_reasoning_levels"`
	ServiceTiers            []runtimeEDebugServiceTier    `json:"service_tiers"`
}

type runtimeEDebugReasoningLevel struct {
	Effort      string `json:"effort"`
	Description string `json:"description"`
}

type runtimeEDebugServiceTier struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

func discoverRuntimeEModels(ctx context.Context, executablePath string) []Model {
	if executablePath == "" {
		executablePath = runtimeCLIName("runtime-e")
	}
	version, err := DetectVersion(ctx, executablePath)
	if err != nil || !runtimeESupportsDebugModels(version) {
		return staticModelsForRuntime("runtime-e")
	}

	raw, err := runRuntimeEDebugModels(ctx, executablePath)
	if err != nil {
		return staticModelsForRuntime("runtime-e")
	}
	models, err := parseRuntimeEModelCatalog(raw)
	if err != nil || len(models) == 0 {
		return staticModelsForRuntime("runtime-e")
	}
	return models
}

func runtimeESupportsDebugModels(version string) bool {
	parsed, err := parseSemver(version)
	if err != nil {
		return false
	}
	minimum, err := parseSemver(minRuntimeEDebugModelsVersion)
	if err != nil {
		return false
	}
	return !parsed.lessThan(minimum)
}

var runtimeEDebugModelsArgs = []string{"debug", "models", "--bundled"}

func runRuntimeEDebugModels(ctx context.Context, executablePath string) ([]byte, error) {
	cmd := newRuntimeCmd(exec.CommandContext(ctx, executablePath, runtimeEDebugModelsArgs...))
	hideAgentWindow(cmd)
	return cmd.Output()
}

func parseRuntimeEModelCatalog(raw []byte) ([]Model, error) {
	var resp runtimeEDebugModelsResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, err
	}
	models := make([]Model, 0, len(resp.Models))
	for _, m := range resp.Models {
		if m.Slug == "" || m.Visibility == "hide" {
			continue
		}
		label := m.DisplayName
		if label == "" {
			label = m.Slug
		}
		models = append(models, Model{
			ID:           m.Slug,
			Label:        label,
			Provider:     "openai",
			Thinking:     runtimeEThinkingFromDebugModel(m),
			ServiceTiers: runtimeEServiceTiersFromDebugModel(m),
		})
	}
	if len(models) > 0 {
		models[0].Default = true
	}
	return models, nil
}

func runtimeEServiceTiersFromDebugModel(m runtimeEDebugModel) []ModelServiceTier {
	tiers := make([]ModelServiceTier, 0, len(m.ServiceTiers))
	for _, tier := range m.ServiceTiers {
		if tier.ID == "" {
			continue
		}
		name := tier.Name
		if name == "" {
			name = tier.ID
		}
		tiers = append(tiers, ModelServiceTier{
			ID:          tier.ID,
			Name:        name,
			Description: tier.Description,
		})
	}
	return tiers
}

func runtimeEThinkingFromDebugModel(m runtimeEDebugModel) *ModelThinking {
	levels := make([]ThinkingLevel, 0, len(m.SupportedReasoningLevel))
	for _, lvl := range m.SupportedReasoningLevel {
		if lvl.Effort == "" {
			continue
		}
		label, ok := runtimeEEffortLabel[lvl.Effort]
		if !ok {

			label = strings.Title(lvl.Effort) //nolint:staticcheck
		}
		levels = append(levels, ThinkingLevel{
			Value:       lvl.Effort,
			Label:       label,
			Description: lvl.Description,
		})
	}
	if len(levels) == 0 {
		return nil
	}
	return &ModelThinking{
		SupportedLevels: levels,
		DefaultLevel:    m.DefaultReasoningLevel,
	}
}

var runtimeDEffortRe = regexp.MustCompile(`--effort\s*(?:<[^>]+>)?\s*[^(]*\(([^)]+)\)`)

var runtimeDEffortLabel = map[string]string{
	"low":    "Low",
	"medium": "Medium",
	"high":   "High",
	"xhigh":  "Extra high",
}

var runtimeDStaticEffortFallback = []string{"low", "medium", "high", "xhigh"}

var (
	runtimeDHelpMu    sync.Mutex
	runtimeDHelpStore = map[string]runtimeDHelpEntry{}
)

const runtimeDHelpTTL = 60 * time.Second

type runtimeDHelpEntry struct {
	output    string
	expiresAt time.Time
}

func runtimeDHelpOutput(ctx context.Context, executablePath string) string {
	if executablePath == "" {
		executablePath = runtimeCLIName("runtime-d")
	}
	key := executablePath
	runtimeDHelpMu.Lock()
	if entry, ok := runtimeDHelpStore[key]; ok && time.Now().Before(entry.expiresAt) {
		runtimeDHelpMu.Unlock()
		return entry.output
	}
	runtimeDHelpMu.Unlock()

	runCtx, cancel := context.WithTimeout(ctx, 35*time.Second)
	defer cancel()
	cmd := newRuntimeCmd(exec.CommandContext(runCtx, executablePath, "--help"))
	hideAgentWindow(cmd)
	out, _ := cmd.CombinedOutput()
	result := string(out)

	if result != "" {
		runtimeDHelpMu.Lock()
		runtimeDHelpStore[key] = runtimeDHelpEntry{output: result, expiresAt: time.Now().Add(runtimeDHelpTTL)}
		runtimeDHelpMu.Unlock()
	}
	return result
}

func annotateRuntimeDThinking(ctx context.Context, models []Model, executablePath string) {
	if executablePath == "" {
		executablePath = runtimeCLIName("runtime-d")
	}
	version, _ := DetectVersion(ctx, executablePath)
	key := thinkingCacheKey{provider: "codebuddy", executablePath: executablePath, cliVersion: version}
	if cached, ok := thinkingCacheGet(key); ok {
		for i := range models {
			if t, ok := cached[models[i].ID]; ok && t != nil {
				models[i].Thinking = t
			}
		}
		return
	}

	levels := runtimeDEffortSuperset(ctx, executablePath)
	thinkingLevels := make([]ThinkingLevel, 0, len(levels))
	for _, value := range levels {
		label, ok := runtimeDEffortLabel[value]
		if !ok {
			label = strings.Title(value) //nolint:staticcheck
		}
		thinkingLevels = append(thinkingLevels, ThinkingLevel{Value: value, Label: label})
	}

	result := map[string]*ModelThinking{}
	if len(thinkingLevels) > 0 {
		thinking := &ModelThinking{
			SupportedLevels: thinkingLevels,
			DefaultLevel:    "medium",
		}
		for _, m := range models {
			result[m.ID] = thinking
		}
	}
	thinkingCachePut(key, result)

	for i := range models {
		if t, ok := result[models[i].ID]; ok && t != nil {
			models[i].Thinking = t
		}
	}
}

func runtimeDEffortSuperset(ctx context.Context, executablePath string) []string {
	helpOut := runtimeDHelpOutput(ctx, executablePath)
	if helpOut == "" {
		return append([]string(nil), runtimeDStaticEffortFallback...)
	}
	parsed := parseRuntimeDEffortHelp(helpOut)
	if len(parsed) == 0 {
		return append([]string(nil), runtimeDStaticEffortFallback...)
	}
	return parsed
}

func parseRuntimeDEffortHelp(helpText string) []string {
	match := runtimeDEffortRe.FindStringSubmatch(helpText)
	if len(match) < 2 {
		return nil
	}
	var out []string
	for _, raw := range strings.Split(match[1], ",") {
		token := strings.TrimSpace(raw)
		if token == "" {
			continue
		}
		out = append(out, token)
	}
	return out
}

func ValidateThinkingLevel(ctx context.Context, runtimeCode, executablePath, model, value string) (bool, error) {
	if value == "" {
		return true, nil
	}

	if model == "" && runtimeCode == "runtime-e" {
		return false, nil
	}
	models, err := ListModels(ctx, runtimeCode, executablePath)
	if err != nil {
		return false, err
	}
	target := model
	if target == "" {

		for _, m := range models {
			if m.Default {
				target = m.ID
				break
			}
		}
		if target == "" {
			if runtimeCode == "runtime-m" {
				return anyModelSupportsThinkingValue(models, value), nil
			}
			return false, nil
		}
	}
	for _, m := range models {
		if m.ID != target {
			continue
		}
		if m.Thinking == nil {
			return false, nil
		}
		for _, lvl := range m.Thinking.SupportedLevels {
			if lvl.Value == value {
				return true, nil
			}
		}
		return false, nil
	}
	return false, nil
}

func ValidateServiceTier(ctx context.Context, runtimeCode, executablePath, model, value string) (bool, error) {
	if value == "" {
		return true, nil
	}
	if runtimeCode != "runtime-e" || model == "" {
		return false, nil
	}
	models, err := ListModels(ctx, runtimeCode, executablePath)
	if err != nil {
		return false, err
	}
	for _, m := range models {
		if m.ID != model {
			continue
		}
		for _, tier := range m.ServiceTiers {
			if tier.ID == value {
				return true, nil
			}
		}
		return false, nil
	}
	return false, nil
}

func anyModelSupportsThinkingValue(models []Model, value string) bool {
	for _, m := range models {
		if m.Thinking == nil {
			continue
		}
		for _, lvl := range m.Thinking.SupportedLevels {
			if lvl.Value == value {
				return true
			}
		}
	}
	return false
}

var runtimeThinkingEnums = map[string]map[string]bool{
	"runtime-c": {
		"low":    true,
		"medium": true,
		"high":   true,
		"xhigh":  true,
		"max":    true,
	},
	"runtime-d": {
		"low":    true,
		"medium": true,
		"high":   true,
		"xhigh":  true,
	},

	"runtime-i": {
		"low":    true,
		"medium": true,
		"high":   true,
	},
}

func IsKnownThinkingValue(runtimeCode, value string) bool {
	if value == "" {
		return true
	}
	if runtimeCode == "runtime-e" || runtimeCode == "runtime-m" {
		return isValidDynamicThinkingValue(value)
	}
	enum, ok := runtimeThinkingEnums[runtimeCode]
	if !ok {
		return false
	}
	return enum[value]
}

func IsKnownServiceTier(runtimeCode, value string) bool {
	if value == "" {
		return true
	}
	return runtimeCode == "runtime-e" && isValidDynamicThinkingValue(value)
}

func isValidDynamicThinkingValue(value string) bool {
	if len(value) > 64 {
		return false
	}
	for i, r := range value {
		valid := r >= 'a' && r <= 'z' ||
			r >= 'A' && r <= 'Z' ||
			r >= '0' && r <= '9' ||
			r == '-' || r == '_' || r == '.'
		if !valid {
			return false
		}
		if i == 0 && (r == '-' || r == '_' || r == '.') {
			return false
		}
	}
	return true
}
