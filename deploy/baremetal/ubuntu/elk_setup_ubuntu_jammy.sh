#!/usr/bin/env bash
# Elastic GenAI SecOps: single-host Elastic Stack installer for Ubuntu 22.04 (profile "siem")
#
#   sudo ./deploy/baremetal/ubuntu/elk_setup_ubuntu_jammy.sh
#
# - Elasticsearch on https://127.0.0.1:9200 (TLS, security on, Basic license)
# - Kibana on http://<host>:5601
# - Logstash with every pipeline in integrations/sources (ports: integrations/sources/README.md)
# - ILM, templates, roles and detection rules loaded by content/bootstrap.sh
# - Credentials are written to /root/esm-credentials (mode 0600), never printed
#
# Settings (environment variables):
#   ELASTIC_VERSION  Elastic Stack version to install and hold   (default 8.13.4)
#   RETENTION_DAYS   delete logs/metrics after N days             (default 90)
#   SNAPSHOT_POLICY  existing SLM policy to wait for before delete (default none)
#   SNAPSHOT_REPO_PATH  shared directory (e.g. an NFS mount) for snapshots; registers repository
#                    esm-snapshots and policy esm-daily, and ILM then waits for it (default none)
#   KIBANA_BIND      interface Kibana listens on                  (default 0.0.0.0)
#   ES_NAMESPACE     data stream namespace                        (default default)
#   FORTIGATE_TZ     time zone of FortiGate clocks, e.g. Europe/Istanbul   (default UTC)
#   PANOS_TZ         time zone of PAN-OS clocks                   (default UTC)
#
# Safe to re-run: existing certificates, keys and passwords are kept.
set -Eeuo pipefail

ELASTIC_VERSION="${ELASTIC_VERSION:-8.13.4}"
RETENTION_DAYS="${RETENTION_DAYS:-90}"
SNAPSHOT_POLICY="${SNAPSHOT_POLICY:-}"
SNAPSHOT_REPO_PATH="${SNAPSHOT_REPO_PATH:-}"
KIBANA_BIND="${KIBANA_BIND:-0.0.0.0}"
ES_NAMESPACE="${ES_NAMESPACE:-default}"
FORTIGATE_TZ="${FORTIGATE_TZ:-UTC}"
PANOS_TZ="${PANOS_TZ:-UTC}"

if [[ -n "${SNAPSHOT_REPO_PATH}" && ! "${SNAPSHOT_REPO_PATH}" =~ ^/[A-Za-z0-9._/-]+$ ]]; then
  echo "SNAPSHOT_REPO_PATH must be an absolute path (letters, digits, . _ - /)" >&2; exit 1
fi
# Snapshot settings for content/bootstrap.sh
SNAPSHOT_ENV=(SNAPSHOT_POLICY="${SNAPSHOT_POLICY}")
if [[ -n "${SNAPSHOT_REPO_PATH}" ]]; then
  SNAPSHOT_ENV+=(SNAPSHOT_REPOSITORY_TYPE=fs "SNAPSHOT_REPOSITORY_SETTINGS={\"location\": \"${SNAPSHOT_REPO_PATH}\"}")
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../../.." && pwd)"
CONFIG_DIR="${SCRIPT_DIR}/config"
SOURCES_DIR="${REPO_ROOT}/integrations/sources"
CONTENT_DIR="${REPO_ROOT}/content"

ES_BIN=/usr/share/elasticsearch/bin
ES_CERT_DIR=/etc/elasticsearch/certs
ES_CA_CRT="${ES_CERT_DIR}/ca.crt"
ES_URL=https://localhost:9200
CREDENTIALS_FILE=/root/esm-credentials

ELASTIC_PASSWORD=""
KIBANA_SYSTEM_PASSWORD=""
LOGSTASH_INGEST_PASSWORD=""

step() { echo; echo "==> $*"; LAST_STEP="$*"; }
info() { echo "    $*"; }
warn() { echo "[!] $*" >&2; }
on_error() {
  echo "[-] Failed during: ${LAST_STEP:-start}" >&2
  for svc in elasticsearch kibana logstash; do
    echo "----- ${svc} journal (last 30 lines) -----" >&2
    journalctl -u "${svc}" -n 30 --no-pager >&2 || true
  done
}
trap on_error ERR

random_secret() { openssl rand -base64 48 | tr -dc 'A-Za-z0-9' | head -c "${1:-24}"; }

es_auth_ok() {  # es_auth_ok <password>
  curl -s -o /dev/null -w '%{http_code}' --cacert "${ES_CA_CRT}" -u "elastic:$1" "${ES_URL}/_security/_authenticate" | grep -q 200
}

wait_for_es() {
  for _ in $(seq 1 120); do
    curl -s --cacert "${ES_CA_CRT}" "${ES_URL}" >/dev/null 2>&1 && return 0
    sleep 2
  done
  warn "Elasticsearch did not answer on ${ES_URL}"; return 1
}

load_credentials() {
  if [[ -f "${CREDENTIALS_FILE}" ]]; then
    # shellcheck disable=SC1090
    . "${CREDENTIALS_FILE}"
  fi
  ELASTIC_PASSWORD="${ELASTIC_PASSWORD:-$(random_secret)}"
  KIBANA_SYSTEM_PASSWORD="${KIBANA_SYSTEM_PASSWORD:-$(random_secret)}"
  LOGSTASH_INGEST_PASSWORD="${LOGSTASH_INGEST_PASSWORD:-$(random_secret)}"
}

save_credentials() {
  umask 077
  cat > "${CREDENTIALS_FILE}" <<EOF
# Elastic GenAI SecOps credentials (created by elk_setup_ubuntu_jammy.sh)
ELASTIC_PASSWORD=${ELASTIC_PASSWORD}
KIBANA_SYSTEM_PASSWORD=${KIBANA_SYSTEM_PASSWORD}
LOGSTASH_INGEST_PASSWORD=${LOGSTASH_INGEST_PASSWORD}
EOF
  chmod 0600 "${CREDENTIALS_FILE}"
}

########################################
step "1/9 Checks"
[[ ${EUID} -eq 0 ]] || { echo "Run as root (sudo)." >&2; exit 1; }
grep -q 'jammy' /etc/os-release || warn "This script targets Ubuntu 22.04 (jammy); continuing anyway."
for f in "${CONFIG_DIR}/elasticsearch.yml" "${CONFIG_DIR}/kibana.yml" "${SOURCES_DIR}/pipelines.yml" "${CONTENT_DIR}/bootstrap.sh"; do
  [[ -f "${f}" ]] || { echo "Missing ${f}; run the script from a full repository checkout." >&2; exit 1; }
done
export DEBIAN_FRONTEND=noninteractive
load_credentials

########################################
step "2/9 APT repository (Elastic ${ELASTIC_VERSION})"
apt-get update -y
apt-get install -y --no-install-recommends ca-certificates curl gpg unzip openssl
install -d -m 0755 /etc/apt/keyrings
if [[ ! -s /etc/apt/keyrings/elastic.gpg ]]; then
  curl -fsSL https://artifacts.elastic.co/GPG-KEY-elasticsearch | gpg --dearmor -o /etc/apt/keyrings/elastic.gpg
fi
echo "deb [signed-by=/etc/apt/keyrings/elastic.gpg] https://artifacts.elastic.co/packages/${ELASTIC_VERSION%%.*}.x/apt stable main" \
  > /etc/apt/sources.list.d/elastic.list
apt-get update -y

########################################
step "3/9 Kernel settings"
sysctl -w vm.max_map_count=262144 >/dev/null
echo "vm.max_map_count=262144" > /etc/sysctl.d/99-elasticsearch.conf

########################################
step "4/9 Install Elasticsearch, Kibana, Logstash ${ELASTIC_VERSION}"
apt-mark unhold elasticsearch kibana logstash >/dev/null 2>&1 || true
apt-get install -y --allow-downgrades \
  "elasticsearch=${ELASTIC_VERSION}" "kibana=${ELASTIC_VERSION}" "logstash=1:${ELASTIC_VERSION}-1"
apt-mark hold elasticsearch kibana logstash >/dev/null

########################################
step "5/9 TLS certificates"
install -d -m 0750 -o root -g elasticsearch "${ES_CERT_DIR}"
make_cert() {  # make_cert <name> <instances yaml>
  local name="$1"
  [[ -f "${ES_CERT_DIR}/${name}.crt" && -f "${ES_CERT_DIR}/${name}.key" ]] && return 0
  local tmp; tmp="$(mktemp -d)"
  printf '%s\n' "$2" > "${tmp}/instances.yml"
  "${ES_BIN}/elasticsearch-certutil" cert --silent --pem --in "${tmp}/instances.yml" --out "${tmp}/cert.zip" \
    --ca-cert "${ES_CA_CRT}" --ca-key "${ES_CERT_DIR}/ca.key"
  unzip -q -o "${tmp}/cert.zip" -d "${tmp}"
  cp "${tmp}/${name}/${name}.crt" "${tmp}/${name}/${name}.key" "${ES_CERT_DIR}/"
  rm -rf "${tmp}"
}
if [[ ! -f "${ES_CA_CRT}" ]]; then
  tmp="$(mktemp -d)"
  "${ES_BIN}/elasticsearch-certutil" ca --silent --pem --out "${tmp}/ca.zip"
  unzip -q -o "${tmp}/ca.zip" -d "${tmp}"
  cp "${tmp}/ca/ca.crt" "${tmp}/ca/ca.key" "${ES_CERT_DIR}/"
  rm -rf "${tmp}"
fi
HOST_SHORT="$(hostname -s)"
make_cert http "$(printf 'instances:\n  - name: http\n    dns: [localhost, %s]\n    ip: [127.0.0.1, "::1"]' "${HOST_SHORT}")"
make_cert transport "$(printf 'instances:\n  - name: transport\n    dns: [localhost, %s]\n    ip: [127.0.0.1]' "${HOST_SHORT}")"
chown root:elasticsearch "${ES_CERT_DIR}"/*
chmod 0640 "${ES_CERT_DIR}"/*.key
chmod 0644 "${ES_CERT_DIR}"/*.crt
install -d -m 0755 /etc/logstash/certs /etc/kibana/certs
install -m 0644 "${ES_CA_CRT}" /etc/logstash/certs/ca.crt
install -m 0644 "${ES_CA_CRT}" /etc/kibana/certs/ca.crt

########################################
step "6/9 Configuration files"
install -m 0660 -o root -g elasticsearch "${CONFIG_DIR}/elasticsearch.yml" /etc/elasticsearch/elasticsearch.yml
if [[ -n "${SNAPSHOT_REPO_PATH}" ]]; then
  # A snapshot repository must be listed in path.repo; with several nodes it must be shared storage
  install -d -m 0750 -o elasticsearch -g elasticsearch "${SNAPSHOT_REPO_PATH}"
  echo "path.repo: [\"${SNAPSHOT_REPO_PATH}\"]" >> /etc/elasticsearch/elasticsearch.yml
fi
# The package's security auto-configuration stores passwords for its own PKCS#12 files; we use PEM files instead
for key in xpack.security.http.ssl.keystore.secure_password \
           xpack.security.transport.ssl.keystore.secure_password \
           xpack.security.transport.ssl.truststore.secure_password; do
  if "${ES_BIN}/elasticsearch-keystore" list 2>/dev/null | grep -qx "${key}"; then
    "${ES_BIN}/elasticsearch-keystore" remove "${key}"
  fi
done
# Sets the elastic password on the first start of a new cluster (ignored afterwards)
printf '%s' "${ELASTIC_PASSWORD}" | "${ES_BIN}/elasticsearch-keystore" add -x -f bootstrap.password

install -m 0660 -o root -g kibana "${CONFIG_DIR}/kibana.yml" /etc/kibana/kibana.yml
if [[ ! -s /etc/kibana/kibana.env ]]; then
  umask 077
  cat > /etc/kibana/kibana.env <<EOF
KBN_SECURITY_KEY=$(random_secret 48)
KBN_SAVEDOBJ_KEY=$(random_secret 48)
KBN_REPORTING_KEY=$(random_secret 48)
EOF
fi
sed -i '/^KIBANA_BIND=/d' /etc/kibana/kibana.env
echo "KIBANA_BIND=${KIBANA_BIND}" >> /etc/kibana/kibana.env
chmod 0600 /etc/kibana/kibana.env
install -d -m 0755 /etc/systemd/system/kibana.service.d
printf '[Service]\nEnvironmentFile=/etc/kibana/kibana.env\n' > /etc/systemd/system/kibana.service.d/10-esm.conf

# Logstash: every source pipeline, plus the shared pipelines.yml
rm -rf /etc/logstash/conf.d
install -d -m 0755 /etc/logstash/conf.d
cp -a "${SOURCES_DIR}/." /etc/logstash/conf.d/
install -m 0644 "${SOURCES_DIR}/pipelines.yml" /etc/logstash/pipelines.yml
chown -R root:logstash /etc/logstash/conf.d
LS_ENV=/etc/default/logstash
touch "${LS_ENV}"; chmod 0600 "${LS_ENV}"
grep -q '^LOGSTASH_KEYSTORE_PASS=' "${LS_ENV}" || echo "LOGSTASH_KEYSTORE_PASS=$(random_secret)" >> "${LS_ENV}"
sed -i -E '/^(ES_HOSTS|ES_USER|ES_CA_CERT|ES_NAMESPACE|LS_SOURCES_DIR|FORTIGATE_TZ|PANOS_TZ)=/d' "${LS_ENV}"
cat >> "${LS_ENV}" <<EOF
ES_HOSTS=${ES_URL}
ES_USER=logstash_ingest
ES_CA_CERT=/etc/logstash/certs/ca.crt
ES_NAMESPACE=${ES_NAMESPACE}
LS_SOURCES_DIR=/etc/logstash/conf.d
FORTIGATE_TZ=${FORTIGATE_TZ}
PANOS_TZ=${PANOS_TZ}
EOF
systemctl daemon-reload

########################################
step "7/9 Start Elasticsearch and set passwords"
systemctl enable elasticsearch kibana logstash >/dev/null
systemctl restart elasticsearch
wait_for_es
if ! es_auth_ok "${ELASTIC_PASSWORD}"; then
  # Existing cluster with an unknown elastic password: reset it
  ELASTIC_PASSWORD="$("${ES_BIN}/elasticsearch-reset-password" -u elastic -b -s --url "${ES_URL}")"
  es_auth_ok "${ELASTIC_PASSWORD}" || { echo "Could not authenticate as elastic." >&2; exit 1; }
fi
save_credentials
curl -sS -f --cacert "${ES_CA_CRT}" -u "elastic:${ELASTIC_PASSWORD}" -H 'Content-Type: application/json' \
  -X POST "${ES_URL}/_security/user/kibana_system/_password" -d "{\"password\":\"${KIBANA_SYSTEM_PASSWORD}\"}" >/dev/null

info "loading ILM, templates, roles and the logstash_ingest user"
ES_URL="${ES_URL}" ES_PASSWORD="${ELASTIC_PASSWORD}" ES_CA_CERT="${ES_CA_CRT}" RETENTION_DAYS="${RETENTION_DAYS}" \
  env "${SNAPSHOT_ENV[@]}" LOGSTASH_INGEST_PASSWORD="${LOGSTASH_INGEST_PASSWORD}" bash "${CONTENT_DIR}/bootstrap.sh"

########################################
step "8/9 Keystores, start Kibana and Logstash"
KBN_KEYSTORE=/usr/share/kibana/bin/kibana-keystore
[[ -f /etc/kibana/kibana.keystore ]] || "${KBN_KEYSTORE}" create
printf '%s' "${KIBANA_SYSTEM_PASSWORD}" | "${KBN_KEYSTORE}" add elasticsearch.password --stdin --force
chown root:kibana /etc/kibana/kibana.keystore; chmod 0660 /etc/kibana/kibana.keystore

set -a
# LS_ENV is generated above
# shellcheck disable=SC1090
. "${LS_ENV}"
set +a
LS_KEYSTORE=(/usr/share/logstash/bin/logstash-keystore --path.settings /etc/logstash)
if ! "${LS_KEYSTORE[@]}" list >/dev/null 2>&1; then
  rm -f /etc/logstash/logstash.keystore
  "${LS_KEYSTORE[@]}" create >/dev/null
fi
printf '%s\n' "${LOGSTASH_INGEST_PASSWORD}" | "${LS_KEYSTORE[@]}" add --force ES_PW >/dev/null
chown logstash:logstash /etc/logstash/logstash.keystore; chmod 0600 /etc/logstash/logstash.keystore

systemctl restart kibana logstash

########################################
step "9/9 Detection rules"
ES_URL="${ES_URL}" ES_PASSWORD="${ELASTIC_PASSWORD}" ES_CA_CERT="${ES_CA_CRT}" RETENTION_DAYS="${RETENTION_DAYS}" \
  env "${SNAPSHOT_ENV[@]}" KIBANA_URL="http://127.0.0.1:5601" bash "${CONTENT_DIR}/bootstrap.sh"

cat <<EOF

==================== Elastic GenAI SecOps ====================
Kibana          http://$(hostname -I | awk '{print $1}'):5601   (user: elastic)
Credentials     ${CREDENTIALS_FILE}   (root only)
Elasticsearch   ${ES_URL}   (localhost only)
Retention       ${RETENTION_DAYS} days (ILM policy esm-logs)
Snapshots       ${SNAPSHOT_REPO_PATH:-${SNAPSHOT_POLICY:-none (indices are deleted without a snapshot)}}

Log source ports (open them in your firewall as needed):
  5044/tcp  Palo Alto (Filebeat panw)      5514/udp+tcp  syslog RFC3164
  5045/tcp  Windows (Winlogbeat, WEF)      5515/tcp      syslog RFC5424
  5046/tcp  Metricbeat / Heartbeat         5516/udp+tcp  FortiGate
  5047/tcp  Kaspersky                      5517/udp+tcp  Palo Alto syslog
  e.g.: ufw allow 5601/tcp && ufw allow 5044:5047/tcp && ufw allow 5514:5517/tcp && ufw allow 5514:5517/udp
================================================================
EOF
