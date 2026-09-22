package redact

import (
	"strings"
	"testing"
)

func TestRedactAWSAccessKey(t *testing.T) {
	t.Parallel()
	input := "Found key AKIAIOSFODNN7EXAMPLE in config"
	got := Text(input)
	if strings.Contains(got, "AKIAIOSFODNN7EXAMPLE") {
		t.Fatalf("AWS key not redacted: %s", got)
	}
	if !strings.Contains(got, "[REDACTED AWS KEY]") {
		t.Fatalf("expected [REDACTED AWS KEY] placeholder, got: %s", got)
	}
}

func TestRedactAWSSecretKey(t *testing.T) {
	t.Parallel()
	input := "aws_secret_access_key = wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"
	got := Text(input)
	if strings.Contains(got, "wJalrXUtnFEMI") {
		t.Fatalf("AWS secret not redacted: %s", got)
	}
}

func TestRedactPrivateKey(t *testing.T) {
	t.Parallel()
	input := "Here is the key:\n-----BEGIN RSA PRIVATE KEY-----\nMIIEow...\n-----END RSA PRIVATE KEY-----\nDone."
	got := Text(input)
	if strings.Contains(got, "MIIEow") {
		t.Fatalf("private key content not redacted: %s", got)
	}
	if !strings.Contains(got, "[REDACTED PRIVATE KEY]") {
		t.Fatalf("expected [REDACTED PRIVATE KEY] placeholder, got: %s", got)
	}
}

func TestRedactGitHubToken(t *testing.T) {
	t.Parallel()
	input := "export GITHUB_TOKEN=ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmn"
	got := Text(input)
	if strings.Contains(got, "ghp_") {
		t.Fatalf("GitHub token not redacted: %s", got)
	}
}

func asm(parts ...string) string { return strings.Join(parts, "") }

func TestRedactGitHubFineGrainedToken(t *testing.T) {
	t.Parallel()
	input := "cloning with token " + asm("github_", "pat_", "11ABCDE0Q0abcdefghijkl_MNOPQRSTUVWXYZ0123456789abcdefghijklmnopqrstuvwxyzABCD")
	got := Text(input)
	if strings.Contains(got, asm("github_", "pat_", "11ABCDE0Q0")) {
		t.Fatalf("fine-grained GitHub PAT not redacted: %s", got)
	}
	if !strings.Contains(got, "[REDACTED GITHUB TOKEN]") {
		t.Fatalf("expected [REDACTED GITHUB TOKEN] placeholder, got: %s", got)
	}
}

func TestRedactGoogleAPIKeyEndingWithDash(t *testing.T) {
	t.Parallel()
	input := `the config file still had "` + asm("AIza", "SyB1cD3fGhIjKlMnOpQrStUvWxYz012345-") + `" in it`
	got := Text(input)
	if strings.Contains(got, asm("AIza", "SyB1cD3f")) {
		t.Fatalf("Google API key ending with dash not redacted: %s", got)
	}
	if !strings.Contains(got, `"[REDACTED GOOGLE API KEY]" in it`) {
		t.Fatalf("expected delimiter to be preserved, got: %s", got)
	}
}

func TestRedactOpenAIKey(t *testing.T) {
	t.Parallel()
	input := "OPENAI_API_KEY=sk-proj-abc123def456ghi789jkl012mno345"
	got := Text(input)
	if strings.Contains(got, "sk-proj-abc123") {
		t.Fatalf("OpenAI key not redacted: %s", got)
	}
}

func TestRedactSlackToken(t *testing.T) {
	t.Parallel()
	input := "token: xoxb-123456789012-1234567890123-AbCdEfGhIjKl"
	got := Text(input)
	if strings.Contains(got, "xoxb-") {
		t.Fatalf("Slack token not redacted: %s", got)
	}
}

func TestRedactSlackAppAndConfigTokens(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, in, leak string }{
		{"app-level xapp-", "connecting with " + asm("xa", "pp-1-A0000000000-1111111111-abcdefdeadbeefcafe") + " now", asm("xa", "pp-1-A0000000000")},
		{"config xoxe-", "refresh with " + asm("xo", "xe-1-My0abcdefghijklmnopqrstuvwx") + " now", asm("xo", "xe-1-My0abcdef")},
	} {
		got := Text(tc.in)
		if strings.Contains(got, tc.leak) {
			t.Fatalf("%s not redacted: %s", tc.name, got)
		}
		if !strings.Contains(got, "[REDACTED SLACK TOKEN]") {
			t.Fatalf("%s: expected [REDACTED SLACK TOKEN], got: %s", tc.name, got)
		}
	}
}

func TestRedactGoogleAPIKey(t *testing.T) {
	t.Parallel()
	input := "calling gemini with " + asm("AIza", "SyD1234567890abcdefghijklmnopqrstuv") + " now"
	got := Text(input)
	if strings.Contains(got, asm("AIza", "SyD1234567890")) {
		t.Fatalf("Google API key not redacted: %s", got)
	}
	if !strings.Contains(got, "[REDACTED GOOGLE API KEY]") {
		t.Fatalf("expected [REDACTED GOOGLE API KEY], got: %s", got)
	}
}

func TestRedactStripeLiveKey(t *testing.T) {
	t.Parallel()
	if got := Text("STRIPE_SECRET_KEY=" + asm("sk_", "live_", "51Abcdef0000000000000000")); strings.Contains(got, asm("sk_", "live_51Abcdef")) {
		t.Fatalf("Stripe live key not redacted: %s", got)
	}
	if got := Text("restricted " + asm("rk_", "live_", "51Abcdef0000000000000000")); !strings.Contains(got, "[REDACTED STRIPE KEY]") {
		t.Fatalf("Stripe restricted key not redacted: %s", got)
	}

	if got := Text(asm("pk_", "live_", "51Abcdef0000000000000000")); !strings.Contains(got, asm("pk_", "live_51Abcdef")) {
		t.Fatalf("publishable key should NOT be redacted: %s", got)
	}
}

func TestRedactBearerToken(t *testing.T) {
	t.Parallel()
	input := "Authorization: Bearer eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.abc123"
	got := Text(input)
	if strings.Contains(got, "eyJhbGci") {
		t.Fatalf("Bearer token not redacted: %s", got)
	}
}

func TestRedactBearerMCPToken(t *testing.T) {
	t.Parallel()
	input := "connecting with Authorization: Bearer mcp_AbCdEf0123456789-_token"
	got := Text(input)
	if strings.Contains(got, "mcp_AbCdEf0123456789") {
		t.Fatalf("Bearer mcp_ token not redacted: %s", got)
	}
	if !strings.Contains(got, "Bearer [REDACTED]") {
		t.Fatalf("expected Bearer [REDACTED] placeholder, got: %s", got)
	}
}

func TestRedactGenericCredentials(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		input string
	}{
		{"API_KEY", "API_KEY=mysupersecretkey123"},
		{"DATABASE_URL", "DATABASE_URL=postgres://user:pass@host/db"},
		{"DB_PASSWORD", "DB_PASSWORD: hunter2"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Text(tc.input)
			if !strings.Contains(got, "[REDACTED CREDENTIAL]") {
				t.Fatalf("expected credential redaction for %s, got: %s", tc.name, got)
			}
		})
	}
}

func TestRedactHomeDirectory(t *testing.T) {
	t.Parallel()
	if homeDir == "" || username == "" {
		t.Skip("cannot determine home dir or username")
	}
	input := "Reading file at " + homeDir + "/Documents/secret.txt"
	got := Text(input)
	if strings.Contains(got, username) {
		t.Fatalf("home directory username not redacted: %s", got)
	}
	if !strings.Contains(got, "****") {
		t.Fatalf("expected **** in path, got: %s", got)
	}
}

func TestNoFalsePositivesOnNormalText(t *testing.T) {
	t.Parallel()
	inputs := []string{
		"This is a normal commit message about fixing a bug",
		"The function returns skip-navigation as the class name",
		"Created PR #42 for the authentication feature",
		"Running tests in /tmp/test-workspace/project",
		"The API endpoint /api/issues/123 was updated",
	}
	for _, input := range inputs {
		got := Text(input)
		if got != input {
			t.Fatalf("false positive redaction:\n  input:  %s\n  output: %s", input, got)
		}
	}
}

func TestRedactGitLabToken(t *testing.T) {
	t.Parallel()
	input := "GITLAB_TOKEN=glpat-AbCdEfGhIjKlMnOpQrStUvWx"
	got := Text(input)
	if strings.Contains(got, "glpat-") {
		t.Fatalf("GitLab token not redacted: %s", got)
	}
}

func TestRedactJWT(t *testing.T) {
	t.Parallel()
	input := "token: eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c"
	got := Text(input)
	if strings.Contains(got, "eyJhbGci") {
		t.Fatalf("JWT not redacted: %s", got)
	}
}

func TestRedactConnectionString(t *testing.T) {
	t.Parallel()
	input := "connecting to postgres://admin:s3cret@db.example.com:5432/mydb"
	got := Text(input)
	if strings.Contains(got, "s3cret") {
		t.Fatalf("connection string password not redacted: %s", got)
	}
}

func TestRedactPasswordEnvVar(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		input string
	}{
		{"PASSWORD", "PASSWORD=hunter2"},
		{"SECRET", "SECRET=mysecretvalue"},
		{"TOKEN", "TOKEN=abc123xyz"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Text(tc.input)
			if !strings.Contains(got, "[REDACTED CREDENTIAL]") {
				t.Fatalf("expected credential redaction for %s, got: %s", tc.name, got)
			}
		})
	}
}

func TestInputMap(t *testing.T) {
	t.Parallel()
	m := map[string]any{
		"command":   "echo sk-proj-abc123def456ghi789jkl012mno345",
		"file_path": "/tmp/test.txt",
		"count":     42,
	}
	got := InputMap(m)
	if s, ok := got["command"].(string); ok {
		if strings.Contains(s, "sk-proj") {
			t.Fatalf("API key in input map not redacted: %s", s)
		}
	}

	if got["count"] != 42 {
		t.Fatalf("non-string value altered: %v", got["count"])
	}

	if got["file_path"] != "/tmp/test.txt" {
		t.Fatalf("clean string altered: %v", got["file_path"])
	}
}

func TestInputMapNil(t *testing.T) {
	t.Parallel()
	if got := InputMap(nil); got != nil {
		t.Fatalf("expected nil, got: %v", got)
	}
}

func TestRedactMultipleSecrets(t *testing.T) {
	t.Parallel()
	input := "Keys: AKIAIOSFODNN7EXAMPLE and ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmn"
	got := Text(input)
	if strings.Contains(got, "AKIAIOSFODNN7EXAMPLE") {
		t.Fatal("AWS key not redacted in multi-secret text")
	}
	if strings.Contains(got, "ghp_") {
		t.Fatal("GitHub token not redacted in multi-secret text")
	}
}

func TestRedactStructuredAndGoosarCredentials(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		input  string
		secret string
	}{
		{"goosar PAT in cli config", `"token": "gsl_0123456789abcdef0123456789abcdef01234567"`, "gsl_0123456789abcdef0123456789abcdef01234567"},
		{"bare daemon token in prose", "registered daemon mdt_89abcdef89abcdef89abcdef89abcdef89abcdef ok", "mdt_89abcdef89abcdef89abcdef89abcdef89abcdef"},
		{"bare agent task token", "spawning with mat_11112222333344445555666677778888aaaabbbb", "mat_11112222333344445555666677778888aaaabbbb"},
		{"json password", `{"level":"info","password":"Zaq12wsx1C"}`, "Zaq12wsx1C"},
		{"yaml secret key", `  secret_key: "kQb2mVsecretvalue"`, "kQb2mVsecretvalue"},
		{"1c connection string", `Usr="Администратор";Pwd="Qwerty123";`, "Qwerty123"},
		{"russian password label", "пароль 1С: Zaq12wsx!", "Zaq12wsx!"},
		{"http basic header", "Authorization: Basic c3RhdGlvbjpQYTU1dzByZFNlY3JldA==", "c3RhdGlvbjpQYTU1dzByZFNlY3JldA"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := Text(tc.input)
			if strings.Contains(got, tc.secret) {
				t.Fatalf("secret survived redaction\n  in:  %s\n  out: %s", tc.input, got)
			}
			if !strings.Contains(got, "[REDACTED") {
				t.Fatalf("nothing was redacted\n  in:  %s\n  out: %s", tc.input, got)
			}
		})
	}
}

func TestRedactStructured_LeavesOperatorIdentifiers(t *testing.T) {
	t.Parallel()
	kept := []string{
		`{"request_id":"c0ffee-000012","issue_id":"018f3a2b-4c5d-6e7f-8091-a2b3c4d5e6f7"}`,
		`{"migration":"000296","applied":296}`,
		`{"addr":"127.0.0.1:8081","pid":89123}`,
		"Basic auth is required for this endpoint",
		`{"server_version":"v1.0.0","min_daemon_version":"0.2.21"}`,
	}
	for _, in := range kept {
		if got := Text(in); got != in {
			t.Errorf("input was modified\n  in:  %s\n  out: %s", in, got)
		}
	}
}
