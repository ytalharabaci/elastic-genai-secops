# ADR-027: Correlation windows in NATS KV, shared by alert-gateway replicas

**Status:** Accepted · **Date:** 2026-10-16 · Resolves the correlation-state limitation in ROADMAP.md

**Context:** `alert-gateway` acknowledges an alert as soon as it is added to an open correlation
window. The windows lived in process memory, so a restart lost every alert of an unfinished window,
and a second replica would have formed its own, overlapping incidents. The platform already runs
NATS JetStream for the bus (ADR-017); Elasticsearch would add a second dependency to the hot path.

**Decision:**

- Windows are stored in the JetStream key-value bucket `esm-correlation` (file storage, history 1,
  TTL equal to the `ESM` stream's 7-day MaxAge). The key is a hash of host|user|technique; the value
  is the incident so far plus the window's expiry.
- Every write is compare-and-set on the entry's revision. A conflicting replica reads the window
  again and retries, so replicas sharing the `alert-gateway` consumer form one incident per window.
- A complete window (threshold reached or expired) is first marked closed, then published, then
  deleted. Nothing is added to a closed window. Whoever finds a closed window, on ingest or in the
  15-second flush, publishes and deletes it; while that publish fails, alerts for the key are
  redelivered and, after MaxDeliver attempts, go to `esm.dlq`.
- An alert is acknowledged once it is stored, open or closed. A failed publish leaves the window
  closed for the next ingest or flush instead of redelivering the alert into a second window.
- The incident ID is the JetStream message ID, so concurrent publications of one window are
  deduplicated (10-minute duplicate window). `case-service` creates cases with `op_type=create`.

**Consequences:** Restarts keep open windows, and `alert-gateway` can run several replicas. A
replica that stops between publishing and deleting a window, with no replica flushing within 10
minutes, causes a second publication of the same incident; the case is not
duplicated, but the LLM may triage it twice. The correlator now needs NATS: another `BUS_BACKEND`
must provide its own `Store` (tests use the in-memory one). `detection-service` still runs alone
because it owns the alert checkpoint.
