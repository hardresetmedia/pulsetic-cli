package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// clearPulseticEnv removes all PULSETIC_* variables for the duration of the
// test so a loaded-from-shell var doesn't leak in.
func clearPulseticEnv(t *testing.T) {
	t.Helper()
	vars := []string{
		"PULSETIC_API_TOKEN",
		"PULSETIC_CLI_OUTPUT_DIR",
		"PULSETIC_CLI_SINCE",
		"PULSETIC_CLI_BASE_URL",
		"XDG_CONFIG_HOME",
	}
	for _, v := range vars {
		t.Setenv(v, "")
		_ = os.Unsetenv(v)
	}
}

func TestDefaultsAreApplied(t *testing.T) {
	clearPulseticEnv(t)
	t.Setenv("PULSETIC_API_TOKEN", "abc")
	t.Setenv("HOME", t.TempDir()) // ensure ResolveConfigPath lands on nothing

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Token != "abc" {
		t.Fatalf("token: want abc, got %q", cfg.Token)
	}
	if cfg.Output.Dir != "./audit" {
		t.Fatalf("output dir: %q", cfg.Output.Dir)
	}
	if cfg.Client.RetryMax != 3 {
		t.Fatalf("retry max: %d", cfg.Client.RetryMax)
	}
}

func TestTOMLFileOverridesDefaults(t *testing.T) {
	clearPulseticEnv(t)
	t.Setenv("PULSETIC_API_TOKEN", "abc")

	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	contents := `
[output]
dir = "/var/log/pulsetic"
file_pattern = "p-{YYYY}.jsonl"

[defaults]
since = "7d"

[client]
timeout = "1m"
retry_max = 5
retry_backoff = "1s"
`
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Output.Dir != "/var/log/pulsetic" {
		t.Fatalf("dir: %q", cfg.Output.Dir)
	}
	if cfg.Output.FilePattern != "p-{YYYY}.jsonl" {
		t.Fatalf("pattern: %q", cfg.Output.FilePattern)
	}
	if cfg.Defaults.Since != "7d" {
		t.Fatalf("since: %q", cfg.Defaults.Since)
	}
	if cfg.Client.RetryMax != 5 {
		t.Fatalf("retry max: %d", cfg.Client.RetryMax)
	}
	if cfg.Client.Timeout.Std() != time.Minute {
		t.Fatalf("timeout: %v", cfg.Client.Timeout.Std())
	}
}

func TestEnvOverridesFile(t *testing.T) {
	clearPulseticEnv(t)
	t.Setenv("PULSETIC_API_TOKEN", "from-env")
	t.Setenv("PULSETIC_CLI_OUTPUT_DIR", "/env/dir")
	t.Setenv("PULSETIC_CLI_SINCE", "1h")

	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	contents := `
[output]
dir = "/file/dir"
file_pattern = "p.jsonl"

[defaults]
since = "24h"
`
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Output.Dir != "/env/dir" {
		t.Fatalf("env did not override file: %q", cfg.Output.Dir)
	}
	if cfg.Defaults.Since != "1h" {
		t.Fatalf("since override: %q", cfg.Defaults.Since)
	}
	if cfg.Token != "from-env" {
		t.Fatalf("token: %q", cfg.Token)
	}
}

func TestValidateRequiresToken(t *testing.T) {
	cfg := Defaults()
	if err := cfg.Validate(); err == nil {
		t.Fatal("want error when token is empty")
	}
	cfg.Token = "present"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestOutputPathInterpolates(t *testing.T) {
	cfg := Defaults()
	cfg.Output.Dir = "/tmp/a"
	cfg.Output.FilePattern = "pulsetic-{YYYY}-{MM}.jsonl"
	got := cfg.OutputPath(time.Date(2026, 4, 11, 0, 0, 0, 0, time.UTC))
	want := "/tmp/a/pulsetic-2026-04.jsonl"
	if got != want {
		t.Fatalf("want %q got %q", want, got)
	}
}
