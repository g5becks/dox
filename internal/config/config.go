package config

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/knadh/koanf/parsers/toml/v2"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
	"github.com/samber/oops"
)

func configFilenames() []string {
	return []string{"dox.toml", ".dox.toml"}
}

func Load(configPath string) (*Config, error) {
	resolvedPath, err := resolveConfigPath(configPath)
	if err != nil {
		return nil, err
	}

	absConfigPath, err := filepath.Abs(resolvedPath)
	if err != nil {
		return nil, oops.Wrapf(err, "resolving absolute config path")
	}

	cfg := &Config{}
	k := koanf.New(".")

	if loadErr := k.Load(file.Provider(absConfigPath), toml.Parser()); loadErr != nil {
		return nil, oops.
			Code("CONFIG_INVALID").
			With("path", absConfigPath).
			Hint("Fix TOML syntax and required fields in your config").
			Wrapf(loadErr, "loading config from %q", absConfigPath)
	}

	if unmarshalErr := k.Unmarshal("", cfg); unmarshalErr != nil {
		return nil, oops.
			Code("CONFIG_INVALID").
			With("path", absConfigPath).
			Hint("Fix config structure to match dox schema").
			Wrapf(unmarshalErr, "decoding config from %q", absConfigPath)
	}

	cfg.ConfigDir = filepath.Dir(absConfigPath)
	cfg.ApplyDefaults()

	if valErr := cfg.Validate(); valErr != nil {
		return nil, valErr
	}

	if !filepath.IsAbs(cfg.Output) {
		cfg.Output = filepath.Clean(filepath.Join(cfg.ConfigDir, cfg.Output))
	}

	return cfg, nil
}

func FindConfigFile() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", oops.Wrapf(err, "getting working directory")
	}

	for {
		foundPath, found, findErr := findConfigInDirectory(dir)
		if findErr != nil {
			return "", findErr
		}

		if found {
			return foundPath, nil
		}

		parentDir := filepath.Dir(dir)
		if parentDir == dir {
			return "", oops.
				Code("CONFIG_NOT_FOUND").
				Hint("Run 'dox init' to create a config file").
				Errorf("no dox.toml or .dox.toml found in any parent directory")
		}

		dir = parentDir
	}
}

func resolveConfigPath(configPath string) (string, error) {
	if configPath != "" {
		if _, err := os.Stat(configPath); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return "", oops.
					Code("CONFIG_NOT_FOUND").
					With("path", configPath).
					Hint("Create the file or pass a valid --config path").
					Errorf("config file %q does not exist", configPath)
			}

			return "", oops.Wrapf(err, "checking config file %q", configPath)
		}

		return configPath, nil
	}

	return FindConfigFile()
}

func findConfigInDirectory(dir string) (string, bool, error) {
	for _, name := range configFilenames() {
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); err == nil {
			return path, true, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", false, oops.Wrapf(err, "checking for config file at %q", path)
		}
	}

	return "", false, nil
}
