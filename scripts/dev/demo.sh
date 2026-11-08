#!/bin/sh
# Build Forgeline and (re)start the demo console. Usage: scripts/dev/demo.sh [data-dir] [listen]
# The server token lives in <data-dir>/.token (created once, mode 0600), so a restart keeps it.
# Only the server this script started for this data directory is stopped (its PID is kept in
# <data-dir>/server.pid); any other Forgeline you run is left alone.
set -eu
cd "$(dirname "$0")/../.."
DATA=${1:-/tmp/fl-demo}
LISTEN=${2:-127.0.0.1:8080}
mkdir -p "$DATA"
[ -f "$DATA/.token" ] || (umask 077; head -c 24 /dev/urandom | base64 | tr -d '/+=\n' > "$DATA/.token")
go build -o bin/forgeline ./cmd/forgeline
if [ -f "$DATA/server.pid" ]; then
  pid=$(cat "$DATA/server.pid")
  # Stop it only if that PID is still a Forgeline server (a PID can be reused after a reboot).
  if kill -0 "$pid" 2>/dev/null && ps -p "$pid" -o args= | grep -q 'forgeline server'; then
    kill "$pid"; sleep 1
  fi
  rm -f "$DATA/server.pid"
fi
FORGELINE_TOKEN=$(cat "$DATA/.token") nohup ./bin/forgeline server --listen "$LISTEN" --data-dir "$DATA" >"$DATA/server.log" 2>&1 &
echo $! > "$DATA/server.pid"
echo "Forgeline at http://$LISTEN (log: $DATA/server.log, token: $DATA/.token)"
