"""Live check of the MCP server with the official MCP client (streamable HTTP + bearer token).

Run inside the compose network after pipeline_test.py has created at least one case:
    docker run --rm --network esm-siem_default -v "$PWD:/src" -w /src \
      -e MCP_URL=http://mcp-server:8090/mcp -e ANALYST_TOKEN=... python:3.14-slim \
      sh -c "pip install -q 'mcp>=2.3,<3' && python tests/e2e/mcp_client_test.py"
"""

from __future__ import annotations

import asyncio
import json
import os
import re
import sys

import httpx2
from mcp import ClientSession
from mcp.client.streamable_http import streamable_http_client

URL = os.getenv("MCP_URL", "https://localhost:8090/mcp")
CA = os.getenv("MCP_CA_CERT")  # the stack's CA (deploy/compose certs volume); default: system store
TOKEN = os.environ["ANALYST_TOKEN"]  # one analyst's token from ANALYST_TOKENS
PLAINTEXT = re.compile(r"E2E-PC-|e2e-user-|PC-LAB-01")  # identifiers written by the e2e tests


def text(result) -> str:  # type: ignore[no-untyped-def]
    return result.content[0].text


async def main() -> None:
    checks: dict[str, bool] = {}
    http = httpx2.AsyncClient(headers={"Authorization": f"Bearer {TOKEN}"},
                              timeout=httpx2.Timeout(30, read=300), verify=CA or True)
    async with http, streamable_http_client(URL, http_client=http) as (r, w):
        async with ClientSession(r, w) as session:
            await session.initialize()
            tools = {t.name for t in (await session.list_tools()).tools}
            checks["read-only tool set"] = tools == {"list_cases", "get_case", "alert_statistics",
                                                     "list_hunts", "run_hunt"}

            cases = json.loads(text(await session.call_tool("list_cases", {"review_status": "all"})))
            checks["cases listed"] = len(cases) > 0
            # Pick a case whose summary references a host token (firewall-only cases have none)
            with_host = next((c for c in cases if "host_" in c.get("summary", "")), cases[0])
            detail = text(await session.call_tool("get_case", {"case_id": with_host["id"]}))
            listing = json.dumps(cases)
            checks["no plaintext identifiers in case data"] = not PLAINTEXT.search(listing + detail)
            checks["masked tokens present"] = bool(re.search(r"host_[0-9a-f]{6}", detail))

            stats = json.loads(text(await session.call_tool("alert_statistics", {"hours": 48})))
            checks["alert statistics"] = stats["total"] > 0
            hunt = json.loads(text(await session.call_tool("run_hunt",
                                                           {"hunt_id": "office-child-processes",
                                                            "hours": 48})))
            checks["hunt ran"] = hunt["total_hits"] > 0 and "process.name" in hunt["top_values"]
            print(json.dumps({"case": json.loads(detail)["summary"], "hunt": hunt["top_values"]},
                             indent=2))

    for name, ok in checks.items():
        print(f"  {'PASS' if ok else 'FAIL'}  {name}")
    if not all(checks.values()):
        sys.exit("FAILED")
    print("MCP server OK")


if __name__ == "__main__":
    asyncio.run(main())
