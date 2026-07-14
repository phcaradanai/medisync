// Auth context: user session, login, logout.
import {
  createContext,
  useContext,
  useState,
  useCallback,
  useEffect,
  type ReactNode,
} from "react";
import { identityClient, setTokenProvider, resetTransport } from "../api/client";
import type { User, LoginResponse } from "@medisync/proto/medisync/identity/v1/identity_pb";

const SESSION_KEY = "medisync_admin_session";

interface Session {
  accessToken: string;
  expiresAt: string; // ISO string
  user: User;
}

interface AuthState {
  user: User | null;
  loading: boolean;
  error: string | null;
  login: (username: string, password: string) => Promise<void>;
  logout: () => void;
}

const AuthContext = createContext<AuthState | null>(null);

function loadSession(): Session | null {
  try {
    const raw = sessionStorage.getItem(SESSION_KEY);
    if (!raw) return null;
    const s: Session = JSON.parse(raw);
    if (new Date(s.expiresAt) <= new Date()) {
      sessionStorage.removeItem(SESSION_KEY);
      return null;
    }
    return s;
  } catch {
    return null;
  }
}

function saveSession(session: Session) {
  sessionStorage.setItem(SESSION_KEY, JSON.stringify(session));
}

function clearSession() {
  sessionStorage.removeItem(SESSION_KEY);
}

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<User | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  // On mount, restore session and verify via WhoAmI.
  useEffect(() => {
    const session = loadSession();
    if (!session) {
      setLoading(false);
      return;
    }
    setTokenProvider(() => session.accessToken);
    resetTransport();

    identityClient
      .whoAmI({})
      .then((res) => {
        if (res.user) {
          setUser(res.user);
        } else {
          clearSession();
        }
      })
      .catch(() => {
        clearSession();
      })
      .finally(() => setLoading(false));
  }, []);

  const login = useCallback(async (username: string, password: string) => {
    setError(null);
    setLoading(true);
    try {
      const res: LoginResponse = await identityClient.login({
        username,
        password,
      });
      if (!res.accessToken || !res.user) {
        throw new Error("Invalid response from server");
      }
      const session: Session = {
        accessToken: res.accessToken,
        expiresAt: res.expiresAt
          ? new Date(
              Number(res.expiresAt.seconds) * 1000 +
                Number(res.expiresAt.nanos) / 1e6,
            ).toISOString()
          : new Date(Date.now() + 3600_000).toISOString(),
        user: res.user,
      };
      saveSession(session);
      setTokenProvider(() => session.accessToken);
      resetTransport();
      setUser(res.user);
    } catch (e: unknown) {
      const msg =
        e instanceof Error ? e.message : "Login failed";
      setError(msg);
      throw e;
    } finally {
      setLoading(false);
    }
  }, []);

  const logout = useCallback(() => {
    clearSession();
    setTokenProvider(() => null);
    resetTransport();
    setUser(null);
  }, []);

  return (
    <AuthContext.Provider value={{ user, loading, error, login, logout }}>
      {children}
    </AuthContext.Provider>
  );
}

export function useAuth(): AuthState {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error("useAuth must be used within AuthProvider");
  return ctx;
}
