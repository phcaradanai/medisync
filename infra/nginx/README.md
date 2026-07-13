# infra/nginx

This directory is reserved for a production reverse proxy (TLS termination, routing).

Currently, each frontend app (admin, kiosk) runs its own nginx container with
an embedded nginx.conf at `apps/<name>/nginx.conf`. This is adequate for the
MVP deployment where each app exposes its own port.

When a unified reverse proxy is needed (TLS, single entry point, rate limiting):

1. Place the reverse-proxy nginx config here.
2. Update `infra/docker-compose.prod.yml` to add the reverse proxy service.
3. Remove direct port exposure from the admin/kiosk services.

See `docs/DEPLOYMENT.md` for the known blocker on TLS termination.
