FROM node:22-bookworm-slim AS builder
RUN apt-get update && apt-get install -y --no-install-recommends git ca-certificates && rm -rf /var/lib/apt/lists/*
WORKDIR /build
RUN git clone --depth 1 https://github.com/Novage/wt-tracker.git .
RUN npm ci --omit=dev

FROM node:22-bookworm-slim
COPY --from=builder /build /app/wt-tracker
COPY config.json /app/config.json
WORKDIR /app
CMD ["node", "--experimental-strip-types", "wt-tracker/bin/wt-tracker", "/app/config.json"]
