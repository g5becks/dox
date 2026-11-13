package dox

import (
	"github.com/g5becks/dox/internal/config"
)

// Project is a loaded dox configuration and the documentation synced for it.
type Project struct {
	cfg *config.Config
}

// LoadOption customizes Load.
type LoadOption func(*config.LoadOptions)

// WithExternalOutput allows an output directory outside the configuration
// directory, like the CLI's --allow-external-output. The configuration
// directory itself and the filesystem root remain refused.
func WithExternalOutput() LoadOption {
	return func(options *config.LoadOptions) { options.AllowExternalOutput = true }
}

// Load reads and validates a dox.toml. An empty configPath searches the
// working directory and its parents for dox.toml or .dox.toml, like the CLI.
func Load(configPath string, options ...LoadOption) (*Project, error) {
	var loadOptions config.LoadOptions
	for _, option := range options {
		option(&loadOptions)
	}
	cfg, err := config.LoadWithOptions(configPath, loadOptions)
	if err != nil {
		return nil, wrapError(err)
	}
	return &Project{cfg: cfg}, nil
}

// Output returns the directory that holds the synced documentation.
func (p *Project) Output() string { return p.cfg.Output }
