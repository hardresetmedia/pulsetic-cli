package cli

import (
	"context"
	"fmt"
	"strconv"

	"github.com/spf13/cobra"
)

func newMonitorsCmd(opts *globalOpts, version string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "monitors",
		Short: "Query and record monitor data",
	}

	cmd.AddCommand(&cobra.Command{
		Use:     "list",
		Short:   "List all monitors (paginated)",
		Example: `  pulsetic-cli monitors list`,
		RunE: func(c *cobra.Command, args []string) error {
			rc, err := opts.prepare(version, c.OutOrStdout())
			if err != nil {
				return err
			}
			defer rc.close()
			_, err = listAllMonitors(c.Context(), rc, "monitors.list")
			return err
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:     "get <id>",
		Short:   "Record a single monitor's configuration",
		Example: `  pulsetic-cli monitors get 4172`,
		Args:    cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			id, err := parseID(args[0])
			if err != nil {
				return err
			}
			rc, err := opts.prepare(version, c.OutOrStdout())
			if err != nil {
				return err
			}
			defer rc.close()
			path := fmt.Sprintf("/monitors/%d", id)
			_, err = rc.callAndRecord(c.Context(), "monitors.get", "GET", path, nil)
			return err
		},
	})

	var createData string
	createCmd := &cobra.Command{
		Use:   "create",
		Short: "Create one or more monitors",
		Long:  `Pass JSON via --data, e.g. --data '{"urls":["https://example.com"],"add_account_email":true}'`,
		Example: `  pulsetic-cli monitors create --data '{"urls":["https://example.com"],"add_account_email":true}'
  echo '{"urls":["https://example.com"]}' | pulsetic-cli monitors create --data -`,
		RunE: func(c *cobra.Command, args []string) error {
			body, err := readJSONInput(createData, true)
			if err != nil {
				return err
			}
			rc, err := opts.prepare(version, c.OutOrStdout())
			if err != nil {
				return err
			}
			defer rc.close()
			_, err = rc.callAndRecordWithBody(c.Context(), "monitors.create", "POST", "/monitors", nil, body)
			return err
		},
	}
	createCmd.Flags().StringVar(&createData, "data", "", `JSON body (or - for stdin)`)
	cmd.AddCommand(createCmd)

	var updateData string
	updateCmd := &cobra.Command{
		Use:     "update <id>",
		Short:   "Update a monitor's configuration",
		Example: `  pulsetic-cli monitors update 4172 --data '{"name":"Production","uptime_check_frequency":"300"}'`,
		Args:    cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			id, err := parseID(args[0])
			if err != nil {
				return err
			}
			body, err := readJSONInput(updateData, true)
			if err != nil {
				return err
			}
			rc, err := opts.prepare(version, c.OutOrStdout())
			if err != nil {
				return err
			}
			defer rc.close()
			path := fmt.Sprintf("/monitors/%d", id)
			_, err = rc.callAndRecordWithBody(c.Context(), "monitors.update", "PUT", path, nil, body)
			return err
		},
	}
	updateCmd.Flags().StringVar(&updateData, "data", "", `JSON body (or - for stdin)`)
	cmd.AddCommand(updateCmd)

	cmd.AddCommand(&cobra.Command{
		Use:     "delete <id>",
		Short:   "Delete a monitor",
		Example: `  pulsetic-cli monitors delete 4172`,
		Args:    cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			id, err := parseID(args[0])
			if err != nil {
				return err
			}
			rc, err := opts.prepare(version, c.OutOrStdout())
			if err != nil {
				return err
			}
			defer rc.close()
			path := fmt.Sprintf("/monitors/%d", id)
			_, err = rc.callAndRecordWithBody(c.Context(), "monitors.delete", "DELETE", path, nil, nil)
			return err
		},
	})

	var withChecks bool
	historyCmd := &cobra.Command{
		Use:   "history <id>",
		Short: "Record uptime history (snapshots, events, downtime) for one monitor",
		Example: `  pulsetic-cli monitors history 4172 --since=7d
  pulsetic-cli monitors history 4172 --since=30d --include-checks`,
		Args: cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			id, err := parseID(args[0])
			if err != nil {
				return err
			}
			rc, err := opts.prepare(version, c.OutOrStdout())
			if err != nil {
				return err
			}
			defer rc.close()
			return captureMonitorHistoryInteractive(c.Context(), rc, id, withChecks)
		},
	}
	historyCmd.Flags().BoolVar(&withChecks, "include-checks", false, "also record individual checks (can be very large)")
	cmd.AddCommand(historyCmd)

	return cmd
}

// captureMonitorHistoryInteractive wraps captureMonitorHistory and
// optionally adds the /checks endpoint (excluded from snapshot by
// default due to its size).
func captureMonitorHistoryInteractive(ctx context.Context, rc *runCtx, monitorID int64, includeChecks bool) error {
	if err := captureMonitorHistory(ctx, rc, monitorID, "monitors.history"); err != nil {
		return err
	}
	if !includeChecks {
		return nil
	}
	q := map[string]string{
		"start_dt": formatPulseticTime(rc.start),
		"end_dt":   formatPulseticTime(rc.end),
	}
	path := fmt.Sprintf("/monitors/%d/checks", monitorID)
	_, err := rc.callAndRecord(ctx, "monitors.history.checks", "GET", path, q)
	return err
}

func parseID(s string) (int64, error) {
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("invalid id %q: must be a positive integer (e.g. 4172)", s)
	}
	return id, nil
}
