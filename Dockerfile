FROM golang:1.25.5 AS build
WORKDIR /go/src/app
COPY . .

RUN --mount=type=cache,target=/go/pkg/mod \
    set -eux; \
    CGO_ENABLED=0 GO111MODULE=on go install ./cmd/centauri; \
    go run github.com/google/go-licenses@latest save ./... --save_path=/notices \
        --ignore github.com/go-acme/tencentclouddnspod/v20210323 \
        --ignore github.com/go-acme/tencentedgdeone/v20220901 \
        --ignore github.com/nrdcg/oci-go-sdk/common/v1065 \
        --ignore github.com/nrdcg/oci-go-sdk/common/v1065/auth \
        --ignore github.com/nrdcg/oci-go-sdk/common/v1065/utils \
        --ignore github.com/nrdcg/oci-go-sdk/dns/v1065 \
    ; \
    mkdir -p /mounts/data;

FROM ghcr.io/greboid/dockerbase/nonroot:1.20250803.0
COPY --from=build /go/bin/centauri /centauri
COPY --from=build /notices /notices
COPY --from=build --chown=65532:65532 /mounts /
VOLUME /data
ENV CONFIG=/centauri.conf \
    USER_DATA=/data/user.pem \
    CERTIFICATE_STORE=/data/certs.json \
    TAILSCALE_DIR=/data/tailscale
ENTRYPOINT ["/centauri"]
