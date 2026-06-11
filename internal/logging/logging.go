// Package logging configures the global zerolog logger for the CLI.
package logging

import (
	"os"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

const (
	// EnvLevel is the environment variable used as the default log level.
	EnvLevel = "LOG_LEVEL"
	// LevelsHelp lists the accepted levels for the --log-level flag help text.
	LevelsHelp = "trace, debug, info, warn, error, fatal, panic"
)

// DefaultLevel returns the level from the LOG_LEVEL environment variable, or
// zerolog.InfoLevel when it is unset or invalid.
func DefaultLevel() zerolog.Level {
	v := os.Getenv(EnvLevel)
	if v == "" {
		return zerolog.InfoLevel
	}
	lvl, err := zerolog.ParseLevel(v)
	if err != nil {
		log.Warn().Err(err).Msgf("invalid %s, using %q", EnvLevel, zerolog.InfoLevel.String())
		return zerolog.InfoLevel
	}
	return lvl
}

// Configure sets the global level and initializes the logger to emit
// human-readable output to stderr (keeping stdout free for command output).
func Configure(level zerolog.Level) {
	zerolog.SetGlobalLevel(level)
	log.Logger = zerolog.New(zerolog.ConsoleWriter{
		Out:        os.Stderr,
		TimeFormat: time.RFC3339,
	}).With().Timestamp().Logger()
}

// LevelFlag adapts zerolog.Level to pflag.Value so cobra parses --log-level
// directly into a level. An invalid value is not fatal: it warns and keeps the
// current (default) level.
type LevelFlag struct {
	level zerolog.Level
}

// NewLevelFlag returns a LevelFlag initialized to def.
func NewLevelFlag(def zerolog.Level) *LevelFlag { return &LevelFlag{level: def} }

// Level returns the parsed level.
func (f *LevelFlag) Level() zerolog.Level { return f.level }

// String implements pflag.Value.
func (f *LevelFlag) String() string { return f.level.String() }

// Set implements pflag.Value, parsing s with zerolog.ParseLevel.
func (f *LevelFlag) Set(s string) error {
	lvl, err := zerolog.ParseLevel(s)
	if err != nil {
		log.Warn().Err(err).Msgf("invalid log level, keeping %q", f.level.String())
		return nil
	}
	f.level = lvl
	return nil
}

// Type implements pflag.Value.
func (f *LevelFlag) Type() string { return "level" }
