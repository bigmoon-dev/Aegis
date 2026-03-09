#!/bin/bash
# Aegis stop script

cd "$(dirname "$0")"

PID_FILE="./aegis.pid"

if [ ! -f "$PID_FILE" ]; then
    echo "PID file not found, aegis may not be running"
    exit 0
fi

PID=$(cat "$PID_FILE")
if kill -0 "$PID" 2>/dev/null; then
    echo "Stopping aegis (PID $PID)..."
    kill "$PID"
    # Wait up to 10 seconds for graceful shutdown
    for i in $(seq 1 10); do
        if ! kill -0 "$PID" 2>/dev/null; then
            break
        fi
        sleep 1
    done
    if kill -0 "$PID" 2>/dev/null; then
        echo "Force killing..."
        kill -9 "$PID"
    fi
    echo "aegis stopped"
else
    echo "aegis not running (stale PID $PID)"
fi

rm -f "$PID_FILE"
