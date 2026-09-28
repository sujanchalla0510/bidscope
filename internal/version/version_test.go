package version

import (
	"regexp"
	"testing"
)

func TestVersionIsSet(t *testing.T) {
	if Version == "" {
		t.Fatal("Version must not be empty")
	}
}

func TestVersionLooksLikeSemver(t *testing.T) {
	// Accepts "0.0.1", "0.0.1-dev", "1.2.3-rc.1", etc.
	re := regexp.MustCompile(`^\d+\.\d+\.\d+(-[0-9A-Za-z.\-]+)?$`)
	if !re.MatchString(Version) {
		t.Fatalf("Version %q does not look like semver", Version)
	}
}
