# Architecture Decision Records

Architecture Decision Record (ADR) log for the Elastic GenAI SecOps AI-assisted SOC/NOC platform. Each entry records the decision, the alternatives considered, the rationale, open risks, and the date. The design rules in `ROADMAP.md` state the rules; this log explains *why*.

Format per entry: **Decision · Status · Date · Context · Alternatives · Rationale · Risks / follow-ups.**

| ADR | Decision |
|---|---|
| [ADR-001](001-llm-per-incident.md) | LLM is triggered per INCIDENT, never per alert or per log |
| [ADR-002](002-deterministic-core-human-in-the-loop.md) | Deterministic core, LLM as ranker; human-in-the-loop for all actions |
| [ADR-003](003-context-curation-not-rag.md) | Token & privacy minimization is achieved by context curation, NOT by RAG |
| [ADR-004](004-mandatory-pii-masking.md) | Mandatory PII masking / pseudonymization before the LLM |
| [ADR-005](005-llm-model-gemini-2-5-flash.md) | LLM model: Gemini 2.5 Flash, behind a swappable provider interface |
| [ADR-006](006-llm-via-vertex-ai.md) | Run the LLM via GCP Vertex AI (NOT Google AI Studio) |
| [ADR-007](007-gke-kubernetes-platform.md) | GKE + Kubernetes for the platform; stateless services autoscale |
| [ADR-008](008-elasticsearch-self-hosted-eck.md) | Elasticsearch self-hosted via ECK on GKE (open source, AGPLv3/ELv2) — NOT Elastic Cloud |
| [ADR-009](009-stateful-ops-baseline.md) | Stateful operations baseline: 3-node MVP cluster, ILM, GCS snapshots |
| [ADR-010](010-archive-tier-gcs.md) | Archive tier: GCS object storage (NOT Google Drive) |
| [ADR-011](011-engineering-process-ci-cd.md) | Engineering process: GitHub two-branch flow, DevSecOps CI/CD, GKE deploy |
| [ADR-012](012-backend-language-split.md) | Backend language split: Go for operational services, Python for AI/enrichment services |
| [ADR-013](013-message-bus-pubsub.md) | Message bus: GCP Pub/Sub (NOT Kafka) |
| [ADR-014](014-elastic-basic-tier-features.md) | Elastic Basic-tier feature availability (initial research) |
| [ADR-015](015-masking-reverse-map-in-elasticsearch.md) | masking-service reverse-map stored in Elasticsearch (NOT in-memory) |
| [ADR-016](016-gke-autopilot.md) | GKE Autopilot (NOT standard node pool) |
| [ADR-017](017-portable-message-bus.md) | Portable message bus: NATS JetStream on-prem, Pub/Sub on GCP |
| [ADR-018](018-multiple-llm-providers.md) | Multiple LLM providers behind the provider interface |
| [ADR-019](019-deployment-profiles-and-targets.md) | Deployment profiles and targets |
| [ADR-020](020-apache-2-0-license.md) | Apache-2.0 license for the consolidated repository |
| [ADR-021](021-naming.md) | Naming: Elastic GenAI SecOps and the `esm` prefix |
| [ADR-022](022-mcp-server-read-only-masked.md) | MCP server is read-only and returns masked data |
| [ADR-023](023-outbound-integrations.md) | Outbound integrations inform and record, they never act |
| [ADR-024](024-staging-production-pipeline.md) | `staging` → `production` branch flow and a single DevSecOps pipeline |
| [ADR-025](025-masking-reverse-map-lifecycle.md) | Reverse-map lifecycle: delete after review, TTL as a backstop |
| [ADR-026](026-right-sized-pipeline.md) | A pipeline sized for a small open-source project |
| [ADR-027](027-persistent-correlation-windows.md) | Correlation windows in NATS KV, shared by alert-gateway replicas |
| [ADR-028](028-analyst-tokens.md) | Analyst tokens for the case API and the MCP server |
| [ADR-029](029-compose-https.md) | HTTPS for Kibana, bff and the MCP server on Docker Compose |

## Open items to resolve (carry forward)

- Confirm Vertex AI region + data-processing terms (ADR-006).
- Legal review: KVKK cross-border transfer with masking + in-region inference (ADR-004).
- Determine regulatory log-retention period and set GCS retention accordingly (ADR-010).
- Implement self-hosted embedding model for RAG (ADR-014 — ELSER blocked).
