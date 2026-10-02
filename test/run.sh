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
# The engine keeps its stats under $XDG_DATA_HOME; a fresh directory keeps the
# learner's own stats out of the test, and the test out of them.
export XDG_DATA_HOME="$(mktemp -d "${TMPDIR:-/tmp}/gotyper-test-data-XXXXXX")"
nvim --headless --clean --listen "$SOCK" \
  -c "set rtp^=$ROOT" -c "runtime plugin/gotyper.lua" -c "Gotyper json-api/01-greet-handler" >/dev/null 2>&1 &
PID=$!
trap 'kill $PID 2>/dev/null || true; rm -rf "$SOCK" "$XDG_DATA_HOME"' EXIT
nvim --clean -l "$ROOT/test/drive.lua" "$SOCK" "$ROOT" "$DELAY"
