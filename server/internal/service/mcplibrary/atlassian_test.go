package mcplibrary

import "testing"

func TestAtlassianCredentialFields(t *testing.T) {
	want := []string{"JIRA_PERSONAL_TOKEN", "CONFLUENCE_PERSONAL_TOKEN"}
	if len(AtlassianCredentialFields) != len(want) {
		t.Fatalf("got %d fields, want %d: %v", len(AtlassianCredentialFields), len(want), AtlassianCredentialFields)
	}
	for i, field := range want {
		if AtlassianCredentialFields[i] != field {
			t.Errorf("field %d: got %q, want %q", i, AtlassianCredentialFields[i], field)
		}
	}

	forbidden := []string{"JIRA_USERNAME", "JIRA_API_TOKEN", "CONFLUENCE_USERNAME", "CONFLUENCE_API_TOKEN"}
	for _, bad := range forbidden {
		for _, field := range AtlassianCredentialFields {
			if field == bad {
				t.Errorf("AtlassianCredentialFields must not contain %q", bad)
			}
		}
	}
}
