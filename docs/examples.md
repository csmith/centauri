# Examples

Centauri's git repo includes a number of examples:

## [docker-compose](https://github.com/csmith/centauri/tree/master/examples/docker-compose)

A simple Docker Compose file that defines Centauri and a target service.
The Centauri config file is stored on disk and bind mounted in, and
a "data" directory from disk is bind mounted for Centauri's data.

## [docker-compose-dotege](https://github.com/csmith/centauri/tree/master/examples/docker-compose-dotege)

A Docker Compose file that uses [Dotege](https://github.com/csmith/dotege)
to automatically generate a Centauri config based on containers that are
running on the host.

## [docker-compose-network-config](https://github.com/csmith/centauri/tree/master/examples/docker-compose-network-config)

A Docker Compose file that uses [centauri-docker-confd](https://github.com/csmith/centauri-docker-confd)
to automatically generate a Centauri config based on containers that are
running on the host, and send it directly to Centauri using the
[network config protocol](network-config.md).

## [docker-compose-tailscale](https://github.com/csmith/centauri/tree/master/examples/docker-compose-tailscale)

A simple Docker Compose file that defines Centauri and a target service.
Centauri is configured to serve over Tailscale.

## [docker-compose-tailscale-sidecar](https://github.com/csmith/centauri/tree/master/examples/docker-compose-tailscale-sidecar)

An example of using Centauri as a "sidecar" to expose a single service
over Tailscale. Defines the Centauri config in the Compose file, and
uses [fallback](routes.md#fallback) to avoid having to specify the
FQDN.