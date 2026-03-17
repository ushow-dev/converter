# ── Build stage ───────────────────────────────────────────────────────────────
FROM golang:1.22-alpine AS builder

WORKDIR /build

RUN apk add --no-cache git

COPY go.mod ./
COPY . .

RUN go mod tidy
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w" \
    -o /worker \
    ./cmd/worker

# ── Runtime stage ─────────────────────────────────────────────────────────────
FROM alpine:3.20

# ffmpeg for media conversion, curl for healthcheck, rclone for remote uploads, openssh-client for SSH key auth.
RUN apk add --no-cache ffmpeg curl rclone openssh-client

WORKDIR /app
COPY --from=builder /worker .

# Media volume mounted by compose at /media.
VOLUME ["/media"]

EXPOSE 8001

CMD ["/app/worker"]
