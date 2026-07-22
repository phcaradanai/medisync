/**
 * Auth context and session management for the kiosk.
 * Stores kiosk session in localStorage (persists across tab closes).
 * Session is restored on boot and validated via KioskValidate.
 * Cleared on logout or invalid/expired token.
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
import {
  KioskService,
  KioskLoginRequestSchema,
  KioskValidateRequestSchema,
} from "@medisync/proto/medisync/kiosk/v1/kiosk_pb";
import { create } from "@bufbuild/protobuf";
import type { Kiosk } from "@medisync/proto/medisync/kiosk/v1/kiosk_pb";
import { transport } from "./transport.ts";

const kioskClient = createClient(KioskService, transport);

const STORAGE_PREFIX = "medisync_kiosk_";

export interface KioskAuthState {
  kiosk: Kiosk;
  token: string;
  expiresAt: Date;
}

interface AuthContextValue {
  state: KioskAuthState | null;
  loading: boolean;
  login: (code: string, pin: string) => Promise<string | null>;
  logout: () => void;
}

const AuthContext = createContext<AuthContextValue | null>(null);

export function AuthProvider({ children }: { children: ReactNode }) {
  const [state, setState] = useState<KioskAuthState | null>(null);
  const [loading, setLoading] = useState(true);

  // Attempt to restore session from localStorage on mount.
  useEffect(() => {
    const token = localStorage.getItem(`${STORAGE_PREFIX}token`);
    const expiresAt = localStorage.getItem(`${STORAGE_PREFIX}expires`);
    const kioskJson = localStorage.getItem(`${STORAGE_PREFIX}kiosk`);

    if (token && expiresAt && kioskJson) {
      const expiry = new Date(expiresAt);
      if (expiry > new Date()) {
        try {
          const kiosk = JSON.parse(kioskJson) as Kiosk;
          // Restore from cache, validate in background.
          setState({ kiosk, token, expiresAt: expiry });
          setLoading(false);
          validateKioskToken(token).then((valid) => {
            if (!valid) {
              clearKioskSessionStorage();
              setState(null);
            }
          });
          return;
        } catch {
          clearKioskSessionStorage();
        }
      } else {
        clearKioskSessionStorage();
      }
    }
    setLoading(false);
  }, []);

  const persistSession = useCallback(
    (token: string, expiresAt: Date, kiosk: Kiosk) => {
      localStorage.setItem(`${STORAGE_PREFIX}token`, token);
      localStorage.setItem(`${STORAGE_PREFIX}expires`, expiresAt.toISOString());
      localStorage.setItem(`${STORAGE_PREFIX}kiosk`, JSON.stringify(kiosk, (_key, value) =>
        typeof value === "bigint" ? String(value) : value
      ));
      setState({ kiosk, token, expiresAt });
    },
    [],
  );

  const login = useCallback(
    async (code: string, pin: string): Promise<string | null> => {
      try {
        const req = create(KioskLoginRequestSchema, { code, pin });
        const res = await kioskClient.kioskLogin(req);
        if (!res.kiosk || !res.accessToken) {
          return "เข้าสู่ระบบไม่สำเร็จ กรุณาลองอีกครั้ง";
        }
        const expiresAt = res.expiresAt?.seconds
          ? new Date(Number(res.expiresAt.seconds) * 1000)
          : new Date(Date.now() + 3600_000);
        persistSession(res.accessToken, expiresAt, res.kiosk);
        return null;
      } catch (err: unknown) {
        const msg = err instanceof Error ? err.message : "เกิดข้อผิดพลาด";
        if (msg.includes("Unauthenticated") || msg.includes("401")) {
          return "รหัสเครื่องหรือ PIN ไม่ถูกต้อง";
        }
        return `ไม่สามารถเชื่อมต่อเซิร์ฟเวอร์ได้: ${msg}`;
      }
    },
    [persistSession],
  );

  const logout = useCallback(() => {
    clearKioskSessionStorage();
    setState(null);
  }, []);

  return (
    <AuthContext.Provider value={{ state, loading, login, logout }}>
      {children}
    </AuthContext.Provider>
  );
}

export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error("useAuth must be inside AuthProvider");
  return ctx;
}

async function validateKioskToken(token: string): Promise<boolean> {
  try {
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
    const client = createClient(KioskService, t);
    const req = create(KioskValidateRequestSchema, {});
    await client.kioskValidate(req);
    return true;
  } catch {
    return false;
  }
}

const KIOSK_AUTH_STORAGE_KEYS = [
  `${STORAGE_PREFIX}token`,
  `${STORAGE_PREFIX}expires`,
  `${STORAGE_PREFIX}kiosk`,
] as const;

const KIOSK_TRANSIENT_STORAGE_KEYS = [
  "medisync.kiosk.current-sticker.v1",
  "medisync.kiosk.current-identity.v1",
  "medisync.kiosk.unsubmitted-request.v1",
  "medisync.kiosk.scanner-buffer.v1",
] as const;

/** Clears only kiosk-owned authentication and transient request state.
 * Accepted transaction recovery (`medisync.active-withdrawals.v1`) is intentionally preserved.
 */
export function clearKioskSessionStorage(): void {
  for (const key of [...KIOSK_AUTH_STORAGE_KEYS, ...KIOSK_TRANSIENT_STORAGE_KEYS]) {
    localStorage.removeItem(key);
    sessionStorage.removeItem(key);
  }
}
