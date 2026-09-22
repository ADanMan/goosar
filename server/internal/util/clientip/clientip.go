// Пакет clientip отвечает на вопрос «с какого адреса на самом деле пришёл запрос»
// для всех частей сервера, которые нельзя обмануть подделанным заголовком.
// Выделен отдельно, потому что нужен и rate limiter'у, и журналу аудита.
package clientip

import (
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
)

func ParseTrustedProxies(raw string) []*net.IPNet {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var nets []*net.IPNet
	for _, entry := range strings.Split(raw, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		_, network, err := net.ParseCIDR(entry)
		if err != nil {
			slog.Warn("GOOSAR_TRUSTED_PROXIES: ignoring invalid CIDR", "entry", entry, "error", err)
			continue
		}
		nets = append(nets, network)
	}
	return nets
}

func FromEnv() []*net.IPNet {
	return ParseTrustedProxies(os.Getenv("GOOSAR_TRUSTED_PROXIES"))
}

func Of(r *http.Request, trustedProxies []*net.IPNet) string {
	remoteHost, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		remoteHost = r.RemoteAddr
	}

	if len(trustedProxies) > 0 {
		remoteIP := net.ParseIP(remoteHost)
		if remoteIP != nil && IsTrusted(remoteIP, trustedProxies) {
			if xff := r.Header.Get("X-Forwarded-For"); xff != "" {

				parts := strings.Split(xff, ",")
				for i := len(parts) - 1; i >= 0; i-- {
					candidate := net.ParseIP(strings.TrimSpace(parts[i]))
					if candidate != nil && !IsTrusted(candidate, trustedProxies) {
						return candidate.String()
					}
				}
			}
		}
	}

	if ip := net.ParseIP(remoteHost); ip != nil {
		return ip.String()
	}
	return remoteHost
}

func IsTrusted(ip net.IP, cidrs []*net.IPNet) bool {
	for _, cidr := range cidrs {
		if cidr.Contains(ip) {
			return true
		}
	}
	return false
}
