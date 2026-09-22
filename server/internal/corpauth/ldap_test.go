package corpauth

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type FakeDirectory struct {
	Users map[string]FakeDirectoryUser

	Err error

	Calls int

	LastPassword string
}

type FakeDirectoryUser struct {
	Password string
	DN       string
	Email    string
	Name     string
	IsAdmin  bool
}

func (f *FakeDirectory) Authenticate(_ context.Context, username, password string) (Identity, error) {
	f.Calls++
	f.LastPassword = password
	if f.Err != nil {
		return Identity{}, f.Err
	}

	if strings.TrimSpace(username) == "" || password == "" {
		return Identity{}, ErrInvalidCredentials
	}
	rec, ok := f.Users[strings.ToLower(strings.TrimSpace(username))]

	if !ok || rec.Password != password {
		return Identity{}, ErrInvalidCredentials
	}
	if rec.Email == "" {
		return Identity{}, ErrNoEmail
	}
	return Identity{
		Provider: MethodLDAP,
		Subject:  NormalizeDN(rec.DN),
		Email:    strings.ToLower(rec.Email),
		Name:     rec.Name,
		IsAdmin:  rec.IsAdmin,
	}, nil
}

func newFakeDirectory() *FakeDirectory {
	return &FakeDirectory{Users: map[string]FakeDirectoryUser{
		"jdoe": {
			Password: "correct-horse-battery-staple",
			DN:       "CN=John Doe,OU=Users,DC=example,DC=test",
			Email:    "J.Doe@example.test",
			Name:     "John Doe",
		},
		"admin": {
			Password: "admin-password",
			DN:       "CN=Ann Admin,OU=Users,DC=example,DC=test",
			Email:    "ann@example.test",
			Name:     "Ann Admin",
			IsAdmin:  true,
		},
	}}
}

func TestFakeDirectory_BindOutcomes(t *testing.T) {
	dir := newFakeDirectory()

	t.Run("correct pair binds", func(t *testing.T) {
		id, err := dir.Authenticate(context.Background(), "jdoe", "correct-horse-battery-staple")
		if err != nil {
			t.Fatalf("Authenticate: %v", err)
		}
		if id.Provider != MethodLDAP {
			t.Errorf("Provider: got %q", id.Provider)
		}
		if id.Email != "j.doe@example.test" {
			t.Errorf("Email: got %q, want it folded to lower case", id.Email)
		}
		if id.Subject != "cn=john doe,ou=users,dc=example,dc=test" {
			t.Errorf("Subject: got %q, want the normalized DN", id.Subject)
		}
	})

	t.Run("wrong password is refused", func(t *testing.T) {
		if _, err := dir.Authenticate(context.Background(), "jdoe", "hunter2"); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("got %v, want ErrInvalidCredentials", err)
		}
	})

	t.Run("unknown user is refused indistinguishably", func(t *testing.T) {
		_, unknownErr := dir.Authenticate(context.Background(), "nobody", "whatever")
		_, wrongPwErr := dir.Authenticate(context.Background(), "jdoe", "wrong")
		if !errors.Is(unknownErr, ErrInvalidCredentials) {
			t.Fatalf("unknown user: got %v, want ErrInvalidCredentials", unknownErr)
		}
		if unknownErr.Error() != wrongPwErr.Error() {
			t.Fatalf("an unknown user and a wrong password answer differently (%q vs %q): the form enumerates the directory",
				unknownErr, wrongPwErr)
		}
	})

	t.Run("empty password never binds", func(t *testing.T) {
		if _, err := dir.Authenticate(context.Background(), "jdoe", ""); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("an empty password bound as %v — a conforming LDAP server answers an empty simple bind with SUCCESS", err)
		}
	})

	t.Run("directory down is named, not fatal", func(t *testing.T) {
		down := &FakeDirectory{Err: ErrUnavailable}
		if _, err := down.Authenticate(context.Background(), "jdoe", "x"); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("got %v, want ErrUnavailable", err)
		}
	})
}

func TestLDAPConfig_RequiresEncryptedTransport(t *testing.T) {
	cases := []struct {
		name     string
		url      string
		startTLS bool
		want     bool
	}{
		{"ldaps", "ldaps://dc.example.test:636", false, true},
		{"ldap with StartTLS", "ldap://dc.example.test:389", true, true},
		{"ldap in the clear", "ldap://dc.example.test:389", false, false},
		{"LDAPS upper case", "LDAPS://dc.example.test:636", false, true},
		{"no scheme at all", "dc.example.test", false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := LDAPConfig{URL: tc.url, StartTLS: tc.startTLS, BaseDN: "DC=example,DC=test"}
			if got := cfg.EncryptedTransport(); got != tc.want {
				t.Fatalf("EncryptedTransport: got %v, want %v", got, tc.want)
			}

			if dir := NewLDAPDirectory(cfg); (dir != nil) != tc.want {
				t.Fatalf("NewLDAPDirectory returned non-nil=%v for an encrypted=%v transport", dir != nil, tc.want)
			}
		})
	}
}

func TestLDAPConfig_Configured(t *testing.T) {
	if (LDAPConfig{URL: "ldaps://dc.example.test"}).Configured() {
		t.Fatal("a configuration with no base DN reported as Configured()")
	}
	if (LDAPConfig{BaseDN: "DC=example,DC=test"}).Configured() {
		t.Fatal("a configuration with no URL reported as Configured()")
	}
	if !(LDAPConfig{URL: "ldaps://dc.example.test", BaseDN: "DC=example,DC=test"}).Configured() {
		t.Fatal("a complete configuration reported as not Configured()")
	}
}

func TestNormalizeDN(t *testing.T) {
	cases := []struct{ in, want string }{
		{"CN=John Doe,OU=Users,DC=example,DC=test", "cn=john doe,ou=users,dc=example,dc=test"},
		{"CN=John Doe, OU=Users, DC=example, DC=test", "cn=john doe,ou=users,dc=example,dc=test"},
		{"cn=john doe,ou=users,dc=example,dc=test", "cn=john doe,ou=users,dc=example,dc=test"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := NormalizeDN(tc.in); got != tc.want {
			t.Errorf("NormalizeDN(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestLDAPConfigFromEnv_Defaults(t *testing.T) {
	t.Setenv(LDAPURLEnvVar, "ldaps://dc.example.test:636")
	t.Setenv(LDAPBaseDNEnvVar, "OU=Users,DC=example,DC=test")
	t.Setenv(LDAPUserFilterEnvVar, "")
	t.Setenv(LDAPEmailAttrEnvVar, "")
	t.Setenv(LDAPNameAttrEnvVar, "")

	cfg := LDAPConfigFromEnv()

	if !strings.Contains(cfg.UserFilter, "sAMAccountName") || !strings.Contains(cfg.UserFilter, "userPrincipalName") {
		t.Fatalf("default user filter %q must match both AD login forms", cfg.UserFilter)
	}
	if cfg.EmailAttr != "mail" {
		t.Errorf("default email attribute: got %q, want mail", cfg.EmailAttr)
	}
	if cfg.NameAttr != "displayName" {
		t.Errorf("default name attribute: got %q, want displayName", cfg.NameAttr)
	}
}

func TestParseMethods(t *testing.T) {
	cases := []struct {
		name  string
		raw   string
		email bool
		oidc  bool
		ldap  bool
	}{
		{"absent means email, exactly as before #394", "", true, false, false},
		{"email only", "email", true, false, false},
		{"email and oidc", "email,oidc", true, true, false},
		{"spaces and case", " Email , OIDC ", true, true, false},
		{"oidc alone disables the email form", "oidc", false, true, false},
		{"all three", "email,oidc,ldap", true, true, true},

		{"typo falls back to email", "oidk", true, false, false},
		{"unknown alongside a known method is ignored", "oidc,saml", false, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := ParseMethods(tc.raw)
			if m.Email != tc.email || m.OIDC != tc.oidc || m.LDAP != tc.ldap {
				t.Fatalf("ParseMethods(%q) = %+v, want email=%v oidc=%v ldap=%v",
					tc.raw, m, tc.email, tc.oidc, tc.ldap)
			}
			if !m.Any() {
				t.Fatal("ParseMethods produced a deployment nobody can sign into")
			}
		})
	}
}
