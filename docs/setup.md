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

You can configure the location for certificates using the
[`CERTIFICATES_STORE`](#certificate_store)
setting, and for ACME details with the
[`USER_DATA`](#user_data) setting.

Tailscale will store its data under `/home/nonroot/.config` unless configured
otherwise with [`TAILSCALE_DIR`](#tailscale_dir).

## General configuration options

### `CONFIG`

- **Default**: `centauri.conf`

The path to read the [route configuration](routes.md) from.

The default value is relative to the current working directory, which is
`/home/nonroot` when using the docker image.

### `FRONTEND`

- **Default**: `tcp`
- **Options**: `tcp`, `tailscale` 

Centauri can either serve traffic over the internet (tcp) or privately
over tailscale.

Each frontend has several additional options:
[TCP options](#tcp-options) and [Tailscale options](#tailscale-options).

### `CERTIFICATE_STORE`

- **Default**: `certs.json`

The location that Centauri should save the certificates it obtains or
generates.

This should be persisted across runs of Centauri (i.e., within a docker
volume, or otherwise mounted in.)

The default value is relative to the current working directory, which is
`/home/nonroot` when using the docker image.

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

- **Default**: -

The directory to store Tailscale's state in. This is required to reconnect
to the tailnet when Centauri is restarted (unless you provide a reusable
[key](#tailscale_key)).

If not specified, Tailscale will create a dir under the user config directory.
In the docker image this will be under `/home/nonroot/.config`.

## Lego options

For the lego certificate provider, the following options are used:

### `USER_DATA`

- **Default**: `user.pem`

Path to the file to store ACME user data.

This should be persisted across runs of Centauri (i.e., within a docker
volume, or otherwise mounted in.)

The default value is relative to the current working directory, which is
`/home/nonroot` when using the docker image.

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