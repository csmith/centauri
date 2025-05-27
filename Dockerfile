FROM golang:1.24.3 AS build
WORKDIR /go/src/app
COPY . .

RUN --mount=type=cache,target=/go/pkg/mod \
    set -eux; \
    CGO_ENABLED=0 GO111MODULE=on go install ./cmd/centauri; \
    go run github.com/google/go-licenses@latest save ./... --save_path=/notices; \
    mkdir -p /mounts/data /mounts/home/nonroot/.config;

FROM ghcr.io/greboid/dockerbase/nonroot:1.20250326.0
COPY --from=build /go/bin/centauri /centauri
COPY --from=build /notices /notices
COPY --from=build --chown=65532:65532 /mounts /
VOLUME /data
VOLUME /home/nonroot/.config
ENTRYPOINT ["/centauri"]
