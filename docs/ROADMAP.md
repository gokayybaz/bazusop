# Delivery roadmap

Every spike follows the same delivery loop:

1. Write an executable acceptance test and observe it fail.
2. Implement the smallest coherent vertical slice.
3. Refactor while keeping the suite green.
4. Run tests and production builds.
5. Commit and push the completed spike.

## Spikes

| Spike | Outcome | Acceptance signal |
| --- | --- | --- |
| 0 — Foundation | Go hub, embedded React shell and CI-ready commands | Health API and product shell tests pass; one hub binary is produced |
| 1 — Enrollment | Linux and Windows agents securely enroll with the hub | One-time token becomes a renewable agent identity |
| 2 — Inventory | Agents report normalized host and operating-system facts | Enrolled hosts appear in the instance inventory |
| 3 — Telemetry | CPU, memory, disk and network samples reach TimescaleDB | Instance detail renders current and historical metrics |
| 4 — Services | systemd and Windows Service state is collected | Services can be filtered and inspected per instance |
| 5 — Logs | journald, file and Windows Event logs are searchable | Live tail and bounded historical search work |
| 6 — Jobs | Approved operational actions run through signed jobs | Restart/reboot job has streamed output and a complete audit trail |
| 7 — Alerting | Metric and availability rules create managed incidents | Alert lifecycle and maintenance windows are testable |
| 8 — Cloud discovery | AWS, Azure and GCP inventory reconciles with agents | Provider instances and agent identities are linked safely |
| 9 — Scale and release | Role-split hub, retention, packaging and upgrades | Load targets pass; deb/rpm/MSI/container artifacts are signed |

## Architectural constraints

- Agents initiate outbound connections; no inbound agent port is required.
- Agent identity uses mTLS after one-time enrollment.
- The hub is a modular monolith and serves the embedded React application.
- PostgreSQL owns relational state; TimescaleDB owns time-series samples.
- Remote actions are allowlisted, attributable and auditable by default.

