# Stage 1 — build
FROM golang:1.26-alpine AS build

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /bin/core ./cmd/core

# Stage 2 — run
FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata procps && \
    adduser -D -H -h /app medisync

COPY --from=build /bin/core /app/core
COPY --from=build /src/migrations /app/migrations

WORKDIR /app
USER medisync

EXPOSE 8080

# Process-alive health check only — core has no HTTP server yet.
# TODO(M2): switch to HTTP health check when the Connect-RPC server exists.
HEALTHCHECK --interval=15s --timeout=5s --start-period=30s --retries=3 \
    CMD pgrep -x core || exit 1

ENTRYPOINT ["/app/core"]
