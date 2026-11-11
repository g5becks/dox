package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/knadh/koanf/parsers/toml/v2"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"

	"github.com/g5becks/dox/internal/errs"
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
		return nil, fmt.Errorf("resolving absolute config path: %w", err)
	}

	cfg := &Config{}
	k := koanf.New(".")

	if loadErr := k.Load(file.Provider(absConfigPath), toml.Parser()); loadErr != nil {
		return nil, errs.Wrap(
			errs.Config,
			fmt.Errorf("loading config from %q: %w", absConfigPath, loadErr),
			errs.WithHint("Fix TOML syntax and required fields in your config"),
			errs.WithFields(map[string]any{fieldPath: absConfigPath, errs.LegacyCodeField: errs.LegacyConfigInvalid}),
		)
	}

	if unmarshalErr := k.Unmarshal("", cfg); unmarshalErr != nil {
		return nil, errs.Wrap(
			errs.Config,
			fmt.Errorf("decoding config from %q: %w", absConfigPath, unmarshalErr),
			errs.WithHint("Fix config structure to match dox schema"),
			errs.WithFields(map[string]any{fieldPath: absConfigPath, errs.LegacyCodeField: errs.LegacyConfigInvalid}),
		)
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
		return "", fmt.Errorf("getting working directory: %w", err)
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
			return "", errs.New(
				errs.NotFound,
				"no dox.toml or .dox.toml found in any parent directory",
				errs.WithHint("Run 'dox init' to create a config file"),
				errs.WithFields(map[string]any{errs.LegacyCodeField: errs.LegacyConfigNotFound}),
			)
		}

		dir = parentDir
	}
}

func resolveConfigPath(configPath string) (string, error) {
	if configPath != "" {
		if _, err := os.Stat(configPath); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return "", errs.New(
					errs.NotFound,
					fmt.Sprintf("config file %q does not exist", configPath),
					errs.WithHint("Create the file or pass a valid --config path"),
					errs.WithFields(
						map[string]any{fieldPath: configPath, errs.LegacyCodeField: errs.LegacyConfigNotFound},
					),
				)
			}

			return "", fmt.Errorf("checking config file %q: %w", configPath, err)
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
			return "", false, fmt.Errorf("checking for config file at %q: %w", path, err)
		}
	}

	return "", false, nil
}
