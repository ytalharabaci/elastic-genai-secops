"""Analyst tokens (ADR-028): the same ANALYST_TOKENS as bff, so one credential works for both.

The HTTP transport accepts only known tokens and remembers who is calling for the request; the
case reads go to bff with the caller's own token, so bff records the real analyst.
"""

from __future__ import annotations

import hashlib
import hmac
import json
import logging
import re
from contextvars import ContextVar
from typing import Any

MIN_TOKEN_LEN = 24
_NAME = re.compile(r"[A-Za-z0-9._@-]{1,64}")

current_analyst: ContextVar[str] = ContextVar("current_analyst", default="")
current_token: ContextVar[str] = ContextVar("current_token", default="")

audit_log = logging.getLogger("esm.mcp.audit")


class Analysts:
    """Bearer tokens by analyst, compared as SHA-256 digests in constant time."""

    def __init__(self, spec: str) -> None:
        self._entries: list[tuple[bytes, str]] = []
        for i, entry in enumerate(spec.split(","), start=1):
            entry = entry.strip()
            if not entry:
                continue
            name, sep, token = entry.partition(":")
            if not sep or not _NAME.fullmatch(name):
                # The entry may be a mistyped token: name its position, never its text
                raise ValueError(f"ANALYST_TOKENS: entry {i} must be name:token with a name of "
                                 "1-64 letters, digits, . _ - @")
            if len(token) < MIN_TOKEN_LEN:
                raise ValueError(f"ANALYST_TOKENS: token of {name!r} is shorter than "
                                 f"{MIN_TOKEN_LEN} characters")
            if any(n == name for _, n in self._entries):
                raise ValueError(f"ANALYST_TOKENS: {name!r} is listed twice")
            self._entries.append((hashlib.sha256(token.encode()).digest(), name))
        if not self._entries:
            raise ValueError("ANALYST_TOKENS is empty: set at least one name:token pair "
                             "(init-env / init-secrets create one)")

    def lookup(self, token: str) -> str | None:
        digest = hashlib.sha256(token.encode()).digest()
        found = None
        for expected, name in self._entries:  # check all entries: timing does not depend on which
            if hmac.compare_digest(digest, expected):
                found = name
        return found


class RequireAnalyst:
    """ASGI middleware: 401 without a known bearer token, else sets the caller for the request."""

    def __init__(self, app: Any, analysts: Analysts) -> None:
        self._app = app
        self._analysts = analysts

    async def __call__(self, scope: dict[str, Any], receive: Any, send: Any) -> None:
        if scope["type"] != "http":
            await self._app(scope, receive, send)
            return
        header = dict(scope.get("headers") or []).get(b"authorization", b"").decode("latin-1")
        token = header[len("Bearer "):] if header.startswith("Bearer ") else ""
        name = self._analysts.lookup(token) if token else None
        if name is None:
            audit_log.warning("rejected request without a valid analyst token")
            body = json.dumps({"error": "unauthorized"}).encode()
            await send({"type": "http.response.start", "status": 401,
                        "headers": [(b"content-type", b"application/json"),
                                    (b"www-authenticate", b'Bearer realm="esm"')]})
            await send({"type": "http.response.body", "body": body})
            return
        analyst_reset, token_reset = current_analyst.set(name), current_token.set(token)
        try:
            await self._app(scope, receive, send)
        finally:
            current_analyst.reset(analyst_reset)
            current_token.reset(token_reset)


def audit(tool: str, **args: Any) -> None:
    """One log line per tool call, with the analyst who made it."""
    audit_log.info("tool call analyst=%s tool=%s args=%s", current_analyst.get() or "local", tool,
                   json.dumps(args, default=str))
