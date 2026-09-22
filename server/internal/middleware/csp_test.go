package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestContentSecurityPolicy(t *testing.T) {
	handler := ContentSecurityPolicy(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	csp := rec.Header().Get("Content-Security-Policy")
	assertCSPDirectives(t, csp, []string{
		"script-src 'self'",
		"object-src 'none'",
		"frame-ancestors 'none'",
		"base-uri 'self'",
		"form-action 'self'",
	})
}

func TestContentSecurityPolicyAllowsSameOriginAttachmentPreviews(t *testing.T) {
	handler := ContentSecurityPolicy(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for _, path := range []string{
		"/api/attachments/019f0dae-0315-79b7-b653-f55d6af90403/download",
		"/api/attachments/019f0dae-0315-79b7-b653-f55d6af90403/content",
	} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			csp := rec.Header().Get("Content-Security-Policy")
			assertCSPDirectives(t, csp, []string{
				"script-src 'self'",
				"object-src 'none'",
				"frame-ancestors 'self'",
				"base-uri 'self'",
				"form-action 'self'",
			})
			if strings.Contains(csp, "frame-ancestors 'none'") {
				t.Fatalf("attachment preview CSP must not block same-origin iframe embedding; got: %s", csp)
			}
		})
	}
}

func cspHeaderFor(t *testing.T, path string) string {
	t.Helper()
	handler := ContentSecurityPolicy(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec.Header().Get("Content-Security-Policy")
}

func TestCSPImgSrcDefaultAllowsExternalImages(t *testing.T) {
	t.Setenv("GOOSAR_EXTERNAL_IMAGES", "")
	csp := cspHeaderFor(t, "/")
	assertCSPDirectives(t, csp, []string{"img-src 'self' https: data:"})
}

func TestCSPImgSrcBlockDropsExternalHostsButKeepsStorage(t *testing.T) {
	t.Setenv("GOOSAR_EXTERNAL_IMAGES", "block")
	t.Setenv("GOOSAR_IMAGE_HOSTS", "cdn.example.com")
	t.Setenv("CLOUDFRONT_DOMAIN", "media.goosar.example")
	t.Setenv("LOCAL_UPLOAD_BASE_URL", "http://localhost:8081")

	csp := cspHeaderFor(t, "/")
	assertCSPDirectives(t, csp, []string{
		"img-src 'self' data: media.goosar.example http://localhost:8081",
	})
	if strings.Contains(csp, "https:;") || strings.Contains(csp, "https: ") {
		t.Fatalf("block mode must not allow arbitrary https image hosts; got: %s", csp)
	}
	if strings.Contains(csp, "cdn.example.com") {
		t.Fatalf("GOOSAR_IMAGE_HOSTS must be ignored in block mode; got: %s", csp)
	}
}

func TestCSPImgSrcAllowlistAddsOperatorHosts(t *testing.T) {
	t.Setenv("GOOSAR_EXTERNAL_IMAGES", "allowlist")
	t.Setenv("GOOSAR_IMAGE_HOSTS", " cdn.example.com , https://media.example.com:8443 ")
	t.Setenv("CLOUDFRONT_DOMAIN", "")
	t.Setenv("LOCAL_UPLOAD_BASE_URL", "")

	csp := cspHeaderFor(t, "/")
	assertCSPDirectives(t, csp, []string{
		"img-src 'self' data: cdn.example.com https://media.example.com:8443",
	})
}

func TestCSPImgSrcInvalidModeFailsClosed(t *testing.T) {
	t.Setenv("GOOSAR_EXTERNAL_IMAGES", "blok")
	t.Setenv("GOOSAR_IMAGE_HOSTS", "")
	t.Setenv("CLOUDFRONT_DOMAIN", "")
	t.Setenv("LOCAL_UPLOAD_BASE_URL", "")

	csp := cspHeaderFor(t, "/")
	assertCSPDirectives(t, csp, []string{"img-src 'self' data:;"})
}

func TestCSPImgSrcRejectsHeaderInjectionInHostEntries(t *testing.T) {
	t.Setenv("GOOSAR_EXTERNAL_IMAGES", "allowlist")
	t.Setenv("GOOSAR_IMAGE_HOSTS", "evil.com; script-src *,ok.example.com,*.wild.example.com,javascript://x")
	t.Setenv("CLOUDFRONT_DOMAIN", "")
	t.Setenv("LOCAL_UPLOAD_BASE_URL", "")

	csp := cspHeaderFor(t, "/")
	assertCSPDirectives(t, csp, []string{"img-src 'self' data: ok.example.com;"})
	for _, forbidden := range []string{"script-src *", "*.wild.example.com", "javascript"} {
		if strings.Contains(csp, forbidden) {
			t.Fatalf("unsafe host entry %q leaked into CSP: %s", forbidden, csp)
		}
	}
}

func TestCSPImgSrcModeAppliesToAttachmentPreviewsToo(t *testing.T) {
	t.Setenv("GOOSAR_EXTERNAL_IMAGES", "block")
	t.Setenv("GOOSAR_IMAGE_HOSTS", "")
	t.Setenv("CLOUDFRONT_DOMAIN", "")
	t.Setenv("LOCAL_UPLOAD_BASE_URL", "")

	csp := cspHeaderFor(t, "/api/attachments/019f0dae-0315-79b7-b653-f55d6af90403/content")
	assertCSPDirectives(t, csp, []string{
		"img-src 'self' data:;",
		"frame-ancestors 'self'",
	})
}

func assertCSPDirectives(t *testing.T, csp string, required []string) {
	t.Helper()
	if csp == "" {
		t.Fatal("Content-Security-Policy header is missing")
	}
	for _, directive := range required {
		if !strings.Contains(csp, directive) {
			t.Errorf("CSP missing directive %q; got: %s", directive, csp)
		}
	}
}
