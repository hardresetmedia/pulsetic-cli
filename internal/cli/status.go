package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/program-the-brain-not-the-heartbeat/pulsetic-cli/internal/config"
	"github.com/program-the-brain-not-the-heartbeat/pulsetic-cli/internal/pulsetic"
)

// monitorStatus holds the fields we display in the status table.
type monitorStatus struct {
	ID           int64   `json:"id"`
	URL          string  `json:"url"`
	Name         string  `json:"name"`
	Status       string  `json:"status"`
	ResponseTime float64 `json:"response_time"`
	Uptime30d    float64 `json:"uptime_30d"`
}

func newStatusCmd(opts *globalOpts, version string) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show a live overview of all monitors with status, response time, and uptime",
		Example: `  pulsetic-cli status
  pulsetic-cli status --filter=offline
  pulsetic-cli status --filter=novastream
  pulsetic-cli status --format=csv`,
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

			out := c.OutOrStdout()
			ctx := c.Context()

			// Collect all monitors across pages.
			var allItems []json.RawMessage
			for page := 1; page <= maxPages; page++ {
				q := map[string]string{
					"page":     fmt.Sprintf("%d", page),
					"per_page": fmt.Sprintf("%d", snapshotPerPage),
				}
				call, err := client.Do(ctx, "GET", "/monitors", q, nil)
				if err != nil {
					return err
				}
				if call.Status >= 400 {
					return fmt.Errorf("monitors returned HTTP %d", call.Status)
				}
				items, _ := pulsetic.ExtractArray(call.Body)
				allItems = append(allItems, items...)
				if len(items) < snapshotPerPage {
					break
				}
			}

			// Apply --filter.
			allItems = filterItems(allItems, opts.filter)

			// Parse into monitorStatus structs.
			monitors := make([]monitorStatus, 0, len(allItems))
			for _, raw := range allItems {
				var m struct {
					ID           int64   `json:"id"`
					URL          string  `json:"url"`
					Name         *string `json:"name"`
					Status       string  `json:"status"`
					ResponseTime float64 `json:"response_time"`
				}
				if err := json.Unmarshal(raw, &m); err != nil {
					continue
				}
				name := ""
				if m.Name != nil {
					name = *m.Name
				}
				monitors = append(monitors, monitorStatus{
					ID:           m.ID,
					URL:          m.URL,
					Name:         name,
					Status:       m.Status,
					ResponseTime: m.ResponseTime,
				})
			}

			if !opts.quiet {
				fmt.Fprintf(os.Stderr, "%d monitors\n", len(monitors))
			}

			// Output based on --format.
			format := opts.format
			if opts.jsonOutput {
				format = formatJSON
			}

			switch format {
			case formatJSON:
				env := jsonEnvelope{OK: true, Command: "status", Records: len(monitors)}
				for _, m := range monitors {
					b, _ := json.Marshal(m)
					env.Data = append(env.Data, b)
				}
				enc := json.NewEncoder(out)
				enc.SetEscapeHTML(false)
				return enc.Encode(env)

			case formatCSV:
				fmt.Fprintln(out, "id,url,name,status,response_time_ms,uptime_30d")
				for _, m := range monitors {
					fmt.Fprintf(out, "%d,%s,%s,%s,%.0f,%.4f\n",
						m.ID, m.URL, m.Name, m.Status, m.ResponseTime*1000, m.Uptime30d)
				}
				return nil

			case formatJSONL:
				enc := json.NewEncoder(out)
				enc.SetEscapeHTML(false)
				for _, m := range monitors {
					if err := enc.Encode(m); err != nil {
						return err
					}
				}
				return nil

			default: // stdout / table
				tw := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
				fmt.Fprintf(tw, "ID\tURL\tSTATUS\tRESP\n")
				for _, m := range monitors {
					resp := fmt.Sprintf("%.0fms", m.ResponseTime*1000)
					fmt.Fprintf(tw, "%d\t%s\t%s\t%s\n",
						m.ID, m.URL, m.Status, resp)
				}
				return tw.Flush()
			}
		},
	}
}
