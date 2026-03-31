# ── Build stage ───────────────────────────────────────────────────────────────
FROM node:20-alpine AS builder

WORKDIR /app

# Install dependencies first (layer cache)
COPY package.json ./
RUN npm install

# Copy source and build
COPY . .
RUN mkdir -p public

# NEXT_PUBLIC_* vars must be available at build time (inlined into JS bundle)
ARG NEXT_PUBLIC_P2P_ENABLED=true
ARG NEXT_PUBLIC_P2P_TRACKER_URL=wss://t.pimor.online
ARG NEXT_PUBLIC_API_URL=https://api.pimor.online
ARG NEXT_PUBLIC_VAST_TAG=

RUN npm run build

# ── Runtime stage ─────────────────────────────────────────────────────────────
FROM node:20-alpine AS runner

WORKDIR /app

ENV NODE_ENV=production

RUN addgroup --system --gid 1001 nodejs && \
    adduser --system --uid 1001 nextjs

# Standalone output bundles the server; static assets are separate.
# Ensure public exists even when project has no /public directory.
COPY --from=builder /app/public ./public
COPY --from=builder --chown=nextjs:nodejs /app/.next/standalone ./
COPY --from=builder --chown=nextjs:nodejs /app/.next/static ./.next/static

USER nextjs

EXPOSE 3000

ENV PORT=3000
ENV HOSTNAME="0.0.0.0"

CMD ["node", "server.js"]
