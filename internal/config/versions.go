package config

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// UpdateVersions sets the version of the named images in the YAML file at path
// (a config file or a --sources file), changing nothing else: only the text of
// each image's `version:` value is replaced, so comments, ordering, formatting
// and every other value stay byte-for-byte the same. versions maps image name
// to the new version; every named image must be defined in the file.
func UpdateVersions(path string, versions map[string]string) error {
	if len(versions) == 0 {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", path, err)
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("failed to parse %s: %w", path, err)
	}

	// Locate each image's version value node.
	type edit struct {
		node    *yaml.Node
		version string
	}
	var edits []edit
	found := map[string]bool{}
	var walk func(images *yaml.Node) error
	walk = func(images *yaml.Node) error {
		if images == nil || images.Kind != yaml.MappingNode {
			return nil
		}
		for i := 0; i+1 < len(images.Content); i += 2 {
			name, img := images.Content[i].Value, images.Content[i+1]
			if version, ok := versions[name]; ok {
				node := mappingValue(img, "version")
				if node == nil || node.Kind != yaml.ScalarNode {
					return fmt.Errorf("%s: image %q has no version to update", path, name)
				}
				edits = append(edits, edit{node: node, version: version})
				found[name] = true
			}
			if err := walk(mappingValue(img, "images")); err != nil {
				return err
			}
		}
		return nil
	}
	if len(doc.Content) > 0 {
		if err := walk(mappingValue(doc.Content[0], "images")); err != nil {
			return err
		}
	}
	var missing []string
	for name := range versions {
		if !found[name] {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("%s: images not defined in this file: %s", path, strings.Join(missing, ", "))
	}

	// Replace each value's text in place, last first so earlier offsets hold.
	lines := strings.SplitAfter(string(data), "\n")
	sort.Slice(edits, func(i, j int) bool {
		if edits[i].node.Line != edits[j].node.Line {
			return edits[i].node.Line > edits[j].node.Line
		}
		return edits[i].node.Column > edits[j].node.Column
	})
	for _, e := range edits {
		n := e.node
		quote := ""
		switch n.Style {
		case yaml.DoubleQuotedStyle:
			quote = `"`
		case yaml.SingleQuotedStyle:
			quote = `'`
		}
		oldText := quote + n.Value + quote
		newText := quote + e.version + quote

		line, col := n.Line-1, n.Column-1 // yaml.v3 positions are 1-based
		if line >= len(lines) || col+len(oldText) > len(lines[line]) || lines[line][col:col+len(oldText)] != oldText {
			return fmt.Errorf("%s:%d: cannot locate version %q to update", path, n.Line, n.Value)
		}
		lines[line] = lines[line][:col] + newText + lines[line][col+len(oldText):]
	}

	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	return os.WriteFile(path, []byte(strings.Join(lines, "")), info.Mode().Perm())
}

// mappingValue returns the value node for key in a mapping node, or nil.
func mappingValue(m *yaml.Node, key string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}
