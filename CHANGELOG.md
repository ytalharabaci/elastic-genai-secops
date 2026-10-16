# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

First consolidated version of three earlier projects (an Ubuntu ELK installer, an MCP-based GenAI
SOC prototype and an AI-SOC platform).

### Fixed

- Repository links, clone instructions and README badge point to the renamed repository
  (`elastic-genai-secops`), its `production` branch and the current pipeline workflow.
- README architecture diagram redrawn top to bottom in four layers; it was one row of 15 boxes
  and unreadable at GitHub's page width.

### Changed

- **Dependencies**: Python services build on `python:3.14-slim` and CI tests on 3.14;
  mcp-server moves to the MCP Python SDK 2.x (`FastMCP` → `MCPServer`, the streamable HTTP
  options are passed to `streamable_http_app`). The `elasticsearch` client stays on 8.x to match
  the 8.13 stack; Dependabot ignores its major updates until the stack upgrade.
- **CI/CD**: one DevSecOps pipeline for the `staging` → `production` flow (ADR-024), sized for the
  project (ADR-026): golangci-lint with gosec, gitleaks, CodeQL, govulncheck, pip-audit, dependency
  review and Trivy IaC on every change; Trivy image scans weekly; the e2e test on promotion PRs.
  Semgrep, the OWASP ZAP scan, SBOMs, image signing and the Scorecard workflow were removed.
- Relicensed under Apache-2.0 (ADR-020); identifiers renamed to the `esm` prefix (ADR-021).
- Product renamed from Elastic-SecOps-Mastery to **Elastic GenAI SecOps** (repository
  `elastic-genai-secops`, ADR-021). Images now publish to `ghcr.io/<owner>/elastic-genai-secops/<service>`;
  the `esm` prefix of indices, roles and subjects is unchanged.

### Added

- **SIEM profile** on three targets: Docker Compose (`deploy/compose/siem.yml`), bare-metal Ubuntu
  22.04 installer, Kubernetes with ECK 3.5 and kustomize (`lab`, `onprem`, `gke` overlays).
- **Log sources** (`integrations/sources`): Windows (Winlogbeat, WEF, Sysmon), FortiGate, Palo Alto
  (syslog and Filebeat panw), Kaspersky, syslog RFC3164/5424, Metricbeat/Heartbeat; GeoIP for firewalls.
- **Detection content**: 30 MITRE-mapped Kibana rules, ILM policy `esm-logs`, roles and users
  loaded by `content/bootstrap.sh`; allow-listed hunting queries.
- **AI triage pipeline** over NATS JetStream: detection-service, alert-gateway (correlation),
  enrichment-service, masking-service, llm-orchestrator, case-service, bff.
- **LLM providers**: mock, Ollama, OpenAI-compatible (verified with DeepSeek), Anthropic, Vertex AI;
  token budget per window and an audit record per call.
- **MCP server**: read-only, masked tools; stdio or bearer-token HTTP.
- **Outbound integrations**: Slack, Teams, webhook and e-mail notifications; TheHive and Jira
  tickets after analyst approval; AbuseIPDB and MISP threat intel for external IPs.
- **Windows endpoint rollout** via GPO startup scripts and `prepare-share.ps1` (verified downloads).
- **CI**: Go and Python lint/tests, rule and manifest validation (kubeconform with ECK schemas),
  shellcheck, image builds with Trivy, GHCR publishing from `production`.
- End-to-end tests for the pipeline and the MCP server; architecture decision records ADR-001 to ADR-023.
- **Reverse-map lifecycle** (ADR-025): case-service deletes an incident's masking map after the
  analyst review; masking-service purges maps older than `MASKING_MAP_TTL_HOURS` (default 14 days).
  mcp-server reads display tokens from the new, stateless `POST /tokens` endpoint, so viewing a case
  no longer writes plaintext back into a map.

### Security

- Git history rewritten to remove a hard-coded password, internal IP addresses and host names, and
  committed installers.
