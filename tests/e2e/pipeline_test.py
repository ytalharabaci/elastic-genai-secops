"""End-to-end check of the triage pipeline against a running stack (siem + platform).

Compose:
    cd deploy/compose && docker compose -f siem.yml -f platform.yml up -d --build
    python tests/e2e/pipeline_test.py
Kubernetes (port-forward svc/esm-es-http 9200 and svc/bff 8080 first):
    ELASTIC_PASSWORD=$(kubectl -n esm get secret esm-es-elastic-user -o jsonpath='{.data.elastic}' | base64 -d) \
      python tests/e2e/pipeline_test.py
ES_URL and BFF_URL override the default https://localhost:9200 and http://localhost:8080.

Writes five synthetic Sysmon "Office spawned PowerShell" events for one host and user, then waits
for: Kibana rule WIN-001 alerts -> detection-service -> alert-gateway (threshold 5) -> enrichment +
masking -> llm-orchestrator (mock) -> case-service -> bff. Checks that the case is un-masked and
that the LLM audit record contains only masked identifiers. Uses only the standard library.
"""

from __future__ import annotations

import base64
import json
import os
import ssl
import sys
import time
import urllib.error
import urllib.request
import uuid
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]


def _setting(key: str) -> str:
    if value := os.getenv(key):
        return value
    env = dict(line.split("=", 1) for line in (ROOT / "deploy/compose/.env").read_text().splitlines()
               if "=" in line and not line.startswith("#"))
    return env[key]


def analyst_token() -> tuple[str, str]:
    """The first analyst in ANALYST_TOKENS (name:token,...) from the environment or .env."""
    name, _, token = _setting("ANALYST_TOKENS").split(",")[0].strip().partition(":")
    return name, token


ES = os.getenv("ES_URL", "https://localhost:9200").rstrip("/")
BFF = os.getenv("BFF_URL", "http://localhost:8080").rstrip("/")
AUTH = "Basic " + base64.b64encode(f"elastic:{_setting('ELASTIC_PASSWORD')}".encode()).decode()
ANALYST, _token = analyst_token()
ANALYST_AUTH = f"Bearer {_token}"
CTX = ssl._create_unverified_context()  # local stack with a self-signed CA
TIMEOUT = 15 * 60

RUN = uuid.uuid4().hex[:6]
HOST, USER = f"E2E-PC-{RUN}", f"e2e-user-{RUN}"


def call(url: str, body: object | None = None, method: str | None = None, auth: str = AUTH):
    data = json.dumps(body).encode() if isinstance(body, (dict, list)) else body
    headers = {"Content-Type": "application/x-ndjson" if isinstance(body, bytes) else "application/json",
               "Authorization": auth}
    req = urllib.request.Request(url, data=data, method=method, headers=headers)
    with urllib.request.urlopen(req, context=CTX, timeout=30) as resp:
        return json.loads(resp.read() or b"null")


def step(msg: str) -> None:
    print(f"[{time.strftime('%H:%M:%S')}] {msg}", flush=True)


def wait_for(desc: str, fn, timeout: float = TIMEOUT, every: float = 15):  # type: ignore[no-untyped-def]
    step(f"waiting for {desc}")
    deadline = time.time() + timeout
    while time.time() < deadline:
        try:
            result = fn()
            if result:
                return result
        except (urllib.error.URLError, KeyError, IndexError, TypeError):
            pass
        time.sleep(every)
    sys.exit(f"FAILED: timed out waiting for {desc}")


def main() -> None:
    ts = time.strftime("%Y-%m-%dT%H:%M:%S.000Z", time.gmtime())
    doc = {"@timestamp": ts, "host": {"name": HOST}, "user": {"name": USER},
           "event": {"code": "1", "category": ["process"], "type": ["start"]},
           "process": {"name": "powershell.exe", "parent": {"name": "WINWORD.EXE"},
                       "command_line": f"powershell -enc AAAA {USER}"}}
    bulk = "".join(json.dumps({"create": {}}) + "\n" + json.dumps(doc) + "\n" for _ in range(5))
    res = call(f"{ES}/logs-windows-default/_bulk?refresh=true", bulk.encode(), "POST")
    assert not res["errors"], res
    step(f"indexed 5 events for host {HOST} / user {USER}")

    def alerts() -> int:
        q = {"size": 0, "query": {"bool": {"filter": [
            {"term": {"kibana.alert.rule.rule_id": "esm-win-001"}},
            {"term": {"host.name": HOST}}]}}}
        n = call(f"{ES}/.alerts-security.alerts-*/_search", q, "POST")
        return n["hits"]["total"]["value"] >= 5

    wait_for("5 WIN-001 alerts from Kibana (rules run every 5 minutes)", alerts)

    def case() -> dict | None:
        cases = call(f"{BFF}/api/cases?size=100", auth=ANALYST_AUTH)["cases"]
        return next((c for c in cases if HOST in c.get("affected_hosts", [])), None)

    c = wait_for("the case in the bff API", case, every=10)
    step(f"case {c['id']}: triage_status={c['triage_status']} severity={c['severity']}")

    checks = {
        "triaged by the LLM": c["triage_status"] == "triaged",
        "alerts correlated into one incident": c["alert_count"] == 5,
        "host un-masked": c["affected_hosts"] == [HOST],
        "user un-masked": c["affected_users"] == [USER],
        "LLM summary un-masked": HOST in c["summary"] and USER in c["summary"],
        "pending analyst review": c["review_status"] == "pending",
    }

    def audit_record() -> dict | None:
        hits = call(f"{ES}/esm-llm-audit/_search", {
            "size": 1, "query": {"term": {"incident_id.keyword": c["incident_id"]}}}, "POST")
        return hits["hits"]["hits"][0]["_source"] if hits["hits"]["hits"] else None

    # The audit record is written just before the case exists, but becomes searchable only after
    # the next index refresh (about a second later)
    record = wait_for("the LLM audit record", audit_record, timeout=60, every=2)
    sent = record["data_block"] + (record["response"] or "")
    checks["audit record written"] = record["outcome"] == "triaged"
    checks["no plaintext host/user/command line reached the LLM"] = (
        HOST not in sent and USER not in sent and "-enc" not in sent)

    # The body claims another analyst; bff must record the one the token belongs to (ADR-028)
    reviewed = call(f"{BFF}/api/cases/{c['id']}/review",
                    {"status": "approved", "analyst": "someone-else", "notes": "pipeline test"},
                    "POST", auth=ANALYST_AUTH)
    checks["analyst review recorded"] = reviewed["review_status"] == "approved"
    checks["review recorded under the token's analyst"] = reviewed.get("reviewed_by") == ANALYST
    try:
        call(f"{BFF}/api/cases?size=1", auth="Bearer not-a-valid-analyst-token-000000")
        checks["bff rejects an unknown token"] = False
    except urllib.error.HTTPError as err:
        checks["bff rejects an unknown token"] = err.code == 401

    for name, ok in checks.items():
        print(f"  {'PASS' if ok else 'FAIL'}  {name}")
    if not all(checks.values()):
        sys.exit("FAILED")
    step("end-to-end pipeline OK")


if __name__ == "__main__":
    main()
