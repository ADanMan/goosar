// Пакет deploymentprofile задаёт тип стенда (GOOSAR_DEPLOYMENT_PROFILE):
// perimeter — закрытая сеть заказчика, demo — превью-стенд, dev — стенд разработки,
// local — локальный запуск.
package deploymentprofile

import (
	"fmt"
	"os"
	"strings"
)

const EnvVar = "GOOSAR_DEPLOYMENT_PROFILE"

type Profile string

const (
	Perimeter Profile = "perimeter"

	Demo Profile = "demo"

	Dev Profile = "dev"

	Local Profile = "local"
)

func Parse(raw string) (Profile, error) {
	switch p := Profile(strings.ToLower(strings.TrimSpace(raw))); p {
	case "":
		return Perimeter, nil
	case Perimeter, Demo, Dev, Local:
		return p, nil
	default:
		return Perimeter, fmt.Errorf("invalid %s %q: must be one of %q, %q, %q, %q",
			EnvVar, raw, Perimeter, Demo, Dev, Local)
	}
}

func FromEnv() (Profile, error) {
	return Parse(os.Getenv(EnvVar))
}
