package mcplibrary

import "testing"

func TestBuildSpecsSkipsEmptyAddresses(t *testing.T) {
	env := map[string]string{
		EnvJiraURL: "https://jira.example.com",
		EnvEWSURL:  "",
	}
	getenv := func(k string) string { return env[k] }
	specs := BuildSpecs(getenv)
	if len(specs) != 1 {
		t.Fatalf("expected exactly one spec, got %d: %+v", len(specs), specs)
	}
	if specs[0].Name != NameJira {
		t.Fatalf("expected jira spec, got %s", specs[0].Name)
	}
}

func TestBuildSpecsAllFive(t *testing.T) {
	env := map[string]string{
		EnvJiraURL:       "https://jira.example.com",
		EnvConfluenceURL: "https://wiki.example.com",
		EnvEWSURL:        "https://mail.example.com/EWS/Exchange.asmx",
		EnvBitrix24URL:   "https://b24.example.com/rest/",
		EnvMCPGatewayURL: "https://gw.example.com/mcp-proxy",
	}
	getenv := func(k string) string { return env[k] }
	specs := BuildSpecs(getenv)
	if len(specs) != 5 {
		t.Fatalf("expected 5 specs, got %d", len(specs))
	}
	names := map[string]bool{}
	for _, s := range specs {
		names[s.Name] = true
	}
	for _, want := range []string{NameJira, NameConfluence, NameEWS, NameBitrix24, NameMCPGateway} {
		if !names[want] {
			t.Fatalf("missing spec %q", want)
		}
	}
}

func TestBuildSpecsNoneConfigured(t *testing.T) {
	specs := BuildSpecs(func(string) string { return "" })
	if len(specs) != 0 {
		t.Fatalf("expected no specs, got %d", len(specs))
	}
}

func TestMCPGatewaySpecHasNoCredentialSchema(t *testing.T) {
	specs := BuildSpecs(func(k string) string {
		if k == EnvMCPGatewayURL {
			return "https://gw.example.com/mcp-proxy"
		}
		return ""
	})
	if len(specs) != 1 {
		t.Fatalf("expected 1 spec, got %d", len(specs))
	}
	if len(specs[0].CredentialSchema) != 0 {
		t.Fatalf("mcp-gateway must have no credential_schema, got %+v", specs[0].CredentialSchema)
	}
}

func TestAtlassianCredentialFieldsMatchesJiraConfluenceSchema(t *testing.T) {
	specs := BuildSpecs(func(k string) string {
		switch k {
		case EnvJiraURL:
			return "https://jira.example.com"
		case EnvConfluenceURL:
			return "https://wiki.example.com"
		}
		return ""
	})
	got := map[string]bool{}
	for _, s := range specs {
		for _, f := range s.CredentialSchema {
			got[f.Key] = true
		}
	}
	for _, want := range AtlassianCredentialFields {
		if !got[want] {
			t.Fatalf("expected credential field %q among %v", want, got)
		}
	}
}
