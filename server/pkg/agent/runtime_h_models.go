package agent

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

const runtimeHModelsDiscoveryTimeout = 15 * time.Second

func discoverRuntimeHModels(ctx context.Context, executablePath string) ([]Model, error) {
	binary := executablePath
	if binary == "" {
		binary = runtimeCLIName("runtime-h")
	}
	if _, err := exec.LookPath(binary); err != nil {
		return []Model{}, nil
	}

	runCtx, cancel := context.WithTimeout(ctx, runtimeHModelsDiscoveryTimeout)
	defer cancel()

	cmd := newRuntimeCmd(exec.CommandContext(runCtx, binary, "models"))
	hideAgentWindow(cmd)
	out, _ := cmd.Output()

	if models := parseRuntimeHModels(string(out)); len(models) > 0 {
		return models, nil
	}
	return []Model{}, nil
}

func runtimeHModelIDLine(line string) (id string, ok bool) {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return "", false
	}
	id = fields[0]
	switch {
	case strings.HasPrefix(id, "{"), strings.HasPrefix(id, "["), strings.HasPrefix(id, "\""):
		return "", false
	case !strings.Contains(id, "/"):
		return "", false
	case id == strings.ToUpper(id):
		return "", false
	}
	return id, true
}

func parseRuntimeHModels(output string) []Model {
	seen := make(map[string]bool)
	models := make([]Model, 0)
	for _, rawLine := range strings.Split(output, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		id, ok := runtimeHModelIDLine(line)
		if !ok || seen[id] {
			continue
		}
		seen[id] = true

		provider := ""
		if slash := strings.Index(id, "/"); slash > 0 {
			provider = id[:slash]
		}
		models = append(models, Model{ID: id, Label: id, Provider: provider})
	}
	return models
}
