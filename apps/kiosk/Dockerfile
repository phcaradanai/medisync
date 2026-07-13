# Stage 1 — build
FROM node:24-alpine AS build

WORKDIR /app
COPY package.json package-lock.json ./
COPY apps/kiosk/package.json ./apps/kiosk/
COPY packages/ ./packages/
RUN npm ci

COPY apps/kiosk/ ./apps/kiosk/
RUN npm run build -w apps/kiosk

# Stage 2 — serve
FROM nginx:1.27-alpine

COPY --from=build /app/apps/kiosk/dist /usr/share/nginx/html
COPY apps/kiosk/nginx.conf /etc/nginx/conf.d/default.conf

EXPOSE 80
