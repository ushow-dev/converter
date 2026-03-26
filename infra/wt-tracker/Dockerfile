FROM node:22-alpine AS builder
RUN apk add --no-cache git python3 make g++
WORKDIR /build
RUN git clone --depth 1 https://github.com/Novage/wt-tracker.git .
RUN npm ci
RUN npx tsc --skipLibCheck

FROM node:22-alpine
RUN apk add --no-cache git python3 make g++
COPY --from=builder /build /app/wt-tracker
WORKDIR /app/wt-tracker
RUN npm ci --omit=dev && apk del git python3 make g++
COPY config.json /app/config.json
WORKDIR /app
CMD ["node", "wt-tracker/bin/wt-tracker", "/app/config.json"]
