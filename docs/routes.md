# Routes

To tell Centauri how it should route requests, you need to supply it with
a route configuration file. This has a simple line-by-line format. The
most basic example is something like:

```
route example.com
    upstream server1:8080
```

This accepts requests for `example.com` and routes them to `server1:8080`.
The indentation is optional, but it helps to keep things organised!

## Config directives

### `route`

```
route example.com example.net www.example.com
```

Defines a route with a list of domain names that will be accepted from clients

Routes must have at least one domain name (even if they're the [fallback](#fallback) route).
The first domain will be used as the subject for the certificate, while others will
be used as alternate names.

Routes are the only "top level" directive. Everything else is a per-route
setting, and applies to most recently defined route.

### `upstream`

```
upstream server:1234
```

Provides the hostname/IP and port of the upstream server the request will be
proxied to. Routes must have at least one upstream. If they have more than one,
an upstream will be picked at random for each request.

### `provider`

```
provider selfsigned
```

Specifies a particular certificate provider (from those [configured](setup.md#certificate_providers))
that should be used for a particular route. This is optional, and not required
in normal use.

### `header add`

```
header add X-Via Centauri
```

Adds a header to all responses to the client. If the upstream response
also contained the header, then the client will receive multiple. 

### `header replace`

```
header replace Server It's a secret
```

Sets a header on all responses to the client, replacing any values for
that header sent by the upstream.

### `header default`

```
header default Strict-Transport-Security max-age=15768000
```

Sets a header on all responses to the client if it is not set by the
upstream.

### `header delete`

```
header delete X-Cache
```

Ensures that the specified header is never sent to the client, even if
set by upstream.

### `fallback`

```
fallback
```

Marks the route as the fallback if no other route matches.
This may only be specified on one route. Centauri's normal behaviour is
to close connections for non-matching requests, as it won't be able to
provide a valid certificate for that connection.

## Comments and whitespace

Lines that are empty or start with a `#` character are ignored, as is
any whitespace at the start or end of lines. It is recommended to indent
each `route` for readability, but it is entirely optional.

## Example
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