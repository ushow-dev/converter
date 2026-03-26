FROM node:22-slim AS builder
RUN apt-get update && apt-get install -y git python3 make g++ && rm -rf /var/lib/apt/lists/*
WORKDIR /build
RUN git clone --depth 1 https://github.com/Novage/wt-tracker.git .
RUN npm ci
RUN npx tsc --skipLibCheck

FROM node:22-slim
COPY --from=builder /build /app/wt-tracker
WORKDIR /app/wt-tracker
RUN npm ci --omit=dev
COPY config.json /app/config.json
WORKDIR /app
CMD ["node", "wt-tracker/bin/wt-tracker", "/app/config.json"]
