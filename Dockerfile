FROM reg.c5h.io/golang AS build
WORKDIR /go/src/app
COPY . .

RUN set -eux; \
    CGO_ENABLED=0 GO111MODULE=on go install ./cmd/centauri; \
    go run github.com/google/go-licenses@latest save ./... --save_path=/notices; \
    mkdir -p /mounts/data /mounts/home/nonroot/.config;

FROM reg.c5h.io/base
COPY --from=build /go/bin/centauri /centauri
COPY --from=build /notices /notices
COPY --from=build --chown=65532:65532 /mounts /
VOLUME /data
VOLUME /home/nonroot/.config
ENTRYPOINT ["/centauri"]
