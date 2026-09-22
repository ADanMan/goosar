package handler

import (
	"fmt"

	"github.com/adanman/goosar/server/internal/perimeterpolicy"
)

func providerPolicyMessage(provider string) string {
	return fmt.Sprintf("agent provider %q is not allowed by this deployment's provider policy (%s)", provider, perimeterpolicy.EnvAllowedProviders)
}
