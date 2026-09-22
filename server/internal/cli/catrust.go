package cli

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
	"strings"
)

const CAFileEnv = "GOOSAR_CA_FILE"

func ResolveCAFile(flagValue string, конфиг CLIConfig) string {
	if v := strings.TrimSpace(flagValue); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv(CAFileEnv)); v != "" {
		return v
	}
	return strings.TrimSpace(конфиг.CAFile)
}

func ConfigureCA(path string) error {
	transport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return fmt.Errorf("configure CA: http.DefaultTransport is not an *http.Transport")
	}
	if path == "" {
		if transport.TLSClientConfig != nil {
			transport.TLSClientConfig.RootCAs = nil
		}
		return nil
	}
	pool, err := loadCAPool(path)
	if err != nil {
		return err
	}
	if transport.TLSClientConfig == nil {
		transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	transport.TLSClientConfig.RootCAs = pool
	return nil
}

func loadCAPool(path string) (*x509.CertPool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%s (--ca-file / %s): %w", path, CAFileEnv, err)
	}
	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		pool = x509.NewCertPool()
	}
	if !pool.AppendCertsFromPEM(data) {
		return nil, fmt.Errorf("%s (--ca-file / %s): no PEM certificates found in the file", path, CAFileEnv)
	}
	return pool, nil
}

func ExportCAEnv(path string) {
	for _, key := range []string{CAFileEnv, "SSL_CERT_FILE", "REQUESTS_CA_BUNDLE", "NODE_EXTRA_CA_CERTS"} {
		if os.Getenv(key) == "" {
			_ = os.Setenv(key, path)
		}
	}
}
