package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newStatusPagesCmd(opts *globalOpts, version string) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "status-pages",
		Aliases: []string{"status-page"},
		Short:   "Query and manage status pages, maintenance, and incidents",
	}

	// --- Status page CRUD ---

	cmd.AddCommand(&cobra.Command{
		Use:     "list",
		Short:   "List all status pages",
		Example: `  pulsetic-cli status-pages list`,
		RunE: func(c *cobra.Command, args []string) error {
			rc, err := opts.prepare(version, c.OutOrStdout())
			if err != nil {
				return err
			}
			defer rc.close()
			_, err = listAllStatusPages(c.Context(), rc, "status_pages.list")
			return err
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:     "get <id>",
		Short:   "Record incidents for a status page (detail is included in list)",
		Example: `  pulsetic-cli status-pages get 13`,
		Args:  cobra.ExactArgs(1),
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
			return captureStatusPage(c.Context(), rc, id, "status_pages.get")
		},
	})

	var createData string
	createCmd := &cobra.Command{
		Use:   "create",
		Short: "Create a status page",
		Example: `  pulsetic-cli status-pages create --data '{"title":"My Page","monitors":[1,2]}'`,
		Long:  `Pass JSON via --data, e.g. --data '{"title":"My Status Page","monitors":[1,2]}'`,
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
			_, err = rc.callAndRecordWithBody(c.Context(), "status_pages.create", "POST", "/status-page", nil, body)
			return err
		},
	}
	createCmd.Flags().StringVar(&createData, "data", "", `JSON body (or - for stdin)`)
	cmd.AddCommand(createCmd)

	var updateData string
	updateCmd := &cobra.Command{
		Use:     "update <id>",
		Short:   "Update a status page",
		Example: `  pulsetic-cli status-pages update 13 --data '{"title":"Updated Page"}'`,
		Args:  cobra.ExactArgs(1),
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
			path := fmt.Sprintf("/status-page/%d", id)
			_, err = rc.callAndRecordWithBody(c.Context(), "status_pages.update", "PUT", path, nil, body)
			return err
		},
	}
	updateCmd.Flags().StringVar(&updateData, "data", "", `JSON body (or - for stdin)`)
	cmd.AddCommand(updateCmd)

	cmd.AddCommand(&cobra.Command{
		Use:     "delete <id>",
		Short:   "Delete a status page",
		Example: `  pulsetic-cli status-pages delete 13`,
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
			path := fmt.Sprintf("/status-page/%d", id)
			_, err = rc.callAndRecordWithBody(c.Context(), "status_pages.delete", "DELETE", path, nil, nil)
			return err
		},
	})

	// --- Maintenance ---

	maintenanceCmd := &cobra.Command{
		Use:   "maintenance",
		Short: "Manage scheduled maintenance windows on a status page",
	}

	var maintCreateData string
	maintCreateCmd := &cobra.Command{
		Use:     "create <status-page-id>",
		Short:   "Create a maintenance window",
		Example: `  pulsetic-cli status-pages maintenance create 13 --data '{"name":"Upgrade","monitors":[1]}'`,
		Args:    cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			id, err := parseID(args[0])
			if err != nil {
				return err
			}
			body, err := readJSONInput(maintCreateData, true)
			if err != nil {
				return err
			}
			rc, err := opts.prepare(version, c.OutOrStdout())
			if err != nil {
				return err
			}
			defer rc.close()
			path := fmt.Sprintf("/status-page/%d/maintenance", id)
			_, err = rc.callAndRecordWithBody(c.Context(), "maintenance.create", "POST", path, nil, body)
			return err
		},
	}
	maintCreateCmd.Flags().StringVar(&maintCreateData, "data", "", `JSON body (or - for stdin)`)
	maintenanceCmd.AddCommand(maintCreateCmd)

	var maintUpdateData string
	maintUpdateCmd := &cobra.Command{
		Use:     "update <maintenance-id>",
		Short:   "Update a maintenance window",
		Example: `  pulsetic-cli status-pages maintenance update 8 --data '{"name":"Extended Upgrade"}'`,
		Args:    cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			id, err := parseID(args[0])
			if err != nil {
				return err
			}
			body, err := readJSONInput(maintUpdateData, true)
			if err != nil {
				return err
			}
			rc, err := opts.prepare(version, c.OutOrStdout())
			if err != nil {
				return err
			}
			defer rc.close()
			path := fmt.Sprintf("/status-page/maintenance/%d", id)
			_, err = rc.callAndRecordWithBody(c.Context(), "maintenance.update", "PUT", path, nil, body)
			return err
		},
	}
	maintUpdateCmd.Flags().StringVar(&maintUpdateData, "data", "", `JSON body (or - for stdin)`)
	maintenanceCmd.AddCommand(maintUpdateCmd)

	maintenanceCmd.AddCommand(&cobra.Command{
		Use:     "delete <maintenance-id>",
		Short:   "Delete a maintenance window",
		Example: `  pulsetic-cli status-pages maintenance delete 8`,
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
			path := fmt.Sprintf("/status-page/maintenance/%d", id)
			_, err = rc.callAndRecordWithBody(c.Context(), "maintenance.delete", "DELETE", path, nil, nil)
			return err
		},
	})
	cmd.AddCommand(maintenanceCmd)

	// --- Incidents ---

	incCmd := &cobra.Command{
		Use:   "incidents",
		Short: "Manage incidents on a status page",
	}

	incCmd.AddCommand(&cobra.Command{
		Use:     "list <status-page-id>",
		Short:   "List incidents for a status page",
		Example: `  pulsetic-cli status-pages incidents list 13`,
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
			path := fmt.Sprintf("/status-page/%d/incidents", id)
			_, err = rc.callAndRecord(c.Context(), "status_pages.incidents.list", "GET", path, nil)
			return err
		},
	})

	var incCreateData string
	incCreateCmd := &cobra.Command{
		Use:   "create <status-page-id>",
		Short: "Create an incident on a status page",
		Example: `  pulsetic-cli status-pages incidents create 13 --data '{"title":"Outage","update":{"status":"exploring"}}'`,
		Long:  `Pass JSON via --data, e.g. --data '{"title":"Outage","update":{"status":"exploring"}}'`,
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			id, err := parseID(args[0])
			if err != nil {
				return err
			}
			body, err := readJSONInput(incCreateData, true)
			if err != nil {
				return err
			}
			rc, err := opts.prepare(version, c.OutOrStdout())
			if err != nil {
				return err
			}
			defer rc.close()
			path := fmt.Sprintf("/status-page/%d/incidents", id)
			_, err = rc.callAndRecordWithBody(c.Context(), "incidents.create", "POST", path, nil, body)
			return err
		},
	}
	incCreateCmd.Flags().StringVar(&incCreateData, "data", "", `JSON body (or - for stdin)`)
	incCmd.AddCommand(incCreateCmd)

	var incUpdateData string
	incUpdateCmd := &cobra.Command{
		Use:     "update <incident-id>",
		Short:   "Update an incident",
		Example: `  pulsetic-cli status-pages incidents update 30 --data '{"title":"Resolved"}'`,
		Args:    cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			id, err := parseID(args[0])
			if err != nil {
				return err
			}
			body, err := readJSONInput(incUpdateData, true)
			if err != nil {
				return err
			}
			rc, err := opts.prepare(version, c.OutOrStdout())
			if err != nil {
				return err
			}
			defer rc.close()
			path := fmt.Sprintf("/status-page/incidents/%d", id)
			_, err = rc.callAndRecordWithBody(c.Context(), "incidents.update", "PUT", path, nil, body)
			return err
		},
	}
	incUpdateCmd.Flags().StringVar(&incUpdateData, "data", "", `JSON body (or - for stdin)`)
	incCmd.AddCommand(incUpdateCmd)

	incCmd.AddCommand(&cobra.Command{
		Use:     "delete <incident-id>",
		Short:   "Delete an incident",
		Example: `  pulsetic-cli status-pages incidents delete 30`,
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
			path := fmt.Sprintf("/status-page/incidents/%d", id)
			_, err = rc.callAndRecordWithBody(c.Context(), "incidents.delete", "DELETE", path, nil, nil)
			return err
		},
	})

	// --- Incident updates (sub-resource of incidents) ---

	incUpdateSubCmd := &cobra.Command{
		Use:   "add-update <incident-id>",
		Short: "Add a status update to an incident",
		Example: `  pulsetic-cli status-pages incidents add-update 29 --data '{"status":"identified","message":"Root cause found"}'`,
		Long:  `Pass JSON via --data, e.g. --data '{"status":"identified","message":"Root cause found"}'`,
		Args:  cobra.ExactArgs(1),
	}
	var addUpdateData string
	incUpdateSubCmd.RunE = func(c *cobra.Command, args []string) error {
		id, err := parseID(args[0])
		if err != nil {
			return err
		}
		body, err := readJSONInput(addUpdateData, true)
		if err != nil {
			return err
		}
		rc, err := opts.prepare(version, c.OutOrStdout())
		if err != nil {
			return err
		}
		defer rc.close()
		path := fmt.Sprintf("/incidents/%d/incident-update", id)
		_, err = rc.callAndRecordWithBody(c.Context(), "incidents.add_update", "POST", path, nil, body)
		return err
	}
	incUpdateSubCmd.Flags().StringVar(&addUpdateData, "data", "", `JSON body (or - for stdin)`)
	incCmd.AddCommand(incUpdateSubCmd)

	var editUpdateData string
	editUpdateCmd := &cobra.Command{
		Use:     "edit-update <update-id>",
		Short:   "Edit an existing incident update",
		Example: `  pulsetic-cli status-pages incidents edit-update 57 --data '{"status":"resolved"}'`,
		Args:    cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			id, err := parseID(args[0])
			if err != nil {
				return err
			}
			body, err := readJSONInput(editUpdateData, true)
			if err != nil {
				return err
			}
			rc, err := opts.prepare(version, c.OutOrStdout())
			if err != nil {
				return err
			}
			defer rc.close()
			path := fmt.Sprintf("/incidents/updates/%d", id)
			_, err = rc.callAndRecordWithBody(c.Context(), "incidents.edit_update", "PUT", path, nil, body)
			return err
		},
	}
	editUpdateCmd.Flags().StringVar(&editUpdateData, "data", "", `JSON body (or - for stdin)`)
	incCmd.AddCommand(editUpdateCmd)

	incCmd.AddCommand(&cobra.Command{
		Use:     "delete-update <update-id>",
		Short:   "Delete an incident update",
		Example: `  pulsetic-cli status-pages incidents delete-update 57`,
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
			path := fmt.Sprintf("/incidents/updates/%d", id)
			_, err = rc.callAndRecordWithBody(c.Context(), "incidents.delete_update", "DELETE", path, nil, nil)
			return err
		},
	})

	cmd.AddCommand(incCmd)

	return cmd
}
