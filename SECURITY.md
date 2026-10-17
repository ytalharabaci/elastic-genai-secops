# Security policy

## Reporting a vulnerability

The project is archived and no longer maintained, so vulnerabilities are no longer fixed. Fork the
repository if you need to patch it.

## Supported versions

None. The last version, v1.0.0, receives no further fixes.

## Security model in short

- The LLM receives only masked, curated incident data and can never trigger an action; analysts
  approve or reject every case (see the [design rules](ROADMAP.md#design-rules) and ADR-001 to ADR-004, ADR-022, ADR-023).
- `masking-service` is the only holder of plaintext reverse maps. Restrict network access to it.
- Kibana, the bff API and the Beats inputs are served without TLS by default; put them behind TLS
  and network restrictions before production use (listed in [ROADMAP.md](ROADMAP.md)).
- The MCP server requires a bearer token over HTTP and returns masked, read-only data.
- Container images run as non-root; CI scans them with Trivy and fails on fixable HIGH/CRITICAL
  findings.
