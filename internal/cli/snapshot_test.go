package cli

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/program-the-brain-not-the-heartbeat/pulsetic-cli/internal/audit"
)

// TestSnapshotEndToEnd boots an in-process Pulsetic-shaped server, runs
// the snapshot command against it, and confirms the resulting audit file
// contains the expected records and passes chain verification.
func TestSnapshotEndToEnd(t *testing.T) {
	srv := newFakePulsetic(t)
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	outDir := filepath.Join(dir, "audit")

	t.Setenv("PULSETIC_API_TOKEN", "integration-token")
	t.Setenv("PULSETIC_CLI_BASE_URL", srv.URL+"/api/public")
	// Point HOME at a directory with no config file so Load doesn't pick
	// up user settings from the developer's machine.
	t.Setenv("HOME", dir)

	root := NewRootCmd("test-0.0.0")
	root.SetArgs([]string{"snapshot", "--output", outDir, "--since", "1h"})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := root.ExecuteContext(ctx); err != nil {
		t.Fatalf("snapshot: %v", err)
	}

	// Resolve the expected output file from the default pattern.
	now := time.Now().UTC()
	auditFile := filepath.Join(outDir, fmt.Sprintf("pulsetic-%04d-%02d.jsonl", now.Year(), int(now.Month())))
	if _, err := os.Stat(auditFile); err != nil {
		t.Fatalf("expected audit file at %s: %v", auditFile, err)
	}

	res, err := audit.Verify(auditFile)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !res.OK() {
		t.Fatalf("chain broken: %+v", res)
	}

	// Expected record count:
	//   1 x monitors list page
	//   2 monitors x 5 per-monitor endpoints
	//     (snapshots, events, downtime, stats, notification_channels)  = 10
	//   1 x status-pages list page
	//   1 status page x 1 endpoint (incidents only, no GET detail)     =  1
	//   1 x domains list
	//   1 x heartbeats list
	// ---
	//   15 records
	if res.Records != 15 {
		t.Fatalf("want 16 records, got %d", res.Records)
	}

	// Spot-check: the server must have been hit with the raw token in the
	// Authorization header, no Bearer prefix.
	if got := srv.LastToken(); got != "integration-token" {
		t.Fatalf("auth header: want %q got %q", "integration-token", got)
	}
}

// fakePulsetic is a minimal in-memory stand-in for the Pulsetic public API.
type fakePulsetic struct {
	*httptest.Server
	mu        sync.Mutex
	lastToken string
	calls     map[string]int
}

func (f *fakePulsetic) LastToken() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastToken
}

func newFakePulsetic(t *testing.T) *fakePulsetic {
	t.Helper()
	f := &fakePulsetic{calls: map[string]int{}}
	mux := http.NewServeMux()

	mux.HandleFunc("/api/public/monitors", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.lastToken = r.Header.Get("Authorization")
		f.calls["/monitors"]++
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		// Two monitors, less than per_page=100, so pagination stops here.
		_, _ = w.Write([]byte(`{"data":[{"id":1,"url":"https://a.example"},{"id":2,"url":"https://b.example"}]}`))
	})

	// Per-monitor endpoints: /monitors/<id>/<endpoint> (and /monitors/<id>).
	// Accept any sub-path depth so stats, notification-channels, etc. all work.
	mux.HandleFunc("/api/public/monitors/", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.lastToken = r.Header.Get("Authorization")
		f.mu.Unlock()
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/public/monitors/"), "/")
		w.Header().Set("Content-Type", "application/json")
		id := parts[0]
		endpoint := "detail"
		if len(parts) >= 2 {
			endpoint = strings.Join(parts[1:], "/")
		}
		_, _ = w.Write([]byte(fmt.Sprintf(`{"monitor_id":%s,"endpoint":%q,"data":[]}`, id, endpoint)))
	})

	mux.HandleFunc("/api/public/status-page", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.lastToken = r.Header.Get("Authorization")
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":10,"title":"Public Status"}]}`))
	})

	mux.HandleFunc("/api/public/status-page/", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.lastToken = r.Header.Get("Authorization")
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		// Respond identically for /status-page/10 and /status-page/10/incidents
		if strings.HasSuffix(r.URL.Path, "/incidents") {
			_, _ = w.Write([]byte(`{"data":[]}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":10,"title":"Public Status","maintenance":[]}`))
	})

	mux.HandleFunc("/api/public/domains", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.lastToken = r.Header.Get("Authorization")
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":1,"domain":"example.com"}]}`))
	})

	mux.HandleFunc("/api/public/heartbeats", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.lastToken = r.Header.Get("Authorization")
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":1,"name":"cron-job"}]}`))
	})

	f.Server = httptest.NewServer(mux)
	return f
}
