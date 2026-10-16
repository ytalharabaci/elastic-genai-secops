# ADR-030: TLS from the Beats to Logstash, opt-in per site

**Status:** Accepted · **Date:** 2026-10-16 · Part of the transport-security item in ROADMAP.md

**Context:** Winlogbeat, Metricbeat, Heartbeat and Filebeat send to the Logstash beats inputs
(5044-5047) in plain TCP, so Windows security events cross the network unencrypted and anyone who
reaches the ports can inject events. Turning TLS on changes the configuration of every endpoint,
which a GPO rollout applies only at the next startup.

**Decision:**

- The beats inputs read `BEATS_TLS` (default `false`), `BEATS_TLS_CERT` and `BEATS_TLS_KEY`. The
  key must be PKCS#8. Logstash validates both paths even with TLS off, so their defaults point at
  the ES CA, which every Logstash host has.
- On Compose the `certs` service issues the Logstash certificate from the stack's CA with the
  names and addresses in `LOGSTASH_TLS_SANS`, converts the key to PKCS#8, and re-issues it when
  the list changes.
- `prepare-share.ps1 -CaCert` puts the CA on the deployment share and renders the Beats
  configurations with `ssl.enabled: true` and that CA; the startup scripts copy the CA to
  `C:\ProgramData\Elastic\Beats\esm-ca.crt` and restart a Beat when it changes.
- TLS stays off by default, so an upgrade does not cut off endpoints that still run the old
  configuration. Syslog inputs stay plain (devices send UDP syslog).

**Consequences:** A site turns TLS on in two steps that must meet: Logstash (`BEATS_TLS=true`) and
the endpoints (new share). Between them, Beats on the other setting cannot deliver and buffer
their events. Bare metal and Kubernetes keep plain inputs until their certificates are automated.
