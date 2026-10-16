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

The compose platform layer starts it on `http://127.0.0.1:8090/mcp` (streamable HTTP). Every request
needs `Authorization: Bearer <MCP_TOKEN>`; the token is generated into `deploy/compose/.env` by
`init-env`.

```bash
cd deploy/compose
docker compose -f siem.yml -f platform.yml up -d --build
```

### Claude Code

```bash
claude mcp add --transport http esm http://localhost:8090/mcp \
  --header "Authorization: Bearer <MCP_TOKEN from deploy/compose/.env>"
```

### Claude Desktop

Claude Desktop starts local (stdio) servers; bridge to the HTTP endpoint with `mcp-remote`
(requires Node.js). In `claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "esm": {
      "command": "npx",
      "args": ["mcp-remote", "http://localhost:8090/mcp",
               "--header", "Authorization: Bearer <MCP_TOKEN>"]
    }
  }
}
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
| `MCP_TOKEN` | – | Bearer token for `http`, at least 24 characters |
| `MCP_HOST`, `MCP_PORT` | `0.0.0.0`, `8090` | HTTP listener |
| `MCP_ALLOWED_HOSTS` | `localhost:*,127.0.0.1:*` | Accepted `Host` headers (DNS-rebinding protection) |
| `CASE_API_URL` | `http://bff:8080/api` | Case API (bff) |
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
  -e MCP_URL=http://mcp-server:8090/mcp -e MCP_TOKEN=<token> python:3.14-slim \
  sh -c "pip install -q 'mcp>=2.3,<3' && python tests/e2e/mcp_client_test.py"
```
