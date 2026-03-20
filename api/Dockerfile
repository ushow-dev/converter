# ── Build stage ───────────────────────────────────────────────────────────────
FROM golang:1.22-alpine AS builder

WORKDIR /build

# Install build tools (git needed for some module fetches)
RUN apk add --no-cache git

# Copy module definition first to leverage Docker layer cache.
COPY go.mod ./

# Copy all source (go mod tidy requires source to resolve indirect deps).
COPY . .

# Download and verify dependencies, generate go.sum.
RUN go mod tidy

# Build a static binary.
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w" \
    -o /api \
    ./cmd/api

# ── Runtime stage ─────────────────────────────────────────────────────────────
FROM alpine:3.20

# curl: healthcheck; rclone: subtitle sync to storage server
RUN apk add --no-cache curl rclone

WORKDIR /app
COPY --from=builder /api .

EXPOSE 8000

CMD ["/app/api"]
