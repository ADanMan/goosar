package agent

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var MinVersions = map[string]string{
	"runtime-c": "2.0.0",
	"runtime-e": "0.100.0",
	"runtime-f": "1.0.0",
	"runtime-i": "0.2.89",
	"runtime-q": "0.20.0",
}

const MinQuickCreateCLIVersion = "0.2.21"

const MinQuickCreateFieldsCLIVersion = "0.4.3"

const MinHandoffCLIVersion = "0.3.28"

var (
	ErrCLIVersionMissing = errors.New("goosar CLI version not reported by daemon")
	ErrCLIVersionTooOld  = errors.New("goosar CLI version is below required minimum")
)

var devDescribeRe = regexp.MustCompile(`^v?\d+\.\d+\.\d+-\d+-g[0-9a-fA-F]+`)

var versionRe = regexp.MustCompile(`v?(\d+)\.(\d+)\.(\d+)`)

func isDevBuildVersion(raw string) bool {
	return devDescribeRe.MatchString(raw)
}

func HandoffSupported(cliVersion string) bool {
	trimmed := strings.TrimSpace(cliVersion)
	if trimmed == "" {
		return false
	}
	if isDevBuildVersion(trimmed) {
		return true
	}
	detected, err := parseSemver(trimmed)
	if err != nil {
		return false
	}
	floor, err := parseSemver(MinHandoffCLIVersion)
	if err != nil {
		return false
	}
	return !detected.lessThan(floor)
}

func CheckMinCLIVersion(detected string) error {
	return CheckMinCLIVersionFor(detected, MinQuickCreateCLIVersion)
}

func CheckMinCLIVersionFor(detected, minimum string) error {
	trimmed := strings.TrimSpace(detected)
	if trimmed == "" {
		return ErrCLIVersionMissing
	}
	if isDevBuildVersion(trimmed) {
		return nil
	}

	parsed, err := parseSemver(trimmed)
	if err != nil {
		return ErrCLIVersionMissing
	}
	floor, err := parseSemver(minimum)
	if err != nil {

		return ErrCLIVersionMissing
	}
	if parsed.lessThan(floor) {
		return ErrCLIVersionTooOld
	}
	return nil
}

type semver struct {
	Major, Minor, Patch int
}

func parseSemver(raw string) (semver, error) {
	m := versionRe.FindStringSubmatch(raw)
	if m == nil {
		return semver{}, fmt.Errorf("cannot parse version %q", raw)
	}
	major, _ := strconv.Atoi(m[1])
	minor, _ := strconv.Atoi(m[2])
	patch, _ := strconv.Atoi(m[3])
	return semver{Major: major, Minor: minor, Patch: patch}, nil
}

func (v semver) lessThan(other semver) bool {
	if v.Major != other.Major {
		return v.Major < other.Major
	}
	if v.Minor != other.Minor {
		return v.Minor < other.Minor
	}
	return v.Patch < other.Patch
}

func CheckMinVersion(runtimeCode, detectedVersion string) error {
	minRaw, ok := MinVersions[runtimeCode]
	if !ok {
		return nil
	}
	floor, err := parseSemver(minRaw)
	if err != nil {
		return fmt.Errorf("invalid minimum version %q for %s: %w", minRaw, runtimeCode, err)
	}
	detected, err := parseSemver(detectedVersion)
	if err != nil {
		return fmt.Errorf("cannot parse detected %s version %q: %w", runtimeCode, detectedVersion, err)
	}
	if detected.lessThan(floor) {
		return fmt.Errorf("%s version %s is below minimum required %s — please upgrade", runtimeCode, detectedVersion, minRaw)
	}
	return nil
}
