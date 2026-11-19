FROM golang:1.27.4 AS build
WORKDIR /go/src/app
COPY . .

RUN --mount=type=cache,target=/go/pkg/mod \
    set -eux; \
    CGO_ENABLED=0 GO111MODULE=on go install ./cmd/centauri; \
    mkdir -p /notices; \
    go tool gocredits -skip-missing . > /notices/CREDITS; \
    mkdir -p /mounts/data;

FROM ghcr.io/greboid/dockerbase/nonroot:1.20261022.0
COPY --from=build /go/bin/centauri /centauri
COPY --from=build /notices /notices
COPY --from=build --chown=65532:65532 /mounts /
VOLUME /data
ENV CONFIG=/centauri.conf \
    USER_DATA=/data/user.pem \
    CERTIFICATE_STORE=/data/certs.json \
    TAILSCALE_DIR=/data/tailscale
ENTRYPOINT ["/centauri"]
