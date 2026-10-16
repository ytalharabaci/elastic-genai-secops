[← Back to README](../../README.md)

# Windows endpoints: Winlogbeat, Metricbeat, Heartbeat and Sysmon via Group Policy

Rolls out the collectors to domain-joined Windows machines with GPO computer startup scripts.
Files: [`deploy/endpoints/windows`](../../deploy/endpoints/windows). Configurations come from
[`integrations/sources`](../../integrations/sources/README.md).

Before you start, enable the Windows audit and PowerShell logging policies:
[windows-audit-policy.md](windows-audit-policy.md).

## 1. Build the deployment share

On any Windows machine with internet access (PowerShell 5.1+):

```powershell
.\deploy\endpoints\windows\prepare-share.ps1 `
    -ShareUnc \\fs01\elk `
    -ElkServer 10.0.0.20 `
    -DomainController 10.0.0.10 `
    -ElasticVersion 8.13.4 `
    -OutputPath C:\elk
```

The script:

- downloads the Winlogbeat, Metricbeat and Heartbeat MSIs from `artifacts.elastic.co` and checks
  them against the published SHA-512;
- downloads Sysmon from Sysinternals and checks its Microsoft Authenticode signature;
- writes `winlogbeat.yml`, `metricbeat.yml`, `heartbeat.yml` with your Logstash address and
  `sysmon.xml`;
- writes the four startup scripts with your share path.

```text
C:\elk\
├── winlogbeat.bat   metricbeat.bat   heartbeat.bat   sysmon.bat
├── winlogbeat\  winlogbeat.msi, winlogbeat.yml
├── metricbeat\  metricbeat.msi, metricbeat.yml
├── heartbeat\   heartbeat.msi,  heartbeat.yml
└── sysmon\      Sysmon64.exe,   sysmon.xml
```

The Beats version must match the Elastic Stack version.

## 2. Share it read-only

Copy the folder to a file server and share it:

- Share permission: `Domain Computers` (or `Authenticated Users`) → Read
- NTFS permission: `Domain Computers` → Read & execute

Startup scripts run as `LOCAL SYSTEM`, which accesses the network as the computer account, so the
computer accounts need read access. Nobody needs write access.

## 3. Create the GPO

1. Open Group Policy Management (`gpmc.msc`), right-click the target OU → **Create a GPO in this
   domain, and Link it here…** → name it `ESM-Agent-Deployment`.
2. Edit it: **Computer Configuration → Policies → Windows Settings → Scripts (Startup/Shutdown) →
   Startup → Add**, and add the four scripts from the share (`\\fs01\elk\sysmon.bat`, then
   `winlogbeat.bat`, `metricbeat.bat`, `heartbeat.bat`).
3. Optional: **Computer Configuration → Policies → Administrative Templates → System → Scripts →
   Run startup scripts asynchronously** = Disabled, so they finish before logon.

The scripts are idempotent. On every boot they install a missing agent; otherwise they compare the
configuration on the share with the local copy and only restart the service when it changed. To
roll out a new configuration, update the file on the share.

## 4. Apply and verify

On a client: `gpupdate /force` and restart. Then:

```cmd
sc query winlogbeat
sc query metricbeat
sc query heartbeat
sc query sysmon64
```

Startup-script results are in **Event Viewer → Applications and Services Logs → Microsoft →
Windows → GroupPolicy → Operational** (event IDs 5016 = script completed, 7016 = script failed).

## 5. Winlogbeat ingest pipelines

Winlogbeat 8 parses events in Elasticsearch ingest pipelines of its own version. The SIEM bootstrap
loads them for the stack version, which is what `prepare-share.ps1` installs by default. If you
roll out another `-ElasticVersion`, add it to `BEATS_VERSIONS` and rerun the bootstrap: see
[integrations/sources/README.md](../../integrations/sources/README.md#windows-and-palo-alto-filebeat-ingest-pipelines).

## Notes

- Beats talk to Logstash on ports 5045 (Winlogbeat) and 5046 (Metricbeat, Heartbeat) without TLS.
  Keep these ports reachable only from your client networks until TLS inputs are added (see ROADMAP).
- A Windows Event Collector (WEF) can run Winlogbeat with the `ForwardedEvents` channel instead of
  installing it on every endpoint; see the comment in `integrations/sources/windows/winlogbeat.yml`.
- The Sysinternals license applies to Sysmon; it is downloaded, not redistributed.
