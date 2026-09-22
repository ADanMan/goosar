package corpauth

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/go-ldap/ldap/v3"
)

const (
	LDAPURLEnvVar          = "GOOSAR_LDAP_URL"
	LDAPStartTLSEnvVar     = "GOOSAR_LDAP_START_TLS"
	LDAPBindDNEnvVar       = "GOOSAR_LDAP_BIND_DN"
	LDAPBindPasswordEnvVar = "GOOSAR_LDAP_BIND_PASSWORD"
	LDAPBaseDNEnvVar       = "GOOSAR_LDAP_BASE_DN"
	LDAPUserFilterEnvVar   = "GOOSAR_LDAP_USER_FILTER"
	LDAPEmailAttrEnvVar    = "GOOSAR_LDAP_EMAIL_ATTR"
	LDAPNameAttrEnvVar     = "GOOSAR_LDAP_NAME_ATTR"
	LDAPAdminGroupEnvVar   = "GOOSAR_LDAP_ADMIN_GROUP"
	LDAPDisplayNameEnvVar  = "GOOSAR_LDAP_DISPLAY_NAME"
)

const defaultUserFilter = "(|(sAMAccountName=%s)(userPrincipalName=%s))"

const (
	defaultEmailAttr = "mail"
	defaultNameAttr  = "displayName"
)

const ldapTimeout = 10 * time.Second

type LDAPConfig struct {
	URL      string
	StartTLS bool

	BindDN       string
	BindPassword string
	BaseDN       string
	UserFilter   string
	EmailAttr    string
	NameAttr     string

	AdminGroup  string
	DisplayName string
}

func (c LDAPConfig) Configured() bool { return c.URL != "" && c.BaseDN != "" }

func (c LDAPConfig) EncryptedTransport() bool {
	scheme := strings.ToLower(strings.SplitN(c.URL, ":", 2)[0])
	return scheme == "ldaps" || (scheme == "ldap" && c.StartTLS)
}

func LDAPConfigFromEnv() LDAPConfig {
	filter := strings.TrimSpace(os.Getenv(LDAPUserFilterEnvVar))
	if filter == "" {
		filter = defaultUserFilter
	}
	emailAttr := strings.TrimSpace(os.Getenv(LDAPEmailAttrEnvVar))
	if emailAttr == "" {
		emailAttr = defaultEmailAttr
	}
	nameAttr := strings.TrimSpace(os.Getenv(LDAPNameAttrEnvVar))
	if nameAttr == "" {
		nameAttr = defaultNameAttr
	}
	return LDAPConfig{
		URL:          strings.TrimSpace(os.Getenv(LDAPURLEnvVar)),
		StartTLS:     strings.EqualFold(strings.TrimSpace(os.Getenv(LDAPStartTLSEnvVar)), "true"),
		BindDN:       strings.TrimSpace(os.Getenv(LDAPBindDNEnvVar)),
		BindPassword: os.Getenv(LDAPBindPasswordEnvVar),
		BaseDN:       strings.TrimSpace(os.Getenv(LDAPBaseDNEnvVar)),
		UserFilter:   filter,
		EmailAttr:    emailAttr,
		NameAttr:     nameAttr,
		AdminGroup:   strings.TrimSpace(os.Getenv(LDAPAdminGroupEnvVar)),
		DisplayName:  strings.TrimSpace(os.Getenv(LDAPDisplayNameEnvVar)),
	}
}

type Directory interface {
	Authenticate(ctx context.Context, username, password string) (Identity, error)
}

type ldapDirectory struct {
	cfg LDAPConfig
}

func NewLDAPDirectory(cfg LDAPConfig) Directory {
	if !cfg.Configured() || !cfg.EncryptedTransport() {
		return nil
	}
	return &ldapDirectory{cfg: cfg}
}

func (d *ldapDirectory) Authenticate(ctx context.Context, username, password string) (Identity, error) {
	username = strings.TrimSpace(username)

	if username == "" || password == "" {
		return Identity{}, ErrInvalidCredentials
	}

	conn, err := d.dial(ctx)
	if err != nil {
		return Identity{}, err
	}
	defer conn.Close()

	if d.cfg.BindDN != "" {
		if err := conn.Bind(d.cfg.BindDN, d.cfg.BindPassword); err != nil {
			return Identity{}, fmt.Errorf("%w: service bind rejected", ErrNotConfigured)
		}
	}

	entry, err := d.search(conn, username)
	if err != nil {
		return Identity{}, err
	}

	if err := conn.Bind(entry.DN, password); err != nil {
		return Identity{}, ErrInvalidCredentials
	}

	email := strings.ToLower(strings.TrimSpace(entry.GetAttributeValue(d.cfg.EmailAttr)))
	if email == "" {
		return Identity{}, ErrNoEmail
	}
	name := strings.TrimSpace(entry.GetAttributeValue(d.cfg.NameAttr))
	if name == "" {
		name = username
	}

	subject := NormalizeDN(entry.DN)
	if subject == "" {
		return Identity{}, fmt.Errorf("%w: directory entry carries no DN", ErrInvalidCredentials)
	}

	return Identity{
		Provider: MethodLDAP,
		Subject:  subject,
		Email:    email,
		Name:     name,

		EmailVerified: true,
		IsAdmin:       d.entryIsAdmin(entry),
	}, nil
}

func (d *ldapDirectory) dial(_ context.Context) (*ldap.Conn, error) {
	host, err := ldapHost(d.cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNotConfigured, err)
	}
	conn, err := ldap.DialURL(d.cfg.URL,
		ldap.DialWithDialer(&net.Dialer{Timeout: ldapTimeout}),
		ldap.DialWithTLSConfig(&tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}),
	)
	if err != nil {
		return nil, fmt.Errorf("%w: dial: %v", ErrUnavailable, err)
	}
	conn.SetTimeout(ldapTimeout)

	if d.cfg.StartTLS && strings.HasPrefix(strings.ToLower(d.cfg.URL), "ldap:") {
		if err := conn.StartTLS(&tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}); err != nil {
			conn.Close()

			return nil, fmt.Errorf("%w: starttls: %v", ErrUnavailable, err)
		}
	}
	return conn, nil
}

func (d *ldapDirectory) search(conn *ldap.Conn, username string) (*ldap.Entry, error) {

	safe := ldap.EscapeFilter(username)
	filter := strings.ReplaceAll(d.cfg.UserFilter, "%s", safe)

	attrs := []string{"dn", d.cfg.EmailAttr, d.cfg.NameAttr, "memberOf"}
	res, err := conn.Search(ldap.NewSearchRequest(
		d.cfg.BaseDN,
		ldap.ScopeWholeSubtree, ldap.NeverDerefAliases,

		2, int(ldapTimeout.Seconds()), false,
		filter, attrs, nil,
	))
	if err != nil {
		return nil, fmt.Errorf("%w: search: %v", ErrUnavailable, err)
	}
	if len(res.Entries) != 1 {

		return nil, ErrInvalidCredentials
	}
	return res.Entries[0], nil
}

func (d *ldapDirectory) entryIsAdmin(entry *ldap.Entry) bool {
	if d.cfg.AdminGroup == "" {
		return false
	}
	want := NormalizeDN(d.cfg.AdminGroup)
	for _, dn := range entry.GetAttributeValues("memberOf") {
		if NormalizeDN(dn) == want {
			return true
		}
	}
	return false
}

func NormalizeDN(dn string) string {
	parts := strings.Split(dn, ",")
	for i, p := range parts {
		parts[i] = strings.TrimSpace(p)
	}
	return strings.ToLower(strings.Join(parts, ","))
}

func ldapHost(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("unparseable %s: %v", LDAPURLEnvVar, err)
	}
	if u.Hostname() == "" {
		return "", fmt.Errorf("%s carries no host", LDAPURLEnvVar)
	}
	return u.Hostname(), nil
}
