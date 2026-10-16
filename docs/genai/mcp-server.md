[← Back to README](../../README.md)

# MCP server

`services/mcp-server` gives MCP clients (Claude Desktop, Claude Code, other MCP-capable assistants)
**read-only, masked** access to the platform. Design decisions: [ADR-022](../architecture/adr/022-mcp-server-read-only-masked.md).

| Tool | Returns |
| --- | --- |
| `list_cases(review_status, limit)` | Cases (pending / approved / rejected / all) with severity, risk, triage status and the masked summary |
| `get_case(case_id)` | One case: LLM suggestion, rationale, recommended actions, timeline, affected entities (masked) |
| `alert_statistics(hours)` | Kibana Security alert counts by rule, severity and workflow status |
| `list_hunts()` | The allow-listed hunting queries |
| `run_hunt(hunt_id, hours, top)` | Hit count and top values of non-identifying fields for one hunt |
| `get_defender_status()` | Optional, Windows only (`MCP_ENABLE_DEFENDER=true`): local Microsoft Defender status |

What the server deliberately does **not** do:

- No free-form queries. Hunts come from [`content/hunting/hunts.yml`](../../content/hunting/hunts.yml); a hunt may not group by host, user, IP or account fields.
- No plaintext identifiers. Hosts, users and IPs are replaced with the same tokens the triage LLM saw (`host_1a2b3c`); analyst notes and reviewer names are never returned.
- No actions. Approving or rejecting a case, starting scans or changing configuration stays with humans in the case API.

## Run it

The compose platform layer starts it on `https://127.0.0.1:8090/mcp` (streamable HTTP over TLS, with a
certificate from the stack's CA, see [Trusting the stack's CA](#trusting-the-stacks-ca)). Every request
needs `Authorization: Bearer <token>` with an analyst's token from `ANALYST_TOKENS` (`name:token`
pairs in `deploy/compose/.env`, created by `init-env`; ADR-028). Each tool call is logged with the
analyst, and cases are read from bff with the same token.

```bash
cd deploy/compose
docker compose -f siem.yml -f platform.yml up -d --build
```

### Claude Code

```bash
claude mcp add --transport http esm https://localhost:8090/mcp \
  --header "Authorization: Bearer <your token from ANALYST_TOKENS>"
```

Start Claude Code with `NODE_EXTRA_CA_CERTS=/path/to/esm-ca.crt` so it trusts the stack's CA
([below](#trusting-the-stacks-ca)).

### Claude Desktop

Claude Desktop starts local (stdio) servers; bridge to the HTTP endpoint with `mcp-remote`
(requires Node.js). In `claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "esm": {
      "command": "npx",
      "env": { "NODE_EXTRA_CA_CERTS": "/path/to/esm-ca.crt" },
      "args": ["mcp-remote", "https://localhost:8090/mcp",
               "--header", "Authorization: Bearer <your token from ANALYST_TOKENS>"]
    }
  }
}
```

### Trusting the stack's CA

Kibana, bff and the MCP server use certificates from the CA the `certs` service creates. Copy it
out once and point clients at it (Node-based clients read `NODE_EXTRA_CA_CERTS`):

```bash
cd deploy/compose
docker compose -f siem.yml cp elasticsearch:/usr/share/elasticsearch/config/certs/ca/ca.crt esm-ca.crt
```

### Defender endpoint mode (optional)

To read the Defender status of the analyst's own Windows machine, run a second instance locally over
stdio with `MCP_ENABLE_DEFENDER=true` (Python 3.12+):

```powershell
pip install ./services/mcp-server
$env:MCP_TRANSPORT = "stdio"; $env:MCP_ENABLE_DEFENDER = "true"
esm-mcp
```

The tool only reads `Get-MpComputerStatus`; scans and settings are not exposed.

## Settings

| Variable | Default | Meaning |
| --- | --- | --- |
| `MCP_TRANSPORT` | `stdio` (`http` in the image) | `stdio` or `http` |
| `ANALYST_TOKENS` | – | `http`: accepted analysts, `name:token,...` (tokens of at least 24 characters), same as bff |
| `ANALYST_TOKEN` | – | `stdio`: the local analyst's token, sent to bff for case reads |
| `MCP_HOST`, `MCP_PORT` | `0.0.0.0`, `8090` | HTTP listener |
| `MCP_TLS_CERT_FILE`, `MCP_TLS_KEY_FILE` | – | Serve HTTPS (compose sets both; in Kubernetes TLS ends at the ingress) |
| `MCP_ALLOWED_HOSTS` | `localhost:*,127.0.0.1:*` | Accepted `Host` headers (DNS-rebinding protection) |
| `CASE_API_URL` | `http://bff:8080/api` (`https://` in compose) | Case API (bff), verified against `ELASTIC_CA_CERTS` |
| `MASKING_SERVICE_URL` | `http://masking-service:8001` | Source of the incident tokens |
| `ELASTIC_URL`, `ELASTIC_USER`, `ELASTIC_PASSWORD`, `ELASTIC_CA_CERTS` | | Read access for alert statistics and hunts (`esm_platform` user) |
| `HUNTS_FILE` | `content/hunting/hunts.yml` | Hunt allow-list |
| `MCP_ENABLE_DEFENDER` | `false` | Adds `get_defender_status` (Windows only) |

## Test

```bash
# unit tests
cd services/mcp-server && python -m pytest -q tests
# live check with the official MCP client, inside the compose network
docker run --rm --network esm-siem_default -v "$PWD:/src" -w /src \
  -e MCP_URL=http://mcp-server:8090/mcp -e ANALYST_TOKEN=<token> python:3.14-slim \
  sh -c "pip install -q 'mcp>=2.3,<3' && python tests/e2e/mcp_client_test.py"
```
