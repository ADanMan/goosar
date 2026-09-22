package cli

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type ErrorKind int

const (
	KindNetworkTimeout ErrorKind = iota
	KindNetworkDNS
	KindNetworkRefused
	KindNetworkTLS
	KindNetworkOffline

	KindAuthRequired
	KindForbidden
	KindNotFound
	KindConflict
	KindValidation
	KindRateLimited
	KindServerError

	KindUnknown
)

const (
	ExitGeneric    = 1
	ExitNetwork    = 2
	ExitAuth       = 3
	ExitNotFound   = 4
	ExitValidation = 5
)

func (k ErrorKind) IsNetwork() bool {
	switch k {
	case KindNetworkTimeout, KindNetworkDNS, KindNetworkRefused, KindNetworkTLS, KindNetworkOffline:
		return true
	default:
		return false
	}
}

func (k ErrorKind) String() string {
	switch k {
	case KindNetworkTimeout:
		return "network_timeout"
	case KindNetworkDNS:
		return "network_dns"
	case KindNetworkRefused:
		return "network_refused"
	case KindNetworkTLS:
		return "network_tls"
	case KindNetworkOffline:
		return "network_offline"
	case KindAuthRequired:
		return "auth_required"
	case KindForbidden:
		return "forbidden"
	case KindNotFound:
		return "not_found"
	case KindConflict:
		return "conflict"
	case KindValidation:
		return "validation"
	case KindRateLimited:
		return "rate_limited"
	case KindServerError:
		return "server_error"
	case KindUnknown:
		return "unknown"
	default:
		return fmt.Sprintf("ErrorKind(%d)", int(k))
	}
}

type NetworkError struct {
	Kind ErrorKind
	Op   string
	Err  error
}

func (e *NetworkError) Error() string {
	if e.Op != "" {
		return fmt.Sprintf("%s: %s", e.Op, e.Err.Error())
	}
	return e.Err.Error()
}

func (e *NetworkError) Unwrap() error { return e.Err }

type UserMessageError struct {
	Msg string
	Err error
}

func (e *UserMessageError) Error() string {
	if e.Err != nil {
		return e.Msg + ": " + e.Err.Error()
	}
	return e.Msg
}

func (e *UserMessageError) Unwrap() error { return e.Err }

func WithUserMessage(msg string, err error) error {
	if err == nil {
		return nil
	}
	return &UserMessageError{Msg: msg, Err: err}
}

func (e *HTTPError) Kind() ErrorKind {
	switch e.StatusCode {
	case 401:
		return KindAuthRequired
	case 403:
		return KindForbidden
	case 404:
		return KindNotFound
	case 409:
		return KindConflict
	case 400, 422:
		return KindValidation
	case 429:
		return KindRateLimited
	default:
		if e.StatusCode >= 500 {
			return KindServerError
		}
		return KindUnknown
	}
}

func classifyNetworkError(err error) ErrorKind {
	if err == nil {
		return KindUnknown
	}

	if errors.Is(err, context.DeadlineExceeded) {
		return KindNetworkTimeout
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return KindNetworkTimeout
	}

	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return KindNetworkDNS
	}

	var certVerifyErr *tls.CertificateVerificationError
	if errors.As(err, &certVerifyErr) {
		return KindNetworkTLS
	}
	var unknownAuthorityErr x509.UnknownAuthorityError
	if errors.As(err, &unknownAuthorityErr) {
		return KindNetworkTLS
	}
	var hostnameErr x509.HostnameError
	if errors.As(err, &hostnameErr) {
		return KindNetworkTLS
	}
	var certInvalidErr x509.CertificateInvalidError
	if errors.As(err, &certInvalidErr) {
		return KindNetworkTLS
	}

	if errors.Is(err, syscall.ECONNREFUSED) {
		return KindNetworkRefused
	}

	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "context deadline exceeded"), strings.Contains(msg, "timeout"), strings.Contains(msg, "timed out"):
		return KindNetworkTimeout
	case strings.Contains(msg, "no such host"), strings.Contains(msg, "server misbehaving"), strings.Contains(msg, "name resolution"):
		return KindNetworkDNS
	case strings.Contains(msg, "connection refused"):
		return KindNetworkRefused
	case strings.Contains(msg, "x509"), strings.Contains(msg, "certificate"), strings.Contains(msg, "tls"):
		return KindNetworkTLS
	}
	return KindNetworkOffline
}

func wrapTransport(req *http.Request, err error) error {
	if err == nil {
		return nil
	}
	op := ""
	if req != nil && req.URL != nil {
		op = req.Method + " " + req.URL.Path
	}
	return &NetworkError{Kind: classifyNetworkError(err), Op: op, Err: err}
}

type Language int

const (
	LangEN Language = iota
	LangZH
	LangRU
)

func DetectLanguage() Language {
	for _, key := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		v := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
		if v == "" {
			continue
		}
		if strings.HasPrefix(v, "zh") {
			return LangZH
		}
		if strings.HasPrefix(v, "ru") {
			return LangRU
		}

		return LangEN
	}
	return LangEN
}

var kindMessages = map[ErrorKind][3]string{
	KindNetworkTimeout: {
		"Request timed out: the server did not respond in time. Check your network connection or try again later. You can raise the limit with GOOSAR_HTTP_TIMEOUT.",
		"请求超时：服务器未在规定时间内响应。请检查网络连接或稍后重试。可通过 GOOSAR_HTTP_TIMEOUT 调高超时时间。",
		"Время ожидания вышло: сервер не ответил вовремя. Проверьте подключение к сети или попробуйте позже. Лимит можно поднять через GOOSAR_HTTP_TIMEOUT.",
	},
	KindNetworkDNS: {
		"Could not resolve the Goosar server address. Check your network connection or the --server-url setting.",
		"无法解析 Goosar 服务器地址。请检查网络连接或 --server-url 配置。",
		"Не удалось определить адрес сервера Goosar. Проверьте подключение к сети или настройку --server-url.",
	},
	KindNetworkRefused: {
		"Could not connect to the Goosar server. Make sure the server address is correct and reachable.",
		"无法连接到 Goosar 服务器。请确认服务器地址正确且网络可达。",
		"Не удалось подключиться к серверу Goosar. Проверьте, что адрес сервера верный и сервер доступен.",
	},
	KindNetworkTLS: {
		"The server's certificate is not trusted on this machine (TLS/certificate error). Self-hosted server with its own CA: pass its root once with `goosar setup self-host --ca-file <stand-root-ca.crt>` (env GOOSAR_CA_FILE). Goosar Cloud: a corporate proxy or firewall is intercepting TLS — ask your network administrator for its CA and pass it the same way. Otherwise check the system clock.",
		"本机不信任服务器证书（TLS/证书错误）。自建 CA 的自托管服务器：用 `goosar setup self-host --ca-file <stand-root-ca.crt>` 传入根证书一次（环境变量 GOOSAR_CA_FILE）。Goosar 云端：企业代理或防火墙在拦截 TLS，请向网络管理员索取其 CA 并以同样方式传入。否则请检查系统时间。",
		"Сертификат сервера не доверен на этой машине (ошибка TLS/сертификата). Самостоятельный сервер со своим CA: передайте его корень один раз через `goosar setup self-host --ca-file <stand-root-ca.crt>` (переменная GOOSAR_CA_FILE). Облако Goosar: корпоративный прокси или файрвол перехватывает TLS — запросите его CA у сетевого администратора и передайте так же. Иначе проверьте системные часы.",
	},
	KindNetworkOffline: {
		"Could not reach the Goosar server. Check your network connection.",
		"无法访问 Goosar 服务器。请检查网络连接。",
		"Сервер Goosar недоступен. Проверьте подключение к сети.",
	},
	KindAuthRequired: {
		"Your session has expired or you are not signed in. Run `goosar login` to sign in again. On a self-hosted or non-OAuth setup, ask your administrator for valid credentials.",
		"登录已过期或尚未登录。请运行 `goosar login` 重新登录。自托管或非 OAuth 场景请联系管理员获取有效凭证。",
		"Сессия истекла, или вы не вошли. Выполните `goosar login`, чтобы войти заново. На самостоятельной установке или без OAuth запросите действующие учётные данные у администратора.",
	},
	KindForbidden: {
		"You do not have permission to access this resource. Check that you are in the right workspace, or ask an administrator to grant access.",
		"无权访问该资源。请确认当前 workspace 是否正确，或联系管理员授予权限。",
		"У вас нет доступа к этому ресурсу. Проверьте, что вы в нужном рабочем пространстве, или попросите администратора выдать доступ.",
	},
	KindNotFound: {
		"The requested resource was not found. Check the ID, or run the matching `list` command to see what exists in this workspace.",
		"未找到请求的资源。请核对 ID，或运行对应的 list 命令查看当前 workspace 中已有的内容。",
		"Запрошенный ресурс не найден. Проверьте ID или выполните соответствующую команду `list`, чтобы посмотреть, что есть в этом рабочем пространстве.",
	},
	KindConflict: {
		"The request conflicts with the current state of the resource (it may already exist or have changed since you last fetched it). Re-fetch the latest state and try again.",
		"请求与资源的当前状态冲突（可能已存在，或自上次获取后已被修改）。请重新获取最新状态后再试。",
		"Запрос конфликтует с текущим состоянием ресурса (он может уже существовать или мог измениться с момента последнего чтения). Получите свежее состояние и попробуйте снова.",
	},
	KindValidation: {
		"The request was invalid. Check the values you provided; run the command with --help to see the expected format.",
		"请求无效。请检查所填写的参数；可用 --help 查看期望的格式。",
		"Некорректный запрос. Проверьте переданные значения; запустите команду с --help, чтобы увидеть ожидаемый формат.",
	},
	KindRateLimited: {
		"Too many requests. Please wait a moment and try again; if this keeps happening, reduce how frequently you call the API.",
		"请求过于频繁。请稍候重试；若持续出现，请降低 API 调用频率。",
		"Слишком много запросов. Подождите немного и попробуйте снова; если повторяется, снизьте частоту обращений к API.",
	},
	KindServerError: {
		"The Goosar service is temporarily unavailable (server error). Please try again later; if it persists, contact support. Re-run with --debug to see the raw server response.",
		"Goosar 服务暂时不可用（服务器错误）。请稍后重试；若持续出现请联系支持。可加 --debug 查看服务器原始响应。",
		"Сервис Goosar временно недоступен (ошибка сервера). Попробуйте позже; если не проходит — обратитесь в поддержку. Запустите команду с --debug, чтобы увидеть исходный ответ сервера.",
	},
	KindUnknown: {
		"An unexpected error occurred.",
		"发生未知错误。",
		"Произошла непредвиденная ошибка.",
	},
}

func messageFor(kind ErrorKind, lang Language) string {
	m, ok := kindMessages[kind]
	if !ok {
		m = kindMessages[KindUnknown]
	}
	if lang == LangZH || lang == LangRU {
		return m[lang]
	}
	return m[LangEN]
}

func FormatError(err error, debug bool) string {
	if err == nil {
		return ""
	}
	lang := DetectLanguage()
	base := userMessage(err, lang)
	if debug || debugEnabled() {
		return base + "\n\n" + debugDetail(err)
	}
	return base
}

func userMessage(err error, lang Language) string {

	var um *UserMessageError
	if errors.As(err, &um) {
		return um.Msg
	}

	var netErr *NetworkError
	if errors.As(err, &netErr) {
		return messageFor(netErr.Kind, lang)
	}

	var httpErr *HTTPError
	if errors.As(err, &httpErr) {
		kind := httpErr.Kind()

		if kind == KindValidation {
			if serverMsg := extractServerMessage(httpErr.Body); serverMsg != "" {
				switch lang {
				case LangZH:
					return "请求无效：" + serverMsg
				case LangRU:
					return "Некорректный запрос: " + serverMsg
				default:
					return "Invalid request: " + serverMsg
				}
			}
		}
		return messageFor(kind, lang)
	}

	return strings.TrimSpace(err.Error())
}

func extractServerMessage(body string) string {
	body = strings.TrimSpace(body)
	if body == "" || body[0] != '{' {
		return ""
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		return ""
	}
	for _, key := range []string{"error", "message", "detail", "title"} {
		if v, ok := parsed[key]; ok {
			if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
				return strings.TrimSpace(s)
			}
		}
	}
	return ""
}

func debugDetail(err error) string {
	var sb strings.Builder
	sb.WriteString("[debug] ")
	sb.WriteString(err.Error())

	var netErr *NetworkError
	if errors.As(err, &netErr) {
		fmt.Fprintf(&sb, "\n[debug] network: op=%q kind=%s cause=%v", netErr.Op, netErr.Kind, netErr.Err)
	}
	var httpErr *HTTPError
	if errors.As(err, &httpErr) {
		fmt.Fprintf(&sb, "\n[debug] http: %s %s status=%d body=%s",
			httpErr.Method, httpErr.Path, httpErr.StatusCode, strings.TrimSpace(httpErr.Body))
	}
	return sb.String()
}

func debugEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("GOOSAR_DEBUG"))) {
	case "", "0", "false", "no", "off":
		return false
	default:
		return true
	}
}

func ExitCodeFor(err error) int {
	if err == nil {
		return 0
	}

	var netErr *NetworkError
	if errors.As(err, &netErr) {
		return ExitNetwork
	}

	var httpErr *HTTPError
	if errors.As(err, &httpErr) {
		switch httpErr.Kind() {
		case KindAuthRequired, KindForbidden:
			return ExitAuth
		case KindNotFound:
			return ExitNotFound
		case KindValidation:
			return ExitValidation
		default:
			return ExitGeneric
		}
	}

	return ExitGeneric
}

var clockMessages = [3]string{
	"The server's certificate is not valid at this machine's current time. Check the system clock (and the certificate's dates if the clock is right).",
	"按本机当前时间，服务器证书不在有效期内。请检查系统时间（若时间正确，请检查证书有效期）。",
	"Сертификат сервера недействителен по времени этой машины. Проверьте системные часы (и срок действия сертификата, если часы верны).",
}

func ExplainTransportError(err error) string {
	if err == nil {
		return ""
	}
	lang := DetectLanguage()
	var invalid x509.CertificateInvalidError
	if errors.As(err, &invalid) && invalid.Reason == x509.Expired {
		return clockMessages[lang]
	}
	if strings.Contains(strings.ToLower(err.Error()), "expired or is not yet valid") {
		return clockMessages[lang]
	}
	return messageFor(classifyNetworkError(err), lang)
}

func ProbeTimeout() time.Duration {
	v := strings.TrimSpace(os.Getenv("GOOSAR_HTTP_TIMEOUT"))
	if d, err := time.ParseDuration(v); err == nil && d > 0 {
		return d
	}
	if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
		return time.Duration(secs) * time.Second
	}

	return 5 * time.Second
}
