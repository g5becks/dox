package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"github.com/g5becks/dox/internal/errs"
)

func configFilenames() []string {
	return []string{"dox.toml", ".dox.toml"}
}

// LoadOptions controls explicit opt-ins to configuration behavior.
type LoadOptions struct{ AllowExternalOutput bool }

func Load(configPath string) (*Config, error) { return LoadWithOptions(configPath, LoadOptions{}) }

// LoadWithOptions validates raw paths before normalizing them.
func LoadWithOptions(configPath string, opts LoadOptions) (*Config, error) {
	resolvedPath, err := resolveConfigPath(configPath)
	if err != nil {
		return nil, err
	}

	absConfigPath, err := filepath.Abs(resolvedPath)
	if err != nil {
		return nil, fmt.Errorf("resolving absolute config path: %w", err)
	}

	cfg, err := decodeConfig(absConfigPath)
	if err != nil {
		return nil, err
	}

	cfg.ConfigDir = filepath.Dir(absConfigPath)
	cfg.AllowExternalOutput = opts.AllowExternalOutput
	cfg.ApplyDefaults()

	if valErr := cfg.Validate(); valErr != nil {
		return nil, valErr
	}

	if _, pathErr := NewPaths(cfg.ConfigDir, cfg.Output, opts.AllowExternalOutput); pathErr != nil {
		return nil, pathErr
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

// decodeConfig strictly decodes TOML: types must match exactly and unknown
// keys are errors, each reported with its key path and line.
func decodeConfig(absConfigPath string) (*Config, error) {
	fields := map[string]any{fieldPath: absConfigPath, errs.LegacyCodeField: errs.LegacyConfigInvalid}
	data, err := os.ReadFile(absConfigPath)
	if err != nil {
		return nil, errs.Wrap(
			errs.Config,
			fmt.Errorf("reading config %q: %w", absConfigPath, err),
			errs.WithFields(fields),
		)
	}
	cfg := &Config{}
	decoder := toml.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decodeErr := decoder.Decode(cfg); decodeErr != nil {
		return nil, configDecodeError(absConfigPath, decodeErr, fields)
	}
	return cfg, nil
}

func configDecodeError(absConfigPath string, err error, fields map[string]any) error {
	if strict, ok := errors.AsType[*toml.StrictMissingError](err); ok {
		unknown := make([]string, 0, len(strict.Errors))
		for index := range strict.Errors {
			row, _ := strict.Errors[index].Position()
			unknown = append(unknown, fmt.Sprintf("%q (line %d)", strings.Join(strict.Errors[index].Key(), "."), row))
		}
		fields["unknown_keys"] = unknown
		return errs.New(
			errs.Config,
			fmt.Sprintf("unknown config key(s) in %s: %s", filepath.Base(absConfigPath), strings.Join(unknown, ", ")),
			errs.WithHint("Check the spelling against the Config Reference in the README"),
			errs.WithFields(fields),
		)
	}
	if decodeErr, ok := errors.AsType[*toml.DecodeError](err); ok {
		row, _ := decodeErr.Position()
		key := strings.Join(decodeErr.Key(), ".")
		where := fmt.Sprintf("line %d", row)
		if key != "" {
			where = fmt.Sprintf("%q (line %d)", key, row)
			fields[fieldName] = key
		}
		fields["line"] = row
		return errs.New(
			errs.Config,
			fmt.Sprintf(
				"invalid config at %s in %s: %s",
				where,
				filepath.Base(absConfigPath),
				describeDecodeError(decodeErr),
			),
			errs.WithHint("Fix the value's type or TOML syntax; strings need quotes, lists use [\"a\", \"b\"]"),
			errs.WithFields(fields),
		)
	}
	return errs.Wrap(errs.Config, fmt.Errorf("decoding config %q: %w", absConfigPath, err), errs.WithFields(fields))
}

// describeDecodeError turns go-toml's Go-typed message into config terms.
func describeDecodeError(err *toml.DecodeError) string {
	message := strings.TrimPrefix(err.Error(), "toml: ")
	const into = "into "
	for _, replacement := range [][2]string{
		{"into struct field config.Source.", into},
		{"into struct field config.Config.", into},
		{"into struct field config.Display.", into},
		{" of type []string", " (a list of strings)"},
		{" of type string", " (a string)"},
		{" of type int", " (an integer)"},
		{" of type bool", " (true or false)"},
	} {
		message = strings.ReplaceAll(message, replacement[0], replacement[1])
	}
	return message
}
