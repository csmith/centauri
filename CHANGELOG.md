# Changelog

## Unreleased

### Other changes

- Centauri now enforces read, write and idle timeouts on incoming HTTP
  connections. This reduces the potential effect of badly configured or
  deliberately malicious clients.

## 2.0.0 - 2025-06-01

### Breaking changes

- The default paths used within the Docker image have changed:
    - Centauri's config is now loaded from `/centauri.conf`
      (previously: `/home/nonroot/centauri.conf`)
    - ACME user data is stored in `/data/user.pem`
      (previously: `/home/nonroot/user.pem`)
    - Certificates are stored in `/data/certs.json`
      (previously: `/home/nonroot/certs.json`)
    - Tailscale state is now stored in `/data/tailscale/`
      (previously: `/home/nonroot/.config/tsnet___centauri/`)
- Centauri will no longer accept configurations that:
    - define a route with no upstreams, or
    - define a route with no domains.

### Features

- Added `TAILSCALE_DIR` setting to specify where Tailscale stores its
  state. If not set, uses the previous behaviour of a folder underneath
  the user config directory.
- Centauri now uses structured logging. This allows you to filter logs by
  level (using the `LOG_LEVEL` setting), change the output format to JSON
  (using the `LOG_FORMAT` setting). The default log level is `INFO`; a
  lot of the more spammy day-to-day log lines are now `DEBUG` and hidden by
  default.

### Other changes

- The `FRONTEND` setting is now case-insensitive.
- Added `ACME_DISABLE_PROPAGATION_CHECK` setting, which stops the ACME
  client from querying DNS servers to make sure the challenge records
  have propagated. This shouldn't be needed in normal use, but is handy
  for testing.
- Added `DEBUG_CPU_PROFILE` setting, which will write out a CPU profile
  to the given file. Shouldn't be used in production deployments!

## 1.2.0 - 2025-05-03

### Features

- Centauri will now pass an `X-Forwarded-Host` header to upstreams,
  containing the original hostname that was requested by the client.
- If an upstream cannot be reached, Centauri will now respond with
  a basic error page instead of sending a 502 Bad Gateway response
  with no content. This response will also now have headers applied
  to it per the route configuration.
- Added support for exposing metrics for use with Prometheus or
  compatible systems. If the `METRICS_PORT` setting is configured,
  a separate HTTP server will be started on that port with metrics
  served at `/metrics`. Centauri currently exposes response count,
  broken down by route and HTTP status code, and the total count of
  TLS connections received, broken down by whether or not a cert
  was transmitted.

### Bug fixes

- The X-Forwarded-For header no longer incorrectly lists the client IP
  twice.

## 1.1.0 - 2025-05-02

### Changes

- `TAILSCALE_KEY` is no longer required when using the Tailscale frontend.
  If not specified, Tailscale will print a link to stdout to authorise the
  machine. This only needs to be performed once.

### Other changes

- Updated dependencies

## 1.0.1 - 2025-04-11

### Bug fixes

- Fixed rare crash when the config file is reloaded rapidly with a different
  number of routes. Thanks to @Greboid for the bug report.
  ([issue #159](https://github.com/csmith/centauri/issues/159))

## 1.0.0 - 2025-04-11

_No significant changes in this release, but it marks the point where Centauri
is considered stable. Any breaking changes in the future will result in a major
version bump._

### Other changes

- Updated dependencies

## 0.9.0 - 2025-02-26

### Features

- A "fallback" route can now be specified by adding the `fallback` directive
  to its configuration. If specified, any request that doesn't match other
  routes will be treated as though it matches the fallback route. In practice
  this will result in an invalid certificate being served to clients, but there
  are some niche caches where it's desirable.

## 0.8.2 - 2025-02-26

### Bug fixes

- Centauri should now correctly route requests received on non-standard ports.
  For real this time.

## 0.8.1 - 2025-02-26

### Bug fixes

- Centauri should now correctly route requests received on non-standard ports.
  Thanks to @ShaneMcC for the bug report.

## 0.8.0 - 2025-02-05

### Changes

- Changed the behaviour for obtaining OCSP staples:
  - Existing certificates with the must-staple extension will always have OCSP
    responses stapled to them, regardless of the `OCSP_STAPLING` env var
  - New certificates will continue to have the must-staple flag based on the
    `OCSP_STAPLING` env var
  - This ensures that when migrating between different values `OCSP_STAPLING`,
    existing certificates continue to work. Previously turning `OCSP_STAPLING`
    off would serve must-staple certificates without a staple.
  - To force any changes to take effect immediately, delete the `certs.json`
    file to force Centauri to re-request all certificates.

### Other changes

- Updated dependencies

## 0.7.0 - 2025-02-03

### Changes

- Centauri now defaults to not obtaining OCSP staples. This can be re-enabled
  using the `OCSP_STAPLING` env var. This ensures out-of-the-box compatibility
  with Let's Encrypt who will disable support for OCSP in May.

### Other changes

- Updated dependencies

## 0.6.2 - 2025-01-07

### Other changes

- Updated dependencies

## 0.6.1 - 2024-12-22

### Bug fixes

- Domain matching is now case-insensitive. Previously, if Centauri was
  configured to serve `example.com` it wouldn't handle requests for `EXAMPLE.com`
  even though they're canonically the same.
- Fixed "fast startup" still blocking on retrieving certificates. The initial
  update after the fast load is now ran in a separate goroutine, so the frontend
  can start serving traffic.

### Other changes

- Updated dependencies

## 0.6.0 - 2024-12-16

### Changes

- Added option to disable OCSP stapling entirely. Let's Encrypt
  [intend to stop their OCSP service](https://letsencrypt.org/2024/07/23/replacing-ocsp-with-crls.html)
  and other ACME providers are likely to follow as the CAB leans towards CRLs
  instead of mandating stapling.

### Other changes

- Updated dependencies

## 0.5.3 - 2024-05-07

### Bug fixes

- Fixed the wrong backend being served when a client reuses the same HTTP/2
  connection for a different host. This only happens if both hosts use the
  same SSL certificate (e.g. if wildcard is enabled), and the browser still
  has a connection open from the first host. Big thanks to @ShaneMcC for
  debugging this!
  ([issue #89](https://github.com/csmith/centauri/issues/89))

### Other changes

- Updated dependencies

## 0.5.2 - 2024-04-25

### Bug fixes

- Fixed Tailscale-User-Login and Tailscale-User-Name being swapped

## 0.5.1 - 2024-04-24

### Bug fixes

- Fixed X-Forwarded-Proto header not being set properly

## 0.5.0 - 2024-04-18

### Features

- When using the Tailscale frontend, Centauri will now add details about the
  authenticated user making the request in the following headers:
  - Tailscale-User-Login
  - Tailscale-User-Name
  - Tailscale-User-Profile-Pic

### Other changes

- If using Lego, Centauri will no longer attempt to register a user or obtain
  certificates if it can't write to the user-data file.
- Centauri will now drop the following headers if a client supplies them:
  - Tailscale-User-Login
  - Tailscale-User-Name
  - Tailscale-User-Profile-Pic
- Updated dependencies

## 0.4.2 - 2024-02-09

### Other changes

- Updated dependencies

## 0.4.1 - 2023-08-28

### Other changes

- Fixed issue with build process. No code changes. 

## 0.4.0 - 2023-08-28

### Features

- Upstreams may now specify multiple routes. For now, centauri will
  pick at random between them for each client request. This may change
  in the future.
  ([issue #26](https://github.com/csmith/centauri/issues/26))

### Other changes

- Fix Centauri always sending `X-Forwarded-Proto: https` even when the
  downstream connection was over `http` (e.g. when using the `tailscale`
  frontend).
- Tailscale updated to v1.48.1

## 0.3.0 - 2023-06-01

### Features

- When starting up or changing routes, Centauri will now immediately
  start serving routes with existing certificates if they are still valid.
  Once those routes are being served, it will start obtaining any new
  certificates as required.
- When multiple new routes are added, they will be served as soon as
  certificates are obtained. Previously, none were served until all
  routes had certificates.

### Other changes

- If Centauri can't obtain or update a certificate it will now do its
  best to continue working, and stop serving the route in question if
  it doesn't have a valid certificate.
- Lego updated to v4.12.0
- Tailscale updated to v1.42.0

## 0.2.0 - 2023-01-26 

### Features

- Centauri is now capable of generating self-signed certificates
  for routes, instead of obtaining them via ACME. This is controlled
  on a per-route basis by using the new `provider selfsigned` directive.
  ([issue #15](https://github.com/csmith/centauri/issues/15))
- Centauri can now listen directly on a Tailscale network instead
  of on public TCP ports. A new configuration option `frontend` has
  been added to allow selection of the frontend to use, as well as
  options for configuring the behaviour of the Tailscale frontend.

### Other changes

- Now requires Go 1.19 to build.
- Directives in the config file are now case-insensitive.
- If a route has multiple upstreams an error is now raised, instead of
  silently ignoring some of them.
- Lego updated to v4.9.1

## 0.1.0 - 2022-03-10

_Initial release._