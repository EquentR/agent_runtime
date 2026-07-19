package updater

import (
	"fmt"
	"runtime"
	"strings"
)

const (
	DistributionRelease   = "release"
	DistributionContainer = "container"
	DistributionSource    = "source"
)

type BuildInfo struct {
	Version      string `json:"version"`
	Commit       string `json:"commit"`
	Distribution string `json:"distribution"`
	GOOS         string `json:"goos"`
	GOARCH       string `json:"goarch"`
}

func (b BuildInfo) Normalized() BuildInfo {
	b.Version = strings.TrimSpace(b.Version)
	b.Commit = strings.TrimSpace(b.Commit)
	b.Distribution = strings.TrimSpace(b.Distribution)
	b.GOOS = strings.TrimSpace(b.GOOS)
	b.GOARCH = strings.TrimSpace(b.GOARCH)
	return b
}

func (b BuildInfo) InstallationCapability() (bool, string) {
	b = b.Normalized()
	if b.Distribution != DistributionRelease {
		return false, fmt.Sprintf("distribution %q is not a self-updatable release", b.Distribution)
	}
	if b.GOOS != "windows" && b.GOOS != "linux" {
		return false, fmt.Sprintf("platform %s/%s only supports update notifications", b.GOOS, b.GOARCH)
	}
	if b.GOARCH != "amd64" && b.GOARCH != "arm64" {
		return false, fmt.Sprintf("architecture %s is not supported", b.GOARCH)
	}
	return true, ""
}

func CurrentBuildInfo(version, commit, distribution string) BuildInfo {
	if strings.TrimSpace(distribution) == "" {
		distribution = DistributionSource
	}
	return BuildInfo{Version: version, Commit: commit, Distribution: distribution, GOOS: runtime.GOOS, GOARCH: runtime.GOARCH}.Normalized()
}
