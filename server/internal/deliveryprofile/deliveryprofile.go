// Пакет deliveryprofile задаёт профиль поставки развёртывания
// (GOOSAR_DELIVERY_PROFILE): cloud — публичное поведение по умолчанию,
// perimeter — закрытый контур под управлением оператора.
package deliveryprofile

import (
	"fmt"
	"os"
	"strings"
)

const EnvVar = "GOOSAR_DELIVERY_PROFILE"

type Profile string

const (
	Cloud Profile = "cloud"

	Perimeter Profile = "perimeter"
)

func Parse(raw string) (Profile, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", string(Cloud):
		return Cloud, nil
	case string(Perimeter):
		return Perimeter, nil
	default:
		return Cloud, fmt.Errorf("invalid %s %q: must be %q or %q", EnvVar, raw, Cloud, Perimeter)
	}
}

func FromEnv() (Profile, error) {
	return Parse(os.Getenv(EnvVar))
}

func (p Profile) IsPerimeter() bool {
	return p == Perimeter
}

func (p Profile) PublicValue() string {
	if p == Cloud {
		return ""
	}
	return string(p)
}

func IsPerimeterAdvertised(raw string) bool {
	return strings.EqualFold(strings.TrimSpace(raw), string(Perimeter))
}
