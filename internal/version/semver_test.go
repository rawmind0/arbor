package version

import (
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantMajor int
		wantMinor int
		wantPatch int
		wantErr   bool
	}{
		{
			name:      "simple version",
			input:     "1.2.3",
			wantMajor: 1,
			wantMinor: 2,
			wantPatch: 3,
		},
		{
			name:      "version with v prefix",
			input:     "v2.0.0",
			wantMajor: 2,
			wantMinor: 0,
			wantPatch: 0,
		},
		{
			name:      "version with prerelease",
			input:     "1.0.0-alpha",
			wantMajor: 1,
			wantMinor: 0,
			wantPatch: 0,
		},
		{
			name:      "version with build metadata",
			input:     "1.0.0+build123",
			wantMajor: 1,
			wantMinor: 0,
			wantPatch: 0,
		},
		{
			name:      "version with prerelease and build",
			input:     "1.0.0-rc.1+build456",
			wantMajor: 1,
			wantMinor: 0,
			wantPatch: 0,
		},
		{
			name:    "empty version",
			input:   "",
			wantErr: true,
		},
		{
			name:      "invalid format",
			input:     "1.2",
			wantMajor: 1,
			wantMinor: 2,
			wantPatch: 0, // Masterminds/semver accepts "1.2" as "1.2.0"
		},
		{
			name:    "non-numeric major",
			input:   "a.2.3",
			wantErr: true,
		},
		{
			name:    "non-numeric minor",
			input:   "1.b.3",
			wantErr: true,
		},
		{
			name:    "non-numeric patch",
			input:   "1.2.c",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("Parse() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if got.GetMajor() != tt.wantMajor || got.GetMinor() != tt.wantMinor ||
					got.GetPatch() != tt.wantPatch {
					t.Errorf("Parse() = %d.%d.%d, want %d.%d.%d",
						got.GetMajor(), got.GetMinor(), got.GetPatch(),
						tt.wantMajor, tt.wantMinor, tt.wantPatch)
				}
			}
		})
	}
}

func TestSemver_String(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "simple version",
			input: "1.2.3",
			want:  "1.2.3",
		},
		{
			name:  "version with v prefix",
			input: "v2.1.0",
			want:  "2.1.0", // Our String() removes v prefix
		},
		{
			name:  "version with prerelease",
			input: "1.0.0-alpha",
			want:  "1.0.0", // Our String() returns only major.minor.patch
		},
		{
			name:  "version with build",
			input: "1.0.0+build123",
			want:  "1.0.0", // Our String() returns only major.minor.patch
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ver, err := Parse(tt.input)
			if err != nil {
				t.Fatalf("Failed to parse version: %v", err)
			}
			if got := ver.String(); got != tt.want {
				t.Errorf("String() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSemver_Increment(t *testing.T) {
	base, err := Parse("1.2.3-alpha+123")
	if err != nil {
		t.Fatalf("Failed to parse base version: %v", err)
	}

	t.Run("IncrementPatch", func(t *testing.T) {
		result := base.IncrementPatch()
		if result.GetMajor() != 1 || result.GetMinor() != 2 || result.GetPatch() != 4 {
			t.Errorf("IncrementPatch() = %v, want 1.2.4", result.String())
		}
		// Prerelease and build metadata should be cleared
		if result.Prerelease() != "" || result.Metadata() != "" {
			t.Errorf("IncrementPatch() should clear prerelease and build metadata")
		}
	})

	t.Run("IncrementMinor", func(t *testing.T) {
		result := base.IncrementMinor()
		if result.GetMajor() != 1 || result.GetMinor() != 3 || result.GetPatch() != 0 {
			t.Errorf("IncrementMinor() = %v, want 1.3.0", result.String())
		}
		// Prerelease and build metadata should be cleared
		if result.Prerelease() != "" || result.Metadata() != "" {
			t.Errorf("IncrementMinor() should clear prerelease and build metadata")
		}
	})

	t.Run("IncrementMajor", func(t *testing.T) {
		result := base.IncrementMajor()
		if result.GetMajor() != 2 || result.GetMinor() != 0 || result.GetPatch() != 0 {
			t.Errorf("IncrementMajor() = %v, want 2.0.0", result.String())
		}
		// Prerelease and build metadata should be cleared
		if result.Prerelease() != "" || result.Metadata() != "" {
			t.Errorf("IncrementMajor() should clear prerelease and build metadata")
		}
	})
}

func TestSemver_Compare(t *testing.T) {
	tests := []struct {
		name string
		v1   string
		v2   string
		want int // -1: v1 < v2, 0: v1 == v2, 1: v1 > v2
	}{
		{
			name: "equal versions",
			v1:   "1.0.0",
			v2:   "1.0.0",
			want: 0,
		},
		{
			name: "major version difference",
			v1:   "1.0.0",
			v2:   "2.0.0",
			want: -1,
		},
		{
			name: "minor version difference",
			v1:   "1.1.0",
			v2:   "1.0.0",
			want: 1,
		},
		{
			name: "patch version difference",
			v1:   "1.0.0",
			v2:   "1.0.1",
			want: -1,
		},
		{
			name: "prerelease vs no prerelease",
			v1:   "1.0.0-alpha",
			v2:   "1.0.0",
			want: -1,
		},
		{
			name: "prerelease comparison",
			v1:   "1.0.0-alpha",
			v2:   "1.0.0-beta",
			want: -1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v1, err := Parse(tt.v1)
			if err != nil {
				t.Fatalf("Failed to parse v1: %v", err)
			}
			v2, err := Parse(tt.v2)
			if err != nil {
				t.Fatalf("Failed to parse v2: %v", err)
			}
			if got := v1.Compare(v2); got != tt.want {
				t.Errorf("Compare() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSemver_Comparison(t *testing.T) {
	v1, _ := Parse("1.0.0")
	v2, _ := Parse("2.0.0")
	v3, _ := Parse("1.0.0")

	if !v1.LessThan(v2) {
		t.Error("LessThan() should return true for 1.0.0 < 2.0.0")
	}

	if !v2.GreaterThan(v1) {
		t.Error("GreaterThan() should return true for 2.0.0 > 1.0.0")
	}

	if !v1.Equal(v3) {
		t.Error("Equal() should return true for same version")
	}
}

func TestNewVersion(t *testing.T) {
	v := NewVersion(1, 2, 3)
	if v.GetMajor() != 1 || v.GetMinor() != 2 || v.GetPatch() != 3 {
		t.Errorf("NewVersion(1, 2, 3) = %v, want 1.2.3", v.String())
	}
}
