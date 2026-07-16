# Stage 1: build
FROM node:22-alpine AS build
WORKDIR /app

# Copy workspace root + all workspace package.json files for npm ci
COPY package.json package-lock.json ./
COPY packages/proto-ts/package.json ./packages/proto-ts/
COPY apps/admin/package.json ./apps/admin/
COPY apps/kiosk/package.json ./apps/kiosk/

# Copy full proto-ts package (needed for build)
COPY packages/proto-ts ./packages/proto-ts

# Copy app config files
COPY apps/kiosk/tsconfig.json ./apps/kiosk/
COPY apps/kiosk/vite.config.ts ./apps/kiosk/
COPY apps/kiosk/uno.config.ts ./apps/kiosk/
COPY apps/kiosk/index.html ./apps/kiosk/

RUN npm ci --no-audit --no-fund
RUN npm rebuild

COPY apps/kiosk/src ./apps/kiosk/src

RUN npm run build -w apps/kiosk

# Stage 2: serve
FROM nginx:alpine
COPY apps/kiosk/nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /app/apps/kiosk/dist /usr/share/nginx/html
EXPOSE 80
