FROM golang:1.26-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags "-s -w -X github.com/go-sum/furnace/internal/cli.Version=${VERSION}" \
    -o /usr/local/bin/furnace .

# healthcheck_builder: stdlib-only binary, isolated go.mod, no workspace deps.
FROM golang:1.26-alpine AS healthcheck_builder
WORKDIR /build
COPY docker/healthcheck.go main.go
RUN printf 'module healthcheck\ngo 1.26\n' > go.mod && \
    CGO_ENABLED=0 go build -ldflags='-s -w' -o /healthcheck .

FROM cgr.dev/chainguard/static:latest

COPY --from=build            --chown=nonroot:nonroot /usr/local/bin/furnace     /usr/local/bin/furnace
COPY --from=healthcheck_builder --chown=nonroot:nonroot /healthcheck            /usr/local/bin/healthcheck

USER nonroot

HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
    CMD ["/usr/local/bin/healthcheck"]

ENTRYPOINT ["/usr/local/bin/furnace"]
CMD ["web"]
