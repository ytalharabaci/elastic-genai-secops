[← Back to README](../../README.md)

# Troubleshooting

Organized by symptom. Most entries come from problems hit while testing the platform.

## Quick health check

```bash
# Compose
cd deploy/compose && docker compose -f siem.yml -f platform.yml ps
docker compose -f siem.yml -f platform.yml logs --tail 50 <service>

# Kubernetes
kubectl -n esm get elasticsearch,kibana,logstash,pods
kubectl -n esm logs deploy/<service> --tail 50

# Bare metal
systemctl status elasticsearch kibana logstash
journalctl -u logstash -n 100 --no-pager
```

## Elasticsearch

| Symptom | Cause and fix |
| --- | --- |
| Container or service exits right after start, `max virtual memory areas vm.max_map_count [65530] is too low` | `sysctl -w vm.max_map_count=262144` on the host (the bare-metal installer persists it). Kubernetes manifests avoid this with `node.store.allow_mmap: false`. |
| Killed / `OutOfMemoryError` | Raise `ES_HEAP` (Compose) or the ECK resources; keep the heap at half the container memory. |
| Health `yellow` on a single node | Expected: replicas cannot be assigned on one node. |
| `401` for `elastic` after changing `.env` | The `elastic` password was set when the volume was created. Use the original value, or reset it with `bin/elasticsearch-reset-password -u elastic`. |
| Disk filling up, old `logs-*` indices not deleted | They wait for a snapshot (`SNAPSHOT_POLICY`). Check the SLM policy's `last_failure` and the repository (see [snapshots](../components/elasticsearch.md#snapshots)); deletion resumes after the next successful snapshot. |
| Python client: `CA cert does not include key usage extension` | The stack's CA predates ADR-029 (made by `elasticsearch-certutil`). The platform services accept it; for other clients recreate the CA: `docker compose -f siem.yml -f platform.yml down`, `docker volume rm esm-siem_certs`, `up -d` (browsers and clients must then trust the new `ca.crt`). |

## Compose `.env`

| Symptom | Cause and fix |
| --- | --- |
| Duplicate keys in `.env`, passwords that equal `__GENERATE__` | An older `init-env.ps1` used case-insensitive matching, which fails for the letter `I` on Windows with a Turkish (and some other) culture. Use the current script, remove duplicate lines (keep the first value) and recreate the stack. |
| `variable is not set` errors | Run `./init-env.sh` / `.\init-env.ps1` again; it only adds missing keys. |

## Logstash and ingestion

| Symptom | Cause and fix |
| --- | --- |
| `401` from Elasticsearch right after start | The bootstrap had not created `logstash_ingest` yet. It recovers by itself within a minute or two. If it persists, check `LOGSTASH_INGEST_PASSWORD` matches between bootstrap and Logstash. |
| `Address already in use` | Each source needs its own port; see [integrations/sources](../../integrations/sources/README.md). Ports below 1024 need root; all defaults are above. |
| Events missing in Discover | Create a data view for `logs-*`; check the time range; send a test line: `logger -n <host> -P 5514 -d "test"`. |
| FortiGate / Palo Alto events are hours in the future or past | Set `FORTIGATE_TZ` / `PANOS_TZ` to the device clock's time zone. |
| Windows events have no `process.name`, `event.category` | No ingest pipeline for that Winlogbeat version. Look for the bootstrap's `WARNING: could not download the Beats` line; set `BEATS_VERSIONS` to the endpoints' version or `BEATS_ARTIFACTS_URL` to a mirror and rerun it (see [integrations/sources](../../integrations/sources/README.md#windows-and-palo-alto-filebeat-ingest-pipelines)). |
| Pipeline syntax errors | `bin/logstash --config.test_and_exit -f <file>` (see [logstash.md](../components/logstash.md#adding-a-source)). |

## Detection rules

| Symptom | Cause and fix |
| --- | --- |
| Rule status "partial failure: no index matching" | No data in that index pattern yet; harmless until the source sends data. |
| Rules never fire for firewall events | Wrong device time zone (see above) or no GeoIP data for private addresses (FW-003 needs public destinations). |
| Import failed during bootstrap | Run `python content/detection-rules/build.py --check`; rebuild `rules.ndjson` after editing `rules.yml`. |

## Triage pipeline (platform)

Follow one incident through the logs: detection-service `alert published` → alert-gateway
`incident published` → enrichment-service `masked incident published` → llm-orchestrator
`triage result published` → case-service `case created`.

| Symptom | Cause and fix |
| --- | --- |
| Alerts but no incident | alert-gateway waits for `CORRELATION_THRESHOLD` alerts with the same host/user/technique or for `CORRELATION_WINDOW` to expire (default 5 alerts / 5 minutes). |
| Services restart with `permission denied` on `/certs/ca/ca.crt` | The `certs` volume predates the fix that made certificates world-readable; recreate it: `docker compose -f siem.yml -f platform.yml up -d --force-recreate certs`. |
| `triage_status=llm_failed` | Look at `esm-llm-audit` for `outcome: failed` and the `error` field (wrong `LLM_MODEL_ID`, key, base URL, or a model that does not return JSON). |
| `triage_status=budget_exceeded` | `LLM_MAX_TOKENS_PER_WINDOW` was reached in the current `LLM_BUDGET_WINDOW_SECONDS`; incidents still become cases for manual triage. |
| Messages stuck | Inspect NATS: `docker compose exec nats wget -qO- "http://localhost:8222/jsz?consumers=true"`; failed messages end up on `esm.dlq`. |

## MCP server

| Symptom | Cause and fix |
| --- | --- |
| `401 unauthorized` | Send `Authorization: Bearer <token>` with an analyst's token from `ANALYST_TOKENS` (also for bff `/api`). After an upgrade, run `init-env` (it creates `ANALYST_TOKENS` from the old `MCP_TOKEN`). |
| `421 Misdirected Request` / `Invalid Host header` | Add the host your client uses to `MCP_ALLOWED_HOSTS` (DNS-rebinding protection). |
| Hunts return 0 hits | The `esm_platform` role needs read access to `logs-*-*`; re-run the bootstrap after upgrading. |

## Kubernetes

| Symptom | Cause and fix |
| --- | --- |
| `Apply failed with 1 conflict: conflict with "elastic-operator"` | Use `kubectl apply --server-side --force-conflicts`. |
| Logstash pod never ready, readiness probe `EOF` | ECK 3.5 with the TLS-enabled Logstash API of 8.13 returns empty replies; the base manifest disables TLS for the in-cluster `api` service. Keep that setting. |
| `field is immutable` for Job `esm-bootstrap` | `kubectl -n esm delete job esm-bootstrap` before re-applying changed content. |
| kind: `content digest ... not found` when loading images | Use `docker save --platform linux/amd64` and `kind load image-archive` (see [kubernetes.md](../deployment/kubernetes.md#overlay-notes)). |
| Platform pods in `CreateContainerConfigError` | Secret `esm-secrets` missing: run `deploy/kubernetes/init-secrets.sh <overlay>` and re-apply. |

## Getting help

Open an issue with the output of the quick health check, the relevant service logs and your
target (Compose, Kubernetes overlay, bare metal). Remove passwords and internal addresses first.
