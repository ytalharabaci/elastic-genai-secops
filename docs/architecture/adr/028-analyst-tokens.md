# ADR-028: Analyst tokens for the case API and the MCP server

**Status:** Accepted · **Date:** 2026-10-16 · Replaces `MCP_TOKEN` (ADR-022) · Resolves the
per-analyst identity item in ROADMAP.md

**Context:** bff, the analyst API, had no authentication, and the reviewer of a case was whatever
name the request body claimed, so anyone who could reach bff could approve a case under another
analyst's name. The MCP server shared one token (`MCP_TOKEN`) among all users, and read cases
from bff anonymously, so nothing recorded which analyst looked at what.

**Decision:**

- `ANALYST_TOKENS` lists analysts and their bearer tokens (`name:token,...`, tokens of at least
  24 characters). bff and the MCP server read the same setting, so one credential serves both.
  `init-env` and `init-secrets` create one analyst, `analyst`, reusing an existing `MCP_TOKEN`.
- bff requires a known token on every `/api` request (`/healthz` stays open) and logs each request
  with the analyst. On a review it replaces the body's `analyst` with the token's analyst, so the
  `reviewed_by` stored by case-service is authenticated. Only bff can reach case-service
  (NetworkPolicy, `components/network-policies`).
- The MCP server accepts the same tokens, logs every tool call with the analyst, and reads cases
  from bff with the caller's token, so bff authorises and records the actual analyst. Over stdio it
  uses `ANALYST_TOKEN`, the local analyst's own token.
- Tokens are compared as SHA-256 digests in constant time against every entry; error messages
  name an entry by position, never by its text.

**Consequences:** Clients of bff and the MCP server send `Authorization: Bearer <token>`; an
anonymous bff request gets 401. Adding or revoking an analyst means editing `ANALYST_TOKENS` and
restarting bff and mcp-server. Tokens travel in clear text until TLS is in front of bff and MCP
(ROADMAP.md). An identity provider (OIDC) can replace the static list later without changing how
the analyst reaches case-service.
