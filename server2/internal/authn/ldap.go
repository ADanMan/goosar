// Клиент LDAP (T-029): bind + search поверх собственного минимального BER
// (ldap_ber.go), без github.com/go-ldap/ldap — см.
// server2/docs/adr/0002-auth-providers.md за обоснование. Поддерживает ровно
// то, что нужно "аутентифицировать по логину/паролю через каталог": simple
// bind служебной учётной записью, поиск пользователя по единственному
// equality-фильтру (LDAP_USER_FILTER, вида "(uid=%s)"), затем simple bind
// его собственным DN и паролем.
package authn

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/adanman/goosar/server2/internal/config"
	"github.com/adanman/goosar/server2/internal/httpapi"
)

const ldapDialTimeout = 10 * time.Second

// ldapConn — одно соединение с LDAP-сервером, с собственным счётчиком
// messageID (LDAP требует уникальный id на запрос в рамках соединения).
type ldapConn struct {
	conn  net.Conn
	msgID int
}

// dialLDAP разбирает LDAP_URL (ldap:// или ldaps://) и подключается —
// STARTTLS не реализован (см. decisions.md, раздел T-029): implicit TLS
// (ldaps://) покрывает тот же периметр безопасности проще, а STARTTLS почти
// не встречается в современных LDAP-развёртываниях так же часто, как ESMTP
// (в отличие от internal/mail.SMTPSender, где STARTTLS — основной путь).
func dialLDAP(ctx context.Context, rawURL string) (*ldapConn, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("authn: ldap: LDAP_URL: %w", err)
	}
	host := u.Hostname()
	port := u.Port()
	dialer := &net.Dialer{Timeout: ldapDialTimeout}

	var conn net.Conn
	switch strings.ToLower(u.Scheme) {
	case "ldaps":
		if port == "" {
			port = "636"
		}
		tlsDialer := &tls.Dialer{NetDialer: dialer, Config: &tls.Config{ServerName: host}}
		conn, err = tlsDialer.DialContext(ctx, "tcp", net.JoinHostPort(host, port))
	case "ldap", "":
		if port == "" {
			port = "389"
		}
		conn, err = dialer.DialContext(ctx, "tcp", net.JoinHostPort(host, port))
	default:
		return nil, fmt.Errorf("authn: ldap: неизвестная схема %q (нужен ldap:// или ldaps://)", u.Scheme)
	}
	if err != nil {
		return nil, fmt.Errorf("authn: ldap: соединение: %w", err)
	}
	return &ldapConn{conn: conn}, nil
}

func (c *ldapConn) close() { _ = c.conn.Close() }

func (c *ldapConn) nextMessageID() int {
	c.msgID++
	return c.msgID
}

// bind выполняет simple bind (RFC 4511 §4.2); dn=="" — anonymous bind (не
// используется этим пакетом, но не отвергается протоколом сам по себе).
func (c *ldapConn) bind(dn, password string) error {
	id := c.nextMessageID()
	op := berTLV(berAppBindRequest, concatBytes(
		berPosInt(berTagInteger, 3),
		berOctetString(berTagOctetString, dn),
		berOctetString(berCtxSimpleAuth, password),
	))
	msg := berTLV(berSeq, concatBytes(berPosInt(berTagInteger, id), op))
	if _, err := c.conn.Write(msg); err != nil {
		return fmt.Errorf("authn: ldap: отправка BindRequest: %w", err)
	}

	top, err := readBERMessage(c.conn)
	if err != nil {
		return fmt.Errorf("authn: ldap: чтение BindResponse: %w", err)
	}
	children, err := berParse(top.data)
	if err != nil || len(children) < 2 {
		return errors.New("authn: ldap: BindResponse: не удалось разобрать LDAPMessage")
	}
	if children[1].tag != berAppBindResponse {
		return fmt.Errorf("authn: ldap: ожидался BindResponse, пришёл tag=0x%02x", children[1].tag)
	}
	resultCode, diag, err := parseLDAPResult(children[1].data)
	if err != nil {
		return err
	}
	if resultCode != 0 {
		return fmt.Errorf("authn: ldap: bind отклонён (код %d): %s", resultCode, diag)
	}
	return nil
}

// ldapEntry — одна запись каталога, найденная search'ем: DN плюс
// интересующие атрибуты (только то, что было явно запрошено в attrs).
type ldapEntry struct {
	DN         string
	Attributes map[string][]string
}

// search выполняет один SearchRequest с equality-фильтром (attr=value) под
// baseDN, wholeSubtree, и возвращает первую найденную запись — этому пакету
// нужен ровно один пользователь по логину, не список.
func (c *ldapConn) search(baseDN, attr, value string, attrs []string) (*ldapEntry, error) {
	id := c.nextMessageID()
	filter := berTLV(berCtxEqualityFilter, concatBytes(
		berOctetString(berTagOctetString, attr),
		berOctetString(berTagOctetString, ldapEscapeFilterValue(value)),
	))
	attrSeqContent := make([]byte, 0, 16*len(attrs))
	for _, a := range attrs {
		attrSeqContent = append(attrSeqContent, berOctetString(berTagOctetString, a)...)
	}
	op := berTLV(berAppSearchRequest, concatBytes(
		berOctetString(berTagOctetString, baseDN),
		berPosInt(berTagEnumerated, 2), // scope: wholeSubtree
		berPosInt(berTagEnumerated, 0), // derefAliases: neverDerefAliases
		berPosInt(berTagInteger, 2),    // sizeLimit: нужна максимум одна запись
		berPosInt(berTagInteger, 0),    // timeLimit: без ограничения
		berBool(false),                 // typesOnly
		filter,
		berTLV(berSeq, attrSeqContent),
	))
	msg := berTLV(berSeq, concatBytes(berPosInt(berTagInteger, id), op))
	if _, err := c.conn.Write(msg); err != nil {
		return nil, fmt.Errorf("authn: ldap: отправка SearchRequest: %w", err)
	}

	var found *ldapEntry
	for {
		top, err := readBERMessage(c.conn)
		if err != nil {
			return nil, fmt.Errorf("authn: ldap: чтение ответа на поиск: %w", err)
		}
		children, err := berParse(top.data)
		if err != nil || len(children) < 2 {
			return nil, errors.New("authn: ldap: ответ на поиск: не удалось разобрать LDAPMessage")
		}
		switch children[1].tag {
		case berAppSearchEntry:
			if found == nil {
				e, err := parseSearchEntry(children[1].data)
				if err != nil {
					return nil, err
				}
				found = e
			}
		case berAppSearchDone:
			resultCode, diag, err := parseLDAPResult(children[1].data)
			if err != nil {
				return nil, err
			}
			if resultCode != 0 {
				return nil, fmt.Errorf("authn: ldap: поиск отклонён (код %d): %s", resultCode, diag)
			}
			return found, nil
		default:
			// Referral (0xA3 в этом контексте — protocolOp [19], не путать с
			// filter's [3]) или другой протокольный фрейм, который этому
			// клиенту не нужен — пропускаем и ждём SearchResultDone.
		}
	}
}

func parseLDAPResult(data []byte) (resultCode int, diagnostic string, err error) {
	nodes, err := berParse(data)
	if err != nil || len(nodes) < 1 {
		return 0, "", errors.New("authn: ldap: LDAPResult: не удалось разобрать")
	}
	resultCode = berPosIntValue(nodes[0].data)
	if len(nodes) >= 3 {
		diagnostic = string(nodes[2].data)
	}
	return resultCode, diagnostic, nil
}

func parseSearchEntry(data []byte) (*ldapEntry, error) {
	nodes, err := berParse(data)
	if err != nil || len(nodes) < 2 {
		return nil, errors.New("authn: ldap: SearchResultEntry: не удалось разобрать")
	}
	entry := &ldapEntry{DN: string(nodes[0].data), Attributes: map[string][]string{}}
	attrList, err := berParse(nodes[1].data)
	if err != nil {
		return nil, fmt.Errorf("authn: ldap: атрибуты записи: %w", err)
	}
	for _, partial := range attrList {
		fields, err := berParse(partial.data)
		if err != nil || len(fields) < 2 {
			continue
		}
		name := string(fields[0].data)
		vals, err := berParse(fields[1].data)
		if err != nil {
			continue
		}
		for _, v := range vals {
			entry.Attributes[name] = append(entry.Attributes[name], string(v.data))
		}
	}
	return entry, nil
}

func concatBytes(parts ...[]byte) []byte {
	var out []byte
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// ldapEscapeFilterValue экранирует спецсимволы фильтра (RFC 4515 §3) в
// значении, подставляемом в equality-фильтр — value здесь всегда приходит от
// вызывающего (логин пользователя), так что без экранирования это была бы
// LDAP filter injection.
func ldapEscapeFilterValue(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '*':
			b.WriteString(`\2a`)
		case '(':
			b.WriteString(`\28`)
		case ')':
			b.WriteString(`\29`)
		case '\\':
			b.WriteString(`\5c`)
		case 0:
			b.WriteString(`\00`)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// parseEqualityFilterTemplate разбирает LDAP_USER_FILTER вида "(uid=%s)" на
// имя атрибута и подтверждает, что шаблон содержит ровно один "%s" — этот
// клиент поддерживает только одиночный equality-фильтр, не произвольную
// LDAP filter grammar (см. server2/docs/adr/0002-auth-providers.md).
func parseEqualityFilterTemplate(tmpl string) (attr string, err error) {
	inner := strings.TrimSuffix(strings.TrimPrefix(tmpl, "("), ")")
	if inner == tmpl {
		return "", fmt.Errorf("authn: ldap: LDAP_USER_FILTER должен быть в скобках, например (uid=%%s)")
	}
	idx := strings.IndexByte(inner, '=')
	if idx <= 0 {
		return "", errors.New("authn: ldap: LDAP_USER_FILTER должен быть вида (attr=%s)")
	}
	attr, rhs := inner[:idx], inner[idx+1:]
	if rhs != "%s" {
		return "", errors.New("authn: ldap: LDAP_USER_FILTER поддерживает только простое равенство (attr=%s), без wildcard/AND/OR")
	}
	return attr, nil
}

// loginLDAP — весь протокольный обмен для одного логина: bind служебной
// учётной записью, найти пользователя, bind его собственным паролем, прочитать
// email/displayName. Отдельная функция (не метод Deps) — легче тестировать
// против net.Listener без остальной инфраструктуры authn.
func loginLDAP(ctx context.Context, cfg config.LDAPConfig, username, password string) (dn, email, name string, err error) {
	attr, err := parseEqualityFilterTemplate(cfg.UserFilter)
	if err != nil {
		return "", "", "", err
	}

	bindConn, err := dialLDAP(ctx, cfg.URL)
	if err != nil {
		return "", "", "", err
	}
	defer bindConn.close()
	if err := bindConn.bind(cfg.BindDN, cfg.BindPassword); err != nil {
		return "", "", "", fmt.Errorf("authn: ldap: service bind: %w", err)
	}
	entry, err := bindConn.search(cfg.BaseDN, attr, username, []string{cfg.EmailAttr, cfg.NameAttr})
	if err != nil {
		return "", "", "", err
	}
	if entry == nil {
		return "", "", "", errLDAPUserNotFound
	}

	userConn, err := dialLDAP(ctx, cfg.URL)
	if err != nil {
		return "", "", "", err
	}
	defer userConn.close()
	if err := userConn.bind(entry.DN, password); err != nil {
		return "", "", "", errLDAPInvalidCredentials
	}

	email = firstAttr(entry.Attributes, cfg.EmailAttr)
	name = firstAttr(entry.Attributes, cfg.NameAttr)
	return entry.DN, email, name, nil
}

var (
	errLDAPUserNotFound       = errors.New("authn: ldap: пользователь не найден")
	errLDAPInvalidCredentials = errors.New("authn: ldap: неверный логин или пароль")
	errLDAPMissingEmail       = errors.New("authn: ldap: у записи каталога нет email-атрибута")
)

func firstAttr(attrs map[string][]string, name string) string {
	if vs := attrs[name]; len(vs) > 0 {
		return vs[0]
	}
	return ""
}

// ldapLoginRequest — тело POST /api/auth/ldap/login (contract §3.3).
type ldapLoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// respondLDAPError сводит исходы loginLDAP к двум ответам контракта:
// неверные логин/пароль (или пользователь не найден — не различать эти два
// случая специально, обычная практика для форм входа) — 401; любая другая
// проблема (сеть, TLS, каталог без email-атрибута) — 503, тем же кодом, что
// и "LDAP не настроен", но с логом причины для админа. Живёт рядом с
// loginLDAP: это часть словаря ошибок протокола (errLDAP*), не HTTP-слоя.
func (d *Deps) respondLDAPError(w http.ResponseWriter, dn string, err error) {
	if errors.Is(err, errLDAPInvalidCredentials) || errors.Is(err, errLDAPUserNotFound) {
		httpapi.WriteError(w, http.StatusUnauthorized, "invalid username or password", "invalid_credentials")
		return
	}
	d.Logger.Error("auth: ldap: вход", "err", err, "dn", dn)
	httpapi.WriteError(w, http.StatusServiceUnavailable, "LDAP server unreachable", "ldap_unavailable")
}

// handleLoginLdap — POST /api/auth/ldap/login: аутентификация в LDAP/AD,
// дальше как verify-code (contract §3.3).
func (d *Deps) handleLoginLdap(w http.ResponseWriter, r *http.Request) {
	if !d.Config.AuthMethodEnabled("ldap") || !d.Config.LDAPMethodAvailable() {
		httpapi.WriteError(w, http.StatusNotFound, "LDAP not enabled on this server", "ldap_not_enabled")
		return
	}
	ip := httpapi.ClientIP(r)
	if !d.AuthIPLimiter.Enforce(w, ip, "too many requests") {
		return
	}
	var req ldapLoginRequest
	if err := httpapi.DecodeJSON(r, &req); err != nil || req.Username == "" || req.Password == "" {
		httpapi.BadRequest(w, "username and password are required")
		return
	}
	if !d.AuthEmailLimiter.Enforce(w, req.Username, "too many requests for this username") {
		return
	}

	dn, email, name, err := loginLDAP(r.Context(), d.Config.LDAP, req.Username, req.Password)
	if err == nil && email == "" {
		// LDAP_EMAIL_ATTRIBUTE не заполнен у этой записи каталога — без email
		// нельзя ни найти, ни завести локальный аккаунт (docs/51-data-model.md
		// держит email первичным идентификатором пользователя); та же
		// диагностика, что и недоступность сервера — проблема настройки
		// каталога, не вина вызывающего.
		err = errLDAPMissingEmail
	}
	if err != nil {
		d.respondLDAPError(w, dn, err)
		return
	}

	acct, err := d.loginOrLinkExternal(r, "ldap", dn, email, name)
	if errors.Is(err, errEmailNotAllowed) {
		httpapi.WriteError(w, http.StatusForbidden, "this email is not allowed to sign in on this server", "email_not_allowed")
		return
	}
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	d.completeLogin(w, r, acct)
}
