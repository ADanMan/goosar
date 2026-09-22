package deliveryprofile

import "testing"

func TestParse(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    Profile
		wantErr bool
	}{
		{name: "empty selects cloud", raw: "", want: Cloud},
		{name: "whitespace selects cloud", raw: "   ", want: Cloud},
		{name: "cloud", raw: "cloud", want: Cloud},
		{name: "perimeter", raw: "perimeter", want: Perimeter},
		{name: "case and padding tolerated", raw: "  Perimeter ", want: Perimeter},
		{name: "typo is a hard error, not silent cloud", raw: "perimetr", wantErr: true},
		{name: "unknown future value is a hard error", raw: "airgap", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse(tt.raw)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Parse(%q): want error, got %q", tt.raw, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("Parse(%q): unexpected error: %v", tt.raw, err)
			}
			if got != tt.want {
				t.Fatalf("Parse(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestFromEnv(t *testing.T) {
	t.Setenv(EnvVar, "perimeter")
	got, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv: unexpected error: %v", err)
	}
	if got != Perimeter {
		t.Fatalf("FromEnv = %q, want %q", got, Perimeter)
	}

	t.Setenv(EnvVar, "")
	got, err = FromEnv()
	if err != nil {
		t.Fatalf("FromEnv with empty env: unexpected error: %v", err)
	}
	if got != Cloud {
		t.Fatalf("FromEnv with empty env = %q, want %q", got, Cloud)
	}
}

func TestPublicValue(t *testing.T) {
	if got := Cloud.PublicValue(); got != "" {
		t.Fatalf("Cloud.PublicValue() = %q, want empty (field omitted on cloud)", got)
	}
	if got := Perimeter.PublicValue(); got != "perimeter" {
		t.Fatalf("Perimeter.PublicValue() = %q, want %q", got, "perimeter")
	}
}

func TestIsPerimeterAdvertised(t *testing.T) {
	tests := []struct {
		raw  string
		want bool
	}{
		{"perimeter", true},
		{" Perimeter ", true},
		{"", false},
		{"cloud", false},

		{"airgap", false},
	}
	for _, tt := range tests {
		if got := IsPerimeterAdvertised(tt.raw); got != tt.want {
			t.Fatalf("IsPerimeterAdvertised(%q) = %v, want %v", tt.raw, got, tt.want)
		}
	}
}
