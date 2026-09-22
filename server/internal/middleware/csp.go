package middleware

import (
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
)

const (
	externalImagesAllow     = "allow"
	externalImagesBlock     = "block"
	externalImagesAllowlist = "allowlist"
)

func ExternalImagesMode() string {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("GOOSAR_EXTERNAL_IMAGES"))) {
	case "", externalImagesAllow:
		return externalImagesAllow
	case externalImagesAllowlist:
		return externalImagesAllowlist
	default:
		return externalImagesBlock
	}
}

var cspSourceRe = regexp.MustCompile(`^(?:https?://)?[A-Za-z0-9][A-Za-z0-9.-]*(?::\d{1,5})?$`)

func sanitizeImageHostEntry(raw string) string {
	entry := strings.TrimSpace(raw)
	if entry == "" {
		return ""
	}
	if strings.Contains(entry, "://") {
		u, err := url.Parse(entry)
		if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") {
			return ""
		}
		entry = u.Scheme + "://" + u.Host
	}
	if !cspSourceRe.MatchString(entry) {
		return ""
	}
	return entry
}

func AllowlistedImageHosts() []string {
	var hosts []string
	for _, raw := range strings.Split(os.Getenv("GOOSAR_IMAGE_HOSTS"), ",") {
		if entry := sanitizeImageHostEntry(raw); entry != "" {
			hosts = append(hosts, entry)
		}
	}
	return hosts
}

func storageImageSources() []string {
	var sources []string
	if entry := sanitizeImageHostEntry(os.Getenv("CLOUDFRONT_DOMAIN")); entry != "" {
		sources = append(sources, entry)
	}
	if entry := sanitizeImageHostEntry(os.Getenv("LOCAL_UPLOAD_BASE_URL")); entry != "" {
		sources = append(sources, entry)
	}
	return sources
}

func imgSrcDirective() string {
	mode := ExternalImagesMode()
	if mode == externalImagesAllow {
		return "img-src 'self' https: data:; "
	}

	sources := []string{"'self'", "data:"}
	seen := map[string]bool{}
	appendSource := func(entry string) {
		if entry == "" || seen[entry] {
			return
		}
		seen[entry] = true
		sources = append(sources, entry)
	}
	for _, entry := range storageImageSources() {
		appendSource(entry)
	}
	if mode == externalImagesAllowlist {
		for _, entry := range AllowlistedImageHosts() {
			appendSource(entry)
		}
	}
	return "img-src " + strings.Join(sources, " ") + "; "
}

func buildCSPHeaders() (cspHeader, attachmentPreviewCSPHeader string) {
	base := "default-src 'self'; " +
		"script-src 'self'; " +
		"style-src 'self' 'unsafe-inline'; " +
		imgSrcDirective() +
		"connect-src 'self' wss:; "

	cspHeader = base +
		"frame-ancestors 'none'; " +
		"object-src 'none'; " +
		"base-uri 'self'; " +
		"form-action 'self'"

	attachmentPreviewCSPHeader = base +
		"frame-ancestors 'self'; " +
		"object-src 'none'; " +
		"base-uri 'self'; " +
		"form-action 'self'"
	return cspHeader, attachmentPreviewCSPHeader
}

func ContentSecurityPolicy(next http.Handler) http.Handler {
	cspHeader, attachmentPreviewCSPHeader := buildCSPHeaders()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := cspHeader
		if isAttachmentPreviewDocumentPath(r.URL.Path) {
			header = attachmentPreviewCSPHeader
		}
		w.Header().Set("Content-Security-Policy", header)
		next.ServeHTTP(w, r)
	})
}

func isAttachmentPreviewDocumentPath(path string) bool {
	return strings.HasPrefix(path, "/api/attachments/") &&
		(strings.HasSuffix(path, "/download") || strings.HasSuffix(path, "/content"))
}
