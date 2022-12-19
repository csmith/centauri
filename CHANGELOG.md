# Post v0.1.0

## Features

- Centauri is now capable of generating self-signed certificates
  for routes, instead of obtaining them via ACME. This is controlled
  on a per-route basis by using the new `provider selfsigned` directive.
  ([issue #15](https://github.com/csmith/centauri/issues/15))

## Other changes

- Now requires Go 1.18 to build.
- Directives in the config file are now case-insensitive.
- If a route has multiple upstreams an error is now raised, instead of
  silently ignoring some of them.
- Lego updated to v4.9.1
