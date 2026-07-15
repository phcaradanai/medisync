/**
 * Connect-RPC transport for the kiosk.
 * All requests go to the same origin (Vite proxies /medisync.* to core:8080).
 * Reads the kiosk auth token from localStorage.
 */
import { createConnectTransport } from "@connectrpc/connect-web";
import type { Interceptor } from "@connectrpc/connect";

/** Bearer token interceptor: attaches the stored kiosk JWT to every request. */
const authInterceptor: Interceptor = (next) => async (req) => {
  const token = localStorage.getItem("medisync_kiosk_token");
  if (token) {
    req.header.set("Authorization", `Bearer ${token}`);
  }
  return next(req);
};

export const transport = createConnectTransport({
  baseUrl: "/",
  interceptors: [authInterceptor],
});
