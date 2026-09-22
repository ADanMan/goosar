package preflight

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

type ServerProbe struct {
	OK bool

	Reason string

	TLSUntrusted bool

	Version string
}

const (
	DaemonRunning     = "running"
	DaemonStarting    = "starting"
	DaemonStopped     = "stopped"
	DaemonUnreachable = "unreachable"
	DaemonInvalid     = "invalid"
)

type DaemonProbe struct {
	Status string
	Detail string
}

type KitProbe struct {
	Total int

	ForPlatform int

	Platforms []string
}

type ServerOptions struct {
	ConfigPath string
	Profile    string
	ServerURL  string
	CAFile     string

	Platform string

	ProbeServer       func(ctx context.Context) ServerProbe
	DaemonHealth      func(ctx context.Context) DaemonProbe
	KitManifest       func(ctx context.Context) (KitProbe, error)
	InstalledPackages func() int

	LLMHealth func(ctx context.Context) (LLMHealthProbe, error)
}

type LLMHealthProbe struct {
	Status    string
	LatencyMS int64
}

func RunServer(ctx context.Context, opts ServerOptions) []Result {
	config := checkConfig(opts)
	if config.Status != StatusOK {
		skip := "Сервер не настроен — проверка пропущена."
		return []Result{
			config,
			skipped("server", "Сервер", skip),
			skipped("certificate", "Сертификат", skip),
			skipped("daemon", "Демон", skip),
			skipped("kit-platform", "Кит для этой платформы", skip),
			skipped("llm", "AI-шлюз", skip),
		}
	}

	var probe ServerProbe
	if opts.ProbeServer != nil {
		probe = opts.ProbeServer(ctx)
	}
	server := checkServer(opts, probe)
	cert := checkCertificate(opts, probe)
	daemon := checkDaemon(ctx, opts)
	kit := checkKit(ctx, opts, probe)
	llm := checkLLM(ctx, opts, probe)
	return []Result{config, server, cert, daemon, kit, llm}
}

func skipped(id, name, message string) Result {
	return Result{ID: id, Name: name, Status: StatusSkipped, Message: message}
}

func checkConfig(opts ServerOptions) Result {
	res := Result{
		ID:      "config",
		Name:    "Конфигурация CLI",
		Path:    opts.ConfigPath,
		Message: "Профиль и адрес сервера, с которым работают CLI и демон.",
		Fix:     "goosar setup self-host --server-url https://… --app-url https://… (для стенда с внутренним CA добавьте --ca-file).",
	}
	if strings.TrimSpace(opts.ServerURL) == "" {
		res.Status = StatusMissing
		res.Message = "Сервер не настроен: в конфигурации нет server_url."
		return res
	}
	res.Status = StatusOK
	profile := opts.Profile
	if profile == "" {
		profile = "default"
	}
	res.Detail = "профиль " + profile + ", " + opts.ServerURL
	return res
}

func checkServer(opts ServerOptions, probe ServerProbe) Result {
	res := Result{
		ID:      "server",
		Name:    "Сервер",
		Message: "GET /health на " + opts.ServerURL + " должен отвечать 200 без сессии.",
		Fix:     "Проверьте адрес и что сервер поднят: на его машине состояние показывает docker compose ps.",
	}
	if opts.ProbeServer == nil {
		res.Status = StatusSkipped
		res.Message = "Проба сервера недоступна в этой сборке — проверка пропущена."
		return res
	}
	if !probe.OK {
		res.Status = StatusMissing
		res.Message = "Сервер не ответил: " + probe.Reason
		return res
	}
	res.Status = StatusOK
	res.Version = probe.Version
	if probe.Version != "" {
		res.Detail = "ответил, версия " + probe.Version
	} else {
		res.Detail = "ответил"
	}
	return res
}

func checkCertificate(opts ServerOptions, probe ServerProbe) Result {
	res := Result{
		ID:      "certificate",
		Name:    "Сертификат",
		Message: "Чему CLI и демон доверяют при TLS-соединении со стендом.",
		Fix: "Стенд с внутренним CA: goosar setup self-host … --ca-file <root-ca.crt> " +
			"(файл корневого CA выдаёт администратор сервера), либо установите CA в системное хранилище.",
	}
	if strings.HasPrefix(strings.ToLower(opts.ServerURL), "http://") {
		res.Status = StatusSkipped
		res.Detail = "соединение по HTTP, сертификата нет"
		res.Message = "Сервер работает по HTTP без TLS — проверка пропущена."
		return res
	}
	if probe.TLSUntrusted {
		res.Status = StatusMissing
		res.Message = "Сертификат стенда не доверен: " + probe.Reason
		return res
	}
	if opts.ProbeServer == nil || !probe.OK {
		res.Status = StatusSkipped
		res.Message = "Сервер не ответил — сертификат не проверялся."
		return res
	}
	res.Status = StatusOK
	if opts.CAFile != "" {
		res.Detail = "CA из " + opts.CAFile
		res.Path = opts.CAFile
	} else {
		res.Detail = "системное хранилище доверия"
	}
	return res
}

func checkDaemon(ctx context.Context, opts ServerOptions) Result {
	res := Result{
		ID:      "daemon",
		Name:    "Демон",
		Message: "Демон регистрирует эту машину как рантайм и исполняет задачи.",
		Fix:     "goosar daemon start; если он не поднимается — goosar daemon logs.",
	}
	if opts.DaemonHealth == nil {
		res.Status = StatusSkipped
		res.Message = "Проба демона недоступна в этой сборке — проверка пропущена."
		return res
	}
	probe := opts.DaemonHealth(ctx)
	res.Detail = probe.Detail
	switch probe.Status {
	case DaemonRunning:
		res.Status = StatusOK
		res.Message = "Демон запущен и отвечает."
	case DaemonStarting:
		res.Status = StatusOK
		res.Message = "Демон ещё поднимается: агентские CLI опрашиваются, рантайм зарегистрируется через несколько секунд."
	case DaemonStopped:
		res.Status = StatusMissing
		res.Message = "Демон не запущен: порт здоровья отказал в соединении."
	case DaemonUnreachable:
		res.Status = StatusMissing
		res.Message = "Порт здоровья демона не ответил за отведённое время — процесс завис или порт занят другим процессом."
		res.Fix = "goosar daemon restart; если порт держит чужой процесс — lsof -i :<порт> покажет, кто."
	default:
		res.Status = StatusMissing
		res.Message = "На порту здоровья отвечает не демон: ответ не разобран."
		res.Fix = "Порт занят другим процессом (lsof -i :<порт>). Освободите его или запустите демон под другим профилем: goosar daemon start --profile <имя>."
	}
	return res
}

func checkKit(ctx context.Context, opts ServerOptions, probe ServerProbe) Result {
	res := Result{
		ID:      "kit-platform",
		Name:    "Кит для этой платформы",
		Message: "Скиллы, серверы MCP и среды выполнения приезжают с сервера пакетами под платформу клиента (" + opts.Platform + ").",
		Fix: "Соберите пакеты под эту платформу в кит поставки (SELF_HOSTING.md, «Закрытый контур (offline-поставка)») " +
			"или подключите машину той платформы, для которой пакеты есть.",
	}
	if opts.KitManifest == nil || !probe.OK {
		res.Status = StatusSkipped
		res.Message = "Сервер не ответил — кит не проверялся."
		return res
	}
	kit, err := opts.KitManifest(ctx)
	if err != nil {
		res.Status = StatusMissing
		res.Message = "Манифест провижининга не получен: " + err.Error()
		res.Fix = "Проверьте, что в конфигурации есть workspace_id (goosar login) и что на сервере настроено хранилище провижининга (SELF_HOSTING.md, «Каталог пакетов (provisioning store)»)."
		return res
	}
	if kit.Total == 0 {
		res.Status = StatusSkipped
		res.Message = "Сервер не публикует пакеты провижининга — проверка пропущена."
		return res
	}
	if kit.ForPlatform == 0 {
		res.Status = StatusMissing
		res.Message = fmt.Sprintf("Пакетов %d, но ни одного для %s; есть для: %s.",
			kit.Total, opts.Platform, strings.Join(kit.Platforms, ", "))
		return res
	}
	installed := -1
	if opts.InstalledPackages != nil {
		installed = opts.InstalledPackages()
	}
	res.Status = StatusOK
	res.Detail = strconv.Itoa(kit.ForPlatform) + " доступно"
	if installed >= 0 {
		res.Detail += ", " + strconv.Itoa(installed) + " установлено"
	}
	return res
}

func checkLLM(ctx context.Context, opts ServerOptions, probe ServerProbe) Result {
	res := Result{
		ID:      "llm",
		Name:    "AI-шлюз",
		Message: "Сервер проверяет свой GOOSAR_LLM_API_KEY запросом GET /models к GOOSAR_LLM_BASE_URL.",
		Fix:     "Проверьте GOOSAR_LLM_API_KEY и GOOSAR_LLM_BASE_URL на сервере; goosar doctor --server повторяет эту проверку раз в 60 секунд.",
	}
	if opts.LLMHealth == nil || !probe.OK {
		res.Status = StatusSkipped
		res.Message = "Сервер не ответил — AI-шлюз не проверялся."
		return res
	}
	health, err := opts.LLMHealth(ctx)
	if err != nil {
		res.Status = StatusSkipped
		res.Message = "Проверка AI-шлюза не выполнена: " + err.Error()
		return res
	}
	switch health.Status {
	case "ok":
		res.Status = StatusOK
		res.Detail = "отвечает"
	case "unconfigured":
		res.Status = StatusSkipped
		res.Message = "На сервере не настроен GOOSAR_LLM_API_KEY/GOOSAR_LLM_BASE_URL — проверка пропущена."
	case "auth_rejected":
		res.Status = StatusMissing
		res.Message = "Ключ GOOSAR_LLM_API_KEY отклонён шлюзом (401/403)."
	case "degraded":
		res.Status = StatusMissing
		res.Message = "AI-шлюз отвечает с ошибкой сервера (5xx)."
	default:
		res.Status = StatusMissing
		res.Message = "AI-шлюз недоступен."
	}
	if health.LatencyMS > 0 {
		if res.Detail != "" {
			res.Detail += ", "
		}
		res.Detail += strconv.FormatInt(health.LatencyMS, 10) + " мс"
	}
	return res
}
