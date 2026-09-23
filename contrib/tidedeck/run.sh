#!/bin/sh
# TideDeck's Messages panel: every action is a TideSMS command.
#
#   render      the panel document       tidesms api threads --format tidedeck
#   reply ID    Enter on a conversation  tidesms reply ID
#   app ID      e on a conversation      tidesms --open ID
#
# TideDeck passes the panel's settings to render only, so the binary is found
# the same way for every action: $TIDESMS_BIN, then tidesms on the PATH, then
# ~/.local/bin, where a desktop launcher's PATH may not reach.
set -eu

BIN=${TIDESMS_BIN:-}
if [ -z "$BIN" ]; then
  BIN=$(command -v tidesms 2>/dev/null || true)
fi
if [ -z "$BIN" ] && [ -x "$HOME/.local/bin/tidesms" ]; then
  BIN="$HOME/.local/bin/tidesms"
fi

if [ -z "$BIN" ]; then
  case "${1:-}" in
  render)
    printf '{"schemaVersion":1,"rows":['
    printf '{"type":"text","label":"messages","value":"tidesms not found","tone":"warning"},'
    printf '{"type":"block","body":["Put it on your PATH: install -Dm755 tidesms ~/.local/bin/tidesms"],"bodyTone":"muted"}]}\n'
    exit 0
    ;;
  *)
    echo "tidesms not found: install -Dm755 tidesms ~/.local/bin/tidesms" >&2
    exit 1
    ;;
  esac
fi

case "${1:-render}" in
render)
  COUNT=${TIDEDECK_PLUGIN_COUNT:-6}
  case "$COUNT" in '' | *[!0-9]*) COUNT=6 ;; esac
  [ "$COUNT" -ge 1 ] || COUNT=1
  [ "$COUNT" -le 50 ] || COUNT=50
  if [ "${TIDEDECK_PLUGIN_UNREAD:-false}" = true ]; then
    exec "$BIN" api threads --format tidedeck --limit "$COUNT" --unread
  fi
  exec "$BIN" api threads --format tidedeck --limit "$COUNT"
  ;;
reply)
  exec "$BIN" reply "$2"
  ;;
app)
  exec "$BIN" --open "$2"
  ;;
*)
  echo "usage: run.sh render | reply ID | app ID" >&2
  exit 2
  ;;
esac
