"""Entry point.

MCP_TRANSPORT=stdio (default)  for local clients such as Claude Desktop
MCP_TRANSPORT=http             streamable HTTP on MCP_HOST:MCP_PORT (default 0.0.0.0:8090);
                               every request needs "Authorization: Bearer $MCP_TOKEN";
                               MCP_ALLOWED_HOSTS lists accepted Host headers (localhost:* default)
"""

from __future__ import annotations

import hmac
import os
import sys

from starlette.middleware.base import BaseHTTPMiddleware
from starlette.requests import Request
from starlette.responses import JSONResponse, Response

from app.server import server_from_env


class BearerAuth(BaseHTTPMiddleware):
    def __init__(self, app, token: str) -> None:  # type: ignore[no-untyped-def]
        super().__init__(app)
        self._expected = f"Bearer {token}".encode()

    async def dispatch(self, request: Request, call_next) -> Response:  # type: ignore[no-untyped-def]
        given = request.headers.get("authorization", "").encode()
        if not hmac.compare_digest(given, self._expected):
            return JSONResponse({"error": "unauthorized"}, status_code=401)
        return await call_next(request)


def http_app(token: str):  # type: ignore[no-untyped-def]
    if len(token) < 24:
        raise SystemExit("MCP_TOKEN must be set to a random value of at least 24 characters")
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
    app.add_middleware(BearerAuth, token=token)
    return app


def main() -> None:
    transport = os.getenv("MCP_TRANSPORT", "stdio")
    if transport == "stdio":
        server_from_env().run(transport="stdio")
    elif transport == "http":
        import uvicorn

        uvicorn.run(http_app(os.getenv("MCP_TOKEN", "")), host=os.getenv("MCP_HOST", "0.0.0.0"),
                    port=int(os.getenv("MCP_PORT", "8090")), log_level="info")
    else:
        sys.exit(f"unknown MCP_TRANSPORT {transport!r} (stdio or http)")


if __name__ == "__main__":
    main()
