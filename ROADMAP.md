# Roadmap

What exists today, what is known to be missing, and the features planned next. Proposals are
welcome as issues; keep the [design rules](#design-rules) in mind (masked LLM input, no automatic actions, Basic
license only).

## Design rules

Every change keeps these rules; the [ADRs](docs/architecture/adr/README.md) explain why.

1. **Deterministic core, AI as ranker.** Detection rules and correlation decide; the LLM only
   enriches, summarizes and suggests. LLM output never triggers an action; an analyst reviews every
   case, and response actions are human-approved and reversible (ADR-002).
2. **One LLM call per incident**, never per alert or log; alerts are aggregated first (ADR-001).
3. **Minimal, masked input.** Send curated aggregates and a few fields, never raw logs or full
   documents. Hosts, users, IPs and e-mails are pseudonymized before the LLM; only
   `masking-service` holds reverse maps (ADR-003, ADR-004, ADR-015).
4. **LLM input is attacker-influenced.** Data goes in a delimited block separate from instructions,
   output is schema-validated JSON, and every call is audited: masked prompt, response, model,
   tokens, latency and decision.
5. **Swappable providers.** The LLM provider and the message bus sit behind interfaces; the model
   ID lives in configuration; the queue sits in front of the LLM path (ADR-017, ADR-018).
6. **Basic license only.** Verify every Elastic feature against the free Basic tier; do not rebuild
   what Kibana already does (ADR-014).
7. **No billed LLM calls in tests or CI**; use `LLM_PROVIDER=mock` (ADR-024).
8. **Retention.** 14-day hot tier in Elasticsearch, then snapshots to GCS; regulatory retention is
   a separate setting (ADR-010).

## Status

| Area | State | Verified |
| --- | --- | --- |
| SIEM on Docker Compose (`siem.yml`) | Available | End to end (ingestion, ILM, 30 rules, alerts) |
| SIEM on bare-metal Ubuntu 22.04 | Available | Syntax and shellcheck only; not yet run on a real host |
| Kubernetes (ECK 3.5, kustomize) | Available | `lab` overlay end to end on kind; `onprem`, `gke` schema-validated |
| Log sources: Windows, FortiGate, Palo Alto, Kaspersky, syslog, Beats health | Available | Logstash config test; FortiGate, PAN-OS, syslog with sample events |
| Detection rules (30, MITRE-mapped) | Available | Import and synthetic attacks (6 rules fired) |
| AI triage pipeline (NATS, 7 services) | Available | End to end with mock LLM and DeepSeek |
| LLM providers | mock, openai-compatible verified; ollama, anthropic unit-tested; vertex untested | |
| MCP server (read-only, masked) | Available | Official MCP client against the stack |
| Notifications, ticketing, threat intel | Available | Local webhook catcher; not against real Slack/Teams/SMTP/TheHive/Jira/AbuseIPDB/MISP |
| Windows endpoint rollout (GPO) | Available | Scripts and `prepare-share.ps1` reviewed; not run in a domain yet |
| CI/CD + DevSecOps pipeline (lint, tests, SAST, SCA, IaC, secrets, e2e on promotion) | Available | `staging` → `production`, docs/operations/ci-cd.md, ADR-026 |

## Known limitations

- **Transport security:** Kibana, the bff API and Beats → Logstash run without TLS by default.
- **Kubernetes:** no NetworkPolicies, Ingress or HPA; `detection-service` must stay at one replica.
- **Snapshots:** off by default outside `gke`; enable them per target (docs/components/elasticsearch.md).
- **Logstash API** runs without TLS inside Kubernetes (ECK 3.5 + Logstash 8.13 TLS API issue).
- **Manual steps:** Winlogbeat and Filebeat `panw` ingest pipelines are loaded by hand once per Beats version.
- **Parsing:** Kaspersky events are stored but not parsed; no web server log source (WEB-* rules ship disabled).

## Planned features

### Collection and detection

- [ ] Fleet / Elastic Agent path (compose layer and ECK `Agent`) as an alternative to Beats
- [ ] TLS for Beats inputs and Logstash → Elasticsearch certificate rotation
- [ ] Kaspersky (CEF/LEEF) parsing; web server sources (nginx, Apache, IIS) to enable the WEB-* rules
- [ ] More sources: Microsoft 365 / Entra ID, Linux auditd, Cisco ASA, Suricata/Zeek
- [ ] Automatic loading of Winlogbeat / Filebeat ingest pipelines in the bootstrap
- [ ] Kibana dashboards per source and an ESM overview dashboard
- [ ] Detection rule tests with sample events in CI

### Triage platform

- [x] Persistent correlation windows in NATS KV and multiple alert-gateway replicas (ADR-027)
- [ ] Asset criticality and GeoIP enrichers; OpenCTI threat-intel source
- [ ] Organisation RAG: playbooks and past dispositions with `dense_vector`
- [ ] Triage UI on top of the bff API
- [ ] Human-approved, reversible response actions (`response-service`)
- [x] Masking reverse-map TTL and deletion after the analyst review (ADR-025)
- [ ] GCP Pub/Sub bus adapter (`BUS_BACKEND=pubsub`, ADR-017)
- [ ] Platform metrics into Elastic: LLM cost, request rate, triage latency (design rule 7)
- [ ] Per-analyst identity and audit for MCP and bff calls

### Deployment and operations

- [ ] TLS and authentication in front of Kibana, bff and MCP (Ingress / reverse proxy examples)
- [ ] Kubernetes NetworkPolicies, HPA for stateless services, Helm chart
- [x] `wait_for_snapshot` in ILM when `SNAPSHOT_POLICY` is set (on in the `gke` overlay)
- [x] Snapshot repositories for on-prem: S3-compatible and shared FS on Compose, `onprem` and bare metal
- [ ] Bare-metal installer run in CI on an Ubuntu 22.04 VM; platform services as systemd units
- [ ] Elastic Stack 8.x upgrade path (8.13 → 8.19) and 9.x evaluation
- [ ] Documentation site (mkdocs) built from `docs/`

## History

The repository consolidates three projects. The consolidation shipped as phases 0–8 in pull
requests #1–#7: history cleanup, single layout and license, Elastic content, platform services,
MCP server, Kubernetes, outbound integrations, CI and documentation. See [CHANGELOG.md](CHANGELOG.md).
