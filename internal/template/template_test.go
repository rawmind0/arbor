package template

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rawmind0/container-builder/internal/config"
)

func TestNewProcessor(t *testing.T) {
	cfg := &config.Config{}
	processor := NewProcessor(cfg)

	if processor == nil {
		t.Error("Expected processor to be created")
	}
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

	processor := NewProcessor(cfg)
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

	processor := NewProcessor(cfg)
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

	processor := NewProcessor(cfg)
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
	processor := NewProcessor(cfg)

	content := "FROM ubuntu:22.04\nRUN echo 'test'"
	destPath := filepath.Join(tmpDir, "subdir", "Dockerfile")

	err = processor.WriteDockerfile(content, destPath)
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

	processor := NewProcessor(cfg)

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
