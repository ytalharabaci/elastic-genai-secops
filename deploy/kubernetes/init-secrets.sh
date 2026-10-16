#!/usr/bin/env sh
# Create deploy/kubernetes/overlays/<overlay>/secrets.env with random passwords (git-ignored).
# Existing values are kept; missing keys are added.
#   ./deploy/kubernetes/init-secrets.sh lab|onprem|gke
set -eu
overlay="${1:?usage: init-secrets.sh <overlay>}"
dir="$(cd "$(dirname "$0")" && pwd)/overlays/${overlay}"
[ -d "$dir" ] || { echo "unknown overlay: $overlay" >&2; exit 1; }
file="$dir/secrets.env"
touch "$file"; chmod 600 "$file"

gen() { LC_ALL=C tr -dc 'A-Za-z0-9' </dev/urandom | head -c "$1"; }
add() { grep -q "^$1=" "$file" || echo "$1=$2" >> "$file"; }

add LOGSTASH_INGEST_PASSWORD "$(gen 24)"
add ESM_PLATFORM_PASSWORD "$(gen 24)"
# One analyst to start with; add name:token pairs per person (ADR-028). An older MCP_TOKEN is kept.
token="$(sed -n 's/^MCP_TOKEN=//p' "$file")"
add ANALYST_TOKENS "analyst:${token:-$(gen 48)}"
add LLM_API_KEY ""
echo "wrote $file (set LLM_API_KEY there when LLM_PROVIDER is not mock)"
