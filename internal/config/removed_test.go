package config_test

import (
	"strings"
	"testing"

	"github.com/rawmind0/arbor/internal/config"
	"github.com/rawmind0/arbor/test/examples"
)

// Settings removed with Dockerfile generation fail to load with a message
// saying what replaces them, in the config file and in --sources files.
func TestLoad_RemovedSettings(t *testing.T) {
	tests := []struct {
		name    string
		config  string
		sources string // content of sources.yaml; "" for none
		wantErr []string
	}{
		{
			name:    "old dockerfiles mapping",
			config:  "config:\n  dockerfiles:\n    source: src\n    destination: build\nimages:\n  app:\n    version: 1.0.0\n",
			wantErr: []string{"config.dockerfiles is now the Dockerfiles directory"},
		},
		{
			name:    "envs on a nested image",
			config:  "images:\n  base:\n    version: 1.0.0\n    images:\n      app:\n        version: 1.0.0\n        envs:\n          PORT: \"80\"\n",
			wantErr: []string{`image "app": envs was removed; set them with ENV in its Dockerfile`},
		},
		{
			name:    "envs in a sources file",
			config:  "images:\n  base:\n    version: 1.0.0\n",
			sources: "images:\n  api:\n    version: 1.0.0\n    envs:\n      PORT: \"80\"\n",
			wantErr: []string{"sources.yaml", `image "api": envs was removed`},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			files := map[string]string{"config.yaml": tt.config}
			if tt.sources != "" {
				files["sources.yaml"] = tt.sources
			}
			examples.Write(t, files)
			cfg, err := config.LoadConfig("config.yaml", nil)
			if err == nil {
				err = cfg.LoadAdditionalImages("config.yaml", []string{"*.yaml"})
			}
			if err == nil {
				t.Fatal("load succeeded, want an error")
			}
			for _, want := range tt.wantErr {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error = %v, want it to contain %q", err, want)
				}
			}
		})
	}
}
