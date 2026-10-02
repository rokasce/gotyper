#!/usr/bin/env bash
# Headless end-to-end test of the plugin: starts a real Neovim with only
# gotyper loaded (--clean skips the learner's config), then drives it over
# Neovim's RPC socket key by key with test/drive.lua, which asserts.
#
# usage: test/run.sh [delay_ms]    (delay between keys, default 20)
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DELAY="${1:-20}"
SOCK="$(mktemp -u "${TMPDIR:-/tmp}/gotyper-test-XXXXXX.sock")"
nvim --headless --clean --listen "$SOCK" \
  -c "set rtp^=$ROOT" -c "runtime plugin/gotyper.lua" -c Gotyper >/dev/null 2>&1 &
PID=$!
trap 'kill $PID 2>/dev/null || true; rm -f "$SOCK"' EXIT
nvim --clean -l "$ROOT/test/drive.lua" "$SOCK" "$ROOT" "$DELAY"
