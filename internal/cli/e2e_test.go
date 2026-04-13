package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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

// ---------- test helpers ----------

// crudServer is a richer fake than fakePulsetic: it logs every request
// (method + path + body) and echoes back a JSON response containing
// the request details. This lets tests verify the client sent the
// right method, path, and body without needing endpoint-specific stubs.
type crudServer struct {
	*httptest.Server
	mu       sync.Mutex
	requests []reqLog
}

type reqLog struct {
	Method string
	Path   string
	Query  string
	Body   string
	Auth   string
}

func newCrudServer(t *testing.T) *crudServer {
	t.Helper()
	s := &crudServer{}
	mux := http.NewServeMux()

	// Catch-all handler: record the request, respond with JSON.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		bodyBytes, _ := io.ReadAll(r.Body)
		s.mu.Lock()
		s.requests = append(s.requests, reqLog{
			Method: r.Method,
			Path:   r.URL.Path,
			Query:  r.URL.RawQuery,
			Body:   string(bodyBytes),
			Auth:   r.Header.Get("Authorization"),
		})
		s.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")

		// For list endpoints, return a paginated envelope that stops after
		// page 1 so snapshot/list commands don't loop forever.
		path := r.URL.Path
		switch {
		case r.Method == "GET" && (path == "/api/public/monitors" || path == "/api/public/status-page"):
			_, _ = w.Write([]byte(`{"data":[{"id":1}]}`))
		case r.Method == "GET" && path == "/api/public/domains":
			_, _ = w.Write([]byte(`{"data":[{"id":1,"domain":"example.com"}]}`))
		case r.Method == "GET" && path == "/api/public/heartbeats":
			_, _ = w.Write([]byte(`{"data":[{"id":1,"name":"cron-job"}]}`))
		case r.Method == "DELETE":
			_, _ = w.Write([]byte(`{"success":true}`))
		default:
			// Echo the request details back so tests can inspect.
			resp := map[string]any{
				"method": r.Method,
				"path":   r.URL.Path,
				"ok":     true,
			}
			if len(bodyBytes) > 0 {
				var parsed any
				if json.Unmarshal(bodyBytes, &parsed) == nil {
					resp["received_body"] = parsed
				}
			}
			enc, _ := json.Marshal(resp)
			_, _ = w.Write(enc)
		}
	})

	s.Server = httptest.NewServer(mux)
	t.Cleanup(s.Close)
	return s
}

func (s *crudServer) lastRequest() reqLog {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.requests) == 0 {
		return reqLog{}
	}
	return s.requests[len(s.requests)-1]
}

func (s *crudServer) requestCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.requests)
}

func (s *crudServer) allRequests() []reqLog {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make([]reqLog, len(s.requests))
	copy(cp, s.requests)
	return cp
}

// runCmd constructs a fresh root command and executes it against the
// fake server. Returns the combined stdout+stderr and any error.
func runCmd(t *testing.T, srv *crudServer, args ...string) (string, error) {
	t.Helper()
	dir := t.TempDir()
	outDir := filepath.Join(dir, "audit")

	t.Setenv("PULSETIC_CLI_OUTPUT_DIR", "")
	t.Setenv("PULSETIC_CLI_SINCE", "")
	t.Setenv("PULSETIC_CLI_BASE_URL", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("PULSETIC_API_TOKEN", "test-token")
	t.Setenv("PULSETIC_CLI_BASE_URL", srv.URL+"/api/public")
	t.Setenv("HOME", dir)

	root := NewRootCmd("test")
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)

	fullArgs := append(args, "--output", outDir, "--since", "1h")
	root.SetArgs(fullArgs)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := root.ExecuteContext(ctx)
	return out.String(), err
}

// runCmdDryRun is like runCmd but adds --dry-run so no audit file is written.
func runCmdDryRun(t *testing.T, srv *crudServer, args ...string) (string, error) {
	t.Helper()
	return runCmd(t, srv, append(args, "--dry-run")...)
}

// cmdResult holds separated stdout and stderr for precise output assertions.
type cmdResult struct {
	Stdout string
	Stderr string
}

// runCmdSplit is like runCmd but captures stdout and stderr separately.
func runCmdSplit(t *testing.T, srv *crudServer, args ...string) (cmdResult, error) {
	t.Helper()
	dir := t.TempDir()
	outDir := filepath.Join(dir, "audit")

	t.Setenv("PULSETIC_CLI_OUTPUT_DIR", "")
	t.Setenv("PULSETIC_CLI_SINCE", "")
	t.Setenv("PULSETIC_CLI_BASE_URL", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("PULSETIC_API_TOKEN", "test-token")
	t.Setenv("PULSETIC_CLI_BASE_URL", srv.URL+"/api/public")
	t.Setenv("HOME", dir)

	root := NewRootCmd("test")
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)

	fullArgs := append(args, "--output", outDir, "--since", "1h")
	root.SetArgs(fullArgs)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := root.ExecuteContext(ctx)
	return cmdResult{Stdout: stdout.String(), Stderr: stderr.String()}, err
}

// assertLastMethod checks the most recent request to the fake server.
func assertLastMethod(t *testing.T, srv *crudServer, wantMethod, wantPathSuffix string) {
	t.Helper()
	last := srv.lastRequest()
	if last.Method != wantMethod {
		t.Fatalf("method: want %s got %s", wantMethod, last.Method)
	}
	if !strings.HasSuffix(last.Path, wantPathSuffix) {
		t.Fatalf("path: want suffix %q got %q", wantPathSuffix, last.Path)
	}
}

// assertLastBody checks that the server received the expected JSON body.
func assertLastBody(t *testing.T, srv *crudServer, wantSubstring string) {
	t.Helper()
	last := srv.lastRequest()
	if !strings.Contains(last.Body, wantSubstring) {
		t.Fatalf("body: want substring %q in %q", wantSubstring, last.Body)
	}
}

// ---------- monitors CRUD ----------

func TestMonitorsListE2E(t *testing.T) {
	srv := newCrudServer(t)
	_, err := runCmdDryRun(t, srv, "monitors", "list")
	if err != nil {
		t.Fatalf("monitors list: %v", err)
	}
	// Paginated: at least one GET to /monitors.
	found := false
	for _, r := range srv.allRequests() {
		if r.Method == "GET" && strings.HasSuffix(r.Path, "/monitors") {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected GET /monitors")
	}
}

func TestMonitorsGetE2E(t *testing.T) {
	srv := newCrudServer(t)
	_, err := runCmdDryRun(t, srv, "monitors", "get", "42")
	if err != nil {
		t.Fatalf("monitors get: %v", err)
	}
	assertLastMethod(t, srv, "GET", "/monitors/42")
}

func TestMonitorsCreateE2E(t *testing.T) {
	srv := newCrudServer(t)
	_, err := runCmdDryRun(t, srv, "monitors", "create",
		"--data", `{"urls":["https://example.com"],"add_account_email":true}`)
	if err != nil {
		t.Fatalf("monitors create: %v", err)
	}
	assertLastMethod(t, srv, "POST", "/monitors")
	assertLastBody(t, srv, "https://example.com")
}

func TestMonitorsUpdateE2E(t *testing.T) {
	srv := newCrudServer(t)
	_, err := runCmdDryRun(t, srv, "monitors", "update", "7",
		"--data", `{"name":"Renamed Monitor","uptime_check_frequency":"300"}`)
	if err != nil {
		t.Fatalf("monitors update: %v", err)
	}
	assertLastMethod(t, srv, "PUT", "/monitors/7")
	assertLastBody(t, srv, "Renamed Monitor")
}

func TestMonitorsDeleteE2E(t *testing.T) {
	srv := newCrudServer(t)
	_, err := runCmdDryRun(t, srv, "monitors", "delete", "7")
	if err != nil {
		t.Fatalf("monitors delete: %v", err)
	}
	assertLastMethod(t, srv, "DELETE", "/monitors/7")
}

func TestMonitorsHistoryE2E(t *testing.T) {
	srv := newCrudServer(t)
	_, err := runCmdDryRun(t, srv, "monitors", "history", "42")
	if err != nil {
		t.Fatalf("monitors history: %v", err)
	}
	// History should hit snapshots, events, downtime, stats, notification-channels.
	paths := make(map[string]bool)
	for _, r := range srv.allRequests() {
		paths[r.Path] = true
	}
	for _, want := range []string{"/snapshots", "/events", "/downtime", "/stats", "/notification-channels"} {
		full := "/api/public/monitors/42" + want
		if !paths[full] {
			t.Errorf("missing history call: %s (got %v)", full, paths)
		}
	}
}

func TestMonitorsHistoryIncludeChecksE2E(t *testing.T) {
	srv := newCrudServer(t)
	_, err := runCmdDryRun(t, srv, "monitors", "history", "42", "--include-checks")
	if err != nil {
		t.Fatalf("monitors history --include-checks: %v", err)
	}
	found := false
	for _, r := range srv.allRequests() {
		if strings.HasSuffix(r.Path, "/monitors/42/checks") {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("--include-checks did not fetch /monitors/42/checks")
	}
}

// ---------- status-pages CRUD ----------

func TestStatusPagesListE2E(t *testing.T) {
	srv := newCrudServer(t)
	_, err := runCmdDryRun(t, srv, "status-pages", "list")
	if err != nil {
		t.Fatalf("status-pages list: %v", err)
	}
	found := false
	for _, r := range srv.allRequests() {
		if r.Method == "GET" && strings.HasSuffix(r.Path, "/status-page") {
			found = true
		}
	}
	if !found {
		t.Fatal("expected GET /status-page")
	}
}

func TestStatusPagesCreateE2E(t *testing.T) {
	srv := newCrudServer(t)
	_, err := runCmdDryRun(t, srv, "status-pages", "create",
		"--data", `{"title":"My Page","monitors":[1,2]}`)
	if err != nil {
		t.Fatalf("status-pages create: %v", err)
	}
	assertLastMethod(t, srv, "POST", "/status-page")
	assertLastBody(t, srv, "My Page")
}

func TestStatusPagesUpdateE2E(t *testing.T) {
	srv := newCrudServer(t)
	_, err := runCmdDryRun(t, srv, "status-pages", "update", "13",
		"--data", `{"title":"Updated Page"}`)
	if err != nil {
		t.Fatalf("status-pages update: %v", err)
	}
	assertLastMethod(t, srv, "PUT", "/status-page/13")
}

func TestStatusPagesDeleteE2E(t *testing.T) {
	srv := newCrudServer(t)
	_, err := runCmdDryRun(t, srv, "status-pages", "delete", "13")
	if err != nil {
		t.Fatalf("status-pages delete: %v", err)
	}
	assertLastMethod(t, srv, "DELETE", "/status-page/13")
}

// ---------- maintenance ----------

func TestMaintenanceCreateE2E(t *testing.T) {
	srv := newCrudServer(t)
	_, err := runCmdDryRun(t, srv, "status-pages", "maintenance", "create", "13",
		"--data", `{"name":"Upgrade","monitors":[1]}`)
	if err != nil {
		t.Fatalf("maintenance create: %v", err)
	}
	assertLastMethod(t, srv, "POST", "/status-page/13/maintenance")
	assertLastBody(t, srv, "Upgrade")
}

func TestMaintenanceUpdateE2E(t *testing.T) {
	srv := newCrudServer(t)
	_, err := runCmdDryRun(t, srv, "status-pages", "maintenance", "update", "8",
		"--data", `{"name":"Extended Upgrade"}`)
	if err != nil {
		t.Fatalf("maintenance update: %v", err)
	}
	assertLastMethod(t, srv, "PUT", "/status-page/maintenance/8")
}

func TestMaintenanceDeleteE2E(t *testing.T) {
	srv := newCrudServer(t)
	_, err := runCmdDryRun(t, srv, "status-pages", "maintenance", "delete", "8")
	if err != nil {
		t.Fatalf("maintenance delete: %v", err)
	}
	assertLastMethod(t, srv, "DELETE", "/status-page/maintenance/8")
}

// ---------- incidents ----------

func TestIncidentsListViaStatusPageE2E(t *testing.T) {
	srv := newCrudServer(t)
	_, err := runCmdDryRun(t, srv, "status-pages", "incidents", "list", "13")
	if err != nil {
		t.Fatalf("incidents list: %v", err)
	}
	assertLastMethod(t, srv, "GET", "/status-page/13/incidents")
}

func TestIncidentsCreateE2E(t *testing.T) {
	srv := newCrudServer(t)
	_, err := runCmdDryRun(t, srv, "status-pages", "incidents", "create", "13",
		"--data", `{"title":"Outage","update":{"status":"exploring"}}`)
	if err != nil {
		t.Fatalf("incidents create: %v", err)
	}
	assertLastMethod(t, srv, "POST", "/status-page/13/incidents")
	assertLastBody(t, srv, "Outage")
}

func TestIncidentsUpdateE2E(t *testing.T) {
	srv := newCrudServer(t)
	_, err := runCmdDryRun(t, srv, "status-pages", "incidents", "update", "30",
		"--data", `{"title":"Resolved"}`)
	if err != nil {
		t.Fatalf("incidents update: %v", err)
	}
	assertLastMethod(t, srv, "PUT", "/status-page/incidents/30")
}

func TestIncidentsDeleteE2E(t *testing.T) {
	srv := newCrudServer(t)
	_, err := runCmdDryRun(t, srv, "status-pages", "incidents", "delete", "30")
	if err != nil {
		t.Fatalf("incidents delete: %v", err)
	}
	assertLastMethod(t, srv, "DELETE", "/status-page/incidents/30")
}

func TestIncidentAddUpdateE2E(t *testing.T) {
	srv := newCrudServer(t)
	_, err := runCmdDryRun(t, srv, "status-pages", "incidents", "add-update", "29",
		"--data", `{"status":"identified","message":"Found root cause"}`)
	if err != nil {
		t.Fatalf("add-update: %v", err)
	}
	assertLastMethod(t, srv, "POST", "/incidents/29/incident-update")
	assertLastBody(t, srv, "Found root cause")
}

func TestIncidentEditUpdateE2E(t *testing.T) {
	srv := newCrudServer(t)
	_, err := runCmdDryRun(t, srv, "status-pages", "incidents", "edit-update", "57",
		"--data", `{"status":"resolved","message":"All clear"}`)
	if err != nil {
		t.Fatalf("edit-update: %v", err)
	}
	assertLastMethod(t, srv, "PUT", "/incidents/updates/57")
}

func TestIncidentDeleteUpdateE2E(t *testing.T) {
	srv := newCrudServer(t)
	_, err := runCmdDryRun(t, srv, "status-pages", "incidents", "delete-update", "57")
	if err != nil {
		t.Fatalf("delete-update: %v", err)
	}
	assertLastMethod(t, srv, "DELETE", "/incidents/updates/57")
}

// ---------- top-level incidents (cross-page) ----------

func TestTopLevelIncidentsListE2E(t *testing.T) {
	srv := newCrudServer(t)
	_, err := runCmdDryRun(t, srv, "incidents", "list")
	if err != nil {
		t.Fatalf("incidents list: %v", err)
	}
	// Should first list status pages, then get incidents for each.
	methods := make(map[string]bool)
	for _, r := range srv.allRequests() {
		methods[r.Method+" "+r.Path] = true
	}
	if !methods["GET /api/public/status-page"] {
		t.Fatal("missing GET /status-page")
	}
	if !methods["GET /api/public/status-page/1/incidents"] {
		t.Fatal("missing GET /status-page/1/incidents")
	}
}

// ---------- domains CRUD ----------

func TestDomainsListE2E(t *testing.T) {
	srv := newCrudServer(t)
	_, err := runCmdDryRun(t, srv, "domains", "list")
	if err != nil {
		t.Fatalf("domains list: %v", err)
	}
	assertLastMethod(t, srv, "GET", "/domains")
}

func TestDomainsCreateE2E(t *testing.T) {
	srv := newCrudServer(t)
	_, err := runCmdDryRun(t, srv, "domains", "create",
		"--data", `{"domains":["example.com","api.example.com"]}`)
	if err != nil {
		t.Fatalf("domains create: %v", err)
	}
	assertLastMethod(t, srv, "POST", "/domains")
	assertLastBody(t, srv, "api.example.com")
}

func TestDomainsUpdateE2E(t *testing.T) {
	srv := newCrudServer(t)
	_, err := runCmdDryRun(t, srv, "domains", "update", "5",
		"--data", `{"alias":"Production","is_active":true}`)
	if err != nil {
		t.Fatalf("domains update: %v", err)
	}
	assertLastMethod(t, srv, "PUT", "/domains/5")
	assertLastBody(t, srv, "Production")
}

func TestDomainsDeleteE2E(t *testing.T) {
	srv := newCrudServer(t)
	_, err := runCmdDryRun(t, srv, "domains", "delete", "5")
	if err != nil {
		t.Fatalf("domains delete: %v", err)
	}
	assertLastMethod(t, srv, "DELETE", "/domains/5")
}

// ---------- heartbeats CRUD ----------

func TestHeartbeatsListE2E(t *testing.T) {
	srv := newCrudServer(t)
	_, err := runCmdDryRun(t, srv, "heartbeats", "list")
	if err != nil {
		t.Fatalf("heartbeats list: %v", err)
	}
	assertLastMethod(t, srv, "GET", "/heartbeats")
}

func TestHeartbeatsGetE2E(t *testing.T) {
	srv := newCrudServer(t)
	_, err := runCmdDryRun(t, srv, "heartbeats", "get", "3")
	if err != nil {
		t.Fatalf("heartbeats get: %v", err)
	}
	assertLastMethod(t, srv, "GET", "/heartbeats/3")
}

func TestHeartbeatsCreateE2E(t *testing.T) {
	srv := newCrudServer(t)
	_, err := runCmdDryRun(t, srv, "heartbeats", "create",
		"--data", `{"name":"Nightly Backup","monitoring_interval":86400,"grace_period":3600}`)
	if err != nil {
		t.Fatalf("heartbeats create: %v", err)
	}
	assertLastMethod(t, srv, "POST", "/heartbeats")
	assertLastBody(t, srv, "Nightly Backup")
}

func TestHeartbeatsUpdateE2E(t *testing.T) {
	srv := newCrudServer(t)
	_, err := runCmdDryRun(t, srv, "heartbeats", "update", "3",
		"--data", `{"name":"Updated Beat","grace_period":7200}`)
	if err != nil {
		t.Fatalf("heartbeats update: %v", err)
	}
	assertLastMethod(t, srv, "PUT", "/heartbeats/3")
	assertLastBody(t, srv, "Updated Beat")
}

func TestHeartbeatsDeleteE2E(t *testing.T) {
	srv := newCrudServer(t)
	_, err := runCmdDryRun(t, srv, "heartbeats", "delete", "3")
	if err != nil {
		t.Fatalf("heartbeats delete: %v", err)
	}
	assertLastMethod(t, srv, "DELETE", "/heartbeats/3")
}

// ---------- notification-channels ----------

func TestNotifChannelsListE2E(t *testing.T) {
	srv := newCrudServer(t)
	_, err := runCmdDryRun(t, srv, "notification-channels", "list", "42")
	if err != nil {
		t.Fatalf("notifs list: %v", err)
	}
	assertLastMethod(t, srv, "GET", "/monitors/42/notification-channels")
}

func TestNotifChannelsAddEmailE2E(t *testing.T) {
	srv := newCrudServer(t)
	_, err := runCmdDryRun(t, srv, "notifs", "add-email", "42",
		"--data", `{"email":"oncall@example.com"}`)
	if err != nil {
		t.Fatalf("notifs add-email: %v", err)
	}
	assertLastMethod(t, srv, "POST", "/monitors/42/notification-channels/email")
	assertLastBody(t, srv, "oncall@example.com")
}

func TestNotifChannelsAddSlackE2E(t *testing.T) {
	srv := newCrudServer(t)
	_, err := runCmdDryRun(t, srv, "notifs", "add-slack", "42",
		"--data", `{"webhook_url":"https://hooks.slack.com/T/B/xxx"}`)
	if err != nil {
		t.Fatalf("notifs add-slack: %v", err)
	}
	assertLastMethod(t, srv, "POST", "/monitors/42/notification-channels/slack-webhook")
}

func TestNotifChannelsAddWebhookE2E(t *testing.T) {
	srv := newCrudServer(t)
	_, err := runCmdDryRun(t, srv, "notifs", "add-webhook", "42",
		"--data", `{"webhook":"https://hooks.example.com/alert"}`)
	if err != nil {
		t.Fatalf("notifs add-webhook: %v", err)
	}
	assertLastMethod(t, srv, "POST", "/monitors/42/notification-channels/webhook")
}

func TestNotifChannelsDeleteE2E(t *testing.T) {
	srv := newCrudServer(t)
	_, err := runCmdDryRun(t, srv, "notifs", "delete", "99")
	if err != nil {
		t.Fatalf("notifs delete: %v", err)
	}
	assertLastMethod(t, srv, "DELETE", "/notification-channels/99")
}

// ---------- dry-run mode ----------

func TestDryRunDoesNotWriteAuditFile(t *testing.T) {
	srv := newCrudServer(t)
	dir := t.TempDir()
	outDir := filepath.Join(dir, "audit")

	t.Setenv("PULSETIC_CLI_OUTPUT_DIR", "")
	t.Setenv("PULSETIC_CLI_SINCE", "")
	t.Setenv("PULSETIC_CLI_BASE_URL", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("PULSETIC_API_TOKEN", "test-token")
	t.Setenv("PULSETIC_CLI_BASE_URL", srv.URL+"/api/public")
	t.Setenv("HOME", dir)

	root := NewRootCmd("test")
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"monitors", "list", "--output", outDir, "--dry-run", "--since", "1h"})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := root.ExecuteContext(ctx); err != nil {
		t.Fatalf("dry-run: %v", err)
	}

	// Output directory should not exist because dry-run skips the writer.
	if _, err := os.Stat(outDir); err == nil {
		t.Fatal("dry-run should not create audit directory")
	}

	// Stdout should contain the API response body.
	if !strings.Contains(out.String(), `"data"`) {
		t.Fatalf("dry-run should print JSON to stdout, got: %s", out.String())
	}
}

// ---------- audit chain integrity after write operations ----------

func TestWriteOperationsProduceValidAuditChain(t *testing.T) {
	srv := newCrudServer(t)
	dir := t.TempDir()
	outDir := filepath.Join(dir, "audit")

	t.Setenv("PULSETIC_CLI_OUTPUT_DIR", "")
	t.Setenv("PULSETIC_CLI_SINCE", "")
	t.Setenv("PULSETIC_CLI_BASE_URL", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("PULSETIC_API_TOKEN", "test-token")
	t.Setenv("PULSETIC_CLI_BASE_URL", srv.URL+"/api/public")
	t.Setenv("HOME", dir)

	// Execute several different write operations in sequence, all
	// writing to the same audit file.
	commands := [][]string{
		{"monitors", "create", "--data", `{"urls":["https://a.example"]}`},
		{"monitors", "update", "1", "--data", `{"name":"A"}`},
		{"domains", "create", "--data", `{"domains":["b.example"]}`},
		{"heartbeats", "create", "--data", `{"name":"HB"}`},
		{"monitors", "delete", "1"},
	}

	for _, args := range commands {
		root := NewRootCmd("test")
		root.SetArgs(append(args, "--output", outDir, "--since", "1h"))
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		if err := root.ExecuteContext(ctx); err != nil {
			cancel()
			t.Fatalf("%v: %v", args, err)
		}
		cancel()
	}

	// Find the audit file and verify the full chain.
	now := time.Now().UTC()
	auditFile := filepath.Join(outDir, fmt.Sprintf("pulsetic-%04d-%02d.jsonl", now.Year(), int(now.Month())))
	res, err := audit.Verify(auditFile)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !res.OK() {
		t.Fatalf("chain broken after write ops: %+v", res)
	}
	if res.Records != 5 {
		t.Fatalf("want 5 records from 5 write ops, got %d", res.Records)
	}
}

// ---------- error handling ----------

func TestMissingTokenReturnsError(t *testing.T) {
	t.Setenv("PULSETIC_CLI_OUTPUT_DIR", "")
	t.Setenv("PULSETIC_CLI_SINCE", "")
	t.Setenv("PULSETIC_CLI_BASE_URL", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("PULSETIC_API_TOKEN", "")
	t.Setenv("HOME", t.TempDir())

	root := NewRootCmd("test")
	root.SetArgs([]string{"monitors", "list"})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := root.ExecuteContext(ctx)
	if err == nil {
		t.Fatal("expected error when token is missing")
	}
	if !strings.Contains(err.Error(), "PULSETIC_API_TOKEN") {
		t.Fatalf("error should mention PULSETIC_API_TOKEN, got: %v", err)
	}
}

func TestCreateWithoutDataReturnsError(t *testing.T) {
	srv := newCrudServer(t)
	_, err := runCmdDryRun(t, srv, "monitors", "create")
	if err == nil {
		t.Fatal("expected error when --data is missing for create")
	}
	if !strings.Contains(err.Error(), "--data") {
		t.Fatalf("error should mention --data, got: %v", err)
	}
}

func TestInvalidIDReturnsError(t *testing.T) {
	srv := newCrudServer(t)
	_, err := runCmdDryRun(t, srv, "monitors", "get", "notanumber")
	if err == nil {
		t.Fatal("expected error for invalid ID")
	}
	if !strings.Contains(err.Error(), "invalid id") {
		t.Fatalf("error should mention invalid id, got: %v", err)
	}
}

// ---------- auth token format ----------

// ---------- multi-page snapshot pagination ----------

func TestSnapshotPaginatesSnapshots(t *testing.T) {
	// Build a server that returns exactly snapshotPerPage items on page 1
	// and fewer on page 2 for the /monitors/{id}/snapshots endpoint.
	// This verifies the pagination loop makes multiple calls.
	snapshotCalls := 0
	mux := http.NewServeMux()

	mux.HandleFunc("/api/public/monitors", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":1}]}`))
	})
	mux.HandleFunc("/api/public/monitors/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Only the snapshots endpoint returns paginated data.
		if strings.Contains(r.URL.Path, "/snapshots") {
			snapshotCalls++
			page := r.URL.Query().Get("page")
			if page == "1" {
				// Return exactly snapshotPerPage items so the loop continues.
				items := make([]map[string]int, snapshotPerPage)
				for i := range items {
					items[i] = map[string]int{"id": i + 1}
				}
				b, _ := json.Marshal(map[string]any{"data": items})
				_, _ = w.Write(b)
				return
			}
			// Page 2: return fewer items so the loop stops.
			_, _ = w.Write([]byte(`{"data":[{"id":999}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":[]}`))
	})
	mux.HandleFunc("/api/public/status-page", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[]}`))
	})
	mux.HandleFunc("/api/public/domains", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[]}`))
	})
	mux.HandleFunc("/api/public/heartbeats", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[]}`))
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	outDir := filepath.Join(dir, "audit")
	t.Setenv("PULSETIC_API_TOKEN", "test-token")
	t.Setenv("PULSETIC_CLI_BASE_URL", srv.URL+"/api/public")
	t.Setenv("HOME", dir)

	root := NewRootCmd("test")
	root.SetArgs([]string{"snapshot", "--output", outDir, "--since", "1h"})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := root.ExecuteContext(ctx); err != nil {
		t.Fatalf("snapshot: %v", err)
	}

	// The snapshots endpoint should have been called twice (page 1 + page 2).
	if snapshotCalls != 2 {
		t.Fatalf("snapshot pagination: want 2 calls, got %d", snapshotCalls)
	}

	// Verify the audit file is valid.
	now := time.Now().UTC()
	auditFile := filepath.Join(outDir, fmt.Sprintf("pulsetic-%04d-%02d.jsonl", now.Year(), int(now.Month())))
	res, err := audit.Verify(auditFile)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !res.OK() {
		t.Fatalf("chain broken: %+v", res)
	}
	// Expected: 1 monitor list + 2 snapshot pages + 1 events + 1 downtime +
	// 1 stats + 1 notif-channels + 1 status-page list + 1 domains + 1 heartbeats = 10
	if res.Records != 10 {
		t.Fatalf("want 10 records, got %d", res.Records)
	}
}

// ---------- auth token format ----------

func TestAuthTokenSentWithoutBearerPrefix(t *testing.T) {
	srv := newCrudServer(t)
	_, err := runCmdDryRun(t, srv, "monitors", "list")
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range srv.allRequests() {
		if r.Auth != "test-token" {
			t.Fatalf("auth header should be raw token %q, got %q", "test-token", r.Auth)
		}
	}
}

// ---------- --json flag ----------

func TestJSONFlagMonitorsList(t *testing.T) {
	srv := newCrudServer(t)
	res, err := runCmdSplit(t, srv, "monitors", "list", "--json", "--dry-run")
	if err != nil {
		t.Fatalf("monitors list --json: %v", err)
	}
	var env jsonEnvelope
	if err := json.Unmarshal([]byte(res.Stdout), &env); err != nil {
		t.Fatalf("parse JSON envelope: %v\nstdout: %s", err, res.Stdout)
	}
	if !env.OK {
		t.Fatalf("expected ok=true, got: %+v", env)
	}
	if env.Records < 1 {
		t.Fatalf("expected at least 1 record, got %d", env.Records)
	}
	if len(env.Data) < 1 {
		t.Fatalf("expected at least 1 data entry, got %d", len(env.Data))
	}
	// Data should contain parseable JSON.
	var item map[string]any
	if err := json.Unmarshal(env.Data[0], &item); err != nil {
		t.Fatalf("data[0] not valid JSON: %v", err)
	}
}

func TestJSONFlagMonitorsCreate(t *testing.T) {
	srv := newCrudServer(t)
	res, err := runCmdSplit(t, srv, "monitors", "create",
		"--data", `{"urls":["https://example.com"]}`, "--json", "--dry-run")
	if err != nil {
		t.Fatalf("monitors create --json: %v", err)
	}
	var env jsonEnvelope
	if err := json.Unmarshal([]byte(res.Stdout), &env); err != nil {
		t.Fatalf("parse: %v\nstdout: %s", err, res.Stdout)
	}
	if !env.OK {
		t.Fatal("expected ok=true")
	}
	if env.Records != 1 {
		t.Fatalf("expected 1 record, got %d", env.Records)
	}
	if env.Command != "monitors.create" {
		t.Fatalf("command: want monitors.create, got %q", env.Command)
	}
}

func TestJSONFlagDoesNotPrintRawBodyToStdout(t *testing.T) {
	// With --json, dry-run should NOT print raw JSON bodies line by line.
	// Only the final envelope should appear on stdout.
	srv := newCrudServer(t)
	res, err := runCmdSplit(t, srv, "monitors", "list", "--json", "--dry-run")
	if err != nil {
		t.Fatal(err)
	}
	// Stdout should be a single JSON object (the envelope), not multiple lines.
	lines := strings.Split(strings.TrimSpace(res.Stdout), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 line (envelope), got %d lines:\n%s", len(lines), res.Stdout)
	}
}

func TestJSONFlagVerifyOK(t *testing.T) {
	// Create a valid audit file, then verify with --json.
	dir := t.TempDir()
	path := filepath.Join(dir, "a.jsonl")
	w, err := audit.OpenWriter(path, audit.Actor{Tool: "t", Version: "0", Host: "h"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	req := audit.Request{Method: "GET", Path: "/test", Query: map[string]string{}}
	resp := audit.Response{Status: 200, DurationMS: 1, BodySHA256: "x", Body: json.RawMessage(`{}`)}
	if _, err := w.Append("t", req, resp); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	root := NewRootCmd("test")
	var stdout bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"verify", path, "--json"})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := root.ExecuteContext(ctx); err != nil {
		t.Fatal(err)
	}

	var v map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &v); err != nil {
		t.Fatalf("parse: %v\nstdout: %s", err, stdout.String())
	}
	if v["ok"] != true {
		t.Fatalf("expected ok=true: %v", v)
	}
	if v["records"].(float64) != 1 {
		t.Fatalf("expected 1 record: %v", v)
	}
}

// ---------- --quiet flag ----------

func TestQuietFlagSuppressesStderrProgress(t *testing.T) {
	srv := newCrudServer(t)
	res, err := runCmdSplit(t, srv, "monitors", "list", "--quiet")
	if err != nil {
		t.Fatalf("monitors list --quiet: %v", err)
	}
	// Stderr should have no progress or summary output.
	if res.Stderr != "" {
		t.Fatalf("--quiet should suppress stderr, got: %q", res.Stderr)
	}
}

func TestQuietAndJSONTogether(t *testing.T) {
	srv := newCrudServer(t)
	res, err := runCmdSplit(t, srv, "monitors", "list", "--json", "--quiet", "--dry-run")
	if err != nil {
		t.Fatal(err)
	}
	// Stderr should be empty.
	if res.Stderr != "" {
		t.Fatalf("--quiet should suppress stderr, got: %q", res.Stderr)
	}
	// Stdout should have the JSON envelope.
	var env jsonEnvelope
	if err := json.Unmarshal([]byte(res.Stdout), &env); err != nil {
		t.Fatalf("parse: %v\nstdout: %s", err, res.Stdout)
	}
	if !env.OK {
		t.Fatal("expected ok=true")
	}
}

// ---------- --json pipeline patterns ----------

func TestJSONOutputIsPipelineCompatible(t *testing.T) {
	// Verifies the envelope can be piped to jq-style extraction:
	// pulsetic-cli monitors list --json | jq '.data[0]'
	srv := newCrudServer(t)
	res, err := runCmdSplit(t, srv, "domains", "list", "--json", "--dry-run")
	if err != nil {
		t.Fatal(err)
	}
	var env jsonEnvelope
	if err := json.Unmarshal([]byte(res.Stdout), &env); err != nil {
		t.Fatal(err)
	}
	// Simulate jq '.data[0].data[0].domain' - the envelope's data holds
	// full API responses, each of which has its own "data" array.
	if len(env.Data) < 1 {
		t.Fatal("expected at least 1 data item")
	}
	// Parse the first API response page.
	var page map[string]any
	if err := json.Unmarshal(env.Data[0], &page); err != nil {
		t.Fatalf("data[0] not parseable: %v", err)
	}
	items, ok := page["data"].([]any)
	if !ok || len(items) < 1 {
		t.Fatalf("expected inner data array, got: %v", page)
	}
	domain, ok := items[0].(map[string]any)
	if !ok {
		t.Fatalf("expected object, got: %v", items[0])
	}
	if domain["domain"] != "example.com" {
		t.Fatalf("expected example.com, got %v", domain["domain"])
	}
}

// ---------- --format flag ----------

func TestFormatJSONL(t *testing.T) {
	// Default format: each API response body printed as one JSONL line.
	srv := newCrudServer(t)
	res, err := runCmdSplit(t, srv, "monitors", "list", "--dry-run")
	if err != nil {
		t.Fatal(err)
	}
	// Should have at least one line of JSON on stdout.
	lines := strings.Split(strings.TrimSpace(res.Stdout), "\n")
	if len(lines) < 1 || lines[0] == "" {
		t.Fatalf("expected JSONL output, got: %q", res.Stdout)
	}
	// Each line should be valid JSON.
	for i, line := range lines {
		var v any
		if err := json.Unmarshal([]byte(line), &v); err != nil {
			t.Fatalf("line %d not valid JSON: %v\nline: %s", i, err, line)
		}
	}
}

func TestFormatStdoutPrettyPrints(t *testing.T) {
	srv := newCrudServer(t)
	res, err := runCmdSplit(t, srv, "monitors", "get", "42", "--format=stdout", "--dry-run")
	if err != nil {
		t.Fatal(err)
	}
	// Pretty-printed JSON should have newlines and indentation.
	if !strings.Contains(res.Stdout, "\n  ") {
		t.Fatalf("expected indented JSON, got: %s", res.Stdout)
	}
}

func TestFormatCSV(t *testing.T) {
	srv := newCrudServer(t)
	res, err := runCmdSplit(t, srv, "domains", "list", "--format=csv", "--dry-run")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(res.Stdout), "\n")
	if len(lines) < 2 {
		t.Fatalf("expected CSV header + at least 1 row, got %d lines:\n%s", len(lines), res.Stdout)
	}
	// First line is the header.
	header := lines[0]
	if !strings.Contains(header, "domain") {
		t.Fatalf("CSV header should contain 'domain', got: %s", header)
	}
	// Second line is data.
	if !strings.Contains(lines[1], "example.com") {
		t.Fatalf("CSV row should contain 'example.com', got: %s", lines[1])
	}
}

func TestFormatCSVEmptyResponse(t *testing.T) {
	// When the API returns no items, CSV should produce no output (no header).
	srv := newCrudServer(t)
	res, err := runCmdSplit(t, srv, "heartbeats", "get", "99", "--format=csv", "--dry-run")
	if err != nil {
		t.Fatal(err)
	}
	// The crudServer returns a single object (not an array) for get endpoints.
	// extractItems wraps it as a 1-element slice, so we should get a header + 1 row.
	if strings.TrimSpace(res.Stdout) == "" {
		t.Fatal("expected CSV output for single object response")
	}
}

func TestFormatJSONIsEnvelope(t *testing.T) {
	// --format=json should produce the same envelope as --json.
	srv := newCrudServer(t)
	res, err := runCmdSplit(t, srv, "monitors", "list", "--format=json", "--dry-run")
	if err != nil {
		t.Fatal(err)
	}
	var env jsonEnvelope
	if err := json.Unmarshal([]byte(res.Stdout), &env); err != nil {
		t.Fatalf("not a valid JSON envelope: %v\nstdout: %s", err, res.Stdout)
	}
	if !env.OK {
		t.Fatal("expected ok=true")
	}
	if env.Records < 1 {
		t.Fatalf("expected at least 1 record, got %d", env.Records)
	}
}

func TestFormatInvalid(t *testing.T) {
	srv := newCrudServer(t)
	_, err := runCmdSplit(t, srv, "monitors", "list", "--format=xml", "--dry-run")
	if err == nil {
		t.Fatal("expected error for unsupported format")
	}
	if !strings.Contains(err.Error(), "unsupported value") {
		t.Fatalf("error should mention unsupported value, got: %v", err)
	}
}
