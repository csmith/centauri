# vNext

# v0.9.0

## Features

- A "fallback" route can now be specified by adding the `fallback` directive
  to its configuration. If specified, any request that doesn't match other
  routes will be treated as though it matches the fallback route. In practice
  this will result in an invalid certificate being served to clients, but there
  are some niche caches where it's desirable.

# v0.8.2

## Bug fixes

- Centauri should now correctly route requests received on non-standard ports.
  For real this time.

# v0.8.1

## Bug fixes

- Centauri should now correctly route requests received on non-standard ports.
  Thanks to @ShaneMcC for the bug report.

# v0.8.0

## Changes

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

## Other changes

- Updated dependencies

# v0.7.0

## Changes

- Centauri now defaults to not obtaining OCSP staples. This can be re-enabled
  using the `OCSP_STAPLING` env var. This ensures out-of-the-box compatibility
  with Let's Encrypt who will disable support for OCSP in May.

## Other changes

- Updated dependencies

# v0.6.2

## Other changes

- Updated dependencies

# v0.6.1

## Bug fixes

- Domain matching is now case-insensitive. Previously, if Centauri was
  configured to serve `example.com` it wouldn't handle requests for `EXAMPLE.com`
  even though they're canonically the same.
- Fixed "fast startup" still blocking on retrieving certificates. The initial
  update after the fast load is now ran in a separate goroutine, so the frontend
  can start serving traffic.

## Other changes

- Updated dependencies

# v0.6.0

## Changes

- Added option to disable OCSP stapling entirely. Let's Encrypt
  [intend to stop their OCSP service](https://letsencrypt.org/2024/07/23/replacing-ocsp-with-crls.html)
  and other ACME providers are likely to follow as the CAB leans towards CRLs
  instead of mandating stapling.

## Other changes

- Updated dependencies

# v0.5.3

## Bug fixes

- Fixed the wrong backend being served when a client reuses the same HTTP/2
  connection for a different host. This only happens if both hosts use the
  same SSL certificate (e.g. if wildcard is enabled), and the browser still
  has a connection open from the first host. Big thanks to @ShaneMcC for
  debugging this!
  ([issue #89](https://github.com/csmith/centauri/issues/89))

## Other changes

- Updated dependencies

# v0.5.2

## Bug fixes

- Fixed Tailscale-User-Login and Tailscale-User-Name being swapped

# v0.5.1

## Bug fixes

- Fixed X-Forwarded-Proto header not being set properly

# v0.5.0

## Features

- When using the Tailscale frontend, Centauri will now add details about the
  authenticated user making the request in the following headers:
  - Tailscale-User-Login
  - Tailscale-User-Name
  - Tailscale-User-Profile-Pic

## Other changes

- If using Lego, Centauri will no longer attempt to register a user or obtain
  certificates if it can't write to the user-data file.
- Centauri will now drop the following headers if a client supplies them:
  - Tailscale-User-Login
  - Tailscale-User-Name
  - Tailscale-User-Profile-Pic
- Updated dependencies

# v0.4.2

## Other changes

- Updated dependencies

# v0.4.1

## Other changes

- Fixed issue with build process. No code changes. 

# v0.4.0

## Features

- Upstreams may now specify multiple routes. For now, centauri will
  pick at random between them for each client request. This may change
  in the future.
  ([issue #26](https://github.com/csmith/centauri/issues/26))

## Other changes

- Fix Centauri always sending `X-Forwarded-Proto: https` even when the
  downstream connection was over `http` (e.g. when using the `tailscale`
  frontend).
- Tailscale updated to v1.48.1

# v0.3.0

## Features

- When starting up or changing routes, Centauri will now immediately
  start serving routes with existing certificates if they are still valid.
  Once those routes are being served, it will start obtaining any new
  certificates as required.
- When multiple new routes are added, they will be served as soon as
  certificates are obtained. Previously, none were served until all
  routes had certificates.

## Other changes

- If Centauri can't obtain or update a certificate it will now do its
  best to continue working, and stop serving the route in question if
  it doesn't have a valid certificate.
- Lego updated to v4.12.0
- Tailscale updated to v1.42.0

# v0.2.0 

## Features

- Centauri is now capable of generating self-signed certificates
  for routes, instead of obtaining them via ACME. This is controlled
  on a per-route basis by using the new `provider selfsigned` directive.
  ([issue #15](https://github.com/csmith/centauri/issues/15))
- Centauri can now listen directly on a Tailscale network instead
  of on public TCP ports. A new configuration option `frontend` has
  been added to allow selection of the frontend to use, as well as
  options for configuring the behaviour of the Tailscale frontend.

## Other changes

- Now requires Go 1.19 to build.
- Directives in the config file are now case-insensitive.
- If a route has multiple upstreams an error is now raised, instead of
  silently ignoring some of them.
- Lego updated to v4.9.1
