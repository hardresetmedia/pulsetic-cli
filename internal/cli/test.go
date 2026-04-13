package cli

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/program-the-brain-not-the-heartbeat/pulsetic-cli/internal/config"
	"github.com/program-the-brain-not-the-heartbeat/pulsetic-cli/internal/pulsetic"
)

func newTestCmd(opts *globalOpts, version string) *cobra.Command {
	return &cobra.Command{
		Use:   "test",
		Short: "Validate your API token by making a lightweight API call",
		Example: `  pulsetic-cli test
  pulsetic-cli test --json`,
		RunE: func(c *cobra.Command, args []string) error {
			cfg, err := config.Load(opts.configPath)
			if err != nil {
				return err
			}
			if err := cfg.Validate(); err != nil {
				return err
			}

			client, err := pulsetic.New(pulsetic.Options{
				BaseURL:   cfg.Client.BaseURL,
				Token:     cfg.Token,
				Timeout:   cfg.Client.Timeout.Std(),
				RetryMax:  cfg.Client.RetryMax,
				RetryBase: cfg.Client.RetryBackoff.Std(),
				UserAgent: "pulsetic-cli/" + version,
			})
			if err != nil {
				return err
			}

			ctx := c.Context()
			out := c.OutOrStdout()

			// Hit monitors with per_page=1 to validate token cheaply.
			mCall, err := client.Do(ctx, "GET", "/monitors", map[string]string{"per_page": "1", "page": "1"}, nil)
			if err != nil {
				return fmt.Errorf("API unreachable: %w", err)
			}
			if mCall.Status == 401 || mCall.Status == 403 {
				msg := fmt.Sprintf("token FAILED: HTTP %d", mCall.Status)
				if opts.jsonOutput || opts.format == formatJSON {
					enc := json.NewEncoder(out)
					enc.SetEscapeHTML(false)
					return enc.Encode(map[string]any{"ok": false, "error": msg})
				}
				fmt.Fprintln(out, msg)
				return fmt.Errorf("%s", msg)
			}

			monitorCount := extractTotal(mCall.Body)

			// Also check status pages.
			spCall, err := client.Do(ctx, "GET", "/status-page", map[string]string{"per_page": "1", "page": "1"}, nil)
			if err != nil {
				return err
			}
			statusPageCount := extractTotal(spCall.Body)

			if opts.jsonOutput || opts.format == formatJSON {
				enc := json.NewEncoder(out)
				enc.SetEscapeHTML(false)
				return enc.Encode(map[string]any{
					"ok":           true,
					"monitors":     monitorCount,
					"status_pages": statusPageCount,
				})
			}

			fmt.Fprintf(out, "token OK: %d monitors, %d status pages\n", monitorCount, statusPageCount)
			return nil
		},
	}
}

// extractTotal tries to pull the total count from a paginated response.
// Pulsetic responses include "total" in meta or at the top level.
func extractTotal(body []byte) int {
	var env struct {
		Total int `json:"total"`
		Meta  struct {
			Total int `json:"total"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return -1
	}
	if env.Meta.Total > 0 {
		return env.Meta.Total
	}
	if env.Total > 0 {
		return env.Total
	}
	// Fallback: count data array length (only shows current page).
	items, _ := pulsetic.ExtractArray(body)
	return len(items)
}
