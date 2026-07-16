# Stage 1: build
FROM golang:1.26-alpine AS build

RUN apk add --no-cache gcc musl-dev sqlite-dev

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=1 GOOS=linux go build -ldflags="-s -w" -o /bin/sync-relay ./cmd/sync-relay

# Stage 2: run
FROM alpine:3.21

RUN apk add --no-cache ca-certificates sqlite-libs && \
    adduser -D -H -h /app sync-relay

COPY --from=build /bin/sync-relay /app/sync-relay

WORKDIR /app
USER sync-relay

ENTRYPOINT ["/app/sync-relay"]
