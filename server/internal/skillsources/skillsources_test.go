package skillsources

import (
	"reflect"
	"strings"
	"testing"

	"github.com/adanman/goosar/server/internal/deliveryprofile"
)

func TestParseMatrix(t *testing.T) {
	cases := []struct {
		name         string
		raw          string
		profile      deliveryprofile.Profile
		wantErr      bool
		wantEnabled  map[Source]bool
		wantExplicit bool
	}{
		{
			name:         "cloud unset keeps all sources enabled implicitly",
			raw:          "",
			profile:      deliveryprofile.Cloud,
			wantEnabled:  map[Source]bool{ClawHub: true, GitHub: true, SkillsSh: true},
			wantExplicit: false,
		},
		{
			name:         "cloud whitespace-only behaves like unset",
			raw:          "   ",
			profile:      deliveryprofile.Cloud,
			wantEnabled:  map[Source]bool{ClawHub: true, GitHub: true, SkillsSh: true},
			wantExplicit: false,
		},
		{
			name:         "perimeter unset disables every source",
			raw:          "",
			profile:      deliveryprofile.Perimeter,
			wantEnabled:  map[Source]bool{ClawHub: false, GitHub: false, SkillsSh: false},
			wantExplicit: true,
		},
		{
			name:         "explicit none disables every source on cloud",
			raw:          "none",
			profile:      deliveryprofile.Cloud,
			wantEnabled:  map[Source]bool{ClawHub: false, GitHub: false, SkillsSh: false},
			wantExplicit: true,
		},
		{
			name:         "perimeter with explicit github enables only github",
			raw:          "github",
			profile:      deliveryprofile.Perimeter,
			wantEnabled:  map[Source]bool{ClawHub: false, GitHub: true, SkillsSh: false},
			wantExplicit: true,
		},
		{
			name:         "cloud with clawhub+skillssh disables github",
			raw:          "clawhub,skillssh",
			profile:      deliveryprofile.Cloud,
			wantEnabled:  map[Source]bool{ClawHub: true, GitHub: false, SkillsSh: true},
			wantExplicit: true,
		},
		{
			name:         "full explicit list stays explicit so it is advertised",
			raw:          "clawhub,github,skillssh",
			profile:      deliveryprofile.Cloud,
			wantEnabled:  map[Source]bool{ClawHub: true, GitHub: true, SkillsSh: true},
			wantExplicit: true,
		},
		{
			name:         "names are case- and whitespace-insensitive",
			raw:          " ClawHub , GITHUB ",
			profile:      deliveryprofile.Perimeter,
			wantEnabled:  map[Source]bool{ClawHub: true, GitHub: true, SkillsSh: false},
			wantExplicit: true,
		},
		{
			name:    "unknown source name is a hard error",
			raw:     "clawhub,beehub",
			profile: deliveryprofile.Cloud,
			wantErr: true,
		},
		{
			name:    "separators without names are a hard error",
			raw:     ",",
			profile: deliveryprofile.Perimeter,
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			set, err := Parse(tc.raw, tc.profile)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("Parse(%q, %s): expected error, got %#v", tc.raw, tc.profile, set)
				}
				if !strings.Contains(err.Error(), EnvVar) {
					t.Fatalf("error must name %s for operators, got: %v", EnvVar, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Parse(%q, %s): unexpected error: %v", tc.raw, tc.profile, err)
			}
			for src, want := range tc.wantEnabled {
				if got := set.Enabled(src); got != want {
					t.Errorf("Enabled(%s) = %v, want %v", src, got, want)
				}
			}
			if got := set.Explicit(); got != tc.wantExplicit {
				t.Errorf("Explicit() = %v, want %v", got, tc.wantExplicit)
			}
		})
	}
}

func TestEnabledNamesStableOrderAndNeverNil(t *testing.T) {
	all, err := Parse("skillssh,github,clawhub", deliveryprofile.Cloud)
	if err != nil {
		t.Fatal(err)
	}
	if got := all.EnabledNames(); !reflect.DeepEqual(got, []string{"clawhub", "github", "skillssh"}) {
		t.Fatalf("EnabledNames() = %v, want stable alphabetical order", got)
	}

	none, err := Parse(ValueNone, deliveryprofile.Cloud)
	if err != nil {
		t.Fatal(err)
	}
	if got := none.EnabledNames(); got == nil || len(got) != 0 {
		t.Fatalf("EnabledNames() for none = %#v, want non-nil empty slice", got)
	}
}

func TestFromEnvAndEnabledFromEnv(t *testing.T) {
	t.Run("cloud default enables everything", func(t *testing.T) {
		t.Setenv(deliveryprofile.EnvVar, "")
		t.Setenv(EnvVar, "")
		for _, src := range knownSources() {
			if !EnabledFromEnv(src) {
				t.Errorf("EnabledFromEnv(%s) = false on the cloud default", src)
			}
		}
	})

	t.Run("perimeter default disables everything", func(t *testing.T) {
		t.Setenv(deliveryprofile.EnvVar, "perimeter")
		t.Setenv(EnvVar, "")
		for _, src := range knownSources() {
			if EnabledFromEnv(src) {
				t.Errorf("EnabledFromEnv(%s) = true on the perimeter default", src)
			}
		}
	})

	t.Run("explicit list wins on perimeter", func(t *testing.T) {
		t.Setenv(deliveryprofile.EnvVar, "perimeter")
		t.Setenv(EnvVar, "github")
		if !EnabledFromEnv(GitHub) {
			t.Error("github should be enabled when explicitly listed")
		}
		if EnabledFromEnv(ClawHub) || EnabledFromEnv(SkillsSh) {
			t.Error("unlisted sources must stay disabled")
		}
	})

	t.Run("malformed value fails closed", func(t *testing.T) {
		t.Setenv(deliveryprofile.EnvVar, "")
		t.Setenv(EnvVar, "beehub")
		if _, err := FromEnv(); err == nil {
			t.Fatal("FromEnv should error on an unknown source name")
		}
		for _, src := range knownSources() {
			if EnabledFromEnv(src) {
				t.Errorf("EnabledFromEnv(%s) must fail closed on a malformed value", src)
			}
		}
	})
}

func TestDisabledMessageNamesEnvVar(t *testing.T) {
	msg := DisabledMessage(ClawHub)
	if !strings.Contains(msg, EnvVar) || !strings.Contains(msg, string(ClawHub)) {
		t.Fatalf("DisabledMessage must name the source and %s, got %q", EnvVar, msg)
	}
}
