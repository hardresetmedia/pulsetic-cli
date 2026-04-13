package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/program-the-brain-not-the-heartbeat/pulsetic-cli/internal/config"
)

func newInitCmd(opts *globalOpts) *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Create a default config file",
		Long: "Writes a config file with sensible defaults to the standard location.\n" +
			"Edit it afterwards to customize output directory, time ranges, and client settings.\n" +
			"The API token is always read from the PULSETIC_API_TOKEN environment variable.",
		Example: `  pulsetic-cli init
  pulsetic-cli init --force`,
		RunE: func(c *cobra.Command, args []string) error {
			path := opts.configPath
			if path == "" {
				resolved, err := config.ResolveConfigPath()
				if err != nil {
					return err
				}
				if resolved == "" {
					return fmt.Errorf("could not determine config path (HOME not set)")
				}
				path = resolved
			}

			if !force {
				if _, err := os.Stat(path); err == nil {
					return fmt.Errorf("config file already exists: %s\n  Use --force to overwrite", path)
				}
			}

			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return fmt.Errorf("create config directory: %w", err)
			}

			content := `# pulsetic-cli configuration
# API token comes from PULSETIC_API_TOKEN env var only. Never stored here.

[output]
dir = "./audit"
file_pattern = "pulsetic-{YYYY}-{MM}.jsonl"

[defaults]
since = "24h"

[client]
timeout = "30s"
retry_max = 3
retry_backoff = "500ms"
`
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				return fmt.Errorf("write config: %w", err)
			}

			fmt.Fprintf(c.OutOrStdout(), "Config written to %s\n", path)
			fmt.Fprintf(c.OutOrStdout(), "Next steps:\n")
			fmt.Fprintf(c.OutOrStdout(), "  1. Edit the file to customize settings\n")
			fmt.Fprintf(c.OutOrStdout(), "  2. export PULSETIC_API_TOKEN=your_token\n")
			fmt.Fprintf(c.OutOrStdout(), "  3. pulsetic-cli test\n")
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "overwrite existing config file")
	return cmd
}
