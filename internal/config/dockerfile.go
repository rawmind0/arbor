package config

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// instruction is one logical Dockerfile instruction (continuations joined).
type instruction struct {
	Cmd  string   // upper-cased keyword, e.g. "ARG", "FROM"
	Args []string // whitespace-separated arguments
	Line int      // 1-based line where the instruction starts
}

// heredocRe matches a heredoc opener such as <<EOF, <<-EOF, <<"EOF" or <<'EOF'.
var heredocRe = regexp.MustCompile(`<<(-?)["']?([A-Za-z_][A-Za-z0-9_]*)["']?`)

// parseDockerfile reads the Dockerfile at path into its instructions. It is a
// deliberately small parser: it skips blank lines and comments (which also
// covers parser directives such as `# syntax=`), joins `\` line continuations,
// and skips heredoc bodies. That is all the BASE contract check needs.
func parseDockerfile(path string) ([]instruction, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	var (
		out     []instruction
		cur     strings.Builder
		start   int
		heredoc []string // pending heredoc terminators, in order
		strip   []bool   // whether each terminator allows leading tabs (<<-)
	)
	flush := func() {
		text := strings.TrimSpace(cur.String())
		cur.Reset()
		if text == "" {
			return
		}
		fields := strings.Fields(text)
		out = append(out, instruction{Cmd: strings.ToUpper(fields[0]), Args: fields[1:], Line: start})
		for _, m := range heredocRe.FindAllStringSubmatch(text, -1) {
			heredoc = append(heredoc, m[2])
			strip = append(strip, m[1] == "-")
		}
	}

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := scanner.Text()

		// Inside a heredoc body: consume lines until its terminator.
		if len(heredoc) > 0 {
			check := line
			if strip[0] {
				check = strings.TrimLeft(line, "\t")
			}
			if check == heredoc[0] {
				heredoc, strip = heredoc[1:], strip[1:]
			}
			continue
		}

		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue // blank lines and comments never end a continuation
		}
		if cur.Len() == 0 {
			start = lineNo
		}
		if strings.HasSuffix(trimmed, `\`) {
			cur.WriteString(strings.TrimSuffix(trimmed, `\`))
			cur.WriteString(" ")
			continue
		}
		cur.WriteString(trimmed)
		flush()
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	flush() // a trailing continuation with no final line
	return out, nil
}

// refsBase reports whether an image reference is the BASE build argument:
// $BASE, ${BASE}, or ${BASE:-default} / ${BASE:+alt}.
func refsBase(ref string) bool {
	return ref == "$"+BaseArg || ref == "${"+BaseArg+"}" || strings.HasPrefix(ref, "${"+BaseArg+":")
}

// fromStage is the parsed form of a FROM instruction.
type fromStage struct {
	image string // the image reference (flags such as --platform skipped)
	alias string // the stage name after AS, lower-cased ("" if none)
}

func parseFrom(in instruction) fromStage {
	var st fromStage
	args := in.Args
	for len(args) > 0 && strings.HasPrefix(args[0], "--") {
		args = args[1:]
	}
	if len(args) > 0 {
		st.image = args[0]
	}
	if len(args) >= 3 && strings.EqualFold(args[1], "AS") {
		st.alias = strings.ToLower(args[2])
	}
	return st
}

// baseArg returns the `ARG BASE[=default]` declared before the first FROM:
// whether it is declared, and its default ("" when it has none).
func baseArg(instrs []instruction) (declared bool, def string) {
	for _, in := range instrs {
		if in.Cmd == "FROM" {
			break
		}
		if in.Cmd != "ARG" {
			continue
		}
		for _, a := range in.Args {
			name, value, _ := strings.Cut(a, "=")
			if name == BaseArg {
				return true, strings.Trim(value, `"'`)
			}
		}
	}
	return false, ""
}

// checkBase enforces the BASE contract on a parsed Dockerfile. injected is true
// when arbor passes --build-arg BASE (a child image, or a root with base_image).
//
// Injected: the first instruction must be `ARG BASE[=default]`, and the final
// stage (the image that gets tagged) must come from ${BASE}, directly or through
// stage aliases, so multi-stage builds are fine.
//
// Not injected (a root without base_image): any FROM is fine, but if the file
// uses ${BASE}, `ARG BASE` must have a default since nothing will supply one.
func checkBase(instrs []instruction, injected bool) error {
	var stages []fromStage
	for _, in := range instrs {
		if in.Cmd == "FROM" {
			stages = append(stages, parseFrom(in))
		}
	}
	if len(stages) == 0 {
		return errors.New("no FROM instruction")
	}

	if !injected {
		usesBase := false
		for _, st := range stages {
			if refsBase(st.image) {
				usesBase = true
				break
			}
		}
		if !usesBase {
			return nil
		}
		if declared, def := baseArg(instrs); !declared || def == "" {
			return fmt.Errorf("uses ${%s} but nothing supplies it: set base_image on the image or give `ARG %s` a default", BaseArg, BaseArg)
		}
		return nil
	}

	first := instrs[0]
	if first.Cmd != "ARG" || len(first.Args) == 0 {
		return fmt.Errorf("first instruction must be `ARG %s` (line %d: %s %s)", BaseArg, first.Line, first.Cmd, strings.Join(first.Args, " "))
	}
	if name, _, _ := strings.Cut(first.Args[0], "="); name != BaseArg {
		return fmt.Errorf("first instruction must be `ARG %s` (line %d: ARG %s)", BaseArg, first.Line, strings.Join(first.Args, " "))
	}

	// Resolve which stages derive from BASE, following AS aliases in order.
	fromBase := map[string]bool{}
	derives := func(st fromStage) bool {
		return refsBase(st.image) || fromBase[strings.ToLower(st.image)]
	}
	for _, st := range stages {
		if st.alias != "" {
			fromBase[st.alias] = derives(st)
		}
	}
	final := stages[len(stages)-1]
	if !derives(final) {
		return fmt.Errorf("final stage must be built FROM ${%s} (got FROM %s)", BaseArg, final.image)
	}
	return nil
}

// ValidateDockerfiles checks every image's Dockerfile (across all sources)
// against the BASE contract, so arbor can inject the parent image through
// `--build-arg BASE`. All problems are reported together, one per image.
func (c *Config) ValidateDockerfiles() error {
	var errs []error
	for _, img := range c.allImages() {
		path := img.GetDockerfilePath(c.Config.Dockerfiles)
		instrs, err := parseDockerfile(path)
		if err == nil {
			injected := img.Parent != "" || img.BaseImage != ""
			err = checkBase(instrs, injected)
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("image %q (%s): %w", img.Name, path, err))
		}
	}
	return errors.Join(errs...)
}
