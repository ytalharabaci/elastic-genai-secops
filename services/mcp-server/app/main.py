"""Entry point.

MCP_TRANSPORT=stdio (default)  for local clients such as Claude Desktop
MCP_TRANSPORT=http             streamable HTTP on MCP_HOST:MCP_PORT (default 0.0.0.0:8090), HTTPS
                               with MCP_TLS_CERT_FILE and MCP_TLS_KEY_FILE;
                               every request needs "Authorization: Bearer <analyst token>"
                               from ANALYST_TOKENS (name:token,...), shared with bff (ADR-028);
                               MCP_ALLOWED_HOSTS lists accepted Host headers (localhost:* default)
"""

from __future__ import annotations

import logging
import os
import sys

from app.auth import Analysts, RequireAnalyst
from app.server import server_from_env


def http_app(analyst_tokens: str):  # type: ignore[no-untyped-def]
    try:
        analysts = Analysts(analyst_tokens)
    except ValueError as err:
        raise SystemExit(str(err)) from None
    from mcp.server.transport_security import TransportSecuritySettings

    # DNS-rebinding protection stays on; list the Host values clients use (host:port, * = any)
    allowed = os.getenv("MCP_ALLOWED_HOSTS", "localhost:*,127.0.0.1:*")
    hosts = [h.strip() for h in allowed.split(",") if h.strip()]
    app = server_from_env().streamable_http_app(
        stateless_http=True,
        transport_security=TransportSecuritySettings(
            enable_dns_rebinding_protection=True,
            allowed_hosts=hosts,
            allowed_origins=[f"http://{h}" for h in hosts] + [f"https://{h}" for h in hosts],
        ),
    )
    return RequireAnalyst(app, analysts)


def main() -> None:
    # Audit lines go to stderr; with stdio, stdout carries the protocol
    logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(name)s %(message)s")
    transport = os.getenv("MCP_TRANSPORT", "stdio")
    if transport == "stdio":
        server_from_env().run(transport="stdio")
    elif transport == "http":
        import uvicorn

        # HTTPS when a certificate is configured (compose); in Kubernetes TLS ends at the ingress
        tls = {}
        if os.getenv("MCP_TLS_CERT_FILE"):
            tls = {"ssl_certfile": os.environ["MCP_TLS_CERT_FILE"],
                   "ssl_keyfile": os.environ["MCP_TLS_KEY_FILE"]}
        uvicorn.run(http_app(os.getenv("ANALYST_TOKENS", "")),
                    host=os.getenv("MCP_HOST", "0.0.0.0"),
                    port=int(os.getenv("MCP_PORT", "8090")), log_level="info", **tls)
    else:
        sys.exit(f"unknown MCP_TRANSPORT {transport!r} (stdio or http)")


if __name__ == "__main__":
    main()
