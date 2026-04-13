package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/program-the-brain-not-the-heartbeat/pulsetic-cli/internal/audit"
	"github.com/program-the-brain-not-the-heartbeat/pulsetic-cli/internal/config"
)

// verifyExitCode is returned when the chain is broken. It matches the
// "3 = chain verify failure" contract documented in the plan.
const verifyExitCode = 3

func newVerifyCmd(opts *globalOpts) *cobra.Command {
	return &cobra.Command{
		Use:   "verify [path]",
		Short: "Replay the hash chain of an audit file and report the first broken link",
		Example: `  # Verify today's audit file (path resolved from config)
  pulsetic-cli verify

  # Verify a specific file
  pulsetic-cli verify ./audit/pulsetic-2026-04.jsonl`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			path, err := resolveVerifyPath(opts, args)
			if err != nil {
				return err
			}
			res, err := audit.Verify(path)
			if err != nil {
				return err
			}

			out := c.OutOrStdout()

			if opts.jsonOutput || opts.format == "json" {
				v := map[string]any{
					"ok":        res.OK(),
					"records":   res.Records,
					"last_hash": res.LastHash,
				}
				if !res.OK() {
					v["broken_at"] = res.BrokenAt
					v["broken_line"] = res.BrokenLine
					v["reason"] = res.Reason
				}
				enc := json.NewEncoder(out)
				enc.SetEscapeHTML(false)
				if err := enc.Encode(v); err != nil {
					return err
				}
				if !res.OK() {
					os.Exit(verifyExitCode)
				}
				return nil
			}

			if !res.OK() {
				fmt.Fprintf(c.ErrOrStderr(), "chain broken at seq=%d line=%d: %s\n", res.BrokenAt, res.BrokenLine, res.Reason)
				os.Exit(verifyExitCode)
			}
			fmt.Fprintf(out, "chain OK: %d records, last_hash=%s\n", res.Records, res.LastHash)
			return nil
		},
	}
}

// resolveVerifyPath picks the path to verify. Explicit arg wins; otherwise
// the config's OutputPath for today is used.
func resolveVerifyPath(opts *globalOpts, args []string) (string, error) {
	if len(args) == 1 {
		return args[0], nil
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
