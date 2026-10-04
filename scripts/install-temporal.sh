#!/usr/bin/env bash
set -euo pipefail
task_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
case "$(uname -s)" in
  MINGW*|MSYS*|CYGWIN*)
    asset=temporal_cli_1.9.1_windows_amd64.tar.gz
    expected=42637464c337a3da203fbd4e8c688e4b2d511fd42bd19c337c0422e923a7b619 ;;
  Linux)
    [[ "$(uname -m)" == x86_64 ]] || { echo 'This helper supports amd64 only.' >&2; exit 1; }
    asset=temporal_cli_1.9.1_linux_amd64.tar.gz
    expected=09a0326a51db84d02735e53542b9ebd8c4758daf47482a9ab0abce15844e60d5 ;;
  *) echo 'Use the matching binary from the official Temporal release.' >&2; exit 1 ;;
esac
mkdir -p "$task_root/.cache/temporal"
archive="$task_root/.cache/temporal/$asset"
curl --fail --location --output "$archive" "https://github.com/temporalio/cli/releases/download/v1.9.1/$asset"
actual="$(sha256sum "$archive" | cut -d ' ' -f 1)"
[[ "$actual" == "$expected" ]] || { echo 'Checksum mismatch; do not execute this archive.' >&2; exit 1; }
tar -xzf "$archive" -C "$task_root/.cache/temporal"
echo 'Temporal 1.9.1 installed and SHA-256 verified in .cache/temporal.'
