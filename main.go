package main

import (
	"github.com/rawmind0/arbor/cmd"
	"github.com/rawmind0/arbor/internal/logging"
)

var (
	Version  = "dev"
	Commit   = ""
	Date     = ""
	Platform = "linux/amd64" // Default, will be overridden at build time
)

func init() {
	// Sensible default until the --log-level flag is parsed (see cmd PersistentPreRun)
	logging.Configure(logging.DefaultLevel())
}

func main() {
	// Set the platform from build variable
	cmd.SetPlatform(Platform)
	cmd.Execute()
}
