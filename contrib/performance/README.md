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
| _no proxy_ | 2.36.1 | 3822944 | 0 | 326 (±274) | 382318 (±29853) |
| haproxy | 3.1.7 | 2115714 | 0 | 590 (±586) | 211625 (±19576) |
| traefik | 3.4.1 | 1320016 | 0 | 946 (±726) | 132000 (±13137) |
| centauri | 1.2.0 | 1252629 | 0 | 997 (±587) | 125249 (±16855) |
| nginx | 1.28.0 | 1063706 | 0 | 1174 (±816) | 106384 (±12463) |
| apache | 2.4.63 | 658743 | 0 | 1897 (±4480) | 65814 (±35928) |
| caddy | 2.10.0 | 124735 | 5975 | 9580 (±99104) | 13063 (±20594) |
