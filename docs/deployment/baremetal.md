[← Back to README](../../README.md)

# Bare-metal install (Ubuntu 22.04)

Installs the `siem` profile on one host: Elasticsearch, Kibana and Logstash with every source
pipeline, the ILM policy, templates, roles and detection rules.

| Component | Listens on |
| --- | --- |
| Elasticsearch | `https://127.0.0.1:9200` (TLS, security on, localhost only) |
| Kibana | `http://<host>:5601` |
| Logstash | source ports, see [integrations/sources](../../integrations/sources/README.md) |

## Requirements

- Ubuntu 22.04, 4 vCPU, 8 GB RAM minimum (16 GB recommended), disk sized for your retention
- Outbound HTTPS to `artifacts.elastic.co`
- Root access

## Install

```bash
git clone https://github.com/yusufarbc/elastic-genai-secops.git
cd elastic-genai-secops
sudo ./deploy/baremetal/ubuntu/elk_setup_ubuntu_jammy.sh
```

Optional settings, passed as environment variables:

| Variable | Default | Meaning |
| --- | --- | --- |
| `ELASTIC_VERSION` | `8.13.4` | Version installed and held with `apt-mark hold` |
| `RETENTION_DAYS` | `90` | Logs and metrics are deleted after this many days |
| `SNAPSHOT_POLICY` | none | Existing SLM policy that must snapshot an index before it is deleted ([snapshots](../components/elasticsearch.md#snapshots)) |
| `KIBANA_BIND` | `0.0.0.0` | Interface Kibana listens on |
| `ES_NAMESPACE` | `default` | Data stream namespace (`logs-<dataset>-<namespace>`) |
| `FORTIGATE_TZ`, `PANOS_TZ` | `UTC` | Time zone of the firewall clocks (e.g. `Europe/Istanbul`); a wrong value shifts events out of the rules' time window |

Example: `sudo RETENTION_DAYS=30 ./deploy/baremetal/ubuntu/elk_setup_ubuntu_jammy.sh`

The script is safe to re-run; certificates, keys and passwords are kept.

## What it does

1. Adds the Elastic APT repository and installs the pinned version of the three packages.
2. Creates a CA and PEM certificates for Elasticsearch (HTTP and transport) in `/etc/elasticsearch/certs`.
3. Installs `config/elasticsearch.yml` and `config/kibana.yml`, copies `integrations/sources` to
   `/etc/logstash/conf.d` and installs its `pipelines.yml`.
4. Starts Elasticsearch, sets the `elastic` and `kibana_system` passwords.
5. Runs [`content/bootstrap.sh`](../../content/bootstrap.sh): ILM policy `esm-logs`, the `logs@custom` and
   `metrics@custom` component templates, the `logstash_writer` role and the `logstash_ingest` user.
6. Stores the Kibana and Logstash secrets in their keystores and starts both services.
7. Imports the detection rules into Kibana.

## Credentials

Passwords are written to `/root/esm-credentials` (mode 0600) and are not printed:

```bash
sudo cat /root/esm-credentials
```

Log in to Kibana as `elastic` with `ELASTIC_PASSWORD`.

## Firewall

Open only the ports of the sources you use:

```bash
sudo ufw allow 5601/tcp                     # Kibana
sudo ufw allow 5044:5047/tcp                # Beats inputs
sudo ufw allow 5514:5517/tcp
sudo ufw allow 5514:5517/udp                # syslog, FortiGate, Palo Alto
```

## Next steps

- Connect log sources: [integrations/sources](../../integrations/sources/README.md)
- Roll out Winlogbeat and Sysmon to Windows endpoints: `deploy/endpoints/windows/`
- Troubleshooting: [operations/troubleshooting.md](../operations/troubleshooting.md)

## Checks

```bash
systemctl status elasticsearch kibana logstash
sudo journalctl -u logstash -f
source <(sudo cat /root/esm-credentials)
curl -s --cacert /etc/elasticsearch/certs/ca.crt -u "elastic:${ELASTIC_PASSWORD}" \
  "https://localhost:9200/_data_stream?pretty" | grep '"name"'
```
