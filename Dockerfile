# syntax=docker/dockerfile:1
# Multi-stage build for local/dev images (`make image`).
# Release images are built by GoReleaser from Dockerfile.goreleaser using prebuilt binaries.

FROM --platform=$BUILDPLATFORM node:20-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY api/ /src/api/
COPY web/ ./
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS build
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
ARG COMMIT=unknown
ARG DATE=unknown
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY web/embed.go ./web/embed.go
COPY --from=web /src/web/dist ./web/dist
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath \
    -ldflags "-s -w \
      -X github.com/pessom/nerdui/internal/version.Version=${VERSION} \
      -X github.com/pessom/nerdui/internal/version.Commit=${COMMIT} \
      -X github.com/pessom/nerdui/internal/version.Date=${DATE}" \
    -o /out/nerdui ./cmd/nerdui

# nerdui needs root to access the containerd socket and nerdctl state on the host,
# so the non-root distroless variant is intentionally not used.
FROM gcr.io/distroless/static-debian12
LABEL org.opencontainers.image.licenses="AGPL-3.0-only"
COPY LICENSE NOTICE /usr/share/doc/nerdui/
COPY --from=build /out/nerdui /usr/local/bin/nerdui
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/nerdui"]
