#!/usr/bin/env bash
# Source this file. Parse .env as literal data, never shell code.
NEMI_TASK_ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
if command -v cygpath >/dev/null 2>&1; then
  export GOPATH="$(cygpath -m "$NEMI_TASK_ROOT/.cache/go")"
  export GOCACHE="$(cygpath -m "$NEMI_TASK_ROOT/.cache/go-build")"
else
  export GOPATH="$NEMI_TASK_ROOT/.cache/go"
  export GOCACHE="$NEMI_TASK_ROOT/.cache/go-build"
fi
export GOTELEMETRY=off
export NEXT_TELEMETRY_DISABLED=1
if [[ -f "$NEMI_TASK_ROOT/.env" ]]; then
  while IFS= read -r nemi_env_line || [[ -n "$nemi_env_line" ]]; do
    nemi_env_line="${nemi_env_line#$'\xEF\xBB\xBF'}"
    nemi_env_line="${nemi_env_line%$'\r'}"
    if [[ "$nemi_env_line" =~ ^([A-Z][A-Z0-9_]*)=(.*)$ ]]; then
      nemi_env_key="${BASH_REMATCH[1]}"
      nemi_env_value="${BASH_REMATCH[2]}"
      case "$nemi_env_key" in
        APP_*|CONNECTOR_*|DATABASE_URL|TEMPORAL_*|MODEL_*|TEST_DATABASE_URL|TEST_TEMPORAL_ADDRESS|NEMI_BASE_URL)
          export "$nemi_env_key=$nemi_env_value" ;;
      esac
    fi
  done < "$NEMI_TASK_ROOT/.env"
fi
unset nemi_env_line nemi_env_key nemi_env_value
