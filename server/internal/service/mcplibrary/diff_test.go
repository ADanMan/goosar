package mcplibrary

import (
	"encoding/json"
	"testing"

	"github.com/adanman/goosar/server/internal/handler"
)

func TestConfigMatchesModuloAddressSameAddressDifferentIsMatch(t *testing.T) {
	spec := jiraSpec("https://jira.new.example.com")
	stored, err := json.Marshal(jiraSpec("https://jira.old.example.com").Config)
	if err != nil {
		t.Fatal(err)
	}
	if !configMatchesModuloAddress(spec, stored) {
		t.Fatal("expected match: only the address differs")
	}
}

func TestConfigMatchesModuloAddressOtherFieldDifferIsMismatch(t *testing.T) {
	spec := jiraSpec("https://jira.example.com")
	handEdited := jiraSpec("https://jira.example.com")
	handEdited.Config["command"] = "some-other-binary"
	stored, err := json.Marshal(handEdited.Config)
	if err != nil {
		t.Fatal(err)
	}
	if configMatchesModuloAddress(spec, stored) {
		t.Fatal("expected mismatch: command was hand-edited")
	}
}

func TestConfigMatchesModuloAddressCorruptStoredIsMismatch(t *testing.T) {
	spec := jiraSpec("https://jira.example.com")
	if configMatchesModuloAddress(spec, []byte("not json")) {
		t.Fatal("corrupt stored config must never be treated as a match")
	}
}

func TestCredentialSchemaMatchesNilVsEmptyArray(t *testing.T) {
	spec := mcpGatewaySpec("https://gw.example.com/mcp-proxy")
	if !credentialSchemaMatches(spec, []byte("[]")) {
		t.Fatal("nil spec schema must match a stored empty array")
	}
	if !credentialSchemaMatches(spec, nil) {
		t.Fatal("nil spec schema must match an absent column")
	}
}

func TestCredentialSchemaMatchesDetectsHandEdit(t *testing.T) {
	spec := jiraSpec("https://jira.example.com")
	edited := []handler.McpCredentialField{
		{Key: "SOMETHING_ELSE", Required: true},
	}
	stored, err := json.Marshal(edited)
	if err != nil {
		t.Fatal(err)
	}
	if credentialSchemaMatches(spec, stored) {
		t.Fatal("expected mismatch: schema was hand-edited")
	}
}

func TestCredentialSchemaMatchesIdentical(t *testing.T) {
	spec := jiraSpec("https://jira.example.com")
	stored, err := json.Marshal(spec.CredentialSchema)
	if err != nil {
		t.Fatal(err)
	}
	if !credentialSchemaMatches(spec, stored) {
		t.Fatal("expected match: identical schema")
	}
}
