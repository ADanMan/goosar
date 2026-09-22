//go:build ldapintegration

// The one test that speaks the actual wire protocol.
//
// Everything else about LDAP in this package is tested through the Directory
// interface, because the rules worth guarding are ours, not go-ldap's. But a
// fake proves nothing about whether we can talk to a real directory at all —
// whether the filter substitution produces a query a server accepts, whether
// StartTLS negotiates, whether the attribute names come back as expected. That
// is what this covers, once, against a directory the operator points it at.
//
// It is behind a build tag AND an explicit environment opt-in because it needs
// credentials for a real directory. It must never run in the default suite:
// the default suite has no directory, and a test that silently skips is worse
// than a test that is not there.
//
//	GOOSAR_RUN_LDAP_INTEGRATION=1 \
//	GOOSAR_LDAP_URL=ldaps://dc.example.test:636 \
//	GOOSAR_LDAP_BASE_DN='OU=Users,DC=example,DC=test' \
//	GOOSAR_LDAP_BIND_DN='CN=svc,OU=Service,DC=example,DC=test' \
//	GOOSAR_LDAP_BIND_PASSWORD=... \
//	GOOSAR_LDAP_TEST_USERNAME=jdoe GOOSAR_LDAP_TEST_PASSWORD=... \
//	go test -tags=ldapintegration ./internal/corpauth -run TestLDAPIntegration -count=1 -v
package corpauth

import (
	"context"
	"errors"
	"os"
	"testing"
)

func TestLDAPIntegration_BindsAgainstARealDirectory(t *testing.T) {
	if os.Getenv("GOOSAR_RUN_LDAP_INTEGRATION") != "1" {
		t.Skip("set GOOSAR_RUN_LDAP_INTEGRATION=1 to run against a real directory")
	}
	username := os.Getenv("GOOSAR_LDAP_TEST_USERNAME")
	password := os.Getenv("GOOSAR_LDAP_TEST_PASSWORD")
	if username == "" || password == "" {
		t.Fatal("GOOSAR_LDAP_TEST_USERNAME and GOOSAR_LDAP_TEST_PASSWORD are required")
	}

	cfg := LDAPConfigFromEnv()
	dir := NewLDAPDirectory(cfg)
	if dir == nil {
		t.Fatalf("directory not constructible: configured=%v encrypted=%v",
			cfg.Configured(), cfg.EncryptedTransport())
	}

	identity, err := dir.Authenticate(context.Background(), username, password)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if identity.Email == "" {
		t.Fatalf("the directory returned no address in %q — check GOOSAR_LDAP_EMAIL_ATTR", cfg.EmailAttr)
	}
	if identity.Subject == "" {
		t.Fatal("no subject: the search returned an entry with no DN")
	}
	t.Logf("bound as subject=%q (address withheld from the log)", identity.Subject)

	if _, err := dir.Authenticate(context.Background(), username, password+"-wrong"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("wrong password: got %v, want ErrInvalidCredentials", err)
	}

	if _, err := dir.Authenticate(context.Background(), username, ""); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("empty password: got %v, want ErrInvalidCredentials", err)
	}
}
