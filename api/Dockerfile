# ── Build stage ───────────────────────────────────────────────────────────────
FROM golang:1.22-alpine AS builder

WORKDIR /build

# Install build tools (git needed for some module fetches)
RUN apk add --no-cache git

# Copy shared module first (dependency of api).
COPY shared/ ./shared/

# Copy api module definition to leverage Docker layer cache.
COPY api/go.mod api/go.sum* ./api/

# Copy api source.
COPY api/ ./api/

# Download and verify dependencies, generate go.sum.
RUN cd api && go mod tidy

# Build a static binary.
RUN cd api && CGO_ENABLED=0 GOOS=linux go build \
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
