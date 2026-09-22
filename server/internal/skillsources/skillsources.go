// Пакет skillsources задаёт переключатель внешних источников skills
// (GOOSAR_SKILL_SOURCES): список включённых хостов, с которых сервер
// загружает содержимое skills по запросу пользователей.
package skillsources

import (
	"fmt"
	"os"
	"strings"

	"github.com/adanman/goosar/server/internal/deliveryprofile"
)

const EnvVar = "GOOSAR_SKILL_SOURCES"

type Source string

const (
	ClawHub Source = "clawhub"

	GitHub Source = "github"

	SkillsSh Source = "skillssh"
)

const ValueNone = "none"

func knownSources() []Source {
	return []Source{ClawHub, GitHub, SkillsSh}
}

type Set struct {
	enabled map[Source]bool
}

func Parse(raw string, profile deliveryprofile.Profile) (Set, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		if profile.IsPerimeter() {
			return Set{enabled: map[Source]bool{}}, nil
		}
		return Set{}, nil
	}
	if strings.EqualFold(raw, ValueNone) {
		return Set{enabled: map[Source]bool{}}, nil
	}

	enabled := make(map[Source]bool)
	for _, part := range strings.Split(raw, ",") {
		name := strings.ToLower(strings.TrimSpace(part))
		if name == "" {
			continue
		}
		src := Source(name)
		switch src {
		case ClawHub, GitHub, SkillsSh:
			enabled[src] = true
		default:
			return Set{}, fmt.Errorf("invalid %s entry %q: known sources are %q, %q, %q (or %q to disable all)",
				EnvVar, strings.TrimSpace(part), ClawHub, GitHub, SkillsSh, ValueNone)
		}
	}
	if len(enabled) == 0 {
		return Set{}, fmt.Errorf("invalid %s %q: no sources listed; use %q to disable every external source", EnvVar, raw, ValueNone)
	}
	return Set{enabled: enabled}, nil
}

func FromEnv() (Set, error) {
	profile, err := deliveryprofile.FromEnv()
	if err != nil {
		return Set{}, err
	}
	return Parse(os.Getenv(EnvVar), profile)
}

func EnabledFromEnv(src Source) bool {
	set, err := FromEnv()
	if err != nil {
		return false
	}
	return set.Enabled(src)
}

func (s Set) Enabled(src Source) bool {
	if s.enabled == nil {
		return true
	}
	return s.enabled[src]
}

func (s Set) Explicit() bool {
	return s.enabled != nil
}

func (s Set) EnabledNames() []string {
	names := []string{}
	for _, src := range knownSources() {
		if s.Enabled(src) {
			names = append(names, string(src))
		}
	}
	return names
}

func DisabledMessage(src Source) string {
	return fmt.Sprintf("the %s skill source is disabled on this deployment (%s)", src, EnvVar)
}
