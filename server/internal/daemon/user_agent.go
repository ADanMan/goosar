package daemon

import (
	"fmt"
	"os"
	"strings"
)

const userAgentEnvVar = "GOOSAR_DAEMON_USER_AGENT"

func daemonUserAgent(env map[string]string, version, osName string) string {
	if override := userAgentOverride(env); override != "" {
		return override
	}
	return defaultUserAgent(version, osName)
}

func userAgentOverride(env map[string]string) string {
	if env != nil {
		return strings.TrimSpace(env[userAgentEnvVar])
	}
	return strings.TrimSpace(os.Getenv(userAgentEnvVar))
}

func defaultUserAgent(version, osName string) string {
	if version == "" {
		version = "dev"
	}
	if osName == "" {
		return fmt.Sprintf("HermesGoosar/%s (daemon)", version)
	}
	return fmt.Sprintf("HermesGoosar/%s (daemon; %s)", version, osName)
}
