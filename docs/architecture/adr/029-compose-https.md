# ADR-029: HTTPS for Kibana, bff and the MCP server on Docker Compose

**Status:** Accepted · **Date:** 2026-10-16 · Partly resolves the transport-security limitation in
ROADMAP.md

**Context:** On Compose, Elasticsearch already used TLS, but Kibana, the analyst API (bff) and the
MCP server listened in plain HTTP, so Kibana passwords and the analyst tokens of ADR-028 crossed the
network in clear text. The CA from `elasticsearch-certutil` also has no keyUsage extension, which
Python 3.13+ clients reject by default (`VERIFY_X509_STRICT`, see #69).

**Decision:**

- The `certs` service creates one certificate per HTTPS service (`elasticsearch`, `kibana`, `bff`,
  `mcp-server`; host name = service name plus `localhost`) from the stack's CA. Missing ones are
  added to an existing volume, so upgrading needs no manual step. Each private key belongs to the
  uid of the service that reads it.
- New CAs are made with openssl and carry `keyUsage = keyCertSign, cRLSign`, so strict clients
  accept them. An existing CA is kept; the services still accept it (#69).
- Kibana serves HTTPS on 5601, bff on 8080 (`TLS_CERT_FILE`, `TLS_KEY_FILE`), the MCP server on
  8090 (`MCP_TLS_CERT_FILE`, `MCP_TLS_KEY_FILE`), all TLS 1.2 or newer. The MCP server reaches bff
  over HTTPS and verifies it against the stack's CA.
- In Kubernetes the variables stay unset: TLS ends at the ingress, as for Kibana today.

**Consequences:** Browsers and API clients must trust the stack's CA (`docker compose cp` it out, see
docs/genai/mcp-server.md) or accept it once. URLs change from `http://` to `https://`. Beats →
Logstash and an ingress example for Kubernetes remain open.
