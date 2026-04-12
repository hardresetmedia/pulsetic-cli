package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

func newIncidentsCmd(opts *globalOpts, version string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "incidents",
		Short: "Query and record incident data",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "Record incidents across all status pages",
		Example: `  pulsetic-cli incidents list`,
		Long: "Pulsetic's incidents are scoped to a status page, so this command " +
			"first lists every status page and then records the incident list for each. " +
			"The resulting audit records give a complete view of incidents at this moment.",
		RunE: func(c *cobra.Command, args []string) error {
			rc, err := opts.prepare(version, c.OutOrStdout())
			if err != nil {
				return err
			}
			defer rc.close()
			return runIncidentsList(c.Context(), rc)
		},
	})

	return cmd
}

func runIncidentsList(ctx context.Context, rc *runCtx) error {
	pageIDs, err := listAllStatusPages(ctx, rc, "incidents.status_pages.list")
	if err != nil {
		return err
	}
	for _, id := range pageIDs {
		path := fmt.Sprintf("/status-page/%d/incidents", id)
		if _, err := rc.callAndRecord(ctx, "incidents.list", "GET", path, nil); err != nil {
			return err
		}
	}
	return nil
}
