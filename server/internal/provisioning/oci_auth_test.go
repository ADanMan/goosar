package provisioning

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOCIPackageStore_BearerChallenge(t *testing.T) {
	reg := newStubRegistry()
	fixture := ociFixtureManifest()
	manifestJSON, err := json.Marshal(fixture)
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	blob := []byte("fake oci tar.zst content")
	reg.publish("provisioning/"+fixture.Name, fixture.Version, manifestJSON, blob)

	inner := reg.server(t)

	const wantToken = "issued-registry-token"
	var tokenRequests int

	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, req *http.Request) {
		tokenRequests++
		user, pass, ok := req.BasicAuth()
		if !ok || user != "operator" || pass != "pat-with-read-packages" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"token": wantToken})
	})
	var gateway *httptest.Server
	mux.HandleFunc("/v2/", func(w http.ResponseWriter, req *http.Request) {
		if req.Header.Get("Authorization") != "Bearer "+wantToken {
			w.Header().Set("WWW-Authenticate", fmt.Sprintf(
				`Bearer realm="%s/token",service="registry",scope="repository:x:pull"`, gateway.URL))
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		proxyReq, _ := http.NewRequestWithContext(req.Context(), req.Method, inner.URL+req.URL.Path, nil)
		proxyReq.Header.Set("Accept", req.Header.Get("Accept"))
		resp, err := inner.Client().Do(proxyReq)
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		w.WriteHeader(resp.StatusCode)
		buf := make([]byte, 32*1024)
		for {
			n, readErr := resp.Body.Read(buf)
			if n > 0 {
				w.Write(buf[:n])
			}
			if readErr != nil {
				break
			}
		}
	})
	gateway = httptest.NewServer(mux)
	t.Cleanup(gateway.Close)

	store := &OCIPackageStore{
		BaseURL:    gateway.URL,
		Repository: "provisioning",
		Username:   "operator",
		Password:   "pat-with-read-packages",
		HTTPClient: gateway.Client(),
	}

	got, err := store.Manifest(context.Background(), fixture.Name, fixture.Version)
	if err != nil {
		t.Fatalf("Manifest() through a bearer-challenging registry: %v", err)
	}
	if got.Name != fixture.Name || got.Version != fixture.Version {
		t.Fatalf("Manifest() = %s@%s, want %s@%s", got.Name, got.Version, fixture.Name, fixture.Version)
	}

	before := tokenRequests
	if _, _, err := store.Blob(context.Background(), fixture.Name, fixture.Version); err != nil {
		t.Fatalf("Blob() through a bearer-challenging registry: %v", err)
	}
	if tokenRequests != before {
		t.Fatalf("token endpoint hit %d extra times, want the cached token reused", tokenRequests-before)
	}
}

func TestOCIPackageStore_BearerChallengeBadCredentials(t *testing.T) {
	var gateway *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	mux.HandleFunc("/v2/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Bearer realm="%s/token",service="registry"`, gateway.URL))
		w.WriteHeader(http.StatusUnauthorized)
	})
	gateway = httptest.NewServer(mux)
	t.Cleanup(gateway.Close)

	store := &OCIPackageStore{BaseURL: gateway.URL, Username: "u", Password: "bad", HTTPClient: gateway.Client()}
	_, err := store.Manifest(context.Background(), "office-docx", "1.4.0")
	if err == nil {
		t.Fatal("Manifest() succeeded against a registry that rejected the credentials")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "auth") {
		t.Fatalf("Manifest() error = %v, want it to name an authentication failure", err)
	}
}

func TestOCIPackageStore_RefusesPlaintextAuthRealm(t *testing.T) {
	var hits int
	realm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		json.NewEncoder(w).Encode(map[string]any{"token": "should-never-be-issued"})
	}))
	t.Cleanup(realm.Close)

	store := &OCIPackageStore{
		BaseURL:    "https://registry.internal",
		Username:   "operator",
		Password:   "pat-with-read-packages",
		HTTPClient: realm.Client(),
	}
	_, err := store.fetchRegistryToken(context.Background(), bearerChallenge{realm: realm.URL + "/token"})
	if err == nil {
		t.Fatal("fetchRegistryToken() followed an http realm from an https registry")
	}
	if hits != 0 {
		t.Fatalf("credentials were sent to the plaintext realm %d time(s)", hits)
	}
}
