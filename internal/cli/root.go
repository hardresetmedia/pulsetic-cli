// Package cli wires together the cobra command tree. Each subcommand
// loads config, builds a Pulsetic client, opens the audit writer, and
// records every HTTP call it makes.
package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/program-the-brain-not-the-heartbeat/pulsetic-cli/internal/audit"
	"github.com/program-the-brain-not-the-heartbeat/pulsetic-cli/internal/config"
	"github.com/program-the-brain-not-the-heartbeat/pulsetic-cli/internal/pulsetic"
)

// globalOpts holds the values of persistent flags. A pointer is passed
// to each subcommand constructor so they share the same storage.
type globalOpts struct {
	configPath string
	outputDir  string
	dryRun     bool
	verbose    bool
	since      string
	until      string
}

// NewRootCmd builds the full command tree. version is embedded in the
// Actor field of every audit record.
func NewRootCmd(version string) *cobra.Command {
	opts := &globalOpts{}

	root := &cobra.Command{
		Use:   "pulsetic-cli",
		Short: "Record Pulsetic API responses to an append-only, hash-chained audit log",
		Long: "pulsetic-cli captures the state of your Pulsetic account (monitors, " +
			"uptime history, incidents, status pages) into a local JSONL audit log. " +
			"Each record is SHA-256 chained to the previous one so tampering is detectable.\n\n" +
			"Exit codes:\n" +
			"  0  Success\n" +
			"  1  API or I/O error\n" +
			"  3  Audit chain verification failure (tamper detected)\n\n" +
			"Command aliases:\n" +
			"  status-pages           -> status-page\n" +
			"  heartbeats             -> heartbeat\n" +
			"  notification-channels  -> notifs",
		Example: `  # Set your API token first
  export PULSETIC_API_TOKEN=your_token

  # Capture a full audit snapshot (last 24 hours)
  pulsetic-cli snapshot

  # List all monitors
  pulsetic-cli monitors list --dry-run

  # Get uptime history for a specific monitor
  pulsetic-cli monitors history 4172 --since=7d

  # Verify the audit chain hasn't been tampered with
  pulsetic-cli verify`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.PersistentFlags().StringVar(&opts.configPath, "config", "", "path to TOML config file")
	root.PersistentFlags().StringVar(&opts.outputDir, "output", "", "override output directory for audit records")
	root.PersistentFlags().BoolVar(&opts.dryRun, "dry-run", false, "print API responses to stdout, do not write audit records")
	root.PersistentFlags().BoolVarP(&opts.verbose, "verbose", "v", false, "log each API call to stderr")
	root.PersistentFlags().StringVar(&opts.since, "since", "", "time range start (e.g. 24h, 7d, or RFC3339)")
	root.PersistentFlags().StringVar(&opts.until, "until", "", "time range end (default: now)")

	root.AddCommand(newSnapshotCmd(opts, version))
	root.AddCommand(newMonitorsCmd(opts, version))
	root.AddCommand(newIncidentsCmd(opts, version))
	root.AddCommand(newStatusPagesCmd(opts, version))
	root.AddCommand(newDomainsCmd(opts, version))
	root.AddCommand(newHeartbeatsCmd(opts, version))
	root.AddCommand(newNotificationChannelsCmd(opts, version))
	root.AddCommand(newVerifyCmd(opts))

	return root
}

// runCtx is the per-invocation state passed through each subcommand.
type runCtx struct {
	cfg        config.Config
	client     *pulsetic.Client
	writer     *audit.Writer
	actor      audit.Actor
	dryRun     bool
	dryRunOut  io.Writer // where dry-run prints; defaults to os.Stdout
	verbose    bool
	records    int    // count of records written (for summary)
	outputPath string // resolved audit file path (for summary)
	start      time.Time
	end        time.Time
	now        time.Time
}

// prepare resolves config, builds the client, parses the time range,
// and opens the audit writer (unless --dry-run). dryOut is where
// dry-run output goes (typically cmd.OutOrStdout()). Callers must
// call rc.close() before returning.
func (o *globalOpts) prepare(version string, dryOut io.Writer) (*runCtx, error) {
	cfg, err := config.Load(o.configPath)
	if err != nil {
		return nil, err
	}
	if o.outputDir != "" {
		cfg.Output.Dir = o.outputDir
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
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
		return nil, err
	}

	now := time.Now().UTC()
	sinceExpr := cfg.Defaults.Since
	if o.since != "" {
		sinceExpr = o.since
	}
	start, err := parseRelativeTime(sinceExpr, now)
	if err != nil {
		return nil, fmt.Errorf("--since: %w", err)
	}
	untilExpr := o.until
	if untilExpr == "" {
		untilExpr = "now"
	}
	end, err := parseRelativeTime(untilExpr, now)
	if err != nil {
		return nil, fmt.Errorf("--until: %w", err)
	}
	if end.Before(start) {
		return nil, fmt.Errorf("until (%s) is before since (%s)", end, start)
	}

	host, _ := os.Hostname()
	rc := &runCtx{
		cfg:       cfg,
		client:    client,
		actor:     audit.Actor{Tool: "pulsetic-cli", Version: version, Host: host},
		dryRun:    o.dryRun,
		dryRunOut: dryOut,
		verbose:   o.verbose,
		start:     start,
		end:       end,
		now:       now,
	}

	if !o.dryRun {
		path := cfg.OutputPath(now)
		w, err := audit.OpenWriter(path, rc.actor, nil)
		if err != nil {
			return nil, err
		}
		rc.writer = w
		rc.outputPath = path
	}

	return rc, nil
}

// record writes one Call to the audit log. In --dry-run mode it prints
// the raw response body to the configured dryRunOut writer instead.
func (rc *runCtx) record(command string, call *pulsetic.Call) error {
	if rc.writer == nil {
		out := rc.dryRunOut
		if out == nil {
			out = os.Stdout
		}
		_, err := fmt.Fprintln(out, string(call.Body))
		return err
	}
	// Normalize nil query map to empty so canonical JSON is always "query:{}"
	// rather than sometimes "query:null". Without this, the same logical
	// request (no params) produces different hashes depending on whether the
	// caller passed nil or map[string]string{}.
	q := call.Query
	if q == nil {
		q = map[string]string{}
	}
	req := audit.Request{
		Method: call.Method,
		Path:   call.Path,
		Query:  q,
	}
	// Normalize empty response bodies to JSON null. Some Pulsetic endpoints
	// (e.g. /downtime with no downtime) return HTTP 200 with a 0-byte body,
	// which is not valid JSON and would crash json.Marshal on RawMessage.
	body := json.RawMessage(call.Body)
	if len(body) == 0 {
		body = json.RawMessage("null")
	}
	resp := audit.Response{
		Status:     call.Status,
		DurationMS: call.DurationMS,
		BodySHA256: call.BodySHA256,
		Body:       body,
	}
	_, err := rc.writer.Append(command, req, resp)
	return err
}

// callAndRecord is the standard "one API call + one audit record" pair.
// It returns the Call so callers can parse the body for control flow.
func (rc *runCtx) callAndRecord(ctx context.Context, command, method, path string, query map[string]string) (*pulsetic.Call, error) {
	return rc.callAndRecordWithBody(ctx, command, method, path, query, nil)
}

// callAndRecordWithBody is callAndRecord with an optional JSON request body
// for POST/PUT operations. The request body is NOT stored in the audit
// record (only the response is), but callers can add it via structured
// logging if needed.
func (rc *runCtx) callAndRecordWithBody(ctx context.Context, command, method, path string, query map[string]string, reqBody []byte) (*pulsetic.Call, error) {
	if rc.verbose {
		fmt.Fprintf(os.Stderr, "[API] %s %s", method, path)
		if len(query) > 0 {
			fmt.Fprintf(os.Stderr, "?")
			first := true
			for k, v := range query {
				if !first {
					fmt.Fprintf(os.Stderr, "&")
				}
				fmt.Fprintf(os.Stderr, "%s=%s", k, v)
				first = false
			}
		}
		fmt.Fprintln(os.Stderr)
	}
	call, err := rc.client.Do(ctx, method, path, query, reqBody)
	if err != nil {
		return nil, err
	}
	if rc.verbose {
		fmt.Fprintf(os.Stderr, "[API] %d (%dms, %dB)\n", call.Status, call.DurationMS, len(call.Body))
	}
	if err := rc.record(command, call); err != nil {
		return nil, err
	}
	rc.records++
	return call, nil
}

func (rc *runCtx) close() error {
	if rc.records > 0 && !rc.dryRun && rc.outputPath != "" {
		fmt.Fprintf(os.Stderr, "%d records -> %s\n", rc.records, rc.outputPath)
	} else if rc.records > 0 && rc.dryRun {
		fmt.Fprintf(os.Stderr, "%d records (dry-run, not written)\n", rc.records)
	}
	if rc.writer == nil {
		return nil
	}
	return rc.writer.Close()
}

// parseRelativeTime parses one of: "now", a Go duration ("24h"), an
// extended duration with days ("7d", "30d1h"), or an RFC3339 absolute
// timestamp. Duration forms are interpreted as "now minus duration".
func parseRelativeTime(s string, now time.Time) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "now" {
		return now, nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	d, err := parseExtendedDuration(s)
	if err != nil {
		return time.Time{}, fmt.Errorf("%q is not a duration or RFC3339 time", s)
	}
	return now.Add(-d), nil
}

// parseExtendedDuration extends time.ParseDuration with a days unit.
// Accepted: "30d", "7d12h", plus anything time.ParseDuration accepts.
func parseExtendedDuration(s string) (time.Duration, error) {
	if idx := strings.Index(s, "d"); idx >= 0 {
		days, err := strconv.Atoi(s[:idx])
		if err != nil {
			return 0, fmt.Errorf("invalid day count %q", s[:idx])
		}
		total := time.Duration(days) * 24 * time.Hour
		rest := s[idx+1:]
		if rest != "" {
			r, err := time.ParseDuration(rest)
			if err != nil {
				return 0, err
			}
			total += r
		}
		return total, nil
	}
	return time.ParseDuration(s)
}

// readJSONInput returns the JSON body for a write command. If data is "-",
// it reads from stdin. Otherwise it treats data as a literal JSON string.
// An empty string is an error for commands that require a body.
func readJSONInput(data string, required bool) ([]byte, error) {
	if data == "-" {
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			return nil, fmt.Errorf("read stdin: %w", err)
		}
		return b, nil
	}
	if data == "" {
		if required {
			return nil, fmt.Errorf("--data is required (pass JSON string or - for stdin)")
		}
		return nil, nil
	}
	return []byte(data), nil
}

// formatPulseticTime formats a time as Pulsetic's API expects it:
// "YYYY-MM-DD HH:MM:SS" in UTC. From the docs example:
//
//	?start_dt=2021-01-03+14:00:00
func formatPulseticTime(t time.Time) string {
	return t.UTC().Format("2006-01-02 15:04:05")
}
