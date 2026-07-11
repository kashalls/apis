# syntax=docker/dockerfile:1
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
# Go cross-compiles natively, so the build runs on the host arch even when
# targeting a different one - no QEMU emulation needed for this step.
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /out/juno ./cmd/juno
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /out/trmnl ./cmd/trmnl
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /out/homeassistant ./cmd/homeassistant

FROM alpine:3.24 AS base

# Alpine's repo only keeps the latest revision of each package per release
# branch, so pinning exact apk versions here would go stale and break the
# build whenever Alpine ships an update - unlike Debian/Ubuntu, old versions
# aren't kept around to pin against.
# hadolint ignore=DL3018
RUN apk add --no-cache ca-certificates wget && \
    addgroup -S juno && adduser -S juno -G juno

WORKDIR /app

ENV PORT=8080
EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
    CMD wget -q -O- http://localhost:${PORT}/healthz || exit 1

# --- juno: Discord/Lanyard presence API ---
FROM base AS juno
COPY --from=builder /out/juno /app/juno
USER juno
ENTRYPOINT ["/app/juno"]

# --- trmnl: TRMNL push API, persists uploaded images to /data ---
FROM base AS trmnl
COPY --from=builder /out/trmnl /app/trmnl
RUN mkdir -p /data && chown -R juno:juno /data
ENV DATA_DIR=/data
VOLUME ["/data"]
USER juno
ENTRYPOINT ["/app/trmnl"]

# --- homeassistant: Home Assistant light API ---
FROM base AS homeassistant
COPY --from=builder /out/homeassistant /app/homeassistant
USER juno
ENTRYPOINT ["/app/homeassistant"]
