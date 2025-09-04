package main

import (
	"github.com/rawmind0/container-builder/cmd"
)

var (
	Version = "dev"
	Commit  = ""
	Date    = ""
)

func main() {
	cmd.Execute()
}
