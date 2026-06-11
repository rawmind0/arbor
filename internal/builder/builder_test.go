package builder

import (
	"strings"
	"testing"
)

func TestGetBuilder(t *testing.T) {
	tests := []struct {
		name        string
		builderName string
		mode        BuilderMode
		wantErr     bool
		wantType    string
	}{
		{
			name:        "docker builder normal mode",
			builderName: "docker",
			mode:        ModeNormal,
			wantErr:     false,
			wantType:    "docker",
		},
		{
			name:        "docker builder mock mode",
			builderName: "docker",
			mode:        ModeMock,
			wantErr:     false,
			wantType:    "mock[docker]",
		},
		{
			name:        "docker builder mock-error mode",
			builderName: "docker",
			mode:        ModeMockError,
			wantErr:     false,
			wantType:    "mock[docker]",
		},
		{
			name:        "podman builder",
			builderName: "podman",
			mode:        ModeNormal,
			wantErr:     false,
			wantType:    "podman",
		},
		{
			name:        "buildah builder",
			builderName: "buildah",
			mode:        ModeNormal,
			wantErr:     false,
			wantType:    "buildah",
		},
		{
			name:        "kaniko builder",
			builderName: "kaniko",
			mode:        ModeNormal,
			wantErr:     false,
			wantType:    "kaniko",
		},
		{
			name:        "docker with spaces",
			builderName: "  docker  ",
			mode:        ModeNormal,
			wantErr:     false,
			wantType:    "docker",
		},
		{
			name:        "docker uppercase",
			builderName: "DOCKER",
			mode:        ModeNormal,
			wantErr:     false,
			wantType:    "docker",
		},
		{
			name:        "mock builder legacy",
			builderName: "mock",
			mode:        ModeNormal,
			wantErr:     false,
			wantType:    "mock[docker]",
		},
		{
			name:        "mock-error builder legacy",
			builderName: "mock-error",
			mode:        ModeNormal,
			wantErr:     false,
			wantType:    "mock[docker]",
		},
		{
			name:        "unsupported builder",
			builderName: "unknown",
			mode:        ModeNormal,
			wantErr:     true,
			wantType:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GetBuilder(tt.builderName, tt.mode)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetBuilder() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err == nil && got.Name() != tt.wantType {
				t.Errorf("GetBuilder() = %v, want %v", got.Name(), tt.wantType)
			}
		})
	}
}

func TestIsBuilderSupported(t *testing.T) {
	tests := []struct {
		name    string
		builder string
		want    bool
	}{
		{"docker supported", "docker", true},
		{"podman supported", "podman", true},
		{"buildah supported", "buildah", true},
		{"kaniko supported", "kaniko", true},
		{"unknown not supported", "unknown", false},
		{"empty not supported", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsBuilderSupported(tt.builder); got != tt.want {
				t.Errorf("IsBuilderSupported() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetSupportedBuilders(t *testing.T) {
	supported := GetSupportedBuilders()

	if len(supported) != 6 {
		t.Errorf("GetSupportedBuilders() returned %d builders, want 6", len(supported))
	}

	expected := []string{BuilderDocker, BuilderPodman, BuilderBuildah, BuilderKaniko, BuilderMock, BuilderMockError}
	for _, exp := range expected {
		found := false
		for _, got := range supported {
			if got == exp {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("GetSupportedBuilders() missing %s", exp)
		}
	}
}

func TestDockerCompatible_BuildCommand(t *testing.T) {
	cmd := NewDockerCompatible("docker").BuildSteps(BuildOptions{
		ImageTag:       "myapp:v1.0.0",
		DockerfilePath: "Dockerfile",
		BuildContext:   ".",
		Platforms:      []string{"linux/amd64"},
		BuildArgs:      map[string]string{"VERSION": "1.0.0"},
		Labels:         map[string]string{"app": "myapp"},
		ExtraArgs:      []string{"--no-cache"},
	})[0]

	if cmd[0] != "docker" || cmd[1] != "build" {
		t.Errorf("expected single-arch 'docker build' (no buildx), got %v", cmd[:2])
	}
	cmdStr := strings.Join(cmd, " ")
	for _, check := range []string{
		"--platform linux/amd64", "-f Dockerfile", "-t myapp:v1.0.0",
		"--build-arg VERSION=1.0.0", "--label app=myapp", "--no-cache",
	} {
		if !strings.Contains(cmdStr, check) {
			t.Errorf("missing expected parameter: %s", check)
		}
	}
	if cmd[len(cmd)-1] != "." {
		t.Errorf("build context '.' should be last, got %s", cmd[len(cmd)-1])
	}
}

func TestDockerCompatible_EmptyOptionalParams(t *testing.T) {
	cmd := NewDockerCompatible("docker").BuildSteps(BuildOptions{
		ImageTag: "myapp:v1.0.0", DockerfilePath: "Dockerfile", BuildContext: ".",
	})[0]
	cmdStr := strings.Join(cmd, " ")
	if !strings.Contains(cmdStr, "docker build") || strings.Contains(cmdStr, "buildx") {
		t.Error("single-arch should contain plain 'docker build' (no buildx)")
	}
	if strings.Contains(cmdStr, "--platform") {
		t.Error("should not contain --platform when no platforms specified")
	}
	if !strings.Contains(cmdStr, "-f Dockerfile") || !strings.Contains(cmdStr, "-t myapp:v1.0.0") {
		t.Error("missing -f / -t")
	}
}

func TestBuildah_BuildCommand(t *testing.T) {
	cmd := NewBuildah().BuildSteps(BuildOptions{
		ImageTag: "myapp:v1.0.0", DockerfilePath: "Dockerfile", BuildContext: ".",
		Platforms: []string{"linux/amd64"},
	})[0]
	if cmd[0] != "buildah" || cmd[1] != "build" {
		t.Errorf("expected 'buildah build', got %v", cmd[:2])
	}
	if !strings.Contains(strings.Join(cmd, " "), "--platform linux/amd64") {
		t.Error("missing --platform")
	}
}

func TestKaniko_BuildCommand(t *testing.T) {
	cmd := NewKaniko().BuildSteps(BuildOptions{
		ImageTag: "myapp:v1.0.0", DockerfilePath: "Dockerfile", BuildContext: "dir://workspace",
		BuildArgs: map[string]string{"VERSION": "1.0.0"},
	})[0]
	if cmd[0] != "/kaniko/executor" {
		t.Errorf("expected '/kaniko/executor', got %s", cmd[0])
	}
	cmdStr := strings.Join(cmd, " ")
	for _, check := range []string{"--dockerfile Dockerfile", "--context dir://workspace", "--destination myapp:v1.0.0"} {
		if !strings.Contains(cmdStr, check) {
			t.Errorf("missing: %s", check)
		}
	}
}

func TestCommandBuilder_Methods(t *testing.T) {
	cb := NewCommandBuilder("test", "command")

	// Test AddParam
	cb.AddParam("param1")

	// Test AddParamValue
	cb.AddParamValue("--flag", "value")
	cb.AddParamValue("--empty", "") // Should be skipped

	// Test AddFlag
	cb.AddFlag("--verbose", true)
	cb.AddFlag("--quiet", false) // Should be skipped

	// Test AddMap
	cb.AddMap("--env", map[string]string{
		"KEY1": "value1",
		"KEY2": "value2",
	})

	// Test AddExtra
	cb.AddExtra([]string{"extra1", "extra2"})

	cmd := cb.Build()

	// Verify command structure
	if cmd[0] != "test" || cmd[1] != "command" {
		t.Errorf("Command should start with 'test command', got %s %s", cmd[0], cmd[1])
	}

	cmdStr := strings.Join(cmd, " ")

	// Check all added parameters
	if !strings.Contains(cmdStr, "param1") {
		t.Error("Command missing param1")
	}
	if !strings.Contains(cmdStr, "--flag value") {
		t.Error("Command missing --flag value")
	}
	if strings.Contains(cmdStr, "--empty") {
		t.Error("Command should not contain --empty when value is empty")
	}
	if !strings.Contains(cmdStr, "--verbose") {
		t.Error("Command missing --verbose flag")
	}
	if strings.Contains(cmdStr, "--quiet") {
		t.Error("Command should not contain --quiet when flag is false")
	}
	if !strings.Contains(cmdStr, "--env KEY1=value1") {
		t.Error("Command missing --env KEY1=value1")
	}
	if !strings.Contains(cmdStr, "--env KEY2=value2") {
		t.Error("Command missing --env KEY2=value2")
	}
	if !strings.Contains(cmdStr, "extra1 extra2") {
		t.Error("Command missing extra arguments")
	}
}

func TestMockBuilder_BuildCommand(t *testing.T) {
	// Use GetBuilder with mock mode to get a properly configured mock
	mock, err := GetBuilder("docker", ModeMock)
	if err != nil {
		t.Fatalf("Failed to get mock builder: %v", err)
	}

	opts := BuildOptions{
		ImageTag:       "test:v1.0.0",
		DockerfilePath: "Dockerfile.test",
		BuildContext:   ".",
		Platforms:      []string{"linux/amd64"},
		BuildArgs: map[string]string{
			"VERSION": "1.0.0",
		},
		Labels: map[string]string{
			"version": "1.0.0",
		},
	}

	cmd := mock.BuildSteps(opts)[0]

	// Check command structure
	if cmd[0] != "sh" || cmd[1] != "-c" {
		t.Errorf("Expected mock command to start with 'sh -c', got %s %s", cmd[0], cmd[1])
	}

	// Check that the command contains the real docker command in {}
	if !strings.Contains(cmd[2], "{docker build") {
		t.Error("Mock command should contain the real docker command in {}")
	}

	// Check that the real command contains the expected parameters
	if !strings.Contains(cmd[2], "test:v1.0.0") {
		t.Error("Mock command should contain the image tag")
	}

	// Check that it's a dry-run command
	if !strings.Contains(cmd[2], MockDryRunPrefix) {
		t.Error("Mock command should indicate it's a dry-run")
	}

	// Check that it includes a sleep (for timing simulation)
	if !strings.Contains(cmd[2], "sleep") {
		t.Error("Mock command should include a sleep for timing simulation")
	}

	// Test that we can extract the real command
	realCmd := ExtractRealCommand(cmd[2])
	if !strings.HasPrefix(realCmd, "docker build") {
		t.Errorf("Extracted command should start with 'docker build', got: %s", realCmd)
	}
	if !strings.Contains(realCmd, "--platform linux/amd64") {
		t.Error("Real command should contain platform")
	}
	if !strings.Contains(realCmd, "-t test:v1.0.0") {
		t.Error("Real command should contain tag")
	}
}

func TestMockBuilder_Properties(t *testing.T) {
	mock, err := GetBuilder("docker", ModeMock)
	if err != nil {
		t.Fatalf("Failed to get mock builder: %v", err)
	}

	// Test Name
	expectedName := "mock[docker]"
	if mock.Name() != expectedName {
		t.Errorf("Expected name '%s', got '%s'", expectedName, mock.Name())
	}

	// Test IsAvailable (should depend on real builder)
	mock.IsAvailable() // Just call it, availability depends on docker

	// Test Validate (should depend on real builder)
	mock.Validate() // Just call it, validation depends on docker

	// Test Version
	version, _ := mock.Version()
	if !strings.HasPrefix(version, "mock[") {
		t.Errorf("Expected version to start with 'mock[', got '%s'", version)
	}
}

func TestMockBuilderWithErrors_BuildCommand(t *testing.T) {
	mockError, err := GetBuilder("docker", ModeMockError)
	if err != nil {
		t.Fatalf("Failed to get mock-error builder: %v", err)
	}

	opts := BuildOptions{
		ImageTag:       "test:v1.0.0",
		DockerfilePath: "Dockerfile.test",
		BuildContext:   ".",
	}

	// Run multiple times to test both success and failure cases
	successCount := 0
	failureCount := 0
	iterations := 20

	for i := 0; i < iterations; i++ {
		cmd := mockError.BuildSteps(opts)[0]

		// Check basic command structure
		if cmd[0] != "sh" || cmd[1] != "-c" {
			t.Errorf("Expected mock command to start with 'sh -c', got %s %s", cmd[0], cmd[1])
		}

		// Check if it's an error or success command
		if strings.Contains(cmd[2], "exit 1") {
			failureCount++
			// Error commands should have error message
			if !strings.Contains(cmd[2], MockErrorMarker) {
				t.Error("Error command should contain error marker")
			}
		} else {
			successCount++
			// Success commands should have success message
			if !strings.Contains(cmd[2], MockSuccessMarker) {
				t.Error("Success command should contain success marker")
			}
		}

		// Both should contain the real command
		if !strings.Contains(cmd[2], "{docker build") {
			t.Error("Mock command should contain the real docker command")
		}
	}

	// With 50% error rate, we should have some of each
	// Allow for statistical variance
	if successCount == 0 || failureCount == 0 {
		t.Errorf("Expected mix of success and failure with 50%% error rate, got %d successes and %d failures",
			successCount, failureCount)
	}
}

func TestMockBuilderWithErrors_Properties(t *testing.T) {
	mockError, err := GetBuilder("docker", ModeMockError)
	if err != nil {
		t.Fatalf("Failed to get mock-error builder: %v", err)
	}

	// Test Name
	expectedName := "mock[docker]"
	if mockError.Name() != expectedName {
		t.Errorf("Expected name '%s', got '%s'", expectedName, mockError.Name())
	}

	// Test Version
	version, _ := mockError.Version()
	if !strings.HasPrefix(version, "mock[") {
		t.Errorf("Expected version to start with 'mock[', got '%s'", version)
	}
}

func TestMockHelperFunctions(t *testing.T) {
	// Test ExtractRealCommand
	mockOutput := "[DRY-RUN] Would execute: {docker build --platform linux/amd64 -t test:v1 .}"
	realCmd := ExtractRealCommand(mockOutput)
	if realCmd != "docker build --platform linux/amd64 -t test:v1 ." {
		t.Errorf("ExtractRealCommand failed, got: %s", realCmd)
	}

	// Test ExtractRealCommandArgs
	args := ExtractRealCommandArgs(mockOutput)
	if len(args) != 7 {
		t.Errorf("ExtractRealCommandArgs should return 7 args, got %d", len(args))
	}
	if args[0] != "docker" || args[1] != "build" {
		t.Error("First two args should be 'docker' and 'build'")
	}

	// Test IsMockCommand
	if !IsMockCommand(mockOutput) {
		t.Error("IsMockCommand should return true for mock output")
	}
	if IsMockCommand("regular output") {
		t.Error("IsMockCommand should return false for regular output")
	}

	// Test GetMockCommandStatus
	successOutput := "[DRY-RUN] Would execute: {docker build .} && sleep 2 && echo '[DRY-RUN] SUCCESS'"
	if GetMockCommandStatus(successOutput) != "SUCCESS" {
		t.Error("Should detect SUCCESS status")
	}

	errorOutput := "[DRY-RUN] Would execute: {docker build .} && sleep 2 && echo '[DRY-RUN] ERROR: Build failed' >&2"
	if GetMockCommandStatus(errorOutput) != "ERROR" {
		t.Error("Should detect ERROR status")
	}

	// Test GetMockErrorMessage
	errorMsg := GetMockErrorMessage("[DRY-RUN] ERROR: Permission denied")
	if errorMsg != "Permission denied" {
		t.Errorf("GetMockErrorMessage failed, got: %s", errorMsg)
	}
}
