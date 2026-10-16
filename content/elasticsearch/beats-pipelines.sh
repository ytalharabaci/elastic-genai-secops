# shellcheck shell=bash
# Loads the Elasticsearch ingest pipelines that Winlogbeat and the Filebeat panw module expect
# (`winlogbeat setup --pipelines`, `filebeat setup --pipelines --modules panw`) without running a
# Beat. Sourced by content/bootstrap.sh, which provides log, request and ES_URL.
#
# The pipelines ship inside the Beats packages under the Elastic License 2.0, so they are not kept
# in this repository: they are downloaded at install time and checked against Elastic's SHA-512.
# BEATS_ARTIFACTS_URL can point at a mirror for clusters without internet access.

BEATS_ARTIFACTS_URL="${BEATS_ARTIFACTS_URL:-https://artifacts.elastic.co/downloads/beats}"

# beats_fetch <url> <file>: download <url> and verify it against <url>.sha512
beats_fetch() {
  local want
  want="$(curl -sSfL --connect-timeout 10 "$1.sha512" | awk '{print $1}')" || return 1
  curl -sSfL --connect-timeout 10 --retry 2 -o "$2" "$1" || return 1
  [[ -n "${want}" && "$(sha512sum "$2" | awk '{print $1}')" == "${want}" ]] \
    || { echo "checksum mismatch for $1" >&2; return 1; }
}

# beats_put <pipeline id> <yaml file> <id prefix>: resolve {< IngestPipeline "name" >} to
# <id prefix><name>, the IDs the Beat itself uses, and load the pipeline. Elasticsearch recognises a
# YAML body only by a leading "---", which some of the files lack.
beats_put() {
  sed -E -e '1{/^---/!s/^/---\n/}' -e "s/\{< *IngestPipeline \"([^\"]+)\" *>\}/$3\1/g" "$2" > "$2.resolved"
  request PUT "${ES_URL}/_ingest/pipeline/$1" -H 'Content-Type: application/yaml' \
    --data-binary "@$2.resolved" >/dev/null
}

# load_beats_pipelines <version>: returns 1 if a package could not be downloaded
load_beats_pipelines() {
  local v="$1" work f rc=0
  work="$(mktemp -d)"

  # Winlogbeat: module/<module>/ingest/<name>.yml -> winlogbeat-<v>-<name>; the Beat sends events to
  # winlogbeat-<v>-routing, which calls the others
  if beats_fetch "${BEATS_ARTIFACTS_URL}/winlogbeat/winlogbeat-${v}-windows-x86_64.zip" "${work}/winlogbeat.zip"; then
    unzip -q "${work}/winlogbeat.zip" '*/module/*/ingest/*.yml' -d "${work}"
    rm -f "${work}/winlogbeat.zip"
    for f in "${work}"/winlogbeat-*/module/*/ingest/*.yml; do
      beats_put "winlogbeat-${v}-$(basename "${f}" .yml)" "${f}" "winlogbeat-${v}-"
    done
    log "Winlogbeat ${v} ingest pipelines"
  else
    rc=1
  fi

  # Filebeat panw: module/panw/panos/ingest/<name>.yml -> filebeat-<v>-panw-panos-<name>; the entry
  # point is ...-pipeline. The YAML is the same in every platform's package.
  if beats_fetch "${BEATS_ARTIFACTS_URL}/filebeat/filebeat-${v}-linux-x86_64.tar.gz" "${work}/filebeat.tar.gz"; then
    tar -xzf "${work}/filebeat.tar.gz" -C "${work}" --wildcards '*/module/panw/panos/ingest/*.yml'
    rm -f "${work}/filebeat.tar.gz"
    for f in "${work}"/filebeat-*/module/panw/panos/ingest/*.yml; do
      beats_put "filebeat-${v}-panw-panos-$(basename "${f}" .yml)" "${f}" "filebeat-${v}-panw-panos-"
    done
    log "Filebeat ${v} panw ingest pipelines"
  else
    rc=1
  fi

  rm -rf "${work}"
  return "${rc}"
}
