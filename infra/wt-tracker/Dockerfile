FROM ubuntu:24.04
RUN apt-get update && \
    apt-get install -y --no-install-recommends git ca-certificates curl && \
    curl -fsSL https://deb.nodesource.com/setup_22.x | bash - && \
    apt-get install -y --no-install-recommends nodejs && \
    apt-get clean && rm -rf /var/lib/apt/lists/*
WORKDIR /build
RUN git clone --depth 1 https://github.com/Novage/wt-tracker.git .
RUN npm ci --omit=dev
COPY config.json /app/config.json
WORKDIR /app
CMD ["node", "--experimental-strip-types", "/build/bin/wt-tracker", "/app/config.json"]
