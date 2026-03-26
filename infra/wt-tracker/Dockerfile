FROM node:22-alpine AS builder
RUN apk add --no-cache git python3 make g++
RUN npm install -g wt-tracker
WORKDIR /usr/local/lib/node_modules/wt-tracker
RUN npm install && npx tsc

FROM node:22-alpine
COPY --from=builder /usr/local/lib/node_modules/wt-tracker /usr/local/lib/node_modules/wt-tracker
RUN ln -s /usr/local/lib/node_modules/wt-tracker/bin/wt-tracker /usr/local/bin/wt-tracker
COPY config.json /app/config.json
WORKDIR /app
CMD ["wt-tracker", "/app/config.json"]
