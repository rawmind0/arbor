package generate

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rawmind0/arbor/internal/config"
)

func TestNew(t *testing.T) {
	cfg := &config.Config{}
	processor := New(cfg)

	if processor.config != cfg {
		t.Error("Expected processor to have reference to config")
	}
}

func TestProcessor_GenerateDockerfile(t *testing.T) {
	// Create temporary directory for test templates
	tmpDir, err := os.MkdirTemp("", "template-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create source directory
	srcDir := filepath.Join(tmpDir, "src")
	if err := os.Mkdir(srcDir, 0755); err != nil {
		t.Fatalf("Failed to create src dir: %v", err)
	}

	// Create a test template
	templateContent := `{{ template "base" . }}

{{ define "content" }}
RUN echo "Installing ${TEST_ARG}"
RUN echo "Architecture: ${TARGETARCH}"
{{ end }}`

	templatePath := filepath.Join(srcDir, "Dockerfile.test")
	if err := os.WriteFile(templatePath, []byte(templateContent), 0644); err != nil {
		t.Fatalf("Failed to write template: %v", err)
	}

	cfg := &config.Config{
		Config: config.ConfigSection{
			Builder: "docker",
			Dockerfiles: config.DockerfilesConfig{
				Source: srcDir,
			},
			Labels: map[string]string{
				"maintainer": "test@example.com",
			},
		},
	}

	img := &config.ImageConfig{
		Name:    "test",
		Version: "1.0.0",
		Args: map[string]string{
			"TEST_ARG": "test-package",
		},
		Envs: map[string]string{
			"TEST_ENV": "test-value",
		},
		Dockerfile: "Dockerfile.test",
	}

	processor := New(cfg)
	result, err := processor.GenerateDockerfile(img, "linux/amd64", "ubuntu:22.04")
	if err != nil {
		t.Fatalf("GenerateDockerfile() failed: %v", err)
	}

	// Check that the result contains expected content
	if !strings.Contains(result, "FROM ubuntu:22.04") {
		t.Error("Expected FROM statement not found")
	}
	if !strings.Contains(result, "ARG TEST_ARG=test-package") {
		t.Error("Expected ARG statement not found")
	}
	if !strings.Contains(result, "ENV TEST_ENV=test-value") {
		t.Error("Expected ENV statement not found")
	}
	if !strings.Contains(result, `LABEL maintainer="test@example.com"`) {
		t.Error("Expected LABEL statement not found")
	}
	if !strings.Contains(result, "RUN echo \"Installing ${TEST_ARG}\"") {
		t.Error("Expected custom content not found")
	}
	if !strings.Contains(result, "ARG TARGETARCH") {
		t.Error("Expected TARGETARCH build arg not found")
	}
}

func TestProcessor_GenerateDockerfile_NoTemplate(t *testing.T) {
	// Create temporary directory
	tmpDir, err := os.MkdirTemp("", "template-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := &config.Config{
		Config: config.ConfigSection{
			Dockerfiles: config.DockerfilesConfig{
				Source: tmpDir,
			},
		},
	}

	img := &config.ImageConfig{
		Name:       "test",
		Version:    "1.0.0",
		Dockerfile: "Dockerfile.nonexistent",
	}

	processor := New(cfg)
	result, err := processor.GenerateDockerfile(img, "linux/amd64", "ubuntu:22.04")
	if err != nil {
		t.Fatalf("GenerateDockerfile() failed: %v", err)
	}

	// Should still generate basic dockerfile with base template
	if !strings.Contains(result, "FROM ubuntu:22.04") {
		t.Error("Expected FROM statement not found")
	}
}

func TestProcessor_mergeLabels(t *testing.T) {
	cfg := &config.Config{
		Config: config.ConfigSection{
			Labels: map[string]string{
				"global": "global-value",
				"shared": "global-shared",
			},
		},
	}

	img := &config.ImageConfig{
		Name:    "test",
		Version: "1.0.0",
		Parent:  "parent",
		Labels: map[string]string{
			"image":  "image-value",
			"shared": "image-shared", // Should override global
		},
	}

	processor := New(cfg)
	merged := processor.mergeLabels(img)

	tests := []struct {
		key   string
		value string
	}{
		{"global", "global-value"},
		{"shared", "image-shared"}, // Image should override global
		{"image", "image-value"},
		{"image.name", "test"},     // Automatic label
		{"image.version", "1.0.0"}, // Automatic label
		{"image.parent", "parent"}, // Automatic label
	}

	for _, test := range tests {
		if merged[test.key] != test.value {
			t.Errorf("Expected label %s=%s, got %s", test.key, test.value, merged[test.key])
		}
	}
}

func TestProcessor_WriteDockerfile(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "template-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := &config.Config{}
	processor := New(cfg)

	content := "FROM ubuntu:22.04\nRUN echo 'test'"
	destPath := filepath.Join(tmpDir, "subdir", "Dockerfile")

	err = processor.writeDockerfile(content, destPath)
	if err != nil {
		t.Fatalf("WriteDockerfile() failed: %v", err)
	}

	// Check that file was created
	if _, err := os.Stat(destPath); os.IsNotExist(err) {
		t.Error("Dockerfile was not created")
	}

	// Check content
	written, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("Failed to read written file: %v", err)
	}

	if string(written) != content {
		t.Errorf("Written content doesn't match expected.\nGot:\n%s\nWant:\n%s", string(written), content)
	}
}

func TestTemplateData_Initialization(t *testing.T) {
	cfg := &config.Config{
		Config: config.ConfigSection{
			Builder: "docker",
		},
	}

	img := &config.ImageConfig{
		Name:    "test",
		Version: "1.0.0",
	}

	processor := New(cfg)

	// Test that nil args/envs are initialized to empty maps
	result, err := processor.GenerateDockerfile(img, "linux/amd64", "ubuntu:22.04")
	if err != nil {
		t.Fatalf("GenerateDockerfile() failed: %v", err)
	}

	// Should not panic and should generate valid dockerfile
	if !strings.Contains(result, "FROM ubuntu:22.04") {
		t.Error("Expected valid dockerfile to be generated")
	}
}

func TestGenerateDockerfile_TemplateErrors(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "template-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	tests := []struct {
		name            string
		templateContent string
		wantErr         bool
		errContains     string
	}{
		{
			name: "invalid template syntax",
			templateContent: `{{ template "base" . }}
{{ define "content" }}
{{ .InvalidField.NestedField }}
{{ end }}`,
			wantErr:     true,
			errContains: "", // Template execution error
		},
		{
			name: "missing closing bracket",
			templateContent: `{{ template "base" . }}
{{ define "content"
RUN echo "test"
{{ end }}`,
			wantErr:     true,
			errContains: "", // Parse error
		},
		{
			name: "circular template reference",
			templateContent: `{{ template "circular" . }}
{{ define "circular" }}
{{ template "circular" . }}
{{ end }}`,
			wantErr: true, // Go templates will error on circular reference at execution
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			templatePath := filepath.Join(tmpDir, "Dockerfile.test")
			if err := os.WriteFile(templatePath, []byte(tt.templateContent), 0644); err != nil {
				t.Fatal(err)
			}

			cfg := &config.Config{
				Config: config.ConfigSection{
					Dockerfiles: config.DockerfilesConfig{
						Source: tmpDir,
					},
				},
			}

			img := &config.ImageConfig{
				Name:       "test",
				Version:    "1.0.0",
				Dockerfile: "Dockerfile.test",
			}

			processor := New(cfg)
			_, err := processor.GenerateDockerfile(img, "linux/amd64", "ubuntu:22.04")

			if (err != nil) != tt.wantErr {
				t.Errorf("GenerateDockerfile() error = %v, wantErr %v", err, tt.wantErr)
			}

			if tt.wantErr && tt.errContains != "" && err != nil {
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("Expected error containing '%s', got '%v'", tt.errContains, err)
				}
			}
		})
	}
}

func TestWriteDockerfile_Errors(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "template-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := &config.Config{}
	processor := New(cfg)

	tests := []struct {
		name        string
		content     string
		destPath    string
		setup       func()
		cleanup     func()
		wantErr     bool
		errContains string
	}{
		{
			name:     "write to read-only directory",
			content:  "FROM ubuntu:22.04",
			destPath: filepath.Join(tmpDir, "readonly", "Dockerfile"),
			setup: func() {
				dir := filepath.Join(tmpDir, "readonly")
				os.Mkdir(dir, 0555) // Read-only directory
			},
			cleanup: func() {
				os.Chmod(filepath.Join(tmpDir, "readonly"), 0755)
			},
			wantErr:     true,
			errContains: "permission denied",
		},
		{
			name:     "invalid path with null character",
			content:  "FROM ubuntu:22.04",
			destPath: "/tmp/invalid\x00path/Dockerfile",
			wantErr:  true,
		},
		{
			name:     "very long path",
			content:  "FROM ubuntu:22.04",
			destPath: filepath.Join(tmpDir, strings.Repeat("a", 100), "Dockerfile"),
			wantErr:  false, // Should handle by creating directories
		},
		{
			name:     "path with special characters",
			content:  "FROM ubuntu:22.04",
			destPath: filepath.Join(tmpDir, "special!@#$%", "Dockerfile"),
			wantErr:  false, // Should handle special chars
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setup != nil {
				tt.setup()
			}
			defer func() {
				if tt.cleanup != nil {
					tt.cleanup()
				}
			}()

			err := processor.writeDockerfile(tt.content, tt.destPath)
			if (err != nil) != tt.wantErr {
				t.Errorf("WriteDockerfile() error = %v, wantErr %v", err, tt.wantErr)
			}

			if tt.wantErr && tt.errContains != "" && err != nil {
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("Expected error containing '%s', got '%v'", tt.errContains, err)
				}
			}
		})
	}
}

func TestGenerateDockerfile_ComplexTemplates(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "template-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Complex template with conditionals and loops
	complexTemplate := `{{ template "base" . }}

{{ define "content" }}
# Complex template test
{{ if .Args }}
{{ range $key, $value := .Args }}
ARG {{ $key }}={{ $value }}
{{ end }}
{{ end }}

{{ if eq .Architecture "linux/amd64" }}
RUN echo "AMD64 specific commands"
{{ else if eq .Architecture "linux/arm64" }}
RUN echo "ARM64 specific commands"
{{ else }}
RUN echo "Unknown architecture"
{{ end }}

{{ if .Envs }}
{{ range $key, $value := .Envs }}
ENV {{ $key }}={{ $value }}
{{ end }}
{{ end }}
{{ end }}`

	templatePath := filepath.Join(tmpDir, "Dockerfile.complex")
	if err := os.WriteFile(templatePath, []byte(complexTemplate), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		Config: config.ConfigSection{
			Dockerfiles: config.DockerfilesConfig{
				Source: tmpDir,
			},
			Labels: map[string]string{
				"complex": "true",
			},
		},
	}

	tests := []struct {
		name string
		img  *config.ImageConfig
		arch string
	}{
		{
			name: "with all fields",
			img: &config.ImageConfig{
				Name:       "complex",
				Version:    "1.0.0",
				Dockerfile: "Dockerfile.complex",
				Args: map[string]string{
					"ARG1": "val1",
					"ARG2": "val2",
				},
				Envs: map[string]string{
					"ENV1": "env1",
					"ENV2": "env2",
				},
			},
			arch: "linux/amd64",
		},
		{
			name: "arm64 architecture",
			img: &config.ImageConfig{
				Name:       "complex",
				Version:    "1.0.0",
				Dockerfile: "Dockerfile.complex",
			},
			arch: "linux/arm64",
		},
		{
			name: "unknown architecture",
			img: &config.ImageConfig{
				Name:       "complex",
				Version:    "1.0.0",
				Dockerfile: "Dockerfile.complex",
			},
			arch: "darwin/amd64",
		},
	}

	processor := New(cfg)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := processor.GenerateDockerfile(tt.img, tt.arch, "ubuntu:22.04")
			if err != nil {
				t.Fatalf("GenerateDockerfile() error: %v", err)
			}

			// Verify architecture-specific content
			switch tt.arch {
			case "linux/amd64":
				if !strings.Contains(result, "AMD64 specific commands") {
					t.Error("Expected AMD64 specific content")
				}
			case "linux/arm64":
				if !strings.Contains(result, "ARM64 specific commands") {
					t.Error("Expected ARM64 specific content")
				}
			default:
				if !strings.Contains(result, "Unknown architecture") {
					t.Error("Expected unknown architecture content")
				}
			}

			// Verify args and envs are rendered
			if tt.img.Args != nil {
				for key, val := range tt.img.Args {
					expected := fmt.Sprintf("ARG %s=%s", key, val)
					if !strings.Contains(result, expected) {
						t.Errorf("Expected '%s' in output", expected)
					}
				}
			}
		})
	}
}

func TestProcessor_EdgeCases(t *testing.T) {
	cfg := &config.Config{
		Config: config.ConfigSection{
			Labels: map[string]string{
				"special\"char": "value\"with\"quotes",
				"empty":         "",
			},
		},
	}

	processor := New(cfg)

	tests := []struct {
		name string
		img  *config.ImageConfig
	}{
		{
			name: "empty values",
			img: &config.ImageConfig{
				Name:    "",
				Version: "",
			},
		},
		{
			name: "special characters in values",
			img: &config.ImageConfig{
				Name:    "test-with-dash",
				Version: "1.0.0-beta.1",
				Args: map[string]string{
					"ARG_WITH_UNDERSCORE": "value with spaces",
				},
				Labels: map[string]string{
					"label.with.dots": "value/with/slashes",
				},
			},
		},
		{
			name: "very long values",
			img: &config.ImageConfig{
				Name:    strings.Repeat("a", 100),
				Version: strings.Repeat("1.", 50) + "0",
				Args: map[string]string{
					"LONG_ARG": strings.Repeat("x", 500),
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := processor.GenerateDockerfile(tt.img, "linux/amd64", "ubuntu:22.04")
			if err != nil {
				t.Fatalf("GenerateDockerfile() error: %v", err)
			}

			// Should generate valid dockerfile even with edge cases
			if !strings.Contains(result, "FROM ubuntu:22.04") {
				t.Error("Expected valid FROM statement")
			}
		})
	}
}

func TestWriteDockerfile_ConcurrentWrites(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "template-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := &config.Config{}
	processor := New(cfg)

	// Test concurrent writes to different files
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(id int) {
			path := filepath.Join(tmpDir, fmt.Sprintf("Dockerfile.%d", id))
			content := fmt.Sprintf("FROM ubuntu:22.04\n# File %d", id)

			if err := processor.writeDockerfile(content, path); err != nil {
				t.Errorf("Concurrent write %d failed: %v", id, err)
			}

			// Verify content
			written, _ := os.ReadFile(path)
			if string(written) != content {
				t.Errorf("Content mismatch for file %d", id)
			}

			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}
}
