package config

// RegistryConfig is the global push target: a registry URL and an owner
// (namespace/org/user). Tags are qualified as "<url>/<owner>/<name>:<tag>", or
// "<owner>/<name>:<tag>" when URL is empty (the builder's implicit registry).
type RegistryConfig struct {
	URL   string `yaml:"url,omitempty"`
	Owner string `yaml:"owner,omitempty"`
}

// repository returns "<url>/<owner>/<name>" (or "<owner>/<name>" when URL empty).
func (r RegistryConfig) repository(name string) string {
	repo := name
	if r.Owner != "" {
		repo = r.Owner + "/" + name
	}
	if r.URL != "" {
		return r.URL + "/" + repo
	}
	return repo
}

// Tag is the image tag: "<repo>:<version>" (the multi-arch manifest tag).
func (r RegistryConfig) Tag(name, version string) string {
	return r.repository(name) + ":" + version
}
