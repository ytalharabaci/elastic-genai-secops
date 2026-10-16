# Elastic GenAI SecOps

[![CI/CD](https://github.com/yusufarbc/elastic-genai-secops/actions/workflows/pipeline.yml/badge.svg?branch=production)](https://github.com/yusufarbc/elastic-genai-secops/actions/workflows/pipeline.yml)
![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)
![Elastic Stack](https://img.shields.io/badge/Elastic%20Stack-8.13-005571?logo=elasticsearch)
![Go](https://img.shields.io/badge/Go-1.27-00ADD8?logo=go)
![Python](https://img.shields.io/badge/Python-3.14-3776AB?logo=python)

An open, AI-assisted Security Operations platform built on the **Basic-licensed Elastic Stack**.
Logs are collected and parsed with Logstash and Beats, Elastic detection rules raise alerts, alerts are
correlated into incidents, personal data is masked, and an LLM suggests a triage decision that an
analyst reviews. The LLM never acts on its own.

> **Status:** early release. The SIEM, the AI triage pipeline, the MCP server and the outbound
> integrations run end to end; see [ROADMAP.md](ROADMAP.md) for what is verified, known limitations
> and planned features. The project consolidates three earlier projects (an Ubuntu ELK installer,
> an MCP-based GenAI SOC prototype and an AI-SOC platform).

## Architecture

Four layers, top to bottom. Each layer works without the one below it: the SIEM alone is a complete
Elastic deployment, and the AI triage platform only adds suggestions on top of its alerts.

```mermaid
flowchart TB
    subgraph SRC["1 · Log sources"]
        W["Windows<br/>Winlogbeat · WEF · Sysmon"]
        F["Firewalls<br/>FortiGate · Palo Alto"]
        O["Kaspersky · Syslog"]
    end

    subgraph SIEM["2 · SIEM · Elastic Stack, Basic license"]
        LS[Logstash] --> ES[(Elasticsearch)] --> KB["Kibana<br/>30 detection rules"]
    end

    subgraph AI["3 · AI triage platform · NATS JetStream"]
        DS[detection-service] -->|esm.alerts| AG["alert-gateway<br/>correlation"]
        AG -->|esm.incidents| EN[enrichment-service]
        EN -->|esm.masked-incidents| LO["llm-orchestrator<br/>1 LLM call per incident"]
        LO -->|esm.triage-decisions| CS[case-service]
        EN <-.->|mask| MS["masking-service<br/>PII pseudonyms"]
        CS -.->|unmask| MS
    end

    subgraph OUT["4 · Analyst and integrations"]
        BFF["bff<br/>case API · analyst review"]
        OB["outbound-service<br/>Slack · Teams · TheHive · Jira"]
        MCP["mcp-server<br/>read-only, masked"]
    end

    W & F & O --> LS
    KB -->|alerts| DS
    CS --> BFF
    CS -->|esm.case-events| OB
    MCP -.-> BFF
```

| Layer | What it does |
| --- | --- |
| Log sources | Beats, Windows Event Forwarding and syslog ship events to Logstash ([integrations/sources](integrations/sources/README.md)) |
| SIEM | Logstash parses into ECS data streams; Kibana detection rules (MITRE-mapped) raise alerts |
| AI triage platform | Alerts are correlated into incidents, identifiers are masked, and one LLM call per incident suggests a triage decision ([triage pipeline](docs/genai/triage-pipeline.md)) |
| Analyst and integrations | An analyst approves or rejects every case; notifications and tickets follow; MCP clients get read-only, masked access ([MCP server](docs/genai/mcp-server.md)) |

Design rules (see [ROADMAP.md](ROADMAP.md#design-rules) and the [ADRs](docs/architecture/adr/README.md)):
one LLM call per incident, never per alert; only masked data reaches the LLM; LLM output is
schema-validated JSON and every call is audited; only Basic-tier Elastic features.

## Flexible by design

Choose **what** to install (profile) independently of **where** it runs (target).

| Axis | Options | Status |
| --- | --- | --- |
| Profile | `siem` | Available (Compose, bare metal) |
| Profile | `siem` + platform layer (AI triage pipeline) | Available (Compose, Kubernetes) |
| Target: bare-metal Ubuntu 22.04 | `deploy/baremetal/ubuntu/` | Available (`siem`) |
| Target: Docker Compose | `deploy/compose/siem.yml`, `platform.yml` | Available |
| Target: Kubernetes (ECK 3.5 + kustomize) | `deploy/kubernetes/overlays/` lab · onprem · gke | Available: `lab` tested end to end on kind; `onprem`, `gke` schema-validated |
| LLM provider | `mock` · `ollama` · `openai-compatible` · `anthropic` · `vertex` | Available: `mock` and `openai-compatible` (DeepSeek) tested end to end; `ollama`, `anthropic` tested against mocked HTTP only; `vertex` untested |
| Message bus | NATS JetStream | Available |
| Message bus | GCP Pub/Sub | Planned |
| MCP access | read-only, masked tools; stdio or bearer-token HTTP | Available |
| Notifications | Slack · Teams · webhook · e-mail (no entities by default) | Available |
| Ticketing | TheHive · Jira (after analyst approval) | Available |
| Threat intel | AbuseIPDB · MISP (external IPs, in enrichment) | Available |

## Repository layout

| Path | Contents |
| --- | --- |
| [`services/`](services) | Platform microservices: Go (detection-service, alert-gateway, case-service, bff) and Python (enrichment-service, masking-service, llm-orchestrator, mcp-server, outbound-service) |
| [`libs/`](libs) | Shared code: message contracts, NATS bus, Elasticsearch client (`go-common`, `py-common`) |
| [`content/`](content) | Elastic content loaded by `content/bootstrap.sh`: detection rules, ILM policy, component templates, roles |
| [`integrations/sources/`](integrations/sources) | One folder per log source: Logstash pipeline and collector configuration |
| [`integrations/outbound/`](integrations/outbound) | Notification, ticketing and threat-intel adapters |
| [`deploy/`](deploy) | Deployment targets: `compose/`, `kubernetes/`, `baremetal/ubuntu/`, `endpoints/windows/` (GPO agent rollout) |
| [`docs/`](docs) | Guides, component references, deep dives and architecture decisions |

## Quick start

**SIEM with Docker Compose** (Elasticsearch, Kibana, Logstash, detection rules; needs ~4 GB RAM for Docker):

```bash
git clone https://github.com/yusufarbc/elastic-genai-secops.git
cd elastic-genai-secops/deploy/compose
./init-env.sh                      # Windows: .\init-env.ps1  (creates .env with random passwords)
docker compose -f siem.yml up -d
```

Open <http://localhost:5601> and log in as `elastic` with `ELASTIC_PASSWORD` from `deploy/compose/.env`.
Send a test event with `logger -n localhost -P 5514 -d "hello esm"` and find it in Discover (`logs-*`).
Then connect real sources: [integrations/sources](integrations/sources/README.md).

**SIEM on a single Ubuntu 22.04 host:**

```bash
sudo ./deploy/baremetal/ubuntu/elk_setup_ubuntu_jammy.sh
```

See [docs/deployment/baremetal.md](docs/deployment/baremetal.md).

**AI triage pipeline** on top of the SIEM (mock LLM by default, ~6 GB RAM for Docker; see [docs/genai/triage-pipeline.md](docs/genai/triage-pipeline.md)):

```bash
cd deploy/compose
docker compose -f siem.yml -f platform.yml up -d --build
python ../../tests/e2e/pipeline_test.py        # optional end-to-end check
```

Cases are served at <http://localhost:8080/api/cases>.
The same stack exposes a read-only, masked [MCP server](docs/genai/mcp-server.md) on
`http://localhost:8090/mcp` for Claude Desktop, Claude Code and other MCP clients.

**Kubernetes with ECK** (same profiles; `lab` runs on kind, k3d or Docker Desktop):

```bash
./deploy/kubernetes/init-secrets.sh lab
kubectl kustomize --load-restrictor LoadRestrictionsNone deploy/kubernetes/overlays/lab \
  | kubectl apply --server-side --force-conflicts -f -
```

See [docs/deployment/kubernetes.md](docs/deployment/kubernetes.md) for ECK installation and the onprem / gke overlays.

## Documentation

- Architecture: [overview](docs/architecture/overview.md) · [decision records](docs/architecture/adr/README.md)
- Deployment: [bare metal](docs/deployment/baremetal.md) · [Kubernetes](docs/deployment/kubernetes.md)
- Components: [Elasticsearch](docs/components/elasticsearch.md) · [Kibana](docs/components/kibana.md) · [Logstash](docs/components/logstash.md)
- Integrations: [log sources](integrations/sources/README.md) · [Windows endpoints (GPO)](docs/integrations/windows-endpoints.md) · [Windows audit policy](docs/integrations/windows-audit-policy.md) · [notifications, ticketing, threat intel](docs/integrations/outbound.md)
- GenAI: [triage pipeline](docs/genai/triage-pipeline.md) · [MCP server](docs/genai/mcp-server.md)
- Operations: [troubleshooting](docs/operations/troubleshooting.md) · [CI/CD and DevSecOps pipeline](docs/operations/ci-cd.md)
- Deep dives: [history](docs/deep-dives/history-and-evolution.md) · [Elasticsearch internals](docs/deep-dives/elasticsearch-internals.md) · [ingestion](docs/deep-dives/ingestion-architecture.md) · [Kibana internals](docs/deep-dives/kibana-internals.md) · [security architecture](docs/deep-dives/security-architecture.md) · [comparative analysis](docs/deep-dives/comparative-analysis.md)

## Contributing and security

Contributions are welcome: see [CONTRIBUTING.md](CONTRIBUTING.md). Report vulnerabilities privately as
described in [SECURITY.md](SECURITY.md). Changes are listed in [CHANGELOG.md](CHANGELOG.md).

## License

[Apache License 2.0](LICENSE). See [NOTICE](NOTICE). Elastic, Elasticsearch, Kibana, Logstash and Beats
are trademarks of Elasticsearch B.V.; this project is not affiliated with or endorsed by Elastic.
