// Пакет preflight — матрица зависимостей для goosar doctor: что нужно машине
// с демоном, есть ли это и что именно выполнить, если нет, включая закрытый
// контур без интернета. Пользовательские CLI агентов запускаются только с --version.
package preflight

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/adanman/goosar/server/pkg/agent/runtimeregistry"
)

const (
	StatusOK       = "ok"
	StatusMissing  = "missing"
	StatusOutdated = "outdated"

	StatusSkipped = "skipped"
)

const MinNodeMajor = 20

var versionTimeout = 5 * time.Second

var pathProbedRuntimes = []string{"runtime-c", "runtime-e", "runtime-m"}

func runtimeCLIName(code string) string {
	reg, err := runtimeregistry.LoadDefault()
	if err != nil {
		return ""
	}
	d, _ := reg.ByCode(code)
	return d.CLIName
}

func runtimePathEnv(code string) string {
	return "GOOSAR_RUNTIME_" + strings.ToUpper(strings.TrimPrefix(code, "runtime-")) + "_PATH"
}

type Result struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Status  string `json:"status"`
	Version string `json:"version,omitempty"`
	Path    string `json:"path,omitempty"`

	Detail string `json:"detail,omitempty"`

	Message string `json:"message"`

	Fix string `json:"fix"`
}

type Report struct {
	OK      bool     `json:"ok"`
	Results []Result `json:"results"`
}

func (r Report) Failed() []Result {
	var out []Result
	for _, res := range r.Results {
		if res.Status == StatusMissing || res.Status == StatusOutdated {
			out = append(out, res)
		}
	}
	return out
}

type Options struct {
	Self *SelfInfo

	RequireKerberos bool

	MCPConfigPaths []string

	MCPServersDir string

	lookPath   func(string) (string, error)
	runVersion func(context.Context, string) (string, error)
}

func (o Options) lookup() func(string) (string, error) {
	if o.lookPath != nil {
		return o.lookPath
	}
	return exec.LookPath
}

func (o Options) version() func(context.Context, string) (string, error) {
	if o.runVersion != nil {
		return o.runVersion
	}
	return probeVersion
}

func Run(ctx context.Context, opts Options) Report {
	report := Report{}
	report.Results = append(report.Results,
		checkGoosarOnPath(ctx, opts),
		checkAgentCLI(ctx, opts),
		checkNode(ctx, opts),
		checkTool(ctx, opts, toolSpec{
			id: "npm", name: "npm",
			message: "npm ставит и запускает stdio-серверы MCP из поставки.",
			fix:     "Ставится вместе с Node.js. В контуре: тот же офлайн-пакет Node, что и выше — npm лежит внутри.",
		}),
		checkTool(ctx, opts, toolSpec{
			id: "git", name: "git",
			message: "Git нужен агенту для работы с репозиториями задач.",
			fix:     "macOS: xcode-select --install. В контуре: пакет git с внутреннего зеркала (brew/apt/rpm), интернет не требуется.",
		}),
		checkKerberos(ctx, opts),
		checkDocker(ctx, opts),
		checkMCPPackages(ctx, opts),
	)
	report.OK = len(report.Failed()) == 0
	return report
}

func checkAgentCLI(ctx context.Context, opts Options) Result {
	reported := append([]string{}, pathProbedRuntimes...)
	res := Result{
		ID:   "agent-cli",
		Name: "Agent CLI (" + strings.Join(reported, " / ") + ")",
		Message: "Демон запускает задачи через CLI агента. Нужен хотя бы один: " +
			strings.Join(reported, ", ") + ".",
		Fix: "Поставьте CLI одного из рантаймов на машину. В контуре без интернета: возьмите архив CLI с внутреннего зеркала " +
			"(см. раздел «Закрытый контур (offline-поставка)» в SELF_HOSTING.md) и распакуйте в PATH, " +
			"либо укажите путь явно через соответствующий GOOSAR_RUNTIME_<БУКВА>_PATH (" +
			strings.Join(runtimePathEnvs(reported), " / ") + ").",
	}

	var found []string
	for _, code := range pathProbedRuntimes {
		cliName := runtimeCLIName(code)
		if cliName == "" {
			continue
		}
		path, err := opts.lookup()(cliName)
		if err != nil {
			continue
		}
		version := firstVersion(ctx, opts, path)
		if res.Path == "" {
			res.Path = path
			res.Version = version
		}
		if version != "" {
			found = append(found, code+" "+version)
		} else {
			found = append(found, code)
		}
	}

	if len(found) == 0 {
		res.Status = StatusMissing
		return res
	}
	res.Status = StatusOK
	res.Detail = strings.Join(found, ", ")
	return res
}

func runtimePathEnvs(codes []string) []string {
	out := make([]string, 0, len(codes))
	for _, code := range codes {
		out = append(out, runtimePathEnv(code))
	}
	return out
}

func checkNode(ctx context.Context, opts Options) Result {
	res := Result{
		ID:      "node",
		Name:    "Node.js",
		Message: "Node нужен для запуска серверов MCP из поставки. Минимальная версия — " + strconv.Itoa(MinNodeMajor) + ".",
		Fix: "Поставьте Node LTS. В контуре без интернета: распакуйте офлайн-архив Node с внутреннего зеркала " +
			"и добавьте его bin/ в PATH — установщик и доступ в интернет не нужны.",
	}
	path, err := opts.lookup()("node")
	if err != nil {
		res.Status = StatusMissing
		return res
	}
	res.Path = path
	res.Version = normalizeVersion(firstVersion(ctx, opts, path))
	res.Detail = path
	if major := majorVersion(res.Version); major > 0 && major < MinNodeMajor {
		res.Status = StatusOutdated
		res.Message = "Node " + res.Version + " слишком старый: серверы MCP из поставки требуют минимум " +
			strconv.Itoa(MinNodeMajor) + "."
		return res
	}
	res.Status = StatusOK
	return res
}

type toolSpec struct {
	id      string
	name    string
	message string
	fix     string
}

func checkTool(ctx context.Context, opts Options, spec toolSpec) Result {
	res := Result{ID: spec.id, Name: spec.name, Message: spec.message, Fix: spec.fix}
	path, err := opts.lookup()(spec.id)
	if err != nil {
		res.Status = StatusMissing
		return res
	}
	res.Status = StatusOK
	res.Path = path
	res.Version = normalizeVersion(firstVersion(ctx, opts, path))
	res.Detail = path
	return res
}

func checkKerberos(ctx context.Context, opts Options) Result {
	res := Result{
		ID:      "kinit",
		Name:    "Kerberos (kinit)",
		Message: "kinit нужен только там, где доступ к внутренним системам идёт по Kerberos.",
		Fix: "macOS: kinit входит в систему. Если его нет — поставьте пакет krb5 с внутреннего зеркала " +
			"и положите krb5.conf домена в /etc/krb5.conf.",
	}
	path, err := opts.lookup()("kinit")
	if err == nil {
		res.Status = StatusOK
		res.Path = path
		res.Version = normalizeVersion(firstVersion(ctx, opts, path))
		res.Detail = path
		return res
	}
	if opts.RequireKerberos {
		res.Status = StatusMissing
		res.Message = "Kerberos включён в этой поставке, но kinit на машине не найден."
		return res
	}
	res.Status = StatusSkipped
	res.Message = "Kerberos в этой поставке не используется — проверка пропущена."
	return res
}

func checkDocker(ctx context.Context, opts Options) Result {
	res := Result{
		ID:      "docker",
		Name:    "Docker",
		Message: "Docker нужен, только если сервер MCP запускается контейнером.",
		Fix: "Поставьте Docker Desktop или Colima. В контуре: образ сервера MCP заранее выгружается " +
			"через docker save и загружается через docker load (см. SELF_HOSTING.md, «Закрытый контур (offline-поставка)»).",
	}
	if !mcpWantsDocker(opts.mcpConfigPaths()) {
		res.Status = StatusSkipped
		res.Message = "Ни один настроенный сервер MCP не запускается через Docker — проверка пропущена."
		return res
	}
	path, err := opts.lookup()("docker")
	if err != nil {
		res.Status = StatusMissing
		res.Message = "Настроен сервер MCP, который запускается контейнером, но docker на машине не найден."
		return res
	}
	res.Status = StatusOK
	res.Path = path
	res.Version = normalizeVersion(firstVersion(ctx, opts, path))
	res.Detail = path
	return res
}

func (o Options) mcpConfigPaths() []string {
	if len(o.MCPConfigPaths) > 0 {
		return o.MCPConfigPaths
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return []string{
		filepath.Join(home, ".claude.json"),
		filepath.Join(home, ".cursor", "mcp.json"),
		filepath.Join(home, ".goosar", "mcp.json"),
	}
}

func mcpWantsDocker(paths []string) bool {
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var конфиг struct {
			MCPServers map[string]struct {
				Command string `json:"command"`
			} `json:"mcpServers"`
		}
		if err := json.Unmarshal(data, &конфиг); err != nil {
			continue
		}
		for _, server := range конфиг.MCPServers {
			if filepath.Base(strings.TrimSpace(server.Command)) == "docker" {
				return true
			}
		}
	}
	return false
}

func firstVersion(ctx context.Context, opts Options, path string) string {
	out, err := opts.version()(ctx, path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

func probeVersion(ctx context.Context, path string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, versionTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "--version").Output()
	if err != nil {
		return "", err
	}
	line := strings.TrimSpace(string(out))
	if idx := strings.IndexAny(line, "\r\n"); idx >= 0 {
		line = line[:idx]
	}
	return line, nil
}

var versionPattern = regexp.MustCompile(`\d+(?:\.\d+)*`)

func normalizeVersion(raw string) string {
	if raw == "" {
		return ""
	}
	if m := versionPattern.FindString(raw); m != "" {
		return m
	}
	return raw
}

func majorVersion(version string) int {
	major, _, _ := strings.Cut(version, ".")
	n, err := strconv.Atoi(major)
	if err != nil {
		return 0
	}
	return n
}
