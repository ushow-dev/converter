FROM node:22-bookworm AS builder
WORKDIR /build
RUN git clone --depth 1 https://github.com/Novage/wt-tracker.git .
RUN npm ci
RUN npx tsc --skipLibCheck

FROM node:22-bookworm-slim
COPY --from=builder /build/dist /app/wt-tracker/dist
COPY --from=builder /build/bin /app/wt-tracker/bin
COPY --from=builder /build/node_modules /app/wt-tracker/node_modules
COPY --from=builder /build/package.json /app/wt-tracker/package.json
COPY config.json /app/config.json
WORKDIR /app
CMD ["node", "wt-tracker/bin/wt-tracker", "/app/config.json"]
