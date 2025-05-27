#!/bin/bash

set -e

# Parse arguments
PPROF_FILE=""
if [[ "$1" == "--pprof" ]]; then
    PPROF_FILE="centauri-$(date +%Y%m%d-%H%M%S).pprof"
    touch "$PPROF_FILE"
    chmod 666 "$PPROF_FILE"
    export CENTAURI_PPROF_FILE="/host/$PPROF_FILE"
    echo "Created pprof file: $PPROF_FILE"
fi

echo "Building centauri-local container..."
docker compose build centauri-local

echo "Starting services..."
docker compose up -d static-web-server centauri-local

echo "Waiting for services to start..."
sleep 10

echo "Running bombardier test..."
bombardier_output=$(bombardier -p r -o json -k https://localhost:9992/10kb)

echo "Stopping containers..."
docker compose down

if [[ -n "$PPROF_FILE" ]]; then
    chmod 600 "$PPROF_FILE"
fi

echo ""
echo "=== BENCHMARK RESULTS ==="
echo ""

# Extract key metrics and format them nicely
echo "$bombardier_output" | jq -r '
"Requests:      " + (.result.req2xx + .result.req3xx | tostring) + " successful, " + (.result.req4xx + .result.req5xx + .result.others | tostring) + " failed",
"Latency (μs):  " + (.result.latency.mean | round | tostring) + " ± " + (.result.latency.stddev | round | tostring) + " (mean ± stddev)",
"RPS:           " + (.result.rps.mean | round | tostring) + " ± " + (.result.rps.stddev | round | tostring) + " (mean ± stddev)",
"Duration:      " + (.result.timeTakenSeconds | tostring) + "s"
'

echo ""
echo "========================="

if [[ -n "$PPROF_FILE" ]]; then
    echo ""
    echo "CPU profile saved to: $PPROF_FILE"
    echo "Analyze with: go tool pprof $PPROF_FILE"
fi