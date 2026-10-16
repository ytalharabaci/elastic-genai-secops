"""Audit log for every LLM call (design rule 4, ROADMAP.md).

Each call is logged to stdout and, when ELASTIC_URL is set, indexed into esm-llm-audit:
prompt hash, full masked prompt, response, provider, model, token usage, latency, outcome
and the resulting decision. The prompt only ever contains masked data (ADR-004).
"""

from __future__ import annotations

import os
from datetime import UTC, datetime

import httpx
import structlog
from esm_common.contracts import MaskedIncident, TriageDecision
from esm_common.tls import ca_context

from app.provider.base import LLMResponse

logger = structlog.get_logger("llm.audit")

AUDIT_INDEX = "esm-llm-audit"


class AuditLogger:
    def __init__(self, client: httpx.AsyncClient | None = None) -> None:
        self._client = client

    @classmethod
    def from_env(cls) -> AuditLogger:
        url = os.getenv("ELASTIC_URL")
        if not url:
            return cls(None)
        client = httpx.AsyncClient(
            base_url=url.rstrip("/"), verify=ca_context(os.getenv("ELASTIC_CA_CERTS")), timeout=15,
            auth=(os.getenv("ELASTIC_USER", "elastic"), os.getenv("ELASTIC_PASSWORD", "")),
        )
        return cls(client)

    async def record(self, incident: MaskedIncident, system_prompt: str, data_block: str,
                     prompt_hash: str, provider: str, response: LLMResponse | None,
                     outcome: str, decision: TriageDecision | None, error: str = "") -> None:
        doc = {
            "@timestamp": datetime.now(UTC).isoformat(),
            "incident_id": incident.incident_id,
            "provider": provider,
            "model_id": response.model_id if response else None,
            "prompt_hash": prompt_hash,
            "system_prompt": system_prompt,
            "data_block": data_block,
            "response": response.content if response else None,
            "input_tokens": response.input_tokens if response else 0,
            "output_tokens": response.output_tokens if response else 0,
            "latency_ms": round(response.latency_ms, 1) if response else None,
            "outcome": outcome,
            "error": error or None,
            "decision": decision.model_dump() if decision else None,
        }
        logger.info("llm_call", **{k: v for k, v in doc.items()
                                   if k not in ("system_prompt", "data_block")})
        if self._client is None:
            return
        try:
            resp = await self._client.post(f"/{AUDIT_INDEX}/_doc", json=doc)
            resp.raise_for_status()
        except Exception as exc:
            logger.error("audit write to Elasticsearch failed", error=str(exc),
                         incident_id=incident.incident_id)

    async def close(self) -> None:
        if self._client is not None:
            await self._client.aclose()
