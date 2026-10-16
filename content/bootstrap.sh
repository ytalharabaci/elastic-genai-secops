#!/usr/bin/env bash
# Load Elastic GenAI SecOps content into Elasticsearch and Kibana. Idempotent; safe to re-run.
#
# Used by every deployment target:
#   - bare metal: deploy/baremetal/ubuntu/elk_setup_ubuntu_jammy.sh
#   - compose:    the "setup" service in deploy/compose
#
# Environment:
#   ES_URL                    Elasticsearch URL                    (default https://localhost:9200)
#   ES_USER / ES_PASSWORD     superuser credentials                (default user: elastic)
#   ES_CA_CERT                CA certificate for ES and Kibana     (optional; empty = system trust)
#   RETENTION_DAYS            delete logs/metrics after N days     (default 90)
#   LOGSTASH_INGEST_PASSWORD  create/update user logstash_ingest   (optional)
#   ESM_PLATFORM_PASSWORD     create/update user esm_platform      (optional; platform services)
#   KIBANA_URL                import detection rules into Kibana   (optional, e.g. http://localhost:5601)
#   BEATS_VERSIONS            load the Winlogbeat and Filebeat panw ingest pipelines for these Beats
#                             versions (default: the Elasticsearch version; "none" to skip)
#   BEATS_ARTIFACTS_URL       mirror of https://artifacts.elastic.co/downloads/beats (air-gapped)
set -Eeuo pipefail

CONTENT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ES_URL="${ES_URL:-https://localhost:9200}"
ES_USER="${ES_USER:-elastic}"
: "${ES_PASSWORD:?ES_PASSWORD is required}"
RETENTION_DAYS="${RETENTION_DAYS:-90}"

if ! [[ "${RETENTION_DAYS}" =~ ^[0-9]+$ ]] || (( RETENTION_DAYS < 1 )); then
  echo "RETENTION_DAYS must be a positive integer" >&2; exit 1
fi
# Short retention: roll over daily; long retention: weekly (keeps the shard count low on small clusters)
if (( RETENTION_DAYS <= 30 )); then ROLLOVER_MAX_AGE="1d"; else ROLLOVER_MAX_AGE="7d"; fi

CURL=(curl -sS -u "${ES_USER}:${ES_PASSWORD}")
[[ -n "${ES_CA_CERT:-}" ]] && CURL+=(--cacert "${ES_CA_CERT}")

log() { echo "[bootstrap] $*"; }

# request <method> <url> [curl args...]: prints the body, fails with the body on non-2xx
request() {
  local method="$1" url="$2"; shift 2
  local out code
  out="$(mktemp)"
  code="$("${CURL[@]}" -o "${out}" -w '%{http_code}' -X "${method}" "${url}" "$@")" || code=000
  if [[ "${code}" != 2* ]]; then
    echo "${method} ${url} failed (HTTP ${code}): $(cat "${out}")" >&2
    rm -f "${out}"; return 1
  fi
  cat "${out}"; rm -f "${out}"
}

es_put() {  # es_put <path> <json body>
  request PUT "${ES_URL}$1" -H 'Content-Type: application/json' --data-binary "$2" >/dev/null
}

wait_for() {  # wait_for <description> <url> [seconds]
  local deadline=$((SECONDS + ${3:-300}))
  until "${CURL[@]}" -f -o /dev/null "$2" 2>/dev/null; do
    (( SECONDS > deadline )) && { echo "timed out waiting for $1 ($2)" >&2; return 1; }
    sleep 5
  done
}

log "waiting for Elasticsearch at ${ES_URL}"
wait_for Elasticsearch "${ES_URL}/_cluster/health?wait_for_status=yellow&timeout=5s"

log "ILM policy esm-logs (delete after ${RETENTION_DAYS}d, rollover ${ROLLOVER_MAX_AGE})"
policy="$(sed -e "s/__RETENTION_DAYS__/${RETENTION_DAYS}/" -e "s/__ROLLOVER_MAX_AGE__/${ROLLOVER_MAX_AGE}/" \
  "${CONTENT_DIR}/elasticsearch/ilm/esm-logs.json")"
es_put "/_ilm/policy/esm-logs" "${policy}"

for f in "${CONTENT_DIR}"/elasticsearch/component-templates/*.json; do
  name="$(basename "${f}" .json)"
  log "component template ${name}"
  es_put "/_component_template/${name}" "$(cat "${f}")"
done

# Winlogbeat and Filebeat send events through Logstash to the ingest pipeline of their own version;
# endpoints run the stack version (deploy/endpoints/windows/prepare-share.ps1)
# shellcheck source=elasticsearch/beats-pipelines.sh
source "${CONTENT_DIR}/elasticsearch/beats-pipelines.sh"
if [[ -z "${BEATS_VERSIONS:-}" ]]; then
  BEATS_VERSIONS="$(request GET "${ES_URL}/" | sed -n 's/.*"number" *: *"\([^"]*\)".*/\1/p')"
fi
if [[ "${BEATS_VERSIONS}" != none ]]; then
  for v in ${BEATS_VERSIONS}; do
    [[ "${v}" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo "BEATS_VERSIONS: invalid version ${v}" >&2; exit 1; }
    if "${CURL[@]}" -f -o /dev/null "${ES_URL}/_ingest/pipeline/winlogbeat-${v}-routing" 2>/dev/null &&
       "${CURL[@]}" -f -o /dev/null "${ES_URL}/_ingest/pipeline/filebeat-${v}-panw-panos-pipeline" 2>/dev/null; then
      log "Beats ${v} ingest pipelines already loaded"
    elif ! load_beats_pipelines "${v}"; then
      log "WARNING: could not download the Beats ${v} packages from ${BEATS_ARTIFACTS_URL}; Windows and"
      log "  Palo Alto (Filebeat) events stay unparsed until the pipelines are loaded (BEATS_ARTIFACTS_URL"
      log "  for a mirror, or BEATS_VERSIONS=none and integrations/sources/README.md to do it by hand)"
    fi
  done
fi

for f in "${CONTENT_DIR}"/elasticsearch/roles/*.json; do
  name="$(basename "${f}" .json)"
  log "role ${name}"
  es_put "/_security/role/${name}" "$(cat "${f}")"
done

if [[ -n "${LOGSTASH_INGEST_PASSWORD:-}" ]]; then
  log "user logstash_ingest (role logstash_writer)"
  es_put "/_security/user/logstash_ingest" \
    "{\"password\":\"${LOGSTASH_INGEST_PASSWORD}\",\"roles\":[\"logstash_writer\"],\"full_name\":\"Logstash ingest\"}"
fi

if [[ -n "${ESM_PLATFORM_PASSWORD:-}" ]]; then
  log "user esm_platform (role esm_platform)"
  es_put "/_security/user/esm_platform" \
    "{\"password\":\"${ESM_PLATFORM_PASSWORD}\",\"roles\":[\"esm_platform\"],\"full_name\":\"Elastic GenAI SecOps platform services\"}"
fi

if [[ -n "${KIBANA_URL:-}" ]]; then
  log "waiting for Kibana at ${KIBANA_URL}"
  wait_for Kibana "${KIBANA_URL}/api/status" 600
  # The detection engine needs its alerts index before rules can be imported
  "${CURL[@]}" -o /dev/null -X POST "${KIBANA_URL}/api/detection_engine/index" -H 'kbn-xsrf: esm' || true

  log "importing detection rules"
  result="$(request POST "${KIBANA_URL}/api/detection_engine/rules/_import?overwrite=true" \
    -H 'kbn-xsrf: esm' -F "file=@${CONTENT_DIR}/detection-rules/rules.ndjson")"
  echo "[bootstrap] ${result}"
  if ! grep -q '"success":true' <<<"${result}"; then
    echo "rule import reported errors" >&2; exit 1
  fi
fi

log "done"
