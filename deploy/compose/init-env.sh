#!/usr/bin/env sh
# Create deploy/compose/.env from .env.example, replacing __GENERATE__ with random secrets.
# If .env already exists, only settings missing from it are appended; existing values are kept.
set -eu
cd "$(dirname "$0")"

gen() { LC_ALL=C tr -dc 'A-Za-z0-9' </dev/urandom | head -c "$1"; }

created=0
[ -f .env ] || { : > .env; created=1; }
added=0
while IFS= read -r line || [ -n "$line" ]; do
  case "$line" in
    ''|'#'*) [ "$created" = 1 ] && echo "$line" >> .env; continue ;;
  esac
  key="${line%%=*}"
  grep -q "^${key}=" .env && continue
  case "$line" in
    KIBANA_ENCRYPTION_KEY=__GENERATE__) echo "${key}=$(gen 48)" >> .env ;;
    # Keeps the token of an older .env's MCP_TOKEN, so connected MCP clients keep working
    *=__GENERATE_ANALYST__)
      token="$(sed -n 's/^MCP_TOKEN=//p' .env)"
      echo "${key}=analyst:${token:-$(gen 48)}" >> .env ;;
    *=__GENERATE__) echo "${key}=$(gen 24)" >> .env ;;
    *) echo "$line" >> .env ;;
  esac
  added=$((added + 1))
done < .env.example
chmod 600 .env
if [ "$created" = 1 ]; then
  echo "created deploy/compose/.env (Kibana login: elastic / ELASTIC_PASSWORD in .env)"
else
  echo "deploy/compose/.env: added $added missing setting(s)"
fi
