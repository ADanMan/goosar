package auth

import (
	"strings"
	"testing"
)

func TestValidateJWTSecret(t *testing.T) {
	tests := []struct {
		name    string
		appEnv  string
		secret  string
		wantErr bool
	}{
		{name: "production with empty secret refuses", appEnv: "production", secret: "", wantErr: true},
		{name: "production with whitespace secret refuses", appEnv: "production", secret: "   ", wantErr: true},
		{name: "production with dev fallback refuses", appEnv: "production", secret: defaultJWTSecret, wantErr: true},
		{name: "production with compose placeholder refuses", appEnv: "production", secret: "change-me-in-production", wantErr: true},
		{name: "production case-insensitive env match", appEnv: "Production", secret: "", wantErr: true},
		{name: "production with strong secret starts", appEnv: "production", secret: "0f3b2c1d4e5a6978aabbccddeeff00112233445566778899", wantErr: false},
		{name: "dev with empty secret starts", appEnv: "development", secret: "", wantErr: false},
		{name: "unset APP_ENV with empty secret starts", appEnv: "", secret: "", wantErr: false},
		{name: "dev with placeholder starts", appEnv: "development", secret: "change-me-in-production", wantErr: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("APP_ENV", tt.appEnv)
			t.Setenv("JWT_SECRET", tt.secret)

			err := ValidateJWTSecret()
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected refusal error, got nil")
				}

				if !strings.Contains(err.Error(), "JWT_SECRET") {
					t.Fatalf("refusal error must name JWT_SECRET, got: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("expected startup allowed, got error: %v", err)
			}
		})
	}
}

func TestUsingDevJWTSecret(t *testing.T) {
	t.Run("unset secret is the dev fallback", func(t *testing.T) {
		t.Setenv("JWT_SECRET", "")
		if !UsingDevJWTSecret() {
			t.Fatal("expected dev fallback for empty JWT_SECRET")
		}
	})
	t.Run("placeholder secret is weak", func(t *testing.T) {
		t.Setenv("JWT_SECRET", "change-me-in-production")
		if !UsingDevJWTSecret() {
			t.Fatal("expected placeholder JWT_SECRET to be reported as weak")
		}
	})
	t.Run("strong secret is not weak", func(t *testing.T) {
		t.Setenv("JWT_SECRET", "0f3b2c1d4e5a6978aabbccddeeff00112233445566778899")
		if UsingDevJWTSecret() {
			t.Fatal("expected strong JWT_SECRET to not be reported as weak")
		}
	})
}
