import json
from pathlib import Path

import httpx
import pytest

from app.clients import Masking
from app.hunts import Hunt, build_query, load_hunts
from app.server import build_server
from app.views import mask_case

HUNTS = Path(__file__).resolve().parents[3] / "content" / "hunting" / "hunts.yml"

CASE = {
    "id": "inc-1", "incident_id": "inc-1", "created_at": "2026-10-07T18:00:00Z",
    "severity": "high", "risk_score": 60, "alert_count": 5, "triage_status": "triaged",
    "review_status": "pending", "mitre_techniques": ["T1059"],
    "summary": "Macro on PC-1 and PC-10 by alice from 10.0.0.5",
    "recommended_actions": ["Isolate PC-10"], "rationale": "alice opened a document",
    "affected_hosts": ["PC-1", "PC-10"], "affected_users": ["alice"], "source_ips": ["10.0.0.5"],
    "timeline": [{"timestamp": "t", "rule": "WIN-001", "severity": "high", "host": "PC-10",
                  "user": "alice"}],
    "analyst_notes": "called alice, she is on leave", "reviewed_by": "bob",
}


class FakeMasking:
    async def tokens(self, incident_id, items):  # type: ignore[no-untyped-def]
        return {v: f"{k}_{abs(hash((incident_id, v))) % 16**6:06x}" for k, v in items if v}


class FakeCases:
    async def list(self, review_status, size):  # type: ignore[no-untyped-def]
        return [CASE]

    async def get(self, case_id):  # type: ignore[no-untyped-def]
        return CASE if case_id == "inc-1" else None


class FakeES:
    def __init__(self) -> None:
        self.calls: list[tuple[str, dict]] = []

    async def search(self, index, body):  # type: ignore[no-untyped-def]
        self.calls.append((index, body))
        aggs = {name: {"buckets": [{"key": "x", "doc_count": 3}]} for name in body.get("aggs", {})}
        return {"hits": {"total": {"value": 7}}, "aggregations": aggs}


def text(result) -> str:  # type: ignore[no-untyped-def]
    return result.content[0].text


async def test_case_view_is_masked_and_hides_notes() -> None:
    view = await mask_case(CASE, FakeMasking(), detail=True)  # type: ignore[arg-type]
    payload = json.dumps(view)
    for secret in ("PC-1", "alice", "10.0.0.5", "on leave", "bob"):
        assert secret not in payload, secret
    # PC-10 must become its own token, not "<token of PC-1>0"
    assert view["affected_hosts"][1] in view["summary"]
    assert view["timeline"][0]["host"] == view["affected_hosts"][1]


def test_hunts_file_is_valid() -> None:
    hunts = load_hunts(HUNTS)
    assert "encoded-powershell" in hunts and len(hunts) >= 5


def test_hunt_cannot_group_by_entities(tmp_path: Path) -> None:
    for field in ("host.name", "user.name", "source.ip", "winlog.event_data.TargetUserName"):
        f = tmp_path / "h.yml"
        f.write_text("hunts:\n  - {id: x, title: t, description: d, index: i, query: q, "
                     f"group_by: [{field}]}}\n")
        with pytest.raises(ValueError):
            load_hunts(f)


def test_build_query_limits_window() -> None:
    hunt = Hunt("x", "t", "d", "logs-*", "event.code:1", ("process.name",), ())
    q = build_query(hunt, 6, 5)
    assert q["query"]["bool"]["filter"][0] == {"range": {"@timestamp": {"gte": "now-6h"}}}
    assert q["aggs"]["process.name"]["terms"]["size"] == 5


async def test_tools() -> None:
    es = FakeES()
    mcp = build_server(FakeCases(), FakeMasking(), es, load_hunts(HUNTS))  # type: ignore[arg-type]
    names = {t.name for t in await mcp.list_tools()}
    assert names == {"list_cases", "get_case", "alert_statistics", "list_hunts", "run_hunt"}

    listed = json.loads(text(await mcp.call_tool("list_cases", {})))
    assert listed[0]["id"] == "inc-1" and "alice" not in json.dumps(listed)
    assert "not found" in text(await mcp.call_tool("get_case", {"case_id": "nope"}))

    hunt = json.loads(text(await mcp.call_tool("run_hunt", {"hunt_id": "failed-logons",
                                                            "hours": 99999})))
    assert hunt["total_hits"] == 7 and hunt["window_hours"] == 24 * 30
    assert es.calls[-1][0] == "logs-windows-*"
    assert "unknown hunt" in text(await mcp.call_tool("run_hunt", {"hunt_id": "x; DROP"}))

    stats = json.loads(text(await mcp.call_tool("alert_statistics", {"hours": 1})))
    assert stats["total"] == 7 and stats["by_rule"] == {"x": 3}


def test_http_requires_token(monkeypatch: pytest.MonkeyPatch) -> None:
    from starlette.testclient import TestClient

    from app.main import http_app

    monkeypatch.setenv("HUNTS_FILE", str(HUNTS))
    monkeypatch.setenv("MCP_ALLOWED_HOSTS", "testserver")
    with pytest.raises(SystemExit):
        http_app("short")
    token = "t" * 32
    with TestClient(http_app(token)) as client:
        assert client.post("/mcp", json={}).status_code == 401
        wrong = {"Authorization": "Bearer wrong"}
        assert client.post("/mcp", json={}, headers=wrong).status_code == 401
        ok = client.post("/mcp", headers={"Authorization": f"Bearer {token}",
                                          "Accept": "application/json, text/event-stream"},
                         json={"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": {
                             "protocolVersion": "2025-03-26", "capabilities": {},
                             "clientInfo": {"name": "test", "version": "1"}}})
        assert ok.status_code == 200, ok.text


async def test_masking_client_uses_stateless_tokens_endpoint():
    seen: list[tuple[str, dict]] = []

    def handler(request: httpx.Request) -> httpx.Response:
        body = json.loads(request.content)
        seen.append((request.url.path, body))
        return httpx.Response(200, json={"tokens": [f"t{i}" for i in range(len(body["items"]))]})

    client = httpx.AsyncClient(base_url="http://masking", transport=httpx.MockTransport(handler))
    got = await Masking("http://masking", client).tokens(
        "inc-1", [("user", "alice"), ("host", "PC-1"), ("user", "alice"), ("ip", "")])
    assert got == {"alice": "t0", "PC-1": "t1"}
    assert seen == [("/tokens", {"incident_id": "inc-1", "items": [
        {"kind": "user", "plaintext": "alice"}, {"kind": "host", "plaintext": "PC-1"}]})]
