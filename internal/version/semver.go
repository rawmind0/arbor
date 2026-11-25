package version

import (
	"fmt"
	"regexp"
	"strconv"

	"github.com/Masterminds/semver/v3"
)

// Semver wraps the Masterminds semver.Version for our use
type Semver struct {
	*semver.Version
}

// Parse parses a semantic version string using the Masterminds library
func Parse(version string) (*Semver, error) {
	if version == "" {
		return nil, fmt.Errorf("empty version string")
	}

	v, err := semver.NewVersion(version)
	if err != nil {
		return nil, fmt.Errorf("invalid version format: %s (%w)", version, err)
	}

	return &Semver{Version: v}, nil
}

// NewVersion creates a new semantic version
func NewVersion(major, minor, patch int) *Semver {
	v := semver.New(uint64(major), uint64(minor), uint64(patch), "", "")
	return &Semver{Version: v}
}

// String returns the string representation
func (v *Semver) String() string {
	// Return without 'v' prefix for consistency with our format
	return fmt.Sprintf("%d.%d.%d", v.Major(), v.Minor(), v.Patch())
}

// Compare compares two versions
// Returns:
//
//	-1 if v < other
//	 0 if v == other
//	 1 if v > other
func (v *Semver) Compare(other *Semver) int {
	return v.Version.Compare(other.Version)
}

// LessThan returns true if v < other
func (v *Semver) LessThan(other *Semver) bool {
	return v.Version.LessThan(other.Version)
}

// GreaterThan returns true if v > other
func (v *Semver) GreaterThan(other *Semver) bool {
	return v.Version.GreaterThan(other.Version)
}

// Equal returns true if v == other
func (v *Semver) Equal(other *Semver) bool {
	return v.Version.Equal(other.Version)
}

// Major returns the major version
func (v *Semver) GetMajor() int {
	return int(v.Major())
}

// Minor returns the minor version
func (v *Semver) GetMinor() int {
	return int(v.Minor())
}

// Patch returns the patch version
func (v *Semver) GetPatch() int {
	return int(v.Patch())
}

// tagVersionRe splits an image tag version into an optional "v" prefix, up to
// three dot-separated numbers, and an optional "-suffix" or "+suffix" (e.g.
// "-alpine"), the same forms Parse accepts.
var tagVersionRe = regexp.MustCompile(`^(v?)(\d+)(?:\.(\d+))?(?:\.(\d+))?([-+].*)?$`)

// Bump returns version with its patch (or, when minor is true, its minor)
// number incremented, editing the tag as written: image tags are not strict
// semver, so each number keeps its zero-padding ("22.04.1" → "22.04.2", minor
// "22.05.0"), a missing patch number is added ("22.04" → "22.04.1"), and any
// "v" prefix or suffix ("-alpine") is kept.
func Bump(version string, minor bool) (string, error) {
	m := tagVersionRe.FindStringSubmatch(version)
	if m == nil {
		return "", fmt.Errorf("invalid version format: %s", version)
	}
	prefix, major, min, patch, rest := m[1], m[2], m[3], m[4], m[5]
	if min == "" {
		min = "0"
	}
	if patch == "" {
		patch = "0"
	}
	if minor {
		min = increment(min)
		patch = fmt.Sprintf("%0*d", len(patch), 0)
	} else {
		patch = increment(patch)
	}
	return prefix + major + "." + min + "." + patch + rest, nil
}

// increment adds one to a decimal number, keeping its zero-padding width.
func increment(n string) string {
	v, _ := strconv.ParseUint(n, 10, 64) // digits only, guaranteed by tagVersionRe
	return fmt.Sprintf("%0*d", len(n), v+1)
}
