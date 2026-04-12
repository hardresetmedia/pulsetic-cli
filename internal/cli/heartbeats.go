package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newHeartbeatsCmd(opts *globalOpts, version string) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "heartbeats",
		Aliases: []string{"heartbeat"},
		Short:   "Query and manage heartbeat (cron/job) monitors",
	}

	cmd.AddCommand(&cobra.Command{
		Use:     "list",
		Short:   "List all heartbeats",
		Example: `  pulsetic-cli heartbeats list`,
		RunE: func(c *cobra.Command, args []string) error {
			rc, err := opts.prepare(version, c.OutOrStdout())
			if err != nil {
				return err
			}
			defer rc.close()
			_, err = rc.callAndRecord(c.Context(), "heartbeats.list", "GET", "/heartbeats", nil)
			return err
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:     "get <id>",
		Short:   "Get a specific heartbeat",
		Example: `  pulsetic-cli heartbeats get 1`,
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
			path := fmt.Sprintf("/heartbeats/%d", id)
			_, err = rc.callAndRecord(c.Context(), "heartbeats.get", "GET", path, nil)
			return err
		},
	})

	var createData string
	createCmd := &cobra.Command{
		Use:   "create",
		Short: "Create a heartbeat monitor",
		Long:  `Pass JSON via --data, e.g. --data '{"name":"My Heartbeat","monitoring_interval":60,"grace_period":180}'`,
		Example: `  pulsetic-cli heartbeats create --data '{"name":"Nightly Backup","monitoring_interval":86400,"grace_period":3600}'`,
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
			_, err = rc.callAndRecordWithBody(c.Context(), "heartbeats.create", "POST", "/heartbeats", nil, body)
			return err
		},
	}
	createCmd.Flags().StringVar(&createData, "data", "", `JSON body (or - for stdin)`)
	cmd.AddCommand(createCmd)

	var updateData string
	updateCmd := &cobra.Command{
		Use:     "update <id>",
		Short:   "Update a heartbeat monitor",
		Example: `  pulsetic-cli heartbeats update 1 --data '{"name":"Updated Beat","grace_period":7200}'`,
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
			path := fmt.Sprintf("/heartbeats/%d", id)
			_, err = rc.callAndRecordWithBody(c.Context(), "heartbeats.update", "PUT", path, nil, body)
			return err
		},
	}
	updateCmd.Flags().StringVar(&updateData, "data", "", `JSON body (or - for stdin)`)
	cmd.AddCommand(updateCmd)

	cmd.AddCommand(&cobra.Command{
		Use:     "delete <id>",
		Short:   "Delete a heartbeat monitor",
		Example: `  pulsetic-cli heartbeats delete 1`,
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
			path := fmt.Sprintf("/heartbeats/%d", id)
			_, err = rc.callAndRecordWithBody(c.Context(), "heartbeats.delete", "DELETE", path, nil, nil)
			return err
		},
	})

	return cmd
}
