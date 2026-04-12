package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/program-the-brain-not-the-heartbeat/pulsetic-cli/internal/audit"
)

// TestVerifyCommandOnValidFile exercises the verify subcommand end-to-end
// against a file produced via the audit package directly. This is the
// complement to TestSnapshotEndToEnd: snapshot produces the file, verify
// consumes it.
func TestVerifyCommandOnValidFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.jsonl")

	// Write a 2-record valid audit file directly.
	w, err := audit.OpenWriter(path, audit.Actor{Tool: "t", Version: "0", Host: "h"}, nil)
	if err != nil {
		t.Fatalf("OpenWriter: %v", err)
	}
	req := audit.Request{Method: "GET", Path: "/monitors", Query: map[string]string{"page": "1"}}
	resp := audit.Response{Status: 200, DurationMS: 5, BodySHA256: "x", Body: json.RawMessage(`{"ok":true}`)}
	for i := 0; i < 2; i++ {
		if _, err := w.Append("t", req, resp); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	// Run the verify subcommand with stdout captured.
	root := NewRootCmd("test")
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"verify", path})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := root.ExecuteContext(ctx); err != nil {
		t.Fatalf("verify: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "chain OK") {
		t.Fatalf("expected chain OK in output, got: %s", got)
	}
	if !strings.Contains(got, "2 records") {
		t.Fatalf("expected record count 2 in output, got: %s", got)
	}
}
