package updater

import "testing"

func TestBuildInfoInstallationCapabilityAllowsOnlyNativeOfficialReleases(t *testing.T) {
	for _, test := range []struct {
		name string
		info BuildInfo
		want bool
	}{
		{name: "windows release", info: BuildInfo{Distribution: DistributionRelease, GOOS: "windows", GOARCH: "amd64"}, want: true},
		{name: "linux release", info: BuildInfo{Distribution: DistributionRelease, GOOS: "linux", GOARCH: "arm64"}, want: true},
		{name: "container", info: BuildInfo{Distribution: DistributionContainer, GOOS: "linux", GOARCH: "amd64"}, want: false},
		{name: "source", info: BuildInfo{Distribution: DistributionSource, GOOS: "windows", GOARCH: "amd64"}, want: false},
		{name: "darwin", info: BuildInfo{Distribution: DistributionRelease, GOOS: "darwin", GOARCH: "arm64"}, want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, _ := test.info.InstallationCapability()
			if got != test.want {
				t.Fatalf("InstallationCapability() = %v, want %v", got, test.want)
			}
		})
	}
}
