package handler

import (
	"strings"
	"testing"

	"github.com/adanman/goosar/server/internal/deliveryprofile"
)

func TestRequireSecretKeyForPerimeter(t *testing.T) {
	tests := []struct {
		name          string
		profile       deliveryprofile.Profile
		keyConfigured bool
		wantErr       bool
	}{
		{"perimeter without key refuses startup", deliveryprofile.Perimeter, false, true},
		{"perimeter with key starts", deliveryprofile.Perimeter, true, false},
		{"cloud without key still starts (warn-only)", deliveryprofile.Cloud, false, false},
		{"cloud with key starts", deliveryprofile.Cloud, true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := RequireSecretKeyForPerimeter(tt.profile, tt.keyConfigured)
			if tt.wantErr != (err != nil) {
				t.Fatalf("RequireSecretKeyForPerimeter(%q, %v) error = %v, wantErr %v",
					tt.profile, tt.keyConfigured, err, tt.wantErr)
			}
		})
	}
}

func TestRequireSecretKeyForPerimeterMessageIsActionable(t *testing.T) {
	err := RequireSecretKeyForPerimeter(deliveryprofile.Perimeter, false)
	if err == nil {
		t.Fatal("want an error")
	}
	for _, want := range []string{
		"GOOSAR_MCP_SECRET_KEY",
		deliveryprofile.EnvVar,
		"openssl rand -base64 32",
		"plaintext",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error message does not mention %q: %s", want, err)
		}
	}
}

func TestMissingSecretKeyWarningNamesPlaintextCredentials(t *testing.T) {
	msg := strings.ToLower(MissingSecretKeyWarning())
	for _, want := range []string{"plaintext", "backup", "openssl rand -base64 32"} {
		if !strings.Contains(msg, want) {
			t.Errorf("warning does not mention %q: %s", want, msg)
		}
	}
}
