#!/bin/bash

set -e

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
