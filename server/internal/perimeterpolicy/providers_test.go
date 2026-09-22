package perimeterpolicy

import (
	"strings"
	"testing"

	"github.com/adanman/goosar/server/internal/deliveryprofile"
)

func TestParseProviderPolicy_CloudUnsetIsNil(t *testing.T) {
	for _, raw := range []string{"", "  ", ", ,"} {
		p, err := ParseProviderPolicy(deliveryprofile.Cloud, raw)
		if err != nil {
			t.Fatalf("ParseProviderPolicy(cloud, %q): %v", raw, err)
		}
		if p != nil {
			t.Errorf("ParseProviderPolicy(cloud, %q) = %+v, want nil", raw, p)
		}
		if !p.Allows("runtime-c") || !p.Allows("runtime-j") {
			t.Error("nil policy must allow every provider")
		}
		if p.AllowedList() != nil {
			t.Error("nil policy must report a nil list (field omitted from /api/config)")
		}
	}
}

func TestParseProviderPolicy_PerimeterUnsetIsInHouseRuntimeOnly(t *testing.T) {
	p, err := ParseProviderPolicy(deliveryprofile.Perimeter, "")
	if err != nil {
		t.Fatalf("ParseProviderPolicy: %v", err)
	}
	if p == nil {
		t.Fatal("perimeter policy must not be nil")
	}
	if !p.Allows("runtime-j") {
		t.Error("the in-house runtime must be allowed on perimeter by default")
	}
	for _, slug := range []string{"runtime-c", "runtime-e", "runtime-g"} {
		if p.Allows(slug) {
			t.Errorf("provider %q must be rejected on perimeter by default", slug)
		}
	}
	if got := strings.Join(p.AllowedList(), ","); got != "runtime-j" {
		t.Errorf("AllowedList = %q, want runtime-j", got)
	}
}

func TestParseProviderPolicy_PerimeterEnvExtendsInHouseRuntime(t *testing.T) {
	p, err := ParseProviderPolicy(deliveryprofile.Perimeter, " Runtime-C , runtime-e ")
	if err != nil {
		t.Fatalf("ParseProviderPolicy: %v", err)
	}
	for _, slug := range []string{"runtime-j", "runtime-c", "runtime-e"} {
		if !p.Allows(slug) {
			t.Errorf("provider %q must be allowed", slug)
		}
	}
	if p.Allows("runtime-g") {
		t.Error("unlisted provider must stay rejected")
	}
	if got := strings.Join(p.AllowedList(), ","); got != "runtime-c,runtime-e,runtime-j" {
		t.Errorf("AllowedList = %q, want runtime-c,runtime-e,runtime-j", got)
	}
}

func TestParseProviderPolicy_CloudEnvSetIsExact(t *testing.T) {
	p, err := ParseProviderPolicy(deliveryprofile.Cloud, "runtime-c")
	if err != nil {
		t.Fatalf("ParseProviderPolicy: %v", err)
	}
	if !p.Allows("runtime-c") || !p.Allows(" RUNTIME-C ") {
		t.Error("listed provider must be allowed, case/space-insensitively")
	}
	if p.Allows("runtime-j") {
		t.Error("cloud env list is exact — the in-house runtime is not implicitly added outside perimeter")
	}
}

func TestParseProviderPolicy_InvalidSlugIsHardError(t *testing.T) {
	for _, raw := range []string{"runtime-zz", "runtime-j,not-a-provider", "openai"} {
		if _, err := ParseProviderPolicy(deliveryprofile.Perimeter, raw); err == nil {
			t.Errorf("ParseProviderPolicy(%q): want error for unknown slug", raw)
		} else if !strings.Contains(err.Error(), EnvAllowedProviders) {
			t.Errorf("error must name %s: %v", EnvAllowedProviders, err)
		}
	}
}
