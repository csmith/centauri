# Post v0.1.0

## Other changes

- Now requires Go 1.18 to build.
- Directives in the config file are now case-insensitive.
- If a route has multiple upstreams an error is now raised, instead of
  silently ignoring some of them.
