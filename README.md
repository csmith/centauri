# Centauri

Centauri is a TLS-terminating reverse-HTTP proxy written in Go.

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

If running in Docker, you will need to persist the directory
at `/home/nonroot/.config` or the Tailscale client will lose
its authorisation whenever the container restarts.

## Usage

Centauri is packaged as a docker container, with the latest
stable version available at `ghcr.io/csmith/centauri:latest`.
You can also use specific major, minor or patch versions
such as `:0.2.0` or `:0.2`. The `:dev` tag follows the master
branch.

Some example setups can be found in the `examples` directory.

## Configuration

Centauri's behaviour is configured by environment vars. The following
options are available:

- `CONFIG` - path to the route configuration (see below).
  Default: `centauri.conf`.
- `FRONTEND` - the frontend to use to serve requests. Valid options
  are `tcp` and `tailscale`. Default: `tcp`.
- `CERTIFICATE_STORE` - path to the file to store certificates in.
  Default: `certs.json`
- `WILDCARD_DOMAINS` - space separated list of domains that should
  use a wildcard certificate instead of listing individual subdomains.
  See below.
- `CERTIFICATE_PROVIDERS` - a space separated list of certificate
  providers to try to get a certificate from, in order, if a route
  does not have an explicit `provider`. Default: `lego selfsigned`.
- `OCSP_STAPLING` - whether to request certificates with the
  "must-staple" extension. Certificates with the "must-staple" extension
  will always have responses stapled, regardless of this setting. To force
  refresh all certificates after changing this setting, delete the
  `CERTIFICATE_STORE` file. Default: `false`.
- `METRICS_PORT` - if specified, Centauri will expose a HTTP server on the
  given port, which will only respond to `/metrics` with Prometheus-style
  metrics.
- `LOG_LEVEL` - the minimum log level Centauri should output. One of
  `DEBUG`, `INFO`, `WARN`, `ERROR`. Default: `INFO`
- `LOG_FORMAT` - the format logs should be output in. One of `TEXT`, `JSON`.
  Default: `TEXT`

For the TCP frontend, the following options are used:

- `HTTP_PORT` - port to listen on for non-TLS connections. These are
  automatically redirected to https.
  Default: `8080`.
- `HTTPS_PORT` - port to listen on for TLS connections.
  Default: `8443`.

For the Tailscale frontend, the following options are used:

- `TAILSCALE_HOSTNAME` - the hostname to use on the Tailscale network.
  Default: `centauri`.
- `TAILSCALE_KEY` - the key to use to register the machine on the Tailscale
  network. If not specified, interactive authentication will be required;
  check the logs for a link.
- `TAILSCALE_MODE` - either `http` to serve all proxy traffic over
  http, or `https` to serve proxy traffic over https with a redirect
  from http to https. Default: `http`.

For the lego certificate provider, the following options are used:

- `USER_DATA` - path to the file to store ACME user data in.
  Default: `user.pem`.
- `DNS_PROVIDER` - the name of the DNS provider to use for ACME DNS-01
  challenges. See "ACME DNS Configuration" below.
- `ACME_EMAIL` - the e-mail address to supply to the ACME server when
  registering.
- `ACME_DIRECTORY` - the URL of the ACME directory to obtain certs from.
  Default: `https://acme-v02.api.letsencrypt.org/directory`
- `ACME_DISABLE_PROPAGATION_CHECK` - stops the ACME client from checking
  that its DNS records have propagated before requesting a certificate.
  Default: `false`

### ACME DNS Configuration

In order to support wildcard domains, Centauri uses the DNS-01 challenge
when proving ownership of domains. The `DNS_PROVIDER` env var must be set
to one of the providers supported by [Lego](https://go-acme.github.io/lego/dns/),
and any credentials required for that provider must be specified in the
relevant environment variables.

For example to configure Centauri to use the
[`httpreq`](https://go-acme.github.io/lego/dns/httpreq/) provider:

```env
DNS_PROVIDER: httpreq
HTTPREQ_ENDPOINT: https://httpreq.example.com/
HTTPREQ_USERNAME: dade
HTTPREQ_PASSWORD: h4ck_7h3_p14n37
```

### Wildcard domains

If you put a domain such as `example.com` in the `WILDCARD_DOMAINS` list,
whenever a route needs a certificate for `anything.example.com` Centauri will
instead replace it with `*.example.com`.

For example, a route with names `example.com` `test.example.com` and
`admin.example.com` will result in a certificate issued for
`example.com *.example.com`. This can be useful if you have a lot of
different subdomains, or you don't want a particular subdomain exposed
in certificate information.

Note that only one level of wildcard is supported: a wildcard certificate
for `*.example.com` won't match `foo.bar.example.com`, nor will it match
`example.com`.

### Route configuration

To tell Centauri how it should route requests, you need to supply it with
a route configuration file. This has a simple line-by-line format with
the following directives:

- `route` - defines a route with a list of domain names that will be
  accepted from clients
- `upstream` - provides the hostname/IP and port of the upstream server
  the request will be proxied to
- `provider` - specifies which certificate provider will be used
  for the route
- `header add` - sets a header on all responses to the client,
  adding it to any issued by upstream.
- `header replace` - sets a header on all responses to the client,
  replacing any with the same name issued by upstream.
- `header default` - sets a header on all response to the client,
  only if upstream has not set the same header.
- `header delete` - removes a header from all responses to the client.
- `fallback` - marks the route as the fallback if no other route matches.
  This may only be specified on one route. Centauri's normal behaviour is
  to close connections for non-matching requests, as it won't be able to
  provide a valid certificate for that connection.

Lines that are empty or start with a `#` character are ignored, as is
any whitespace at the start or end of lines. It is recommended to indent
each `route` for readability, but it is entirely option.

A full route config may look something like this:

```
# This route will answer requests made to `example.com` or `www.example.com`.
# They will be proxied to `server1:8080`, with an extra `X-Via: Centauri`
# header sent to the upstream. In the response to the client, the `Server`
# header will be removed, and the `Strict-Transport-Security` header will be
# set to `max-age=15768000` if the upstream didn't set it.
route example.com www.example.com
    upstream server1:8080
    header delete server
    header default Strict-Transport-Security max-age=15768000  
    header add X-Via Centauri

# This route will answer requests made to `example.net`. They'll be proxied to
# `server1:8081`. Certificates will be generated using the `selfsigned`
# provider instead of Centauri's default, and the `Content-Security-Policy`
# header will always be set to `default-src 'self'` on responses to the client.
route example.net
    upstream server1:8081
    header replace Content-Security-Policy default-src 'self';
    provider selfsigned
    
# This route will answer requests made to `placeholder.example.com` and any
# other domain that is not covered by the other routes (because it's a fallback
# route). These requests will be proxied to either `server1:8082` or
# `server1:8083` (picked at random).
route placeholder.example.com
    upstream server1:8082
    upstream server1:8083
    fallback
```

#### Providers

The following certificate providers are supported:

* `lego` - uses the Lego library to obtain certificates from Let's Encrypt
  using a DNS-01 challenge (default).
* `selfsigned` - generates a self-signed certificate. This will not be
  trusted by browsers, but may be useful for certain advanced scenarios.

Routes can specify which provider to use with the `provider` directive,
and you can configure the global defaults using the `CERTIFICATE_PROVIDERS`
environment var.

## Build tags

If you know in advance you will only use a single DNS provider, you can use build tags to include only support
for that provider in the binary. For example to support only the `httpreq` provider you can build with
`go build -tags lego_httpreq`. See the [legotapas](https://github.com/csmith/legotapas) project for more
info.

You can also disable Centauri's frontends by specifying the `notcp` and `notailscale` build tags.

## Metrics

When configured with a valid `METRICS_PORT`, Centauri will expose some metrics in a Prometheus-compatible
format at `/metrics`.

The following Centauri-specific metrics are exported:

- `centauri_tls_hello_total` - counter of TLS Client Hello messages received
  (i.e., the number of TLS connections opened to Centauri). Labels:
  - `known`: `true` if the `ServerName` is one Centauri knows and will serve
    a certificate for; `false` if it was unknown and the connection was closed.
- `centauri_response_total` - counter of HTTP responses sent to clients,
  excluding automatic redirects from HTTP->HTTPS. Labels:
  - `route`: the name (first listed domain) of the route the response was for
  - `status`: the HTTP response status sent to the client

In addition, the built-in Prometheus collectors for Go and process specific
metrics are enabled.

## FAQ

### What headers does/doesn't Centauri pass to the upstream service?

Centauri will automatically set the following headers:

- `X-Forwarded-For` - to include the IP address of the client making the request
- `X-Forwarded-Proto` - to indicate the protocol of the downstream connection (http/https)
- `X-Forwarded-Host` - the original hostname the request was received for
- `Tailscale-User-Login` - username of the Tailscale user making the request, if applicable
- `Tailscale-User-Name` - display name of the Tailscale user making the request, if applicable
- `Tailscale-User-Profile-Pic` - profile picture of the Tailscale user making the request, if applicable

It will also actively remove any of the following headers sent by clients:

- `X-Real-IP`
- `True-Client-IP`
- `X-Forwarded-Host`
- `Forwarded`

### Can Centauri select upstreams based on {url,port,cookies,etc}?

No. Centauri currently performs all routing based on the requested hostname.
Additional methods may be added in the future, but Centauri will likely stick
to being simple and easy to understand vs becoming a swiss army knife.

### Can Centauri automatically route traffic to containers based on labels?

Not on its own. This is a deliberate design decision, in order to avoid exposing
a service with access to the docker daemon to the Internet. Instead, you can
use a tool such as [Dotege](https://github.com/csmith/dotege) that will
generate a config file for Centauri. See `examples/docker-compose-dotege` for
a worked example.

## Feedback / Contributing

Feedback, feature requests, bug reports and pull requests are all welcome!
