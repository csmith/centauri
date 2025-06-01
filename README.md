# Centauri

Centauri is a TLS-terminating reverse HTTP proxy written in Go.

## Current status

Centauri is considered stable and feature complete. It is deployed in production
in several places. Any breaking changes going forward will result in major
version bumps.

## Features

### Automatic TLS certificates and OCSP stapling

Centauri will obtain TLS certificates from an ACME provider such as
Let's Encrypt. It will keep these up to date, and ensure each one
has a valid OCSP staple that can be sent to clients.

Centauri runs with sensible defaults for establishing TLS connections,
in line with Mozilla's Intermediate recommendations. This balances
security with accessibility for older clients.

### Simple route configuration

Centauri's route configuration looks like this:

```
route www.example.com example.com
    upstream server1.internal.example.com:8080

route www.example.net
    upstream server1.internal.example.com:8080
```

You don't need to configure separate front-ends or back-ends, or
deal with `proxy_pass` instructions.

### Native Tailscale support

Centauri can listen directly on a Tailscale network instead of
a public TCP port, removing the need for complex configuration
or sidecar containers. Change the "frontend" setting to
"tailscale", supply an API key, and Centauri will connect
directly to your Tailscale network!

Centauri will also pass details of the Tailscale user making
the request to the upstream service, via the following headers:

- `Tailscale-User-Login`
- `Tailscale-User-Name`
- `Tailscale-User-Profile-Pic`

If running in Docker, you will need to persist the state
directory (`/home/nonroot/.config` by default, configurable
with `TAILSCALE_DIR`) or the Tailscale client will lose
its authorisation whenever the container restarts.

## Usage

Documentation is available at https://centauri.readthedocs.io/en/latest/.

## Feedback / Contributing

Feedback, feature requests, bug reports and pull requests are all welcome!
