#!/usr/bin/env bash
set -euo pipefail
task_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$task_root"
source scripts/env.sh
task_suffix=""
case "$(uname -s)" in MINGW*|MSYS*|CYGWIN*) task_suffix=.exe ;; esac
mkdir -p .cache/bin
for task_entry in control-api runtime-worker notification-worker relay; do
  go build -o ".cache/bin/$task_entry$task_suffix" "./cmd/$task_entry"
done
echo 'Nemi binaries built in .cache/bin.'
