package plan

import (
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/template"
	"time"

	"gopkg.in/yaml.v3"
)

//go:embed templates/*.tmpl
var templateFS embed.FS

// formatTemplates renders the text output formats (human, commands, graph). The
// structured formats (json, yaml) use encoders instead — they are serialization,
// not text rendering.
var formatTemplates = template.Must(template.New("plan").Funcs(template.FuncMap{
	"join":    strings.Join,
	"add":     func(a, b int) int { return a + b },
	"sub":     func(a, b int) int { return a - b },
	"rfc3339": func(t time.Time) string { return t.Format(time.RFC3339) },
	// commandFor finds a queue's build command by image tag (batches list tags).
	"commandFor": func(q Queue, tag string) *BuildCommand {
		for i := range q.Commands {
			if q.Commands[i].ImageTag == tag {
				return &q.Commands[i]
			}
		}
		return nil
	},
}).ParseFS(templateFS, "templates/*.tmpl"))

// Formatter renders a build plan to a writer in a chosen format.
type Formatter struct {
	plan   *BuildPlan
	writer io.Writer
}

// NewFormatter creates a new formatter.
func NewFormatter(plan *BuildPlan, writer io.Writer) *Formatter {
	return &Formatter{plan: plan, writer: writer}
}

// Format renders the plan in the given format.
func (f *Formatter) Format(format string) error {
	switch format {
	case "human", "text", "":
		return f.formatHuman()
	case "commands":
		return f.formatCommands()
	case "graph":
		return f.formatGraph()
	case "json":
		return f.formatJSON()
	case "yaml", "yml":
		return f.formatYAML()
	default:
		return fmt.Errorf("unsupported format: %s", format)
	}
}

func (f *Formatter) formatHuman() error    { return f.render("human.tmpl") }
func (f *Formatter) formatCommands() error { return f.render("commands.tmpl") }
func (f *Formatter) formatGraph() error    { return f.render("graph.tmpl") }

// render executes the named template against the plan, writing to the output.
func (f *Formatter) render(name string) error {
	return formatTemplates.ExecuteTemplate(f.writer, name, f.plan)
}

// formatJSON outputs the plan as indented JSON.
func (f *Formatter) formatJSON() error {
	encoder := json.NewEncoder(f.writer)
	encoder.SetIndent("", "  ")
	return encoder.Encode(f.plan)
}

// formatYAML outputs the plan as YAML.
func (f *Formatter) formatYAML() error {
	encoder := yaml.NewEncoder(f.writer)
	if err := encoder.Encode(f.plan); err != nil {
		_ = encoder.Close()
		return err
	}
	return encoder.Close()
}
