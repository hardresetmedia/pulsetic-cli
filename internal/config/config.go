// Package config loads pulsetic-cli settings from (in order of precedence):
//
//  1. CLI flag overrides (applied by the cli package after Load)
//  2. Environment variables
//  3. TOML config file
//  4. Compiled defaults
//
// The API token is intentionally env-only (PULSETIC_API_TOKEN). It is never
// read from the config file so the file can be committed.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// Config is the full resolved configuration for a run.
type Config struct {
	Token  string        // from env only
	Output OutputConfig  `toml:"output"`
	Client ClientConfig  `toml:"client"`
	Defaults DefaultsConfig `toml:"defaults"`
}

type OutputConfig struct {
	Dir         string `toml:"dir"`
	FilePattern string `toml:"file_pattern"`
}

type DefaultsConfig struct {
	Since string `toml:"since"`
}

type ClientConfig struct {
	Timeout      Duration `toml:"timeout"`
	RetryMax     int      `toml:"retry_max"`
	RetryBackoff Duration `toml:"retry_backoff"`
	BaseURL      string   `toml:"base_url"`
}

// Duration is a time.Duration that unmarshals from a TOML string like "30s".
type Duration time.Duration

// UnmarshalText implements encoding.TextUnmarshaler for TOML decoding.
func (d *Duration) UnmarshalText(text []byte) error {
	dur, err := time.ParseDuration(string(text))
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", string(text), err)
	}
	*d = Duration(dur)
	return nil
}

// Std returns the underlying time.Duration.
func (d Duration) Std() time.Duration { return time.Duration(d) }

// Defaults returns the compiled-in configuration defaults.
func Defaults() Config {
	return Config{
		Output: OutputConfig{
			Dir:         "./audit",
			FilePattern: "pulsetic-{YYYY}-{MM}.jsonl",
		},
		Defaults: DefaultsConfig{Since: "24h"},
		Client: ClientConfig{
			Timeout:      Duration(30 * time.Second),
			RetryMax:     3,
			RetryBackoff: Duration(500 * time.Millisecond),
		},
	}
}

// Load resolves the configuration from defaults, the TOML file at path
// (if it exists - missing is not an error), and environment variables.
// Pass an empty path to skip the file and use ResolveConfigPath logic.
func Load(path string) (Config, error) {
	cfg := Defaults()

	if path == "" {
		resolved, err := ResolveConfigPath()
		if err != nil {
			return cfg, err
		}
		path = resolved
	}

	if path != "" {
		if _, err := os.Stat(path); err == nil {
			if _, err := toml.DecodeFile(path, &cfg); err != nil {
				return cfg, fmt.Errorf("config: decode %s: %w", path, err)
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return cfg, fmt.Errorf("config: stat %s: %w", path, err)
		}
	}

	applyEnv(&cfg)

	return cfg, nil
}

// applyEnv overlays environment variables on cfg. Only the token is
// required to be present at this layer; the rest are optional overrides.
func applyEnv(cfg *Config) {
	if v := os.Getenv("PULSETIC_API_TOKEN"); v != "" {
		cfg.Token = v
	}
	if v := os.Getenv("PULSETIC_CLI_OUTPUT_DIR"); v != "" {
		cfg.Output.Dir = v
	}
	if v := os.Getenv("PULSETIC_CLI_SINCE"); v != "" {
		cfg.Defaults.Since = v
	}
	if v := os.Getenv("PULSETIC_CLI_BASE_URL"); v != "" {
		cfg.Client.BaseURL = v
	}
}

// ResolveConfigPath returns the default config file path per XDG:
//
//	$XDG_CONFIG_HOME/pulsetic-cli/config.toml
//	$HOME/.config/pulsetic-cli/config.toml
//
// An empty string is returned if no reasonable path can be determined
// (e.g. $HOME unset); callers should treat this as "no config file".
func ResolveConfigPath() (string, error) {
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "pulsetic-cli", "config.toml"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "", nil
	}
	return filepath.Join(home, ".config", "pulsetic-cli", "config.toml"), nil
}

// Validate checks the resolved config for required fields.
func (c Config) Validate() error {
	if c.Token == "" {
		return errors.New("PULSETIC_API_TOKEN environment variable is required\n  Get your token: https://app.pulsetic.com/settings/api\n  Then run: export PULSETIC_API_TOKEN=your_token")
	}
	if c.Output.Dir == "" {
		return errors.New("output.dir is required")
	}
	if c.Output.FilePattern == "" {
		return errors.New("output.file_pattern is required")
	}
	return nil
}

// OutputPath interpolates the file pattern with the given time and joins
// it with Output.Dir. Supports {YYYY}, {MM}, {DD} placeholders.
func (c Config) OutputPath(now time.Time) string {
	name := c.Output.FilePattern
	name = strings.ReplaceAll(name, "{YYYY}", fmt.Sprintf("%04d", now.Year()))
	name = strings.ReplaceAll(name, "{MM}", fmt.Sprintf("%02d", int(now.Month())))
	name = strings.ReplaceAll(name, "{DD}", fmt.Sprintf("%02d", now.Day()))
	return filepath.Join(c.Output.Dir, name)
}
