# Metrics

When configured with a valid [`METRICS_PORT`](setup.md#metrics_port), Centauri will expose some metrics in a Prometheus-compatible
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

  Requests that the client cancels (e.g. by disconnecting) before the
  upstream responds are not counted, as no response reaches the client.
- `centauri_upstream_blocked_total` - counter of requests or connections to
  upstreams that Centauri refused. This should always be zero; anything else
  indicates a bug, and each occurrence is also logged as an error. Labels:
    - `reason`: `dial` if the connection wasn't to a configured upstream;
      `unsanitised` if the request hadn't had client-supplied headers removed.

In addition, the built-in Prometheus collectors for Go and process specific
metrics are enabled.