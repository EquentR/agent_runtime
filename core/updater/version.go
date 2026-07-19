package updater

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/hashicorp/go-version"
)

var canonicalStableVersionPattern = regexp.MustCompile(`^v?\d+\.\d+\.\d+$`)

func IsNewerRelease(current, target string) (bool, error) {
	currentVersion, err := parseReleaseVersion(current)
	if err != nil {
		return false, fmt.Errorf("parse current version: %w", err)
	}
	targetVersion, err := parseReleaseVersion(target)
	if err != nil {
		return false, fmt.Errorf("parse target version: %w", err)
	}
	return targetVersion.GreaterThan(currentVersion), nil
}

func parseReleaseVersion(value string) (*version.Version, error) {
	value = strings.TrimSpace(value)
	if !canonicalStableVersionPattern.MatchString(value) {
		return nil, fmt.Errorf("version %q is not a canonical stable release", value)
	}
	value = strings.TrimPrefix(value, "v")
	parsed, err := version.NewVersion(value)
	if err != nil {
		return nil, err
	}
	if len(parsed.Segments()) < 3 {
		return nil, fmt.Errorf("version %q is not a release semver", value)
	}
	return parsed, nil
}
