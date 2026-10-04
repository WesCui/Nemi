FROM golang:1.26.0-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -o /out/control-api ./cmd/control-api && \
    CGO_ENABLED=0 go build -o /out/runtime-worker ./cmd/runtime-worker && \
    CGO_ENABLED=0 go build -o /out/notification-worker ./cmd/notification-worker && \
    CGO_ENABLED=0 go build -o /out/relay ./cmd/relay
FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates && rm -rf /var/lib/apt/lists/*
COPY --from=build /out /app
COPY docs/licenses /usr/share/licenses/nemi
RUN mkdir -p /app/data && chown 65532:65532 /app/data
WORKDIR /app
USER 65532:65532
ENTRYPOINT ["/app/control-api"]
