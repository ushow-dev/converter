FROM node:22-alpine
RUN apk add --no-cache git && npm install -g wt-tracker && apk del git
COPY config.json /app/config.json
WORKDIR /app
CMD ["wt-tracker", "/app/config.json"]
