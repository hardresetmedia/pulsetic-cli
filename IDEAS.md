# Feature ideas for pulsetic-cli

A living document of potential features, with honest tradeoffs. Organized by category, roughly ordered by impact within each section.

---

## Data analysis and reporting

### SLA report generator

Generate a compliance-ready report (HTML/CSV) showing per-monitor uptime percentages, response time percentiles, incident timelines, and SLA threshold violations. Reference audit log hashes for legal evidence.

**Pros:** Directly answers "did we meet our SLA?" without manual spreadsheet work. The hash chain makes the report provably derived from untampered data - a differentiator no competing tool has. High value for compliance officers and account managers.

**Cons:** Report formatting is a rabbit hole (PDF generation in Go is painful, HTML templates are simpler but less polished). Different customers want different SLA definitions (99.9% monthly vs 99.95% rolling). Requires opinionated defaults that won't fit everyone.

**Effort:** Medium. **Benefits:** Compliance, account management.

### Snapshot diff tool

Compare two audit files (or two time ranges within one file) and show what changed: monitors added/removed, config changes, uptime deltas, new incidents.

**Pros:** Answers "what changed since yesterday?" which is the core compliance question. Useful for incident post-mortems ("what was different before the outage?"). The hash chain proves both snapshots are authentic.

**Cons:** Diffing nested JSON is non-trivial - need to decide what counts as a "meaningful" change vs noise (e.g. response time varying by 2ms). Output format is hard to get right for both humans and scripts.

**Effort:** Medium. **Benefits:** SRE, compliance.

### Anomaly detection

Flag unusual patterns: response time spikes, uptime drops, notification channel changes (someone removed an alert?), SSL certificates expiring soon. Compare against recent baselines.

**Pros:** Turns passive recording into active monitoring of the monitoring. Catches "someone silently removed the on-call email" which is a real failure mode.

**Cons:** Statistical baselines are hard to get right without false positives. Thresholds that work for one account don't work for another. Could become a maintenance burden.

**Effort:** Medium-high. **Benefits:** On-call engineers, security.

### Trend visualization

Track uptime and response time trends over weeks/months from audit data. Output sparklines to terminal or export as embeddable JSON for status pages.

**Pros:** The data is already captured; this just queries it. Terminal sparklines are cool and zero-dependency.

**Cons:** Limited value beyond what Pulsetic's own dashboard shows. Better as a "nice to have" than a primary feature.

**Effort:** Low. **Benefits:** Ops dashboards.

---

## Operational tooling

### Audit log rotation and pruning

Roll old audit files, compress with gzip/zstd, archive to cloud storage. Configurable retention policy (e.g. 30 days hot, 1 year archived). Maintain chain integrity across file boundaries by storing the last hash of each rotated file.

**Pros:** Without this, audit files grow forever. Production deployments need lifecycle management. The cross-file chain linking is a unique integrity feature.

**Cons:** Adds complexity around file naming conventions, archive formats, and cloud storage credentials. The cross-file hash linking needs careful design to avoid breaking verification.

**Effort:** Medium. **Benefits:** Ops, storage management.

### Config validation command

`pulsetic-cli config validate` - checks config file syntax, output directory is writable, token format looks reasonable (without hitting the API), timeouts are sane.

**Pros:** Catches misconfigurations before they cause a failed cron run at 2am. Very low effort to build.

**Cons:** Limited scope - `test` command already validates the token against the live API. This mainly catches TOML syntax errors and permission issues.

**Effort:** Low. **Benefits:** All users.

### Scheduled snapshot manager

Define multiple snapshot schedules in config (hourly for critical monitors, daily for everything). Generate cron/systemd files automatically.

**Pros:** Makes the "set up cron" step from the quickstart automatic. `pulsetic-cli systemd generate` is very appealing for Linux admins.

**Cons:** The tool is already cron-friendly. Generating systemd units is straightforward but testing them across distros is not. Risk of over-engineering what a 2-line cron entry already solves.

**Effort:** Low-medium. **Benefits:** Linux admins.

---

## Security and compliance

### Cryptographic signatures

Sign each audit file (or each record) with RSA/ECDSA. Private key per environment. Verify with public key. Creates non-repudiation - proves "this specific server produced this audit at this time."

**Pros:** Upgrades tamper detection (hash chain) to tamper proof (signatures). Meets stricter compliance requirements (SOC 2, ISO 27001). The plan already scoped this as a future tier above hash chaining.

**Cons:** Key management is the hard part - where to store the private key, how to rotate it, what happens when it's lost. Adds operational complexity for a feature many users won't need.

**Effort:** Medium. **Benefits:** Compliance, regulated industries.

### Remote attestation

Submit the latest audit hash to an external timestamping authority (RFC 3161), a blockchain testnet, or even a public git repo. Creates third-party proof the audit existed at time T.

**Pros:** Irrefutable evidence for legal proceedings. "We can prove this audit file existed on April 12 because the hash was recorded on-chain at block N."

**Cons:** Adds external dependencies (blockchain nodes, timestamping services). Cost if using mainnet. Niche audience - most users don't need legal-grade proof.

**Effort:** High. **Benefits:** Legal, regulated industries.

### Token rotation reminder

Warn if the same API token has been generating records for more than a configurable period (default 90 days). Print a reminder to stderr during snapshot.

**Pros:** Trivial to implement (check first record's timestamp vs now). Good security hygiene.

**Cons:** Pulsetic may not support token rotation easily. Could be annoying if there's no action the user can take.

**Effort:** Low. **Benefits:** Security hygiene.

### Immutable storage mode

Option to write audit logs directly to append-only cloud storage (S3 Object Lock, GCS Retention Policy). Prevents deletion even by administrators.

**Pros:** Physical tamper prevention on top of logical tamper detection. Required by some compliance frameworks.

**Cons:** Requires cloud SDK dependencies (aws-sdk-go, google-cloud-go). Increases binary size significantly. Only useful for users with cloud storage.

**Effort:** Medium. **Benefits:** Regulated industries.

---

## Integration and alerting

### Webhook on chain break

If `verify` detects tampering, POST a JSON payload to a user-configured webhook URL. Works with Slack incoming webhooks, PagerDuty, OpsGenie, or any HTTP endpoint.

**Pros:** Extremely low effort, high value. Turns passive verification into active alerting. Natural extension of the existing verify command.

**Cons:** Requires storing a webhook URL in config (security consideration). Needs retry logic for transient webhook failures.

**Effort:** Low. **Benefits:** Incident response, ops.

### Prometheus metrics exporter

Expose metrics via HTTP: `pulsetic_records_total`, `pulsetic_snapshot_duration_seconds`, `pulsetic_api_errors_total`, `pulsetic_chain_valid`. Scrape with Prometheus/Grafana.

**Pros:** Integrates with 80%+ of ops monitoring stacks. Makes the CLI observable. Natural fit for teams already using Prometheus.

**Cons:** Requires a long-running HTTP server or pushgateway integration, which conflicts with the CLI's one-shot design. Either run as a sidecar or push metrics post-snapshot.

**Effort:** Medium. **Benefits:** Ops teams with Prometheus.

### SIEM export

Export audit records in Splunk HEC, DataDog, or ELK-compatible formats. Ship records to a SIEM for centralized security monitoring.

**Pros:** Enterprise customers already have SIEMs. Feeding audit data there creates a unified security view.

**Cons:** Each SIEM has its own format and auth. Supporting even 3 platforms is significant work. Better to document jq recipes for format conversion than build native integrations.

**Effort:** High. **Benefits:** Security teams.

### Slack/email digest

Send a daily summary to Slack or email: snapshot status, any failed API calls, uptime changes, chain integrity. Configurable recipients.

**Pros:** Proactive visibility without logging into anything. Management loves daily digests.

**Cons:** Email requires SMTP config or a third-party service. Slack requires webhook setup. Both add config complexity. Overlaps with what a cron job + webhook can already do.

**Effort:** Medium. **Benefits:** Management, ops.

---

## Developer experience

### Shell completion

Generate and install bash/zsh/fish completion scripts. Cobra already has this built in - just needs to be documented and tested.

**Pros:** Already built into cobra. Just need `pulsetic-cli completion bash > /etc/bash_completion.d/pulsetic-cli`. Huge usability win for power users.

**Cons:** Fish and zsh completion have edge cases. Installation varies by OS and shell. Documentation is the real work, not code.

**Effort:** Low (docs only - cobra does the work). **Benefits:** All CLI users.

### Plugin system

Allow users to write plugins (Go plugins or shell scripts) that hook into the snapshot lifecycle: `before_snapshot`, `after_record`, `after_snapshot`, `on_verify_failure`.

**Pros:** Unlocks unlimited extensibility. Users can add custom analysis, alerting, formatting without forking the repo.

**Cons:** Go's plugin system is platform-limited (Linux/macOS only, no Windows, same Go version required). Shell script hooks are simpler but less powerful. High design effort to get the API right.

**Effort:** High. **Benefits:** Advanced users, integrators.

### Man pages

Auto-generate man pages from cobra command tree. Install via package manager.

**Pros:** Unix convention. `man pulsetic-cli` is expected by experienced admins.

**Cons:** Only useful on Unix systems. Most users will use `--help` instead. Low ROI unless distributing via APT/RPM.

**Effort:** Low (cobra-doc generates them). **Benefits:** Unix admins.

---

## Performance

### Incremental snapshots

Only re-capture monitors whose data changed since the last run. Track last-seen state hash per monitor. Skip unchanged monitors entirely.

**Pros:** Could reduce snapshot time by 80%+ for stable environments. Less rate-limit pressure.

**Cons:** Defeats the audit purpose - the whole point is to record the state at each point in time, even if unchanged. An auditor needs "we checked and it was still 100%" not "we assumed it didn't change." Fundamentally conflicts with the tool's design goal.

**Effort:** Medium-high. **Benefits:** Large accounts. **Risk:** Undermines audit completeness.

### Response caching

Cache GET responses with a configurable TTL. Useful for commands like `status` that hit the same endpoints repeatedly.

**Pros:** Makes `status` faster on subsequent runs. Reduces API calls for interactive use.

**Cons:** Stale data is worse than slow data for a monitoring tool. Cache invalidation bugs would silently show old status. Must never cache in snapshot mode.

**Effort:** Medium. **Benefits:** Interactive users. **Risk:** Stale data.

---

## Distribution and packaging

### Homebrew formula

Create a Homebrew tap: `brew install hardresetmedia/tap/pulsetic-cli`.

**Pros:** Standard macOS install path. Easy to maintain with goreleaser.

**Cons:** Only benefits macOS users. Requires maintaining a separate tap repo.

**Effort:** Low. **Benefits:** macOS users.

### Docker image

Official Docker image with the binary pre-installed. Can be used as a cron base image.

**Pros:** Standard for CI/CD pipelines. `docker run pulsetic-cli snapshot` is clean. Pairs well with Kubernetes CronJobs.

**Cons:** Image maintenance, versioning, registry costs. The binary is already statically linked and doesn't need Docker.

**Effort:** Low. **Benefits:** Container-first teams.

### APT/DEB/RPM packages

Package for major Linux distributions via goreleaser + nfpm.

**Pros:** `apt install pulsetic-cli` is the gold standard for Linux servers.

**Cons:** Testing across distros is a pain. Repo hosting costs. Most Go users prefer direct binary downloads anyway.

**Effort:** Low-medium. **Benefits:** Linux server admins.

---

## Hash chain differentiators

These features specifically leverage the unique hash-chained audit log.

### Merkle tree proofs

Build a Merkle tree over audit records. Generate compact proofs that "record N exists and is unmodified" without sharing the entire file.

**Pros:** Privacy-preserving audits. Share proof of a single record without exposing all records. Useful for multi-tenant scenarios.

**Cons:** Complex to implement correctly. Niche audience. Most auditors are fine receiving the full file.

**Effort:** High. **Benefits:** Multi-tenant compliance.

### Portable chain export

Export the audit chain as a self-contained SQLite database with a verification query built in. Share with auditors who don't have Go installed.

**Pros:** Auditors can verify integrity using any SQLite client. No tooling required on their end. Self-documenting schema.

**Cons:** SQLite adds a dependency or requires CGo. The JSONL + verify binary approach is already portable.

**Effort:** Medium. **Benefits:** Auditors without Go.

### Time-travel queries

Query "what was the state of monitor X at timestamp T?" by walking the audit log backward from the most recent record.

**Pros:** Answers "what did our monitoring look like when the incident started?" directly. Natural extension of `log query`.

**Cons:** Requires scanning potentially large files. The audit log stores API responses, not derived state, so reconstructing "monitor X's config at time T" requires finding the right snapshot record.

**Effort:** Medium. **Benefits:** Incident investigators.

### Chain fork detection

If two audit files claim the same sequence range but different content, detect the fork and identify which is authentic via external timestamps.

**Pros:** Catches sophisticated tampering where someone replaces the entire file with a re-hashed version.

**Cons:** Only relevant if an attacker has full write access to the audit storage. Remote attestation (above) is a better solution to this threat model.

**Effort:** High. **Benefits:** Paranoid security teams.

---

## Priority matrix

Rough ordering if building these over time:

### Quick wins (days)
- Shell completion (docs only)
- Config validation command
- Token rotation reminder
- Webhook on chain break

### Medium term (weeks)
- SLA report generator
- Snapshot diff tool
- Audit log rotation
- Cryptographic signatures
- Prometheus metrics
- Homebrew + Docker

### Long term (months)
- Plugin system
- Remote attestation
- Interactive TUI
- SIEM export
- Merkle tree proofs
