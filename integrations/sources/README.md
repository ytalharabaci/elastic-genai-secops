# Log source integrations

One folder per source. Each folder holds the Logstash pipeline and, where needed, the collector
configuration for the sending side. `pipelines.yml` enables all of them; remove a block to disable a source.

## Ports and data streams

| Source | Folder | Listens on | Sender | Data stream |
| --- | --- | --- | --- | --- |
| Palo Alto (Filebeat) | `paloalto/logstash-beats.conf` | 5044/tcp (beats) | Filebeat `panw` module | `logs-paloalto-*` |
| Windows | `windows/logstash.conf` | 5045/tcp (beats) | Winlogbeat on endpoints or on a WEC server | `logs-windows-*` |
| Metricbeat / Heartbeat | `beats-health/logstash.conf` | 5046/tcp (beats) | Metricbeat, Heartbeat | `metrics-metricbeat-*`, `logs-heartbeat-*` |
| Kaspersky | `kaspersky/logstash.conf` | 5047/tcp (beats) | Filebeat reading the KSC syslog export | `logs-kaspersky-*` |
| Syslog | `syslog/logstash.conf` | 5514/udp+tcp (RFC3164), 5515/tcp (RFC5424) | Any syslog sender | `logs-syslog-*` |
| FortiGate | `fortigate/logstash.conf` | 5516/udp+tcp | FortiGate syslog (key=value) | `logs-fortigate-*` |
| Palo Alto (syslog) | `paloalto/logstash-syslog.conf` | 5517/udp+tcp | PAN-OS syslog server profile | `logs-paloalto-*` |

All ports are above 1024 so Logstash can run unprivileged. The namespace (`*`) is `default` unless
`ES_NAMESPACE` is set.

## Pipeline settings

Every pipeline reads its Elasticsearch connection from environment variables, so the same files work
on bare metal and in containers:

| Variable | Default | Set by |
| --- | --- | --- |
| `ES_HOSTS` | `https://localhost:9200` | compose / `/etc/default/logstash` |
| `ES_USER` | `logstash_ingest` | |
| `ES_PW` | (required) | Logstash keystore (bare metal) or environment (compose) |
| `ES_CA_CERT` | `/etc/logstash/certs/ca.crt` | |
| `ES_NAMESPACE` | `default` | |
| `LS_SOURCES_DIR` | `/etc/logstash/conf.d` | location of this folder on the Logstash host |
| `FORTIGATE_TZ`, `PANOS_TZ` | `UTC` | time zone of the device clocks |

The `logstash_ingest` user and its `logstash_writer` role are created by `content/bootstrap.sh`.

## Windows and Palo Alto (Filebeat): ingest pipelines

Winlogbeat 8 parses Security, Sysmon and PowerShell events, and the Filebeat `panw` module PAN-OS
logs, in Elasticsearch ingest pipelines named after the Beat's version (`winlogbeat-8.13.4-routing`,
`filebeat-8.13.4-panw-panos-pipeline`). Without them events are stored, but fields such as
`process.name` and `event.category` are missing and the Windows detection rules cannot match.

`content/bootstrap.sh` loads them on every target, the same pipelines `winlogbeat setup --pipelines`
and `filebeat setup --pipelines --modules panw` would load. It downloads the Beats packages from
artifacts.elastic.co, checks Elastic's SHA-512 and loads the pipelines; the files are Elastic
License 2.0 and are not stored in this repository. Pipelines that already exist are kept.

| Setting | Default | Use |
| --- | --- | --- |
| `BEATS_VERSIONS` | the Elasticsearch version | Space-separated Beats versions, e.g. `8.13.4 8.14.3` while endpoints are being upgraded; `none` to skip |
| `BEATS_ARTIFACTS_URL` | `https://artifacts.elastic.co/downloads/beats` | Mirror with the same layout (`winlogbeat/winlogbeat-<v>-windows-x86_64.zip`, `filebeat/filebeat-<v>-linux-x86_64.tar.gz`, each with `.sha512`) for hosts without internet access |

The endpoints install the stack version (`prepare-share.ps1 -ElasticVersion`). After moving them to
another version, add it to `BEATS_VERSIONS` and run the bootstrap again. If the download fails the
bootstrap logs a warning and goes on; then load the pipelines by hand from a Windows host that can
reach Elasticsearch:

```powershell
.\winlogbeat.exe setup --pipelines `
  -E output.logstash.enabled=false `
  -E 'output.elasticsearch.hosts=["https://ELK_SERVER:9200"]' `
  -E output.elasticsearch.username=elastic -E output.elasticsearch.password=<password> `
  -E 'output.elasticsearch.ssl.certificate_authorities=["C:\path\to\ca.crt"]'
```

On the bare-metal install Elasticsearch listens on localhost only; use an SSH tunnel
(`ssh -L 9200:localhost:9200 <elk-host>`) and `https://localhost:9200`.

Sysmon configuration: [`windows/sysmon/sysmon.xml`](windows/sysmon/sysmon.xml).

## Palo Alto: two options

- **Syslog direct** (simplest): point a PAN-OS syslog server profile at port 5517. TRAFFIC and THREAT
  logs are parsed into ECS fields; other log types are stored raw.
- **Filebeat panw module** (complete parsing): PAN-OS sends to rsyslog on a collector host
  (`paloalto/collector/rsyslog-30-paloalto.conf`, UDP 5518), Filebeat reads the file and ships it to 5044.
  The bootstrap loads the module's ingest pipelines ([above](#windows-and-palo-alto-filebeat-ingest-pipelines)).

## Test a pipeline

Send a syslog line and look for it in Kibana (Discover, data view `logs-*`):

```bash
logger -n <logstash-host> -P 5514 -d "esm test message"
```
