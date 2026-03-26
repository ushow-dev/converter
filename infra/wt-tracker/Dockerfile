FROM node:22-alpine
RUN npm install -g wt-tracker
COPY config.json /app/config.json
WORKDIR /app
CMD ["wt-tracker", "/app/config.json"]
