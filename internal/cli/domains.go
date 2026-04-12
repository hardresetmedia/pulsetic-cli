package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newDomainsCmd(opts *globalOpts, version string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "domains",
		Short: "Query and manage domain (SSL/expiry) monitors",
	}

	cmd.AddCommand(&cobra.Command{
		Use:     "list",
		Short:   "List all domains",
		Example: `  pulsetic-cli domains list`,
		RunE: func(c *cobra.Command, args []string) error {
			rc, err := opts.prepare(version, c.OutOrStdout())
			if err != nil {
				return err
			}
			defer rc.close()
			_, err = rc.callAndRecord(c.Context(), "domains.list", "GET", "/domains", nil)
			return err
		},
	})

	var createData string
	createCmd := &cobra.Command{
		Use:   "create",
		Short: "Add domains for SSL/expiry monitoring",
		Long:  `Pass JSON via --data, e.g. --data '{"domains":["example.com","example2.com"]}'`,
		Example: `  pulsetic-cli domains create --data '{"domains":["example.com","api.example.com"]}'`,
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
			_, err = rc.callAndRecordWithBody(c.Context(), "domains.create", "POST", "/domains", nil, body)
			return err
		},
	}
	createCmd.Flags().StringVar(&createData, "data", "", `JSON body (or - for stdin)`)
	cmd.AddCommand(createCmd)

	var updateData string
	updateCmd := &cobra.Command{
		Use:     "update <id>",
		Short:   "Update a domain's settings",
		Example: `  pulsetic-cli domains update 1 --data '{"alias":"Production","is_active":true}'`,
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
			path := fmt.Sprintf("/domains/%d", id)
			_, err = rc.callAndRecordWithBody(c.Context(), "domains.update", "PUT", path, nil, body)
			return err
		},
	}
	updateCmd.Flags().StringVar(&updateData, "data", "", `JSON body (or - for stdin)`)
	cmd.AddCommand(updateCmd)

	cmd.AddCommand(&cobra.Command{
		Use:     "delete <id>",
		Short:   "Delete a domain",
		Example: `  pulsetic-cli domains delete 1`,
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
			path := fmt.Sprintf("/domains/%d", id)
			_, err = rc.callAndRecordWithBody(c.Context(), "domains.delete", "DELETE", path, nil, nil)
			return err
		},
	})

	return cmd
}
