# syntax=docker/dockerfile:1

FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build

WORKDIR /src
ARG TARGETOS
ARG TARGETARCH

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go mod download

COPY . .

ARG VERSION=dev
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build \
    -ldflags "-s -w -X github.com/go-sum/furnace/cmd/furnace-web.Version=${VERSION}" \
    -o /usr/local/bin/furnace-web ./cmd/furnace-web

# healthcheck_builder: stdlib-only binary, isolated go.mod, no workspace deps.
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS healthcheck_builder
WORKDIR /build
ARG TARGETOS
ARG TARGETARCH
COPY docker/healthcheck.go main.go
RUN --mount=type=cache,target=/root/.cache/go-build \
    printf 'module healthcheck\ngo 1.26\n' > go.mod && \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -ldflags='-s -w' -o /healthcheck .

FROM cgr.dev/chainguard/static:latest

COPY --from=build            --chown=nonroot:nonroot /usr/local/bin/furnace-web /usr/local/bin/furnace-web
COPY --from=healthcheck_builder --chown=nonroot:nonroot /healthcheck            /usr/local/bin/healthcheck

USER nonroot
EXPOSE 8080

HEALTHCHECK --interval=10s --timeout=5s --start-period=15s --retries=6 \
    CMD ["/usr/local/bin/healthcheck"]

ENTRYPOINT ["/usr/local/bin/furnace-web"]
