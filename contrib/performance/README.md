# Performance testing utilities

This directory contains a docker compose file and some related files to enable
performance testing of Centauri.

The configs for the various servers tested are more-or-less as simple as
possible. If you have any suggestions for optimising them, please feel free
to raise an issue or open a pull request. This isn't meant to be a thorough
comparison of different proxies, just enough to get an idea of how well
Centauri performs in relation to other software.

You can run the tests yourself using the `benchmark.sh` script, and see the
raw data in `benchmark_results.json`. You'll need to install
[bombardier](https://github.com/codesenberg/bombardier), and the script also
expects `docker compose`, `jq` and `awk` to exist.

## Results

| Proxy | Version | Requests | Errors | Latency (μs) | RPS |
|-------|---------|----------|--------|--------------|-----|
| _no proxy_ | 2.36.1 | 3809041 | 0 | 327 (±254) | 380919 (±32051) |
| haproxy | 3.1.7 | 2118939 | 0 | 589 (±725) | 211896 (±20853) |
| centauri | 1.2.0 | 1233108 | 0 | 1013 (±765) | 123315 (±19420) |
| nginx | 1.28.0 | 1063229 | 0 | 1175 (±882) | 106314 (±14806) |
| caddy | 2.10.0 | 96422 | 110 | 14124 (±107375) | 9613 (±18526) |
