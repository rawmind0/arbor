package config

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/rawmind0/arbor/test/testutil"
)

func TestCheckBase(t *testing.T) {
	tests := []struct {
		name       string
		dockerfile string
		injected   bool   // arbor passes --build-arg BASE (child, or root with base_image)
		wantErr    string // substring; "" means valid
	}{
		{name: "arg without default", injected: true,
			dockerfile: "ARG BASE\nFROM ${BASE}\nRUN true\n"},
		{name: "arg with default", injected: true,
			dockerfile: "ARG BASE=alpine:3.18\nFROM ${BASE}\n"},
		{name: "dollar form", injected: true,
			dockerfile: "ARG BASE\nFROM $BASE\n"},
		{name: "default expansion form", injected: true,
			dockerfile: "ARG BASE\nFROM ${BASE:-alpine:3.18}\n"},
		{name: "platform flag and alias", injected: true,
			dockerfile: "ARG BASE\nFROM --platform=$BUILDPLATFORM ${BASE} AS final\n"},
		{name: "comments and syntax directive first", injected: true,
			dockerfile: "# syntax=docker/dockerfile:1\n\n# a comment\nARG BASE\nFROM ${BASE}\n"},
		{name: "lowercase instructions", injected: true,
			dockerfile: "arg BASE\nfrom ${BASE} as final\n"},
		{name: "multi-stage, final stage on BASE", injected: true,
			dockerfile: "ARG BASE\nFROM golang:1.23 AS build\nRUN go build\nFROM ${BASE}\nCOPY --from=build /app /app\n"},
		{name: "multi-stage, final stage through an alias of BASE", injected: true,
			dockerfile: "ARG BASE\nFROM ${BASE} AS runtime\nFROM golang:1.23 AS build\nFROM runtime\nCOPY --from=build /app /app\n"},
		{name: "continuation and heredoc", injected: true,
			dockerfile: "ARG BASE\nFROM ${BASE}\nRUN apt-get update && \\\n    apt-get install -y curl\nRUN <<EOF\nFROM inside-heredoc\nEOF\n"},
		{name: "multi-stage, final stage not on BASE", injected: true,
			dockerfile: "ARG BASE\nFROM ${BASE} AS runtime\nFROM golang:1.23\n",
			wantErr:    "final stage must be built FROM ${BASE} (got FROM golang:1.23)"},
		{name: "hardcoded FROM", injected: true,
			dockerfile: "FROM ubuntu:22.04\n",
			wantErr:    "first instruction must be `ARG BASE` (line 1: FROM ubuntu:22.04)"},
		{name: "other ARG before BASE", injected: true,
			dockerfile: "ARG VERSION=1\nARG BASE\nFROM ${BASE}\n",
			wantErr:    "first instruction must be `ARG BASE` (line 1: ARG VERSION=1)"},
		{name: "ARG BASE but FROM hardcoded", injected: true,
			dockerfile: "ARG BASE\nFROM ubuntu:22.04\n",
			wantErr:    "final stage must be built FROM ${BASE}"},
		{name: "no FROM", injected: true,
			dockerfile: "ARG BASE\n",
			wantErr:    "no FROM instruction"},
		{name: "root without base_image, plain FROM", injected: false,
			dockerfile: "FROM alpine:3.18\n"},
		{name: "root without base_image, BASE with default", injected: false,
			dockerfile: "ARG BASE=alpine:3.18\nFROM ${BASE}\n"},
		{name: "root without base_image, BASE without default", injected: false,
			dockerfile: "ARG BASE\nFROM ${BASE}\n",
			wantErr:    "nothing supplies it"},
		{name: "root without base_image, BASE never declared", injected: false,
			dockerfile: "FROM ${BASE}\n",
			wantErr:    "nothing supplies it"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "Dockerfile")
			testutil.WriteFile(t, path, tt.dockerfile)
			instrs, err := parseDockerfile(path)
			if err != nil {
				t.Fatalf("parseDockerfile: %v", err)
			}
			err = checkBase(instrs, tt.injected)
			switch {
			case tt.wantErr == "" && err != nil:
				t.Fatalf("checkBase() = %v, want nil", err)
			case tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)):
				t.Fatalf("checkBase() = %v, want error containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestParseDockerfile_JoinsContinuations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Dockerfile")
	content := "ARG BASE\nFROM ${BASE}\nRUN a \\\n  # comment inside\n  && b\nCMD [\"x\"]\n"
	testutil.WriteFile(t, path, content)
	instrs, err := parseDockerfile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cmds []string
	for _, in := range instrs {
		cmds = append(cmds, in.Cmd)
	}
	if got := strings.Join(cmds, ","); got != "ARG,FROM,RUN,CMD" {
		t.Fatalf("instructions = %s, want ARG,FROM,RUN,CMD", got)
	}
	if run := instrs[2]; strings.Join(run.Args, " ") != "a && b" || run.Line != 3 {
		t.Fatalf("RUN = %q (line %d), want \"a && b\" on line 3", strings.Join(run.Args, " "), run.Line)
	}
}
