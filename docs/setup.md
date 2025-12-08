# Setup and configuration

Centauri is packaged as a docker container, with the latest stable
version at `ghcr.io/csmith/centauri:latest`.

You can also use specific major, minor or patch versions
such as `:1.1.0` or `:1.1`. The `:dev` tag follows the master
branch (and is not recommended for use in production).

Centauri's behaviour is configured using environment variables, while its
routing table is read from a file.

## Data persistence

Centauri stores certificates, ACME client details, and tailscale network
details on disk. These need to be persisted across runs (e.g. by mounting a
volume into the Docker container).

You can configure the individual paths to these files using the settings:
    - [`CERTIFICATES_STORE`](#certificate_store),
    - [`USER_DATA`](#user_data), and
    - [`TAILSCALE_DIR`](#tailscale_dir)

When using Docker, these default to paths under `/data/`, so you can simply
mount a volume there to persist all of Centauri's data. Centauri runs as
UID `65532`; if you are bind mounting a folder you will need to `chown` or
`chmod` it appropriately so Centauri can write. If you mount a Docker volume
at `/data` it should inherit these automatically.

## Simple example using docker-compose

Here's a minimal example that runs Centauri with a single service behind it.
[More detailed examples are available](examples.md).

```yaml
services:
  centauri:
    image: "ghcr.io/csmith/centauri"
    restart: "always"
    ports:
      - "80:8080"
      - "443:8443"
    volumes:
      - "data:/data"
    configs:
      - "centauri.conf"
    environment:
      ACME_EMAIL: "you@example.com"

      # Centauri uses DNS challenges to prove domain ownership. See the Lego docs for all available
      # providers and their configuration: https://go-acme.github.io/lego/dns/
      DNS_PROVIDER: "httpreq"
      HTTPREQ_ENDPOINT: "https://api.mydnsprovider.example.com/httpreq"
      HTTPREQ_USERNAME: "dns@example.com"
      HTTPREQ_PASSWORD: "myp@ssw0rd"

  # A container that will be proxied by Centauri. As the containers are on the same network (docker
  # compose creates a "default" network) we can refer to this container just as "mycontainer" in the
  # Centauri config.
  mycontainer:
    image: "nginx"
    restart: "always"

  volumes:
    data:

  configs:
    centauri.conf:
      contents: |
        route subdomain.example.com
          upstream mycontainer:80
```

## General configuration options

### `CONFIG`

- **Default (CLI)**: `centauri.conf`
- **Default (Docker)**: `/centauri.conf`

The path to read the [route configuration](routes.md) from.

If the value is not absolute, it is treated as relative to the current working
directory.

### `FRONTEND`

- **Default**: `tcp`
- **Options**: `tcp`, `tailscale` 

Centauri can either serve traffic over the internet (tcp) or privately
over tailscale.

Each frontend has several additional options:
[TCP options](#tcp-options) and [Tailscale options](#tailscale-options).

### `CERTIFICATE_STORE`

- **Default (CLI)**: `certs.json`
- **Default (Docker)**: `/data/certs.json`

The location that Centauri should save the certificates it obtains or
generates.

This should be persisted across runs of Centauri (i.e., within a docker
volume, or otherwise mounted in.)

If the value is not absolute, it is treated as relative to the current working
directory.

### `CERTIFICATE_PROVIDERS`

- **Default**: `lego selfsigned`
- **Options**: `lego`, `selfsigned`, or multiple separated by spaces

An ordered list of providers to use to obtain certificates. Individual
routes may request a specific provider in their [config](routes.md).

The default configuration will use lego to obtain ACME certificates if
it is [fully configured](#lego-options); otherwise it falls back to self-signed certificates.

### `WILDCARD_DOMAINS`

- **Default**: -

A space separated list of domains that should use a
[wildcard certificate](wildcards.md)

### `OCSP_STAPLING`

- **Default**: `false`
- **Options**: `true`, `false`

If enabled, certificates will be requested with the "must-staple"
extension.

Regardless of this setting, any existing certificates with "must-staple"
will be stapled properly. To force all certificates to be re-obtained
with the new setting, delete the [`CERTIFICATE_STORE`](#certificate_store) file.

!!! note

    [Let's Encrypt have shut down their OCSP service](https://letsencrypt.org/2024/07/23/replacing-ocsp-with-crls/).
    Requests for new certificates from Let's Encrypt with this setting enabled will fail.
    If you wish to use OCSP stapling you will need to configure an alternative ACME provider.
    
### `METRICS_PORT`

- **Default**: -

If specified, Centauri will expose a HTTP server on the given port, which
will only respond to `/metrics` with Prometheus-style [metrics](metrics.md).

### `LOG_LEVEL`

- **Default**: `INFO`
- **Options**: `DEBUG`, `INFO`, `WARN`, `ERROR`

Configures the amount of detail Centauri should output.

### `LOG_FORMAT`

- **Default**: `TEXT`
- **Options**: `TEXT`, `JSON`

Configures the output format for Centauri's logs.

### `TRUSTED_DOWNSTREAMS`

- **Default**: -

A comma-separated list of CIDR ranges to trust `X-Forwarded-For`, `X-Forwarded-Host`, 
and `X-Forwarded-Proto` headers from. When a request comes from a trusted downstream,
existing `X-Forwarded-*` headers will be preserved and the current connection's IP
will be appended to `X-Forwarded-For`. When a request comes from an untrusted source,
all `X-Forwarded-*` headers will be replaced with values based on the current connection.

Example: `10.0.0.0/8,172.16.0.0/12,192.168.0.0/16` to trust RFC 1918 private networks.

### `VALIDATE`

- **Default**: `false`
- **Options**: `true`, `false`

If enabled, Centauri will validate the configuration file and exit without
starting the server. This is useful for checking configuration syntax before
deploying changes.

### `DEBUG_CPU_PROFILE`

- **Default**: -

If set, Centauri will write a CPU profile to the given file. This may
impact performance and shouldn't be used on production deployments.

## TCP options

When using the `tcp` frontend, the following options are available:

### `HTTP_PORT`

- **Default**: `8080`

The port to listen on for plain-text HTTP connections. Centauri will
automatically redirect any request on this port to HTTPS.

### `HTTPS_PORT`

- **Default**: `8443`

The port to listen on for HTTPS connections. Requests to this port will
be routed according to the [route configuration](routes.md), and will
be served certificates appropriately.

## Tailscale options

When using the `tailscale` frontend, the following options are available:

### `TAILSCALE_HOSTNAME`

- **Default**: `centauri`

The hostname to use on the Tailscale network.

### `TAILSCALE_KEY`

- **Default**: -

The key to use to register on the Tailscale network. If not specified,
interactive authentication will be required; check the logs for a link.

### `TAILSCALE_MODE`

- **Default**: `http`
- **Options**: `http`, `https`

How to serve traffic on Tailscale. By default, traffic will be served
over plain HTTP with no certificates (as the connection over Tailscale
will be encrypted anyway). If set to `https`, Centauri will obtain
certificates as normal and accept HTTPS traffic over tailscale.

### `TAILSCALE_DIR`

- **Default (CLI)**: -
- **Default (Docker)**: `/data/tailscale`

The directory to store Tailscale's state in. This is required to reconnect
to the tailnet when Centauri is restarted (unless you provide a reusable
[key](#tailscale_key)).

If not specified, Tailscale will create a dir under the user config directory.

## Lego options

For the lego certificate provider, the following options are used:

### `USER_DATA`

- **Default (CLI)**: `user.pem`
- **Default (Docker)**: `/data/user.pem`

Path to the file to store ACME user data.

This should be persisted across runs of Centauri (i.e., within a docker
volume, or otherwise mounted in.)

If the value is not absolute, it is treated as relative to the current working
directory.

### `DNS_PROVIDER`

- **Default**: -

The name of the DNS provider to use for ACME DNS-01 challenges.
Must be one of the providers [supported by Lego](https://go-acme.github.io/lego/dns/).

Any configuration or credentials for the provider will need to be
specified in additional environment variables, according to the Lego docs.

For example to configure Centauri to use the [`httpreq`](https://go-acme.github.io/lego/dns/httpreq/) provider:

```env
DNS_PROVIDER: httpreq
HTTPREQ_ENDPOINT: https://httpreq.example.com/
HTTPREQ_USERNAME: dade
HTTPREQ_PASSWORD: h4ck_7h3_p14n37
```

### `ACME_EMAIL`

- **Default**: -

The e-mail address to use when registering with the ACME provider.

### `ACME_DIRECTORY`

- **Default**: `https://acme-v02.api.letsencrypt.org/directory`

The URL of the ACME directory to obtain certificates from.

For testing, change to the Let's Encrypt staging environment,
which has more generous rate limits: `https://acme-staging-v02.api.letsencrypt.org/directory`.

### `ACME_DISABLE_PROPAGATION_CHECK`

- **Default**: `false`
- **Options**: `true`, `false`

If set, stops the ACME client from checking that its DNS records
have propagated before requesting a certificate. This might be useful
if your local DNS setup doesn't reflect how the ACME server will see it.

### `ACME_PROFILE`

- **Default**: -

The profile to use when requesting a certificate. The valid options depend
on the ACME server being used. See, e.g., 
[the documentation for Let's Encrypt](https://letsencrypt.org/docs/profiles/).