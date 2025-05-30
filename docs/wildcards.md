# Wildcard domains

If you put a domain such as `example.com` in the [`WILDCARD_DOMAINS`](setup.md#wildcard_domains) list,
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