# Centauri

Centauri is a TLS-terminating reverse-HTTP proxy written in Go.

## Current status

Centauri is currently in development. There may be breaking changes as it
evolves. Individual builds _should_ be usable for production purposes, but
you might want to keep another more established proxy around as a backup
just in case.

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
or sidecar containers. Simply change the "frontend" setting to
"tailscale", supply an API key, and you're done!

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

For the TCP frontend, the following options are used:

- `HTTP_PORT` - port to listen on for non-TLS connections. These are
  automatically redirected to https.
  Default: `8080`.
- `HTTPS_PORT` - port to listen on for TLS connections.
  Default: `8443`.

For the Tailscale frontend, the following options are used:

- `TAILSCALE_HOSTNAME` - the hostname to use on the Tailscale network.
  Default: `centauri`.
- `TAILSCALE_KEY` - the key to use to authenticate to Tailscale. 

For the lego certificate provider, the following options are used:

- `USER_DATA` - path to the file to store ACME user data in.
  Default: `user.pem`.
- `DNS_PROVIDER` - the name of the DNS provider to use for ACME DNS-01
  challenges. See "ACME DNS Configuration" below.
- `ACME_EMAIL` - the e-mail address to supply to the ACME server when
  registering.
- `ACME_DIRECTORY` - the URL of the ACME directory to obtain certs from.
  Default: `https://acme-v02.api.letsencrypt.org/directory`

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

Lines that are empty or start with a `#` character are ignored, as is
any whitespace at the start or end of lines. It is recommended to indent
each `route` for readability, but it is entirely option.

A full route config may look something like this:

```
route example.com www.example.com
    upstream server1:8080
    header delete server
    header default Strict-Transport-Security max-age=15768000  
    header add X-Via Centauri

route example.net
    upstream server1:8081
    header replace Content-Security-Policy default-src 'self';
    provider selfsigned
```

#### Providers

The following certificate providers are supported:

* `lego` - uses the Lego library to obtain certificates from Let's Encrypt
  using a DNS-01 challenge (default).
* `selfsigned` - generates a self-signed certificate. This will not be
  trusted by browsers, but may be useful for certain advanced scenarios.

## Build tags

If you know in advance you will only use a single DNS provider, you can use build tags to include only support
for that provider in the binary. For example to support only the `httpreq` provider you can build with
`go build -tags lego_httpreq`. See the [legotapas](https://github.com/csmith/legotapas) project for more
info.

You can also disable Centauri's frontends by specifying the `notcp` and `notailscale` build tags. 

## Feedback / Contributing

Feedback, feature requests, bug reports and pull requests are all welcome!
