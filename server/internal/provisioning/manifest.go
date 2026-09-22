// Пакет provisioning — серверная часть внутриконтурного реестра пакетов:
// абстракция PackageStore над локальным контентно-адресуемым хранилищем или
// OCI-реестром, формат PackageManifest и разрешение зависимостей requires.
package provisioning

import (
	"encoding/json"
	"fmt"
	"regexp"
)

const CurrentSchemaVersion = 1

const (
	PackageTypeSkill     = "skill"
	PackageTypeMCPServer = "mcp-server"
	PackageTypeRuntime   = "runtime"
)

var validPackageTypes = map[string]bool{
	PackageTypeSkill:     true,
	PackageTypeMCPServer: true,
	PackageTypeRuntime:   true,
}

var validPlatforms = map[string]bool{
	"*":            true,
	"darwin-arm64": true,
	"darwin-x64":   true,
	"win-x64":      true,
	"linux-x64":    true,
	"linux-arm64":  true,
}

func ValidPlatform(p string) bool {
	return validPlatforms[p]
}

func ValidPackageType(t string) bool {
	return validPackageTypes[t]
}

func ValidPackageIdentifier(s string) bool {
	return packageNamePattern.MatchString(s)
}

var sha256HexPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

var packageNamePattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9._-]*[A-Za-z0-9])?$`)

var requireRefPattern = regexp.MustCompile(`^([A-Za-z-]+):([A-Za-z0-9](?:[A-Za-z0-9._-]*[A-Za-z0-9])?)@([A-Za-z0-9](?:[A-Za-z0-9._-]*[A-Za-z0-9])?)$`)

type PackageManifest struct {
	SchemaVersion int      `json:"schemaVersion"`
	Name          string   `json:"name"`
	Version       string   `json:"version"`
	Type          string   `json:"type"`
	Platform      string   `json:"platform"`
	SHA256        string   `json:"sha256"`
	Size          int64    `json:"size"`
	Requires      []string `json:"requires"`
}

func (m PackageManifest) MarshalJSON() ([]byte, error) {
	type alias PackageManifest
	requires := m.Requires
	if requires == nil {
		requires = []string{}
	}
	return json.Marshal(struct {
		alias
		Requires []string `json:"requires"`
	}{alias: alias(m), Requires: requires})
}

func ParsePackageManifest(data []byte) (PackageManifest, error) {
	var m PackageManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return PackageManifest{}, fmt.Errorf("provisioning: invalid manifest JSON: %w", err)
	}
	if err := m.Validate(); err != nil {
		return PackageManifest{}, err
	}
	return m, nil
}

func (m PackageManifest) Validate() error {
	if m.SchemaVersion != CurrentSchemaVersion {
		return fmt.Errorf("provisioning: unsupported schemaVersion %d (want %d)", m.SchemaVersion, CurrentSchemaVersion)
	}
	if !packageNamePattern.MatchString(m.Name) {
		return fmt.Errorf("provisioning: invalid package name %q", m.Name)
	}
	if !packageNamePattern.MatchString(m.Version) {
		return fmt.Errorf("provisioning: invalid package version %q", m.Version)
	}
	if !validPackageTypes[m.Type] {
		return fmt.Errorf("provisioning: invalid package type %q", m.Type)
	}
	if !ValidPlatform(m.Platform) {
		return fmt.Errorf("provisioning: invalid platform %q", m.Platform)
	}
	if !sha256HexPattern.MatchString(m.SHA256) {
		return fmt.Errorf("provisioning: invalid sha256 %q (want 64 lowercase hex chars)", m.SHA256)
	}
	if m.Size <= 0 {
		return fmt.Errorf("provisioning: invalid size %d (must be > 0)", m.Size)
	}
	for _, ref := range m.Requires {
		if _, _, _, err := ParseRequireRef(ref); err != nil {
			return fmt.Errorf("provisioning: invalid requires entry %q: %w", ref, err)
		}
	}
	return nil
}

func (m PackageManifest) RequireKey() string {
	return fmt.Sprintf("%s:%s@%s", m.Type, m.Name, m.Version)
}

func (m PackageManifest) BlobFilename() string {
	return fmt.Sprintf("%s-%s-%s.tar.zst", m.Name, m.Version, m.Platform)
}

func (m PackageManifest) MatchesPlatform(platform string) bool {
	return m.Platform == "*" || m.Platform == platform
}

func ParseRequireRef(ref string) (pkgType, name, version string, err error) {
	matches := requireRefPattern.FindStringSubmatch(ref)
	if matches == nil {
		return "", "", "", fmt.Errorf("provisioning: malformed requires reference %q (want type:name@version)", ref)
	}
	pkgType, name, version = matches[1], matches[2], matches[3]
	if !validPackageTypes[pkgType] {
		return "", "", "", fmt.Errorf("provisioning: unknown requires type %q in %q", pkgType, ref)
	}
	return pkgType, name, version, nil
}
