package cli

import (
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func selfSignedServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	cert := srv.Certificate()
	path := filepath.Join(t.TempDir(), "stand-root-ca.crt")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw}), 0o600); err != nil {
		t.Fatal(err)
	}
	return srv, path
}

func TestConfigureCA(t *testing.T) {
	t.Cleanup(func() { _ = ConfigureCA("") })

	t.Run("without a CA file the stand's certificate is refused and classified as TLS", func(t *testing.T) {
		if err := ConfigureCA(""); err != nil {
			t.Fatal(err)
		}
		srv, _ := selfSignedServer(t)
		_, err := (&http.Client{}).Get(srv.URL + "/health")
		if err == nil {
			t.Fatal("want a certificate error, got none")
		}
		if kind := classifyNetworkError(err); kind != KindNetworkTLS {
			t.Fatalf("classification: want %v, got %v (%v)", KindNetworkTLS, kind, err)
		}
	})

	t.Run("with the CA file every default-transport client trusts the stand", func(t *testing.T) {
		srv, caPath := selfSignedServer(t)
		if err := ConfigureCA(caPath); err != nil {
			t.Fatal(err)
		}

		resp, err := (&http.Client{}).Get(srv.URL + "/health")
		if err != nil {
			t.Fatalf("bare client: %v", err)
		}
		resp.Body.Close()

		cloned := &http.Client{Transport: http.DefaultTransport.(*http.Transport).Clone()}
		resp, err = cloned.Get(srv.URL + "/health")
		if err != nil {
			t.Fatalf("cloned transport: %v", err)
		}
		resp.Body.Close()
	})

	t.Run("the file is added to the system pool, not swapped for it", func(t *testing.T) {
		_, caPath := selfSignedServer(t)
		if err := ConfigureCA(caPath); err != nil {
			t.Fatal(err)
		}
		pool := http.DefaultTransport.(*http.Transport).TLSClientConfig.RootCAs
		sys, err := x509.SystemCertPool()
		if err != nil {
			t.Skip("no system pool on this host")
		}
		if pool == nil || pool.Equal(sys) {
			t.Fatal("want the system pool plus the stand CA, got an unchanged or empty pool")
		}
	})

	t.Run("a missing or non-PEM file is an error, not a silent skip", func(t *testing.T) {
		if err := ConfigureCA(filepath.Join(t.TempDir(), "nope.crt")); err == nil {
			t.Fatal("missing file: want error")
		}
		junk := filepath.Join(t.TempDir(), "junk.crt")
		_ = os.WriteFile(junk, []byte("not a certificate"), 0o600)
		if err := ConfigureCA(junk); err == nil {
			t.Fatal("non-PEM file: want error")
		}
	})

	t.Run("an empty path resets to the system pool", func(t *testing.T) {
		_, caPath := selfSignedServer(t)
		_ = ConfigureCA(caPath)
		if err := ConfigureCA(""); err != nil {
			t.Fatal(err)
		}
		if tr := http.DefaultTransport.(*http.Transport); tr.TLSClientConfig != nil && tr.TLSClientConfig.RootCAs != nil {
			t.Fatal("want RootCAs reset")
		}
	})
}

func TestResolveCAFile(t *testing.T) {
	t.Setenv(CAFileEnv, "")
	if got := ResolveCAFile("", CLIConfig{CAFile: "/cfg.crt"}); got != "/cfg.crt" {
		t.Fatalf("config fallback: got %q", got)
	}
	t.Setenv(CAFileEnv, "/env.crt")
	if got := ResolveCAFile("", CLIConfig{CAFile: "/cfg.crt"}); got != "/env.crt" {
		t.Fatalf("env over config: got %q", got)
	}
	if got := ResolveCAFile("/flag.crt", CLIConfig{CAFile: "/cfg.crt"}); got != "/flag.crt" {
		t.Fatalf("flag over env: got %q", got)
	}
}

func TestExportCAEnvDoesNotOverrideMachinePolicy(t *testing.T) {
	t.Setenv("SSL_CERT_FILE", "/machine.pem")
	t.Setenv("REQUESTS_CA_BUNDLE", "")
	t.Setenv("NODE_EXTRA_CA_CERTS", "")
	t.Setenv(CAFileEnv, "")
	ExportCAEnv("/stand.crt")
	if os.Getenv("SSL_CERT_FILE") != "/machine.pem" {
		t.Fatal("an operator-set SSL_CERT_FILE must win over the stand CA")
	}
	if os.Getenv("REQUESTS_CA_BUNDLE") != "/stand.crt" || os.Getenv("NODE_EXTRA_CA_CERTS") != "/stand.crt" || os.Getenv(CAFileEnv) != "/stand.crt" {
		t.Fatal("unset agent CA variables must be filled from the stand CA")
	}
}

func TestCLIConfigRoundTripsCAFile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := SaveCLIConfig(CLIConfig{ServerURL: "https://s", CAFile: "/stand.crt"}); err != nil {
		t.Fatal(err)
	}
	got, err := LoadCLIConfig()
	if err != nil {
		t.Fatal(err)
	}
	if got.CAFile != "/stand.crt" {
		t.Fatalf("ca_file: got %q", got.CAFile)
	}
}
