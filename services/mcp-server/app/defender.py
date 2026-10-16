"""Optional endpoint mode: read Microsoft Defender status on the local Windows machine.

Enabled with MCP_ENABLE_DEFENDER=true when the server runs on Windows over stdio (for example
from Claude Desktop). Read-only: scans and configuration changes are deliberately not exposed,
because LLM output must not trigger actions (design rule 1, ROADMAP.md).
"""

from __future__ import annotations

import json
import platform
import subprocess

from mcp.server.mcpserver import MCPServer

from app.auth import audit

_FIELDS = ("AMServiceEnabled", "AntivirusEnabled", "AntispywareEnabled",
           "RealTimeProtectionEnabled", "BehaviorMonitorEnabled", "IoavProtectionEnabled",
           "AntivirusSignatureLastUpdated", "AntivirusSignatureVersion", "QuickScanAge",
           "FullScanAge", "IsTamperProtected")
_COMMAND = ("Get-MpComputerStatus | Select-Object " + ",".join(_FIELDS)
            + " | ConvertTo-Json -Depth 2")


def defender_status() -> dict[str, object]:
    if platform.system() != "Windows":
        return {"error": "Defender status is only available when the MCP server runs on Windows"}
    # Fixed argument list, no shell: nothing from the client reaches the command line.
    proc = subprocess.run(
        ["powershell.exe", "-NoProfile", "-NonInteractive", "-Command", _COMMAND],
        capture_output=True, text=True, timeout=60, check=False,
    )
    if proc.returncode != 0:
        return {"error": proc.stderr.strip()[:500]}
    return json.loads(proc.stdout)


def register(mcp: MCPServer) -> None:
    @mcp.tool()
    def get_defender_status() -> str:
        """Microsoft Defender protection and signature status of this Windows machine."""
        audit("get_defender_status")
        return json.dumps(defender_status(), indent=2, default=str)
