#!/usr/bin/env bash
# Wait until the Vite dev server accepts connections on 127.0.0.1:9245.
# Used by build/config.yml dev_mode so the app does not race install + cold start.
set -euo pipefail

url="${1:-http://127.0.0.1:9245/}"
attempts="${2:-60}"
delay="${3:-0.5}"

for _ in $(seq 1 "$attempts"); do
  if command -v curl >/dev/null 2>&1; then
    if curl -sf --connect-timeout 1 "$url" >/dev/null 2>&1; then
      exit 0
    fi
  else
    # Git Bash / bash fallback when curl is missing
    host="${url#*://}"
    host="${host%%/*}"
    port="${host##*:}"
    host="${host%:*}"
    if (echo >/dev/tcp/"$host"/"$port") >/dev/null 2>&1; then
      exit 0
    fi
  fi
  sleep "$delay"
done

echo "Vite dev server did not become ready on ${url} within $((attempts)) attempts" >&2
exit 1
