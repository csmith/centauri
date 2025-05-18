#!/bin/bash

set -e

declare -A PROXIES
PROXIES[static-web-server]="2.36.1"
PROXIES[centauri]="1.2.0"
PROXIES[haproxy]="3.1.7"
PROXIES[caddy]="2.10.0"
PROXIES[nginx]="1.28.0"

RESULTS_FILE="benchmark_results.json"
README_FILE="README.md"

echo "[]" > "$RESULTS_FILE"

echo "Starting benchmark tests..."

for proxy in "${!PROXIES[@]}"; do
    version="${PROXIES[$proxy]}"
    echo "Testing $proxy:$version..."
    
    case $proxy in
        static-web-server) export STATIC_WEB_SERVER_VERSION="$version" ;;
        centauri) export CENTAURI_VERSION="$version" ;;
        haproxy) export HAPROXY_VERSION="$version" ;;
        caddy) export CADDY_VERSION="$version" ;;
        nginx) export NGINX_VERSION="$version" ;;
    esac
    
    if [ "$proxy" = "static-web-server" ]; then
        docker compose up -d static-web-server
        echo "Waiting for services to start..."
        sleep 10
        echo "Running bombardier test for $proxy..."
        bombardier_output=$(bombardier -p r -o json http://localhost:9991/10kb)
    else
        docker compose up -d static-web-server "$proxy"
        echo "Waiting for services to start..."
        sleep 10
        echo "Running bombardier test for $proxy..."
        bombardier_output=$(bombardier -p r -o json -k https://localhost:9992/10kb)
    fi
    
    result=$(echo "$bombardier_output" | jq --arg proxy "$proxy" --arg version "$version" '. + {proxy: $proxy, version: $version}')
    
    jq --argjson new_result "$result" '. += [$new_result]' "$RESULTS_FILE" > tmp.json && mv tmp.json "$RESULTS_FILE"
    
    docker compose down
    
    echo "Completed test for $proxy:$version"
done

echo "Generating results table..."

cat > temp_table.md << 'EOF'
| Proxy | Version | Requests | Errors | Latency (μs) | RPS |
|-------|---------|----------|--------|--------------|-----|
EOF

jq -r 'sort_by(-.result.rps.mean) | .[] | [
    (if .proxy == "static-web-server" then "_no proxy_" else .proxy end),
    .version,
    (.result.req2xx + .result.req3xx),
    (.result.req4xx + .result.req5xx + .result.others),
    ((.result.latency.mean | round | tostring) + " (±" + (.result.latency.stddev | round | tostring) + ")"),
    ((.result.rps.mean | round | tostring) + " (±" + (.result.rps.stddev | round | tostring) + ")")
] | "| " + join(" | ") + " |"' "$RESULTS_FILE" >> temp_table.md

awk '/^## Results$/ { print; print ""; while ((getline line < "temp_table.md") > 0) print line; close("temp_table.md"); skip=1; next } /^##/ && skip { skip=0 } !skip { print }' "$README_FILE" > temp_readme && mv temp_readme "$README_FILE"

rm temp_table.md

echo "Benchmark complete! Results updated in $README_FILE"
echo "Raw JSON data available in $RESULTS_FILE"
