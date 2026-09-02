# syntax=docker/dockerfile:1

# ---- Stage 1: build the SPA -------------------------------------------------
FROM node:22-alpine AS build
WORKDIR /app

# Install deps against the lockfile for reproducible builds.
COPY package.json package-lock.json ./
RUN npm ci

# Build the production bundle -> /app/dist
COPY . .
RUN npm run build

# ---- Stage 2: serve via nginx + reverse-proxy /api --------------------------
FROM nginx:1.27-alpine

# Static SPA assets.
COPY --from=build /app/dist /usr/share/nginx/html

# Config template. The nginx:alpine entrypoint runs envsubst over
# /etc/nginx/templates/*.template at container start, writing the result to
# /etc/nginx/conf.d/. ${PORT} and ${BACKEND_URL} are filled from the environment.
COPY nginx/default.conf.template /etc/nginx/templates/default.conf.template

# Cloud Run provides PORT (default 8080); BACKEND_URL is REQUIRED at deploy and
# points at the CreateUploadURL Cloud Run service, e.g.
#   https://create-upload-url-xxxx.a.run.app
ENV PORT=8080

EXPOSE 8080

# Default nginx:alpine CMD launches nginx after templating; keep it.
