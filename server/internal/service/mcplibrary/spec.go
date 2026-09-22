// Пакет mcplibrary собирает seed библиотеки MCP развёртывания: корпоративные
// записи с теми же именами полей credential_schema, что и пресеты онбординга.
// Файл чистый: без БД и сети.
package mcplibrary

import (
	"encoding/json"
	"strings"

	"github.com/adanman/goosar/server/internal/handler"
)

const (
	JiraPersonalTokenField       = "JIRA_PERSONAL_TOKEN"
	ConfluencePersonalTokenField = "CONFLUENCE_PERSONAL_TOKEN"
	EWSUsernameField             = "EWS_USERNAME"
	EWSPasswordField             = "EWS_PASSWORD"
	Bitrix24WebhookField         = "B24_WEBHOOK"
)

var AtlassianCredentialFields = []string{JiraPersonalTokenField, ConfluencePersonalTokenField}

const (
	EnvJiraURL       = "GOOSAR_DEPLOYMENT_JIRA_URL"
	EnvConfluenceURL = "GOOSAR_DEPLOYMENT_CONFLUENCE_URL"
	EnvEWSURL        = "GOOSAR_DEPLOYMENT_EWS_URL"
	EnvBitrix24URL   = "GOOSAR_DEPLOYMENT_BITRIX24_URL"
	EnvMCPGatewayURL = "GOOSAR_DEPLOYMENT_MCP_GATEWAY_URL"
)

const (
	NameJira       = "jira"
	NameConfluence = "confluence"
	NameEWS        = "ews"
	NameBitrix24   = "bitrix24"
	NameMCPGateway = "mcp-gateway"
)

type Spec struct {
	Name             string
	Transport        string
	Config           map[string]any
	CredentialSchema []handler.McpCredentialField
	AddressKeys      []string
}

const atlassianCustomHeaders = "User-Agent=CorporateMCP/1.0 (integration)"

func jiraSpec(url string) Spec {
	return Spec{
		Name:      NameJira,
		Transport: "stdio",
		Config: map[string]any{
			"command": "mcp-atlassian",
			"env": map[string]any{
				"JIRA_URL":             url,
				JiraPersonalTokenField: "",
				"JIRA_CUSTOM_HEADERS":  atlassianCustomHeaders,
			},
		},
		CredentialSchema: []handler.McpCredentialField{
			{Key: JiraPersonalTokenField, Label: "Jira personal access token", Required: true},
		},
		AddressKeys: []string{"env.JIRA_URL"},
	}
}

func confluenceSpec(url string) Spec {
	return Spec{
		Name:      NameConfluence,
		Transport: "stdio",
		Config: map[string]any{
			"command": "mcp-atlassian",
			"env": map[string]any{
				"CONFLUENCE_URL":             url,
				ConfluencePersonalTokenField: "",
				"CONFLUENCE_CUSTOM_HEADERS":  atlassianCustomHeaders,
			},
		},
		CredentialSchema: []handler.McpCredentialField{
			{Key: ConfluencePersonalTokenField, Label: "Confluence personal access token", Required: true},
		},
		AddressKeys: []string{"env.CONFLUENCE_URL"},
	}
}

func ewsSpec(url string) Spec {
	return Spec{
		Name:      NameEWS,
		Transport: "stdio",
		Config: map[string]any{
			"command": "ewsmcp",
			"env": map[string]any{
				"EWS_SERVER_URL": url,
				EWSUsernameField: "",
				EWSPasswordField: "",
			},
		},
		CredentialSchema: []handler.McpCredentialField{
			{Key: EWSUsernameField, Label: "EWS mailbox username", Required: true},
			{Key: EWSPasswordField, Label: "EWS mailbox password", Required: true},
		},
		AddressKeys: []string{"env.EWS_SERVER_URL"},
	}
}

func bitrix24Spec(url string) Spec {
	return Spec{
		Name:      NameBitrix24,
		Transport: "stdio",
		Config: map[string]any{
			"command": "mcp-server-b24",
			"env": map[string]any{
				"B24_WEBHOOK_URL":    url,
				Bitrix24WebhookField: "",
			},
		},
		CredentialSchema: []handler.McpCredentialField{
			{Key: Bitrix24WebhookField, Label: "Bitrix24 webhook token", Required: true},
		},
		AddressKeys: []string{"env.B24_WEBHOOK_URL"},
	}
}

func mcpGatewaySpec(url string) Spec {
	return Spec{
		Name:      NameMCPGateway,
		Transport: "stdio",
		Config: map[string]any{
			"command": "mcp-proxy",
			"args": []any{
				"--transport", "streamablehttp",
				"-H", "Authorization", "Bearer <token>",
				url,
			},
		},
		CredentialSchema: nil,
		AddressKeys:      []string{"args"},
	}
}

func BuildSpecs(getenv func(string) string) []Spec {
	type source struct {
		env   string
		build func(string) Spec
	}
	sources := []source{
		{EnvJiraURL, jiraSpec},
		{EnvConfluenceURL, confluenceSpec},
		{EnvEWSURL, ewsSpec},
		{EnvBitrix24URL, bitrix24Spec},
		{EnvMCPGatewayURL, mcpGatewaySpec},
	}
	specs := make([]Spec, 0, len(sources))
	for _, s := range sources {
		url := strings.TrimSpace(getenv(s.env))
		if url == "" {
			continue
		}
		specs = append(specs, s.build(url))
	}
	return specs
}

func MarshalConfig(config map[string]any) ([]byte, error) {
	return json.Marshal(config)
}
