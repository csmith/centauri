FROM reg.c5h.io/golang
WORKDIR /go/src/app
COPY . .

RUN set -eux; \
    CGO_ENABLED=0 GO111MODULE=on go install ./cmd/centauri; \
    go run github.com/google/go-licenses@latest save ./... --save_path=/notices;

FROM reg.c5h.io/base
COPY --from=build /go/bin/centauri /centauri
COPY --from=build /notices /notices
VOLUME /data
ENTRYPOINT ["/centauri"]
