# Frequently Asked Questions

## What headers does/doesn't Centauri pass to the upstream service?

Centauri will automatically set the following headers:

- `X-Forwarded-For` - to include the IP address of the client making the request
- `X-Forwarded-Proto` - to indicate the protocol of the downstream connection (http/https)
- `X-Forwarded-Host` - the original hostname the request was received for
- `Tailscale-User-Login` - username of the Tailscale user making the request, if applicable
- `Tailscale-User-Name` - display name of the Tailscale user making the request, if applicable
- `Tailscale-User-Profile-Pic` - profile picture of the Tailscale user making the request, if applicable

If the client sends any of these headers, they will be ignored and not
passed on to the upstream. Centauri will also actively remove any of the
following, despite not using them itself:

- `X-Real-IP`
- `True-Client-IP`
- `Forwarded`

## Can Centauri select upstreams based on {url,port,cookies,etc}?

No. Centauri currently performs all routing based on the requested hostname.
Additional methods may be added in the future, but Centauri will likely stick
to being simple and easy to understand vs becoming a swiss army knife.

## Can Centauri automatically route traffic to containers based on labels?

Not on its own. This is a deliberate design decision, in order to avoid exposing
a service with access to the docker daemon to the Internet. Instead, you can
use a tool such as [Dotege](https://github.com/csmith/dotege) that will
generate a config file for Centauri. See [the example](examples.md#docker-compose-dotege)
for more details.