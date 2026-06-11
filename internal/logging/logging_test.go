package logging

import (
	"testing"

	"github.com/rs/zerolog"
)

func TestDefaultLevel(t *testing.T) {
	t.Setenv(EnvLevel, "debug")
	if got := DefaultLevel(); got != zerolog.DebugLevel {
		t.Errorf("DefaultLevel() with %s=debug = %v, want debug", EnvLevel, got)
	}

	t.Setenv(EnvLevel, "")
	if got := DefaultLevel(); got != zerolog.InfoLevel {
		t.Errorf("DefaultLevel() with %s unset = %v, want info", EnvLevel, got)
	}

	t.Setenv(EnvLevel, "bogus")
	if got := DefaultLevel(); got != zerolog.InfoLevel {
		t.Errorf("DefaultLevel() with invalid %s = %v, want info", EnvLevel, got)
	}
}

func TestConfigure(t *testing.T) {
	for _, want := range []zerolog.Level{zerolog.WarnLevel, zerolog.TraceLevel, zerolog.ErrorLevel} {
		Configure(want)
		if got := zerolog.GlobalLevel(); got != want {
			t.Errorf("Configure(%v): global level = %v", want, got)
		}
	}
}

func TestLevelFlag(t *testing.T) {
	f := NewLevelFlag(zerolog.InfoLevel)

	if err := f.Set("warn"); err != nil {
		t.Fatalf("Set(\"warn\") unexpected error: %v", err)
	}
	if f.Level() != zerolog.WarnLevel {
		t.Errorf("Level() = %v, want warn", f.Level())
	}
	if f.String() != "warn" {
		t.Errorf("String() = %q, want warn", f.String())
	}

	// Invalid value is not fatal and keeps the previous level.
	if err := f.Set("bogus"); err != nil {
		t.Errorf("Set(\"bogus\") should not error, got %v", err)
	}
	if f.Level() != zerolog.WarnLevel {
		t.Errorf("invalid Set changed level to %v, want warn (kept)", f.Level())
	}
}
