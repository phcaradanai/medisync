/**
 * Auth context and session management for the kiosk.
 * Token stored in sessionStorage (cleared on tab close = auto logout).
 * Session expires based on JWT expiry.
 */
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useState,
  type ReactNode,
} from "react";
import { createClient } from "@connectrpc/connect";
import { IdentityService, LoginRequestSchema, CardLoginRequestSchema, WhoAmIRequestSchema } from "@medisync/proto/medisync/identity/v1/identity_pb";
import { create } from "@bufbuild/protobuf";
import type { User } from "@medisync/proto/medisync/identity/v1/identity_pb";
import { transport } from "./transport.ts";

const identityClient = createClient(IdentityService, transport);

export interface AuthState {
  user: User;
  token: string;
  expiresAt: Date;
}

interface AuthContextValue {
  state: AuthState | null;
  loading: boolean;
  login: (username: string, password: string) => Promise<string | null>;
  cardLogin: (cardToken: string) => Promise<string | null>;
  logout: () => void;
}

const AuthContext = createContext<AuthContextValue | null>(null);

export function AuthProvider({ children }: { children: ReactNode }) {
  const [state, setState] = useState<AuthState | null>(null);
  const [loading, setLoading] = useState(true);

  // Attempt to restore session from sessionStorage on mount.
  useEffect(() => {
    const token = sessionStorage.getItem("medisync_token");
    const expiresAt = sessionStorage.getItem("medisync_expires");
    if (token && expiresAt) {
      const expiry = new Date(expiresAt);
      if (expiry > new Date()) {
        // Restore session: validate token with WhoAmI.
        validateToken(token).then((user) => {
          if (user) {
            setState({ user, token, expiresAt: expiry });
          } else {
            clearSession();
          }
          setLoading(false);
        });
        return;
      }
      clearSession();
    }
    setLoading(false);
  }, []);

  const persistSession = useCallback((token: string, expiresAt: Date, user: User) => {
    sessionStorage.setItem("medisync_token", token);
    sessionStorage.setItem("medisync_expires", expiresAt.toISOString());
    setState({ user, token, expiresAt });
  }, []);

  const login = useCallback(async (username: string, password: string): Promise<string | null> => {
    try {
      const req = create(LoginRequestSchema, { username, password });
      const res = await identityClient.login(req);
      if (!res.user || !res.accessToken) {
        return "เข้าสู่ระบบไม่สำเร็จ กรุณาลองอีกครั้ง";
      }
      const expiresAt = res.expiresAt?.seconds
        ? new Date(Number(res.expiresAt.seconds) * 1000)
        : new Date(Date.now() + 3600_000);
      persistSession(res.accessToken, expiresAt, res.user);
      return null;
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : "เกิดข้อผิดพลาด";
      if (msg.includes("Unauthenticated") || msg.includes("401")) {
        return "ชื่อผู้ใช้หรือรหัสผ่านไม่ถูกต้อง";
      }
      return `ไม่สามารถเชื่อมต่อเซิร์ฟเวอร์ได้: ${msg}`;
    }
  }, [persistSession]);

  const cardLogin = useCallback(async (cardToken: string): Promise<string | null> => {
    try {
      const req = create(CardLoginRequestSchema, { cardToken });
      const res = await identityClient.cardLogin(req);
      if (!res.user || !res.accessToken) {
        return "ไม่รู้จักบัตร กรุณาลองอีกครั้ง";
      }
      const expiresAt = res.expiresAt?.seconds
        ? new Date(Number(res.expiresAt.seconds) * 1000)
        : new Date(Date.now() + 3600_000);
      persistSession(res.accessToken, expiresAt, res.user);
      return null;
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : "เกิดข้อผิดพลาด";
      return `ไม่รู้จักบัตร: ${msg}`;
    }
  }, [persistSession]);

  const logout = useCallback(() => {
    clearSession();
    setState(null);
  }, []);

  return (
    <AuthContext.Provider value={{ state, loading, login, cardLogin, logout }}>
      {children}
    </AuthContext.Provider>
  );
}

export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error("useAuth must be inside AuthProvider");
  return ctx;
}

async function validateToken(token: string): Promise<User | null> {
  try {
    const req = create(WhoAmIRequestSchema, {});
    // Create a client that uses the stored token.
    const { createConnectTransport } = await import("@connectrpc/connect-web");
    const t = createConnectTransport({
      baseUrl: "/",
      interceptors: [
        (next) => (req) => {
          req.header.set("Authorization", `Bearer ${token}`);
          return next(req);
        },
      ],
    });
    const client = createClient(IdentityService, t);
    const res = await client.whoAmI(req);
    return res.user ?? null;
  } catch {
    return null;
  }
}

function clearSession(): void {
  sessionStorage.removeItem("medisync_token");
  sessionStorage.removeItem("medisync_expires");
}
