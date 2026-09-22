package cli

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/adanman/goosar/server/internal/deliveryprofile"
)

func TestCheckSelfUpdateAllowed_LocalPerimeterEnvRefuses(t *testing.T) {
	t.Setenv(deliveryprofile.EnvVar, "perimeter")

	err := CheckSelfUpdateAllowed("")
	if err == nil {
		t.Fatal("want refusal with GOOSAR_DELIVERY_PROFILE=perimeter, got nil")
	}
	if !strings.Contains(err.Error(), "operator") {
		t.Fatalf("refusal must point at the operator, got: %v", err)
	}
}

func TestCheckSelfUpdateAllowed_ServerPerimeterRefuses(t *testing.T) {
	t.Setenv(deliveryprofile.EnvVar, "")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/config" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"allow_signup":true,"delivery_profile":"perimeter"}`))
	}))
	defer srv.Close()

	err := CheckSelfUpdateAllowed(srv.URL)
	if err == nil {
		t.Fatal("want refusal against a perimeter server, got nil")
	}
	if !strings.Contains(err.Error(), "perimeter") {
		t.Fatalf("refusal must name the perimeter profile, got: %v", err)
	}
}

func TestCheckSelfUpdateAllowed_CloudAndUnknownProfilesAllow(t *testing.T) {
	t.Setenv(deliveryprofile.EnvVar, "")
	for _, body := range []string{
		`{"allow_signup":true}`,
		`{"allow_signup":true,"delivery_profile":"cloud"}`,
		`{"allow_signup":true,"delivery_profile":"airgap"}`,
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(body))
		}))
		if err := CheckSelfUpdateAllowed(srv.URL); err != nil {
			t.Fatalf("CheckSelfUpdateAllowed with body %s: unexpected refusal: %v", body, err)
		}
		srv.Close()
	}
}

func TestCheckSelfUpdateAllowed_FailsOpen(t *testing.T) {
	t.Setenv(deliveryprofile.EnvVar, "")

	if err := CheckSelfUpdateAllowed(""); err != nil {
		t.Fatalf("no configured server must allow: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()
	if err := CheckSelfUpdateAllowed(url); err != nil {
		t.Fatalf("unreachable server must fail open: %v", err)
	}

	srv500 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv500.Close()
	if err := CheckSelfUpdateAllowed(srv500.URL); err != nil {
		t.Fatalf("500 /api/config must fail open: %v", err)
	}
}
