package cli

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/program-the-brain-not-the-heartbeat/pulsetic-cli/internal/pulsetic"
)

// snapshotPerPage is the page size used for paginated index calls.
// Pulsetic defaults to 20; 100 means fewer round trips and less rate-
// limit pressure.
const snapshotPerPage = 100

// maxPages caps pagination loops to prevent infinite iteration if the
// API keeps returning full pages. 1000 pages * 100 items = 100k items.
const maxPages = 1000

func newSnapshotCmd(opts *globalOpts, version string) *cobra.Command {
	return &cobra.Command{
		Use:   "snapshot",
		Short: "Capture a full audit snapshot across monitors, incidents, and status pages",
		Long: "Walks all monitors, status pages, domains, and heartbeats, recording each\n" +
			"API response to the audit log. Per-monitor data includes snapshots, events,\n" +
			"downtime, stats, and notification channels. Designed for cron/systemd scheduling.",
		Example: `  # Default: capture the last 24 hours
  pulsetic-cli snapshot

  # Custom time range
  pulsetic-cli snapshot --since=7d
  pulsetic-cli snapshot --since=2026-04-01T00:00:00Z --until=2026-04-07T00:00:00Z

  # Preview what would be captured without writing audit records
  pulsetic-cli snapshot --dry-run

  # Write to a specific directory
  pulsetic-cli snapshot --output=/var/audit/pulsetic`,
		RunE: func(c *cobra.Command, args []string) error {
			rc, err := opts.prepare(version, c.OutOrStdout())
			if err != nil {
				return err
			}
			defer rc.close()
			return runSnapshot(c.Context(), rc)
		},
	}
}

func runSnapshot(ctx context.Context, rc *runCtx) error {
	// 1. Monitors + per-monitor history.
	monitorIDs, err := listAllMonitors(ctx, rc, "snapshot.monitors.list")
	if err != nil {
		return fmt.Errorf("list monitors: %w", err)
	}
	for _, id := range monitorIDs {
		if err := captureMonitorHistory(ctx, rc, id, "snapshot.monitors"); err != nil {
			return fmt.Errorf("monitor %d: %w", id, err)
		}
	}

	// 2. Status pages + incidents.
	pageIDs, err := listAllStatusPages(ctx, rc, "snapshot.status_pages.list")
	if err != nil {
		return fmt.Errorf("list status pages: %w", err)
	}
	for _, id := range pageIDs {
		if err := captureStatusPage(ctx, rc, id, "snapshot.status_pages"); err != nil {
			return fmt.Errorf("status page %d: %w", id, err)
		}
	}

	// 3. Domains (SSL/expiry monitoring).
	if _, err := rc.callAndRecord(ctx, "snapshot.domains.list", "GET", "/domains", nil); err != nil {
		return fmt.Errorf("list domains: %w", err)
	}

	// 4. Heartbeats (cron/job monitoring).
	if _, err := rc.callAndRecord(ctx, "snapshot.heartbeats.list", "GET", "/heartbeats", nil); err != nil {
		return fmt.Errorf("list heartbeats: %w", err)
	}

	return nil
}

// listAllMonitors walks the /monitors index one page at a time, writing
// one audit record per page, and returns the accumulated IDs.
func listAllMonitors(ctx context.Context, rc *runCtx, command string) ([]int64, error) {
	return paginateAndCollect(ctx, rc, command, "/monitors", pulsetic.ExtractMonitorIDs)
}

func listAllStatusPages(ctx context.Context, rc *runCtx, command string) ([]int64, error) {
	return paginateAndCollect(ctx, rc, command, "/status-page", pulsetic.ExtractStatusPageIDs)
}

// paginateAndCollect walks a paginated index endpoint. It stops when a
// page returns fewer than snapshotPerPage items or zero items. Every
// page produces its own audit record.
func paginateAndCollect(
	ctx context.Context,
	rc *runCtx,
	command string,
	path string,
	extract func([]byte) ([]int64, error),
) ([]int64, error) {
	var ids []int64
	for page := 1; page <= maxPages; page++ {
		q := map[string]string{
			"page":     strconv.Itoa(page),
			"per_page": strconv.Itoa(snapshotPerPage),
		}
		call, err := rc.callAndRecord(ctx, command, "GET", path, q)
		if err != nil {
			return nil, err
		}
		if call.Status >= 400 {
			return nil, fmt.Errorf("%s returned status %d", path, call.Status)
		}
		pageIDs, err := extract(call.Body)
		if err != nil {
			return nil, err
		}
		ids = append(ids, pageIDs...)
		if len(pageIDs) < snapshotPerPage {
			return ids, nil
		}
	}
	fmt.Fprintf(os.Stderr, "warning: %s pagination hit %d page limit\n", path, maxPages)
	return ids, nil
}

// captureMonitorHistory records per-monitor data: snapshots, events,
// downtime, stats, and notification channels. Individual checks are
// excluded from the default snapshot (too chatty).
func captureMonitorHistory(ctx context.Context, rc *runCtx, monitorID int64, commandPrefix string) error {
	timeRange := map[string]string{
		"start_dt": formatPulseticTime(rc.start),
		"end_dt":   formatPulseticTime(rc.end),
	}

	// Snapshots are paginated (default 100/page) and need walking.
	snapshotPath := fmt.Sprintf("/monitors/%d/snapshots", monitorID)
	for page := 1; page <= maxPages; page++ {
		q := map[string]string{
			"start_dt": timeRange["start_dt"],
			"end_dt":   timeRange["end_dt"],
			"page":     strconv.Itoa(page),
			"per_page": strconv.Itoa(snapshotPerPage),
		}
		call, err := rc.callAndRecord(ctx, commandPrefix+".snapshots", "GET", snapshotPath, q)
		if err != nil {
			return err
		}
		if call.Status >= 400 {
			fmt.Fprintf(os.Stderr, "warning: snapshots for monitor %d returned status %d\n", monitorID, call.Status)
			break
		}
		items, extractErr := pulsetic.ExtractArray(call.Body)
		if extractErr != nil {
			fmt.Fprintf(os.Stderr, "warning: could not parse snapshots page %d for monitor %d: %v\n", page, monitorID, extractErr)
			break
		}
		if len(items) < snapshotPerPage {
			break
		}
	}

	// Events use start_dt/end_dt (single call - typically small result set).
	eventsPath := fmt.Sprintf("/monitors/%d/events", monitorID)
	if _, err := rc.callAndRecord(ctx, commandPrefix+".events", "GET", eventsPath, timeRange); err != nil {
		return err
	}

	// Downtime uses ?seconds=N (seconds backward from now), not start_dt/end_dt.
	downtimeSeconds := int(rc.end.Sub(rc.start).Seconds())
	if downtimeSeconds < 1 {
		downtimeSeconds = 3600
	}
	downtimePath := fmt.Sprintf("/monitors/%d/downtime", monitorID)
	downtimeQ := map[string]string{"seconds": strconv.Itoa(downtimeSeconds)}
	if _, err := rc.callAndRecord(ctx, commandPrefix+".downtime", "GET", downtimePath, downtimeQ); err != nil {
		return err
	}

	// Stats gives pre-computed 1d/7d/30d/90d uptime and response time - no params.
	statsPath := fmt.Sprintf("/monitors/%d/stats", monitorID)
	if _, err := rc.callAndRecord(ctx, commandPrefix+".stats", "GET", statsPath, nil); err != nil {
		return err
	}

	// Notification channels prove who was configured to receive alerts.
	notifsPath := fmt.Sprintf("/monitors/%d/notification-channels", monitorID)
	if _, err := rc.callAndRecord(ctx, commandPrefix+".notification_channels", "GET", notifsPath, nil); err != nil {
		return err
	}

	return nil
}

// captureStatusPage records the incidents for a status page. The API does
// not support GET on /status-page/{id} (returns 405) - full details are
// already included in the paginated list response.
func captureStatusPage(ctx context.Context, rc *runCtx, pageID int64, commandPrefix string) error {
	incidentsPath := fmt.Sprintf("/status-page/%d/incidents", pageID)
	if _, err := rc.callAndRecord(ctx, commandPrefix+".incidents", "GET", incidentsPath, nil); err != nil {
		return err
	}
	return nil
}
