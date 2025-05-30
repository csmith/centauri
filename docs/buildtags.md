# Build tags

## DNS providers

Centauri by default supports a huge range of DNS providers thanks to the
lego library it uses. This can, however, cause it to take a while to build
and increase the size of the final binary.

If you know in advance you will only use a single DNS provider, you can use
build tags to include only support for that provider in the binary. For example
to support only the `httpreq` provider you can build with
`go build -tags lego_httpreq`. See the [legotapas](https://github.com/csmith/legotapas) project for more
info.

## Frontends

Similarly, if you know you only want to use the `tcp` or `tailscale`
frontends, you can disable the other with the `notcp` or `notailscale`
build tags.
