#!/bin/bash
# Agent Harness startup script
# Usage: bash start.sh [config_path]

cd "$(dirname "$0")"

CONFIG="${1:-config/harness.yaml}"
LOG_DIR="./logs"
LOG_FILE="$LOG_DIR/harness.log"
PID_FILE="./harness.pid"

mkdir -p "$LOG_DIR" data

# Check if already running
if [ -f "$PID_FILE" ]; then
    OLD_PID=$(cat "$PID_FILE")
    if kill -0 "$OLD_PID" 2>/dev/null; then
        echo "agent-harness already running (PID $OLD_PID)"
        exit 1
    fi
    rm -f "$PID_FILE"
fi

echo "Starting agent-harness with config: $CONFIG"
nohup ./agent-harness "$CONFIG" >> "$LOG_FILE" 2>&1 &
echo $! > "$PID_FILE"
echo "agent-harness started (PID $!), log: $LOG_FILE"
