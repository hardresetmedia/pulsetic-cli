// Package cli wires together the cobra command tree. Each subcommand
// loads config, builds a Pulsetic client, opens the audit writer, and
// records every HTTP call it makes.
package cli

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/program-the-brain-not-the-heartbeat/pulsetic-cli/internal/audit"
	"github.com/program-the-brain-not-the-heartbeat/pulsetic-cli/internal/config"
	"github.com/program-the-brain-not-the-heartbeat/pulsetic-cli/internal/pulsetic"
)

// globalOpts holds the values of persistent flags. A pointer is passed
// to each subcommand constructor so they share the same storage.
// outputFormat controls how API response data is printed to stdout.
const (
	formatJSONL  = "jsonl"  // one JSON object per line (default)
	formatJSON   = "json"   // structured envelope
	formatCSV    = "csv"    // flattened CSV rows
	formatStdout = "stdout" // pretty-printed raw response
)

type globalOpts struct {
	configPath string
	outputDir  string
	format     string
	filter     string
	dryRun     bool
	verbose    bool
	jsonOutput bool // shorthand for --format=json
	quiet      bool
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
	root.PersistentFlags().StringVar(&opts.format, "format", "jsonl", "output format: jsonl, json, csv, stdout")
	root.PersistentFlags().BoolVar(&opts.jsonOutput, "json", false, "shorthand for --format=json")
	root.PersistentFlags().BoolVarP(&opts.quiet, "quiet", "q", false, "suppress all stderr progress output")
	root.PersistentFlags().StringVar(&opts.filter, "filter", "", "filter list output by name/url/status substring (case-insensitive)")
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
	root.AddCommand(newInitCmd(opts))
	root.AddCommand(newTestCmd(opts, version))
	root.AddCommand(newStatusCmd(opts, version))
	root.AddCommand(newLogCmd(opts))

	return root
}

// jsonEnvelope is the structured output emitted to stdout when --json is set.
type jsonEnvelope struct {
	OK      bool              `json:"ok"`
	Command string            `json:"command,omitempty"`
	Records int               `json:"records"`
	Data    []json.RawMessage `json:"data,omitempty"`
	Error   string            `json:"error,omitempty"`
}

// runCtx is the per-invocation state passed through each subcommand.
// The mu mutex protects fields that are modified during concurrent
// snapshot execution (records, lastCmd, collected).
type runCtx struct {
	mu         sync.Mutex
	cfg        config.Config
	client     *pulsetic.Client
	writer     *audit.Writer
	actor      audit.Actor
	dryRun     bool
	out        io.Writer // stdout destination (cobra's OutOrStdout)
	verbose    bool
	format     string // jsonl, json, csv, stdout
	quiet      bool
	records    int    // count of records written (for summary)
	outputPath string // resolved audit file path (for summary)
	lastCmd    string // most recent command name (for envelope)
	collected  []json.RawMessage // collected response bodies for json/csv output
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

	// Resolve output format. --json is shorthand for --format=json.
	outFmt := o.format
	if o.jsonOutput {
		outFmt = formatJSON
	}
	switch outFmt {
	case formatJSONL, formatJSON, formatCSV, formatStdout:
	default:
		return nil, fmt.Errorf("--format: unsupported value %q (use jsonl, json, csv, or stdout)", outFmt)
	}

	host, _ := os.Hostname()
	rc := &runCtx{
		cfg:     cfg,
		client:  client,
		actor:   audit.Actor{Tool: "pulsetic-cli", Version: version, Host: host},
		dryRun:  o.dryRun,
		out:     dryOut,
		verbose: o.verbose && !o.quiet,
		format:  outFmt,
		quiet:   o.quiet,
		start:   start,
		end:     end,
		now:     now,
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

// record writes one Call to the audit log and emits output per --format.
// json and csv formats collect bodies for deferred output in close().
// jsonl and stdout formats emit each response immediately.
func (rc *runCtx) record(command string, call *pulsetic.Call) error {
	rc.mu.Lock()
	defer rc.mu.Unlock()

	rc.lastCmd = command

	out := rc.out
	if out == nil {
		out = os.Stdout
	}

	body := json.RawMessage(call.Body)
	if len(body) == 0 {
		body = json.RawMessage("null")
	}

	// Collect for deferred output (json envelope and csv).
	if rc.format == formatJSON || rc.format == formatCSV {
		rc.collected = append(rc.collected, body)
	}

	// Immediate stdout output for dry-run in jsonl/stdout modes.
	if rc.writer == nil {
		rc.records++
		switch rc.format {
		case formatJSONL:
			_, err := fmt.Fprintln(out, string(call.Body))
			return err
		case formatStdout:
			var pretty bytes.Buffer
			if err := json.Indent(&pretty, call.Body, "", "  "); err != nil {
				_, err = fmt.Fprintln(out, string(call.Body))
				return err
			}
			_, err := fmt.Fprintln(out, pretty.String())
			return err
		case formatJSON, formatCSV:
			// Deferred - emitted in close().
			return nil
		}
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
	auditBody := json.RawMessage(call.Body)
	if len(auditBody) == 0 {
		auditBody = json.RawMessage("null")
	}
	resp := audit.Response{
		Status:     call.Status,
		DurationMS: call.DurationMS,
		BodySHA256: call.BodySHA256,
		Body:       auditBody,
	}
	_, err := rc.writer.Append(command, req, resp)
	if err == nil {
		rc.records++
	}
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
	return call, nil
}

// logStderr prints to stderr unless --quiet is set.
func (rc *runCtx) logStderr(format string, args ...any) {
	if !rc.quiet {
		fmt.Fprintf(os.Stderr, format, args...)
	}
}

func (rc *runCtx) stdout() io.Writer {
	if rc.out != nil {
		return rc.out
	}
	return os.Stdout
}

func (rc *runCtx) close() error {
	// Emit deferred output for json and csv formats.
	if err := rc.flushFormat(); err != nil {
		return err
	}

	if !rc.quiet {
		if rc.records > 0 && !rc.dryRun && rc.outputPath != "" {
			fmt.Fprintf(os.Stderr, "%d records -> %s\n", rc.records, rc.outputPath)
		} else if rc.records > 0 && rc.dryRun {
			fmt.Fprintf(os.Stderr, "%d records (dry-run, not written)\n", rc.records)
		}
	}
	if rc.writer == nil {
		return nil
	}
	return rc.writer.Close()
}

// flushFormat writes collected response data to stdout in the requested
// format. Called by close() for json and csv which defer output until
// all API calls are done.
func (rc *runCtx) flushFormat() error {
	out := rc.stdout()

	switch rc.format {
	case formatJSON:
		env := jsonEnvelope{
			OK:      true,
			Command: rc.lastCmd,
			Records: rc.records,
			Data:    rc.collected,
		}
		enc := json.NewEncoder(out)
		enc.SetEscapeHTML(false)
		return enc.Encode(env)

	case formatCSV:
		return rc.writeCSV(out)
	}
	return nil
}

// writeCSV flattens collected JSON response bodies into CSV rows.
// It extracts items from each response's "data" array (or treats the
// response as a single object). Column headers are auto-detected from
// the first item's keys, sorted alphabetically.
func (rc *runCtx) writeCSV(out io.Writer) error {
	// Gather all items across all collected pages.
	var rows []map[string]any
	for _, raw := range rc.collected {
		items := extractItems(raw)
		for _, item := range items {
			var m map[string]any
			if err := json.Unmarshal(item, &m); err != nil {
				continue
			}
			rows = append(rows, m)
		}
	}
	if len(rows) == 0 {
		return nil
	}

	// Build sorted column headers from the first row.
	colSet := map[string]bool{}
	for k := range rows[0] {
		colSet[k] = true
	}
	cols := make([]string, 0, len(colSet))
	for k := range colSet {
		cols = append(cols, k)
	}
	sort.Strings(cols)

	w := csv.NewWriter(out)
	if err := w.Write(cols); err != nil {
		return err
	}
	for _, row := range rows {
		record := make([]string, len(cols))
		for i, col := range cols {
			record[i] = flattenValue(row[col])
		}
		if err := w.Write(record); err != nil {
			return err
		}
	}
	w.Flush()
	return w.Error()
}

// extractItems pulls individual objects from a JSON response. It handles
// both {"data":[...]} envelopes and bare arrays. Single objects are
// returned as a one-element slice.
func extractItems(raw json.RawMessage) []json.RawMessage {
	// Try {"data":[...]} envelope.
	var env struct {
		Data []json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &env); err == nil && env.Data != nil {
		return env.Data
	}
	// Try bare array.
	var arr []json.RawMessage
	if err := json.Unmarshal(raw, &arr); err == nil {
		return arr
	}
	// Single object.
	return []json.RawMessage{raw}
}

// flattenValue converts a JSON value to a CSV-safe string. Nested objects
// and arrays are serialized as compact JSON strings.
func flattenValue(v any) string {
	if v == nil {
		return ""
	}
	switch val := v.(type) {
	case string:
		return val
	case float64:
		if val == float64(int64(val)) {
			return strconv.FormatInt(int64(val), 10)
		}
		return strconv.FormatFloat(val, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(val)
	default:
		// Nested object or array - serialize as JSON.
		b, err := json.Marshal(val)
		if err != nil {
			return fmt.Sprint(val)
		}
		return string(b)
	}
}

// closeWithError emits an error envelope (for --format=json) and
// closes the writer.
func (rc *runCtx) closeWithError(cmdErr error) error {
	if rc.format == formatJSON && cmdErr != nil {
		out := rc.stdout()
		env := jsonEnvelope{
			OK:      false,
			Command: rc.lastCmd,
			Records: rc.records,
			Error:   cmdErr.Error(),
		}
		enc := json.NewEncoder(out)
		enc.SetEscapeHTML(false)
		_ = enc.Encode(env)
	}
	if rc.writer != nil {
		return rc.writer.Close()
	}
	return nil
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

// matchesFilter checks whether a JSON object matches the --filter string.
// It does a case-insensitive substring search across common fields:
// url, name, domain, status, title.
func matchesFilter(raw json.RawMessage, filter string) bool {
	if filter == "" {
		return true
	}
	f := strings.ToLower(filter)
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return false
	}
	for _, key := range []string{"url", "name", "domain", "status", "title"} {
		if v, ok := m[key]; ok && v != nil {
			if strings.Contains(strings.ToLower(fmt.Sprint(v)), f) {
				return true
			}
		}
	}
	return false
}

// filterItems applies --filter to a slice of JSON items, returning
// only those that match. Returns all items if filter is empty.
func filterItems(items []json.RawMessage, filter string) []json.RawMessage {
	if filter == "" {
		return items
	}
	var out []json.RawMessage
	for _, item := range items {
		if matchesFilter(item, filter) {
			out = append(out, item)
		}
	}
	return out
}
