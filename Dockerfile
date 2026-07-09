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

FROM alpine:3.24

RUN apk add --no-cache ca-certificates wget && \
    addgroup -S juno && adduser -S juno -G juno

WORKDIR /app
COPY --from=builder /out/juno /app/juno

RUN mkdir -p /data && chown -R juno:juno /data
USER juno

ENV PORT=8080 \
    DATA_DIR=/data

EXPOSE 8080
VOLUME ["/data"]

HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
    CMD wget -q -O- http://localhost:${PORT}/healthz || exit 1

ENTRYPOINT ["/app/juno"]
