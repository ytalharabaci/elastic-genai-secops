[← Back to README](../../README.md)

# Elasticsearch

Version **8.13.4**, Basic license, security and TLS always on.

## Where it is configured

| Target | Configuration | TLS material |
| --- | --- | --- |
| Compose | environment in `deploy/compose/siem.yml` (single node, heap `ES_HEAP`) | `certs` volume, created by the `certs` service |
| Bare metal | `deploy/baremetal/ubuntu/config/elasticsearch.yml` (localhost only) | `/etc/elasticsearch/certs/{ca,http,transport}.{crt,key}` (PEM) |
| Kubernetes | `deploy/kubernetes/base/elasticsearch.yaml` (ECK); 3 nodes in `onprem`/`gke` | ECK secrets `esm-es-http-certs-*` |

`node.store.allow_mmap: false` is set on Kubernetes so no privileged `vm.max_map_count` init
container is needed (GKE Autopilot). Bare metal and Docker Desktop set `vm.max_map_count=262144`.

## Content loaded by `content/bootstrap.sh`

| Object | Name | Purpose |
| --- | --- | --- |
| ILM policy | `esm-logs` | Rollover at 50 GB per primary shard or 1 day (≤ 30 days retention) / 7 days; delete after `RETENTION_DAYS`, after a snapshot when `SNAPSHOT_POLICY` is set |
| Component templates | `logs@custom`, `metrics@custom` | Attach `esm-logs` to the built-in `logs` and `metrics` index templates |
| Roles | `logstash_writer`, `esm_platform` | See [architecture overview](../architecture/overview.md#users-and-roles) |
| Users | `logstash_ingest`, `esm_platform` | Passwords from `.env`, `secrets.env` or `/root/esm-credentials` |

Using the `@custom` component templates keeps Elastic's own `logs`/`metrics` templates (ECS
mappings, data stream settings) and only overrides the lifecycle. A separate `logs-*-*` template
would never apply because the built-in one has a higher priority.

## Useful checks

```bash
ES=https://localhost:9200; AUTH="elastic:$ELASTIC_PASSWORD"; CA=--cacert ca.crt   # or -k in a lab
curl -s $CA -u "$AUTH" "$ES/_cluster/health?pretty"
curl -s $CA -u "$AUTH" "$ES/_data_stream/logs-*?filter_path=data_streams.name,data_streams.ilm_policy"
curl -s $CA -u "$AUTH" "$ES/logs-*/_ilm/explain?only_errors=true&pretty"
curl -s $CA -u "$AUTH" "$ES/_cat/indices/esm-*?v"
```

## Snapshots

Snapshots are the archive behind the hot tier (ADR-010): `RETENTION_DAYS` is how long data stays
searchable in Elasticsearch, `SNAPSHOT_EXPIRE_AFTER` (default 730 days) how long snapshots are kept.
With a snapshot policy configured, ILM deletes an index only after that policy has snapshotted it.

| Target | Repository | How to enable |
| --- | --- | --- |
| Kubernetes `gke` | GCS, Workload Identity | On by default (`esm-gcs-snapshots` Job, policy `esm-daily`) |
| Kubernetes `onprem` | S3-compatible (MinIO, Ceph RGW, AWS S3) | Add `components/snapshots-s3`; set `S3_ENDPOINT`, `S3_BUCKET` in it and create Secret `esm-snapshot-s3` (see the component's header) |
| Compose | Shared directory or NFS | `-f snapshots-fs.yml` |
| Compose | S3-compatible | `-f snapshots-s3.yml` with `S3_*` in `.env`; `--profile s3-lab` adds a local SeaweedFS to try it |
| Bare metal | Shared directory or NFS mount | `SNAPSHOT_REPO_PATH=/mnt/esm-snapshots` |

Outside `gke`, `content/bootstrap.sh` registers the repository `esm-snapshots` and the SLM policy:

| Variable | Meaning |
| --- | --- |
| `SNAPSHOT_REPOSITORY_TYPE` | `fs` or `s3`; empty = no repository is registered |
| `SNAPSHOT_REPOSITORY_SETTINGS` | Repository settings as JSON, e.g. `{"location": "/mnt/esm-snapshots"}` or `{"bucket": "esm", "base_path": "esm"}` |
| `SNAPSHOT_POLICY` | SLM policy to create and wait for (default `esm-daily` when a repository is registered) |
| `SNAPSHOT_SCHEDULE` | SLM cron schedule (default `0 30 1 * * ?`, 01:30 daily) |
| `SNAPSHOT_EXPIRE_AFTER` | Delete snapshots older than this (default `730d`; keeps at least 7) |

Registering verifies that every node can write to the repository, so a path missing from
`path.repo`, an unmounted share or wrong S3 credentials fail the bootstrap instead of the first
snapshot. An `fs` repository on a multi-node cluster must be storage that all nodes mount. S3
credentials are secure settings: they go into the Elasticsearch keystore (ECK `secureSettings`, or
`elasticsearch-keystore add s3.client.default.access_key` on bare metal), never into
`elasticsearch.yml`.

`SNAPSHOT_POLICY` alone (without a repository type) points ILM at a policy that already exists, as
the `gke` overlay does. Elasticsearch refuses an ILM policy that names a missing SLM policy, so the
bootstrap waits up to 10 minutes for it. Without any snapshot policy indices are deleted on
schedule and the bootstrap logs a warning.

If snapshots keep failing, indices wait in the delete phase and the disk fills up instead of data
being lost. Check:

```bash
curl -s $CA -u "$AUTH" "$ES/_slm/policy/esm-daily?filter_path=*.last_success,*.last_failure&pretty"
curl -s $CA -u "$AUTH" "$ES/logs-*/_ilm/explain?filter_path=indices.*.step,indices.*.step_info&pretty" \
  | grep -B1 -A2 wait-for-snapshot
```

Restore a data stream's backing indices from the latest snapshot. They are renamed so they do not
clash with live data, made visible (backing indices are hidden), and left out of ILM, which would
otherwise delete them at once because they are older than `RETENTION_DAYS`:

```bash
SNAP=$(curl -s $CA -u "$AUTH" "$ES/_cat/snapshots/esm-snapshots?h=id&s=end_epoch" | tail -1)
curl -s $CA -u "$AUTH" -X POST "$ES/_snapshot/esm-snapshots/$SNAP/_restore" -H 'Content-Type: application/json' \
  -d '{"indices": ".ds-logs-system.syslog-default-*", "rename_pattern": "(.+)", "rename_replacement": "restored-$1",
       "index_settings": {"index.hidden": false}, "ignore_index_settings": ["index.lifecycle.name"]}'
```
