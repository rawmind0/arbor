package version

import (
	"fmt"

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

// IncrementPatch returns a new version with patch incremented
func (v *Semver) IncrementPatch() *Semver {
	newVer := semver.New(v.Major(), v.Minor(), v.Patch()+1, "", "")
	return &Semver{Version: newVer}
}

// IncrementMinor returns a new version with minor incremented and patch reset
func (v *Semver) IncrementMinor() *Semver {
	newVer := semver.New(v.Major(), v.Minor()+1, 0, "", "")
	return &Semver{Version: newVer}
}

// IncrementMajor returns a new version with major incremented and minor/patch reset
func (v *Semver) IncrementMajor() *Semver {
	newVer := semver.New(v.Major()+1, 0, 0, "", "")
	return &Semver{Version: newVer}
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
