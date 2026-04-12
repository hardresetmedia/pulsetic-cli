# pulsetic-cli

A Go CLI that queries [Pulsetic's API](https://pulsetic.com) and records every response to an append-only, SHA-256 hash-chained JSONL audit log. Designed for SLA compliance, incident evidence, and operational auditing.

Every API call the tool makes - read or write - produces a tamper-detectable audit record.

## Install

```bash
go install github.com/program-the-brain-not-the-heartbeat/pulsetic-cli/cmd/pulsetic-cli@latest
```

Or build from source:

```bash
git clone https://github.com/program-the-brain-not-the-heartbeat/pulsetic-cli.git
cd pulsetic-cli
go build -o pulsetic-cli ./cmd/pulsetic-cli
```

## Quick start

```bash
# Set your API token (get it from https://app.pulsetic.com/settings/api)
export PULSETIC_API_TOKEN=your_token_here

# Capture a full audit snapshot (all monitors, status pages, domains, heartbeats)
pulsetic-cli snapshot

# Verify the audit log hasn't been tampered with
pulsetic-cli verify

# Query specific resources
pulsetic-cli monitors list
pulsetic-cli monitors history 4172 --since=7d
pulsetic-cli domains list
pulsetic-cli heartbeats list
```

## Configuration

### API token

The token is always loaded from the `PULSETIC_API_TOKEN` environment variable. It is never stored in config files or written to audit logs.

### Config file

Optional. Place at `~/.config/pulsetic-cli/config.toml` (or `$XDG_CONFIG_HOME/pulsetic-cli/config.toml`):

```toml
# API token comes from PULSETIC_API_TOKEN env var only. Never stored here.

[output]
dir = "./audit"
file_pattern = "pulsetic-{YYYY}-{MM}.jsonl"

[defaults]
since = "24h"

[client]
timeout = "30s"
retry_max = 3
retry_backoff = "500ms"
```

### Precedence

Settings are resolved in this order (later wins):

1. Compiled defaults
2. Config file
3. Environment variables (`PULSETIC_CLI_OUTPUT_DIR`, `PULSETIC_CLI_SINCE`, `PULSETIC_CLI_BASE_URL`)
4. CLI flags (`--output`, `--since`, `--until`, `--config`)

The API token is env-only (`PULSETIC_API_TOKEN`).

### Global flags

| Flag | Description |
|---|---|
| `--config` | Path to TOML config file |
| `--output` | Override output directory |
| `--since` | Time range start (e.g. `24h`, `7d`, `2026-04-01T00:00:00Z`) |
| `--until` | Time range end (default: now) |
| `--dry-run` | Print API responses to stdout, don't write audit records |

## Commands

### snapshot

Capture a complete audit snapshot. This is the primary command for scheduled runs.

```bash
# Default: last 24 hours
pulsetic-cli snapshot

# Custom time range
pulsetic-cli snapshot --since=7d
pulsetic-cli snapshot --since=2026-04-01T00:00:00Z --until=2026-04-07T00:00:00Z

# Write to a specific directory
pulsetic-cli snapshot --output=/var/audit/pulsetic

# Preview without writing records
pulsetic-cli snapshot --dry-run
```

The snapshot captures:
- All monitors (paginated) + per-monitor snapshots, events, downtime, stats, and notification channels
- All status pages + their incidents
- All domains (SSL/expiry monitoring)
- All heartbeats (cron/job monitoring)

### monitors

```bash
# List all monitors
pulsetic-cli monitors list

# Get a single monitor's configuration
pulsetic-cli monitors get 4172

# Capture uptime history for a monitor
pulsetic-cli monitors history 4172 --since=30d

# Include individual checks (can be very large)
pulsetic-cli monitors history 4172 --since=24h --include-checks

# Create monitors
pulsetic-cli monitors create --data '{
  "urls": ["https://example.com", "https://api.example.com"],
  "add_account_email": true
}'

# Update a monitor
pulsetic-cli monitors update 4172 --data '{
  "url": "https://example.com",
  "name": "Production Site",
  "uptime_check_frequency": "300",
  "request": {
    "type": "http",
    "method": "get",
    "headers": [{"name": "Accept", "value": "application/json"}]
  }
}'

# Delete a monitor
pulsetic-cli monitors delete 4172
```

### status-pages

```bash
# List all status pages
pulsetic-cli status-pages list

# Get a status page's detail and incidents
pulsetic-cli status-pages get 13

# Create a status page
pulsetic-cli status-pages create --data '{
  "title": "My Status Page",
  "monitors": [1, 2],
  "subscribe_to_updates": true,
  "show_location_tooltip": true
}'

# Update a status page
pulsetic-cli status-pages update 13 --data '{"title": "Updated Status Page"}'

# Delete a status page
pulsetic-cli status-pages delete 13
```

#### Maintenance windows

```bash
# Create a maintenance window
pulsetic-cli status-pages maintenance create 13 --data '{
  "name": "Scheduled Upgrade",
  "description": "Database migration",
  "monitors": [1, 2],
  "date": {"starting": "2026-05-01", "ending": "2026-05-01"},
  "time": {"starting": "02:00 AM", "ending": "04:00 AM"},
  "timezone": {"value": "Eastern Standard Time", "offset": -5}
}'

# Update a maintenance window
pulsetic-cli status-pages maintenance update 8 --data '{"name": "Extended Upgrade"}'

# Delete a maintenance window
pulsetic-cli status-pages maintenance delete 8
```

#### Incidents

```bash
# List incidents for a status page
pulsetic-cli status-pages incidents list 13

# Create an incident
pulsetic-cli status-pages incidents create 13 --data '{
  "title": "API Degradation",
  "update": {
    "status": "exploring",
    "message": "Investigating increased error rates"
  }
}'

# Update an incident
pulsetic-cli status-pages incidents update 30 --data '{"title": "API Degradation - Resolved"}'

# Add a status update to an incident
pulsetic-cli status-pages incidents add-update 30 --data '{
  "status": "identified",
  "message": "Root cause identified: database connection pool exhaustion",
  "date": "2026-04-12 14:30:00"
}'

# Edit an incident update
pulsetic-cli status-pages incidents edit-update 57 --data '{
  "status": "resolved",
  "message": "Connection pool limits increased. Monitoring."
}'

# Delete an incident update
pulsetic-cli status-pages incidents delete-update 57

# Delete an incident
pulsetic-cli status-pages incidents delete 30
```

### incidents

Top-level shortcut that lists incidents across all status pages in one pass:

```bash
pulsetic-cli incidents list
```

### domains

```bash
# List all domains
pulsetic-cli domains list

# Add domains for SSL/expiry monitoring
pulsetic-cli domains create --data '{"domains": ["example.com", "api.example.com"]}'

# Update a domain
pulsetic-cli domains update 1 --data '{
  "alias": "Production",
  "is_active": true,
  "disable_expired_alert": false,
  "disable_expiring_soon_alert": false
}'

# Delete a domain
pulsetic-cli domains delete 1
```

### heartbeats

```bash
# List all heartbeats
pulsetic-cli heartbeats list

# Get a specific heartbeat
pulsetic-cli heartbeats get 1

# Create a heartbeat monitor
pulsetic-cli heartbeats create --data '{
  "name": "Nightly Backup",
  "monitoring_interval": 86400,
  "grace_period": 3600,
  "alert_email": true,
  "alert_sms": false
}'

# Update a heartbeat
pulsetic-cli heartbeats update 1 --data '{"name": "Nightly Backup v2", "grace_period": 7200}'

# Delete a heartbeat
pulsetic-cli heartbeats delete 1
```

### notification-channels

```bash
# List channels for a monitor
pulsetic-cli notification-channels list 4172

# Add channels (one command per type)
pulsetic-cli notifs add-email 4172 --data '{"email": "oncall@example.com"}'
pulsetic-cli notifs add-phone 4172 --data '{"phone_number": "+15551234567", "sms": true, "calls": false}'
pulsetic-cli notifs add-webhook 4172 --data '{"webhook": "https://hooks.example.com/pulsetic"}'
pulsetic-cli notifs add-slack 4172 --data '{"webhook_url": "https://hooks.slack.com/services/T.../B.../xxx"}'
pulsetic-cli notifs add-discord 4172 --data '{"webhook": "https://discord.com/api/webhooks/..."}'
pulsetic-cli notifs add-ms-teams 4172 --data '{"webhook": "https://outlook.office.com/webhook/..."}'
pulsetic-cli notifs add-signl4 4172 --data '{"signl4_webhook": "https://connect.signl4.com/..."}'

# Delete a channel
pulsetic-cli notifs delete 42
```

### verify

Replay the hash chain and report the first broken link:

```bash
# Verify today's audit file (resolved from config)
pulsetic-cli verify

# Verify a specific file
pulsetic-cli verify ./audit/pulsetic-2026-04.jsonl
```

Output on success:

```
chain OK: 142 records, last_hash=a1b2c3...
```

Output when tampering is detected (exit code 3):

```
chain broken at seq=47 line=47: record_hash mismatch: expected "a1b2..." got "f4e5..."
```

## Audit record format

Each line in the JSONL file is one record:

```json
{
  "seq": 1,
  "ts": "2026-04-12T14:30:00.123456Z",
  "actor": {
    "tool": "pulsetic-cli",
    "version": "0.1.0",
    "host": "audit-server"
  },
  "command": "snapshot.monitors.list",
  "request": {
    "method": "GET",
    "path": "/monitors",
    "query": {"page": "1", "per_page": "100"}
  },
  "response": {
    "status": 200,
    "duration_ms": 142,
    "body_sha256": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
    "body": {"data": [{"id": 1, "url": "https://example.com"}]}
  },
  "prev_hash": "",
  "record_hash": "abc123..."
}
```

### Fields

| Field | Description |
|---|---|
| `seq` | Monotonically increasing sequence number (starts at 1 per file) |
| `ts` | RFC3339Nano UTC timestamp when the record was written |
| `actor` | Tool name, version, and hostname that produced the record |
| `command` | Dot-separated command identifier (e.g. `snapshot.monitors.stats`) |
| `request` | HTTP method, path, and query params. Headers are **never** included (prevents token leaks). |
| `response.status` | HTTP status code |
| `response.duration_ms` | Total request duration including retries |
| `response.body_sha256` | SHA-256 of the raw response bytes as received from Pulsetic |
| `response.body` | The complete API response body |
| `prev_hash` | SHA-256 of the previous record (empty string for the first record in a file) |
| `record_hash` | SHA-256 of this record's canonical JSON (excluding the record_hash field itself) |

### Hash chain

Each record's `record_hash` is computed by:

1. Serialise the record to JSON
2. Remove the `record_hash` field
3. Canonicalise: sort all object keys lexicographically, remove whitespace
4. SHA-256 hash the canonical bytes
5. Hex-encode

The `prev_hash` of record N equals the `record_hash` of record N-1. This forms an append-only chain - modifying any record breaks the chain for all subsequent records.

### Security properties

- **Tamper detection**: Changing any byte in any record is detected by `verify`
- **Token safety**: The `Authorization` header is never written to any record
- **Resumable**: Closing and reopening the file continues the chain correctly
- **Evidence preservation**: API errors (4xx, 5xx) are recorded as normal records, not silently dropped

## Scheduled runs

### Cron

```bash
# Nightly at 2am UTC
0 2 * * * PULSETIC_API_TOKEN=your_token /usr/local/bin/pulsetic-cli snapshot --output=/var/audit/pulsetic
```

### Systemd timer

```ini
# /etc/systemd/system/pulsetic-audit.service
[Unit]
Description=Pulsetic audit snapshot

[Service]
Type=oneshot
Environment=PULSETIC_API_TOKEN=your_token
ExecStart=/usr/local/bin/pulsetic-cli snapshot --output=/var/audit/pulsetic
```

```ini
# /etc/systemd/system/pulsetic-audit.timer
[Unit]
Description=Run Pulsetic audit snapshot daily

[Timer]
OnCalendar=*-*-* 02:00:00 UTC
Persistent=true

[Install]
WantedBy=timers.target
```

## Reading JSON input

All write commands (`create`, `update`) accept a `--data` flag:

```bash
# Inline JSON
pulsetic-cli monitors create --data '{"urls": ["https://example.com"]}'

# From stdin
echo '{"urls": ["https://example.com"]}' | pulsetic-cli monitors create --data -

# From a file via stdin
pulsetic-cli monitors create --data - < monitor.json
```

## Exit codes

| Code | Meaning |
|---|---|
| 0 | Success |
| 1 | API error, I/O error, or general failure |
| 2 | Usage/config error (bad flags, missing token) |
| 3 | Chain verification failure (tamper detected) |

## Time format

The `--since` and `--until` flags accept:

| Format | Example | Meaning |
|---|---|---|
| Go duration | `24h`, `168h`, `30m` | Now minus duration |
| Extended duration | `7d`, `30d`, `1d12h` | Now minus days (+ optional Go duration) |
| RFC3339 | `2026-04-01T00:00:00Z` | Absolute timestamp |
| `now` | `now` | Current time (default for `--until`) |

## Rate limits

Pulsetic's rate limit is `monitor_count * 3 requests/minute` (max 7000/min). The client automatically retries on 429 responses with exponential backoff (500ms, 1s, 2s) and respects `Retry-After` headers.

## Project structure

```
cmd/pulsetic-cli/main.go          Entrypoint
internal/cli/                      Cobra command tree
internal/pulsetic/                 API client (auth, backoff, pagination)
internal/audit/                    JSONL writer + SHA-256 hash chain + verify
internal/config/                   TOML + env var config loader
```

## Dependencies

- [spf13/cobra](https://github.com/spf13/cobra) - subcommand framework
- [BurntSushi/toml](https://github.com/BurntSushi/toml) - config file parsing
- Go stdlib for everything else (`net/http`, `crypto/sha256`, `encoding/json`)

## License

MIT
