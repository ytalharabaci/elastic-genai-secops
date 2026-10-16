[← Back to README](../../README.md)

# Kibana

Version **8.13.4**. Analysts work in Kibana for search (Discover), detection alerts (Security →
Alerts) and dashboards; the platform adds cases with LLM suggestions through the bff API and the
MCP server.

## Access

| Target | URL | Login |
| --- | --- | --- |
| Compose | <https://localhost:5601> (`KIBANA_BIND` to expose on the LAN; certificate from the stack's CA, see below) | `elastic` / `ELASTIC_PASSWORD` in `deploy/compose/.env` |
| Bare metal | `http://<host>:5601` | `elastic` / `/root/esm-credentials` |
| Kubernetes | `kubectl -n esm port-forward svc/esm-kb-http 5601` or your ingress | `elastic` / secret `esm-es-elastic-user` |

Kibana serves plain HTTP on all targets; put a TLS reverse proxy or ingress in front of it before
exposing it beyond a lab.

Kibana talks to Elasticsearch as `kibana_system`. The saved-object encryption keys (required by
detection rules) come from `KIBANA_ENCRYPTION_KEY` (Compose), `/etc/kibana/kibana.env` (bare
metal) or ECK.

## Detection rules

The 30 rules in `content/detection-rules/rules.yml` are built into `rules.ndjson` and imported by
`content/bootstrap.sh` with `overwrite=true`. 21 are enabled; the 9 `WEB-*` rules are disabled
because no web server log source ships yet.

- Edit rules in `rules.yml`, run `python content/detection-rules/build.py`, re-run the bootstrap.
- Rule IDs are `esm-<id>` (for example `esm-win-001`); detection-service carries them into incidents.
- Rules run every 5 minutes over the last 6 minutes; threshold rules use their own window.
- Index patterns: `logs-windows-*`, `logs-fortigate-*`, `logs-paloalto-*`.

Create a data view for `logs-*` in Discover to browse all sources.

## What the platform does not rebuild

Search, dashboards, the alerts table and timelines stay in Kibana (design rule 6, ROADMAP.md). The bff
API only adds what Kibana does not have: incidents built from several alerts, the masked LLM
triage suggestion and the analyst review state.
