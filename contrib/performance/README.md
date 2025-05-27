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
| _no proxy_ | 2.36.1 | 3750596 | 0 | 332 (±246) | 375088 (±37451) |
| haproxy | 3.1.7 | 2111464 | 0 | 591 (±771) | 211150 (±17799) |
| centauri | 1.2.0 | 1258313 | 0 | 992 (±652) | 125832 (±17920) |
| nginx | 1.28.0 | 1063815 | 0 | 1174 (±1012) | 106395 (±14317) |
| apache | 2.4.63 | 669560 | 0 | 1866 (±4566) | 66984 (±36391) |
| caddy | 2.10.0 | 132880 | 290 | 9578 (±47396) | 13327 (±20918) |
