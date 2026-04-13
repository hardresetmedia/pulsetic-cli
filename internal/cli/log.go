package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/program-the-brain-not-the-heartbeat/pulsetic-cli/internal/audit"
	"github.com/program-the-brain-not-the-heartbeat/pulsetic-cli/internal/config"
)

func newLogCmd(opts *globalOpts) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "log",
		Short: "Query and summarize audit log files",
	}
	cmd.AddCommand(newLogSummaryCmd(opts))
	cmd.AddCommand(newLogQueryCmd(opts))
	return cmd
}

// ---------- log summary ----------

func newLogSummaryCmd(opts *globalOpts) *cobra.Command {
	var file string
	cmd := &cobra.Command{
		Use:   "summary",
		Short: "Show a summary of an audit log file",
		Example: `  pulsetic-cli log summary
  pulsetic-cli log summary --file=./audit/pulsetic-2026-04.jsonl
  pulsetic-cli log summary --json`,
		RunE: func(c *cobra.Command, args []string) error {
			path, err := resolveLogFile(opts, file)
			if err != nil {
				return err
			}

			// Verify chain integrity.
			vres, verr := audit.Verify(path)

			// Scan for aggregation.
			f, err := os.Open(path)
			if err != nil {
				return fmt.Errorf("open: %w", err)
			}
			defer f.Close()

			cmds := map[string]int{}
			statuses := map[int]int{}
			var minTS, maxTS string
			var count int

			sc := bufio.NewScanner(f)
			sc.Buffer(make([]byte, 64*1024), 16*1024*1024)
			for sc.Scan() {
				line := sc.Bytes()
				if len(strings.TrimSpace(string(line))) == 0 {
					continue
				}
				var r audit.Record
				if err := json.Unmarshal(line, &r); err != nil {
					continue
				}
				count++
				cmds[r.Command]++
				statuses[r.Response.Status]++
				if minTS == "" || r.TS < minTS {
					minTS = r.TS
				}
				if r.TS > maxTS {
					maxTS = r.TS
				}
			}
			if err := sc.Err(); err != nil {
				return err
			}

			out := c.OutOrStdout()

			if opts.jsonOutput || opts.format == formatJSON {
				chainOK := verr == nil && vres.OK()
				summary := map[string]any{
					"file":      path,
					"records":   count,
					"min_ts":    minTS,
					"max_ts":    maxTS,
					"chain_ok":  chainOK,
					"commands":  cmds,
					"statuses":  statuses,
				}
				if !chainOK && verr == nil {
					summary["chain_broken_at"] = vres.BrokenAt
					summary["chain_reason"] = vres.Reason
				}
				enc := json.NewEncoder(out)
				enc.SetEscapeHTML(false)
				return enc.Encode(summary)
			}

			fmt.Fprintf(out, "File: %s\n", path)
			fmt.Fprintf(out, "Records: %d\n", count)
			if minTS != "" {
				fmt.Fprintf(out, "Time range: %s to %s\n", minTS, maxTS)
			}
			if verr != nil {
				fmt.Fprintf(out, "Chain: ERROR (%v)\n", verr)
			} else if vres.OK() {
				fmt.Fprintf(out, "Chain: OK (last_hash=%s)\n", vres.LastHash)
			} else {
				fmt.Fprintf(out, "Chain: BROKEN at seq=%d (%s)\n", vres.BrokenAt, vres.Reason)
			}

			fmt.Fprintln(out, "\nCommands:")
			tw := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
			sortedCmds := sortedKeys(cmds)
			for _, cmd := range sortedCmds {
				fmt.Fprintf(tw, "  %s\t%d\n", cmd, cmds[cmd])
			}
			tw.Flush()

			fmt.Fprintln(out, "\nHTTP statuses:")
			tw2 := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
			sortedStatuses := sortedIntKeys(statuses)
			for _, s := range sortedStatuses {
				fmt.Fprintf(tw2, "  %d\t%d\n", s, statuses[s])
			}
			tw2.Flush()

			return nil
		},
	}
	cmd.Flags().StringVar(&file, "file", "", "audit file path (default: today's file from config)")
	return cmd
}

// ---------- log query ----------

func newLogQueryCmd(opts *globalOpts) *cobra.Command {
	var (
		file      string
		command   string
		status    string
		path      string
		sinceDur  string
	)
	cmd := &cobra.Command{
		Use:   "query",
		Short: "Search audit log records by command, status, path, or time",
		Example: `  pulsetic-cli log query --command=snapshot.monitors.stats
  pulsetic-cli log query --status='>400'
  pulsetic-cli log query --path=/monitors/141455
  pulsetic-cli log query --since=1h
  pulsetic-cli log query --command=monitors --format=csv`,
		RunE: func(c *cobra.Command, args []string) error {
			filePath, err := resolveLogFile(opts, file)
			if err != nil {
				return err
			}

			f, err := os.Open(filePath)
			if err != nil {
				return fmt.Errorf("open: %w", err)
			}
			defer f.Close()

			// Parse --since as a cutoff time.
			var cutoff time.Time
			if sinceDur != "" {
				t, err := parseRelativeTime(sinceDur, time.Now().UTC())
				if err != nil {
					return fmt.Errorf("--since: %w", err)
				}
				cutoff = t
			}

			// Parse --status filter.
			statusOp, statusVal := parseStatusFilter(status)

			out := c.OutOrStdout()
			var collected []json.RawMessage
			var count int

			sc := bufio.NewScanner(f)
			sc.Buffer(make([]byte, 64*1024), 16*1024*1024)
			for sc.Scan() {
				line := sc.Bytes()
				if len(strings.TrimSpace(string(line))) == 0 {
					continue
				}
				var r audit.Record
				if err := json.Unmarshal(line, &r); err != nil {
					continue
				}

				// Apply filters.
				if command != "" && !strings.Contains(r.Command, command) {
					continue
				}
				if path != "" && !strings.Contains(r.Request.Path, path) {
					continue
				}
				if statusVal > 0 && !matchStatus(r.Response.Status, statusOp, statusVal) {
					continue
				}
				if !cutoff.IsZero() {
					ts, err := time.Parse(time.RFC3339Nano, r.TS)
					if err != nil || ts.Before(cutoff) {
						continue
					}
				}

				count++

				switch {
				case opts.jsonOutput || opts.format == formatJSON || opts.format == formatCSV:
					collected = append(collected, json.RawMessage(line))
				case opts.format == formatStdout:
					var pretty json.RawMessage
					if err := json.Unmarshal(line, &pretty); err == nil {
						b, _ := json.MarshalIndent(pretty, "", "  ")
						fmt.Fprintln(out, string(b))
					}
				default: // jsonl
					fmt.Fprintln(out, string(line))
				}
			}
			if err := sc.Err(); err != nil {
				return err
			}

			// Flush deferred formats.
			if opts.jsonOutput || opts.format == formatJSON {
				env := jsonEnvelope{OK: true, Command: "log.query", Records: count, Data: collected}
				enc := json.NewEncoder(out)
				enc.SetEscapeHTML(false)
				return enc.Encode(env)
			}
			if opts.format == formatCSV && len(collected) > 0 {
				// Flatten records to CSV.
				fmt.Fprintln(out, "seq,ts,command,method,path,status,duration_ms")
				for _, raw := range collected {
					var r audit.Record
					if err := json.Unmarshal(raw, &r); err != nil {
						continue
					}
					fmt.Fprintf(out, "%d,%s,%s,%s,%s,%d,%d\n",
						r.Seq, r.TS, r.Command, r.Request.Method, r.Request.Path,
						r.Response.Status, r.Response.DurationMS)
				}
			}

			if !opts.quiet {
				fmt.Fprintf(os.Stderr, "%d matching records\n", count)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&file, "file", "", "audit file path (default: today's file from config)")
	cmd.Flags().StringVar(&command, "command", "", "filter by command substring")
	cmd.Flags().StringVar(&status, "status", "", "filter by HTTP status (e.g. 200, >400, <300)")
	cmd.Flags().StringVar(&path, "path", "", "filter by request path substring")
	cmd.Flags().StringVar(&sinceDur, "since", "", "filter records newer than this (e.g. 1h, 7d)")
	return cmd
}

// ---------- helpers ----------

func resolveLogFile(opts *globalOpts, explicit string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	cfg, err := config.Load(opts.configPath)
	if err != nil {
		return "", err
	}
	if opts.outputDir != "" {
		cfg.Output.Dir = opts.outputDir
	}
	return cfg.OutputPath(time.Now().UTC()), nil
}

// parseStatusFilter parses a status filter like "200", ">400", "<300".
func parseStatusFilter(s string) (string, int) {
	if s == "" {
		return "", 0
	}
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, ">") {
		n, err := strconv.Atoi(strings.TrimPrefix(s, ">"))
		if err != nil {
			return "", 0
		}
		return ">", n
	}
	if strings.HasPrefix(s, "<") {
		n, err := strconv.Atoi(strings.TrimPrefix(s, "<"))
		if err != nil {
			return "", 0
		}
		return "<", n
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return "", 0
	}
	return "=", n
}

func matchStatus(actual int, op string, val int) bool {
	switch op {
	case ">":
		return actual > val
	case "<":
		return actual < val
	case "=":
		return actual == val
	}
	return true
}

func sortedKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedIntKeys(m map[int]int) []int {
	keys := make([]int, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	return keys
}
