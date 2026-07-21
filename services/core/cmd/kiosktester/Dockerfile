# Stage 1 — build the tester and its idempotent demo-data initializer.
FROM golang:1.26-alpine AS build

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /bin/kiosktester ./cmd/kiosktester && \
    CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /bin/demoseed ./cmd/demoseed

# Stage 2 — small non-root runtime image.
FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata && \
    adduser -D -H -h /app medisync

COPY --from=build /bin/kiosktester /app/kiosktester
COPY --from=build /bin/demoseed /app/demoseed

WORKDIR /app
USER medisync

EXPOSE 8899

HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=5 \
    CMD wget -q -O- http://127.0.0.1:8899/ >/dev/null || exit 1

ENTRYPOINT ["/app/kiosktester"]
CMD ["-mode=serve", "-addr=:8899"]
