# Stage 1 — build
FROM node:24-alpine AS build

WORKDIR /app
COPY package.json package-lock.json ./
COPY apps/admin/package.json ./apps/admin/
COPY packages/ ./packages/
RUN npm ci

COPY apps/admin/ ./apps/admin/
RUN npm run build -w apps/admin

# Stage 2 — serve
FROM nginx:1.27-alpine

COPY --from=build /app/apps/admin/dist /usr/share/nginx/html
COPY apps/admin/nginx.conf /etc/nginx/conf.d/default.conf

EXPOSE 80
