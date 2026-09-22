package preflight

import (
	"context"
	"os"
	"path/filepath"
	"strings"
)

var KitMCPServers = []struct {
	Package string

	Command string

	Preset string
}{
	{Package: "b24-agent", Command: "mcp-server-b24", Preset: "Битрикс24"},
	{Package: "ews-mcp", Command: "ewsmcp", Preset: "Почта и календарь Outlook"},
	{Package: "mcp-atlassian", Command: "mcp-atlassian", Preset: "Jira и Confluence"},
	{Package: "mcp-server-fetch", Command: "mcp-server-fetch", Preset: "Веб-доступ"},
}

func (o Options) mcpServersDir() string {
	if strings.TrimSpace(o.MCPServersDir) != "" {
		return o.MCPServersDir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".hermes", "mcp-servers")
}

const (
	serverOK = iota

	serverNoEntryPoint

	serverAbsent
)

var serverBinSubdirs = []string{filepath.Join(".venv", "bin"), "bin"}

func classifyServer(opts Options, storeDir, pkg, command string) int {
	if storeDir != "" {
		for _, sub := range serverBinSubdirs {
			path := filepath.Join(storeDir, pkg, sub, command)
			if info, err := os.Stat(path); err == nil && !info.IsDir() {
				return serverOK
			}
		}
	}

	if _, err := opts.lookup()(command); err == nil {
		return serverOK
	}
	if storeDir != "" {
		if info, err := os.Stat(filepath.Join(storeDir, pkg)); err == nil && info.IsDir() {
			return serverNoEntryPoint
		}
	}
	return serverAbsent
}

func checkMCPPackages(_ context.Context, opts Options) Result {
	res := Result{
		ID:   "mcp-packages",
		Name: "Пакеты MCP из поставки",
		Message: "Пресеты рабочих инструментов запускают серверы MCP по коротким именам. " +
			"Пакеты приезжают с поставкой и разворачиваются в локальное хранилище.",
		Fix: "Загрузите кит провижининга в хранилище развёртывания (SELF_HOSTING.md, раздел «Каталог пакетов (provisioning store)») " +
			"и перезапустите приложение — недостающие пакеты доедут при следующем запуске. " +
			"В офлайне: offline/load-provisioning-packages.sh с носителя.",
	}

	storeDir := opts.mcpServersDir()
	storeExists := false
	if storeDir != "" {
		if info, err := os.Stat(storeDir); err == nil && info.IsDir() {
			storeExists = true
		}
	}

	var delivered, missing, broken []string
	for _, server := range KitMCPServers {
		switch classifyServer(opts, storeDir, server.Package, server.Command) {
		case serverOK:
			delivered = append(delivered, server.Package)
		case serverNoEntryPoint:
			broken = append(broken, server.Package+" (нет "+server.Command+")")
		default:
			missing = append(missing, server.Package+" — "+server.Preset)
		}
	}

	if !storeExists && len(delivered) == 0 {
		res.Status = StatusSkipped
		res.Message = "Локального хранилища пакетов MCP на этой машине нет — они приедут с сервера развёртывания при первом запуске. Проверка пропущена."
		return res
	}

	res.Detail = storeDir
	if len(delivered) > 0 {
		res.Detail = strings.Join(delivered, ", ") + " — в " + storeDir
	}

	if len(broken) > 0 {
		res.Status = StatusMissing
		res.Message = "Пакеты привезены, но не запускаются: в пакете нет исполняемого файла, который ждёт пресет — " +
			strings.Join(broken, ", ") + ". Эти пресеты можно заполнить в интерфейсе, и они всё равно не запустятся."
		if len(missing) > 0 {
			res.Message += " Кроме того, не привезены: " + strings.Join(missing, ", ") + "."
		}
		return res
	}
	if len(missing) > 0 {

		res.Status = StatusSkipped
		res.Message = "Не привезены: " + strings.Join(missing, ", ") +
			". Если эти пресеты нужны, добавьте пакеты в поставку; если нет — так и должно быть."
		return res
	}
	res.Status = StatusOK
	return res
}
