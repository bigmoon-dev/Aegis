#!/bin/bash
# Aegis startup script
# Usage: bash start.sh [config_path]

cd "$(dirname "$0")"

CONFIG="${1:-config/aegis.yaml}"
LOG_DIR="./logs"
LOG_FILE="$LOG_DIR/aegis.log"
PID_FILE="./aegis.pid"

mkdir -p "$LOG_DIR" data

# Check if already running
if [ -f "$PID_FILE" ]; then
    OLD_PID=$(cat "$PID_FILE")
    if kill -0 "$OLD_PID" 2>/dev/null; then
        echo "aegis already running (PID $OLD_PID)"
        exit 1
    fi
    rm -f "$PID_FILE"
fi

echo "Starting aegis with config: $CONFIG"
nohup ./aegis "$CONFIG" >> "$LOG_FILE" 2>&1 &
echo $! > "$PID_FILE"
echo "aegis started (PID $!), log: $LOG_FILE"
