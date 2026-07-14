import { useState, useEffect, useRef, type FormEvent } from "react";
import { useAuth } from "./auth.tsx";

export default function LoginScreen() {
  const { login, cardLogin } = useAuth();
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [mode, setMode] = useState<"password" | "card">("password");
  const usernameRef = useRef<HTMLInputElement>(null);

  // Auto-focus username on mount.
  useEffect(() => {
    usernameRef.current?.focus();
  }, [mode]);

  const handleLogin = async (e: FormEvent) => {
    e.preventDefault();
    if (!username.trim() || !password.trim()) {
      setError("กรุณากรอกชื่อผู้ใช้และรหัสผ่าน");
      return;
    }
    setError(null);
    setBusy(true);
    const err = await login(username.trim(), password);
    setBusy(false);
    if (err) setError(err);
  };

  const handleCardScan = async () => {
    setError(null);
    setBusy(true);
    // In production this would read from an NFC/QR scanner event.
    // The stub simulates a card token input; real integration calls
    // vending-3d-ctl-agent's MQTT stream (M6 scope).
    const cardToken = prompt("กรุณาแตะบัตรหรือสแกน QR Code");
    if (!cardToken) {
      setBusy(false);
      return;
    }
    const err = await cardLogin(cardToken.trim());
    setBusy(false);
    if (err) setError(err);
  };

  return (
    <div className="kiosk-screen">
      <div className="kiosk-panel">
        <h1 className="kiosk-panel-title">ระบบเบิกจ่ายยา</h1>
        <p className="kiosk-panel-subtitle">MediSync — เข้าสู่ระบบเพื่อเริ่มเบิกยา</p>

        {error && <div className="kiosk-error" role="alert">{error}</div>}

        {mode === "password" ? (
          <form onSubmit={handleLogin}>
            <div style={{ display: "flex", flexDirection: "column", gap: "var(--space-xl)" }}>
              <div className="kiosk-input-group">
                <label className="kiosk-label" htmlFor="username">ชื่อผู้ใช้</label>
                <input
                  ref={usernameRef}
                  id="username"
                  className="kiosk-input"
                  type="text"
                  autoComplete="username"
                  placeholder="กรอกชื่อผู้ใช้"
                  value={username}
                  onChange={(e) => setUsername(e.target.value)}
                  disabled={busy}
                />
              </div>

              <div className="kiosk-input-group">
                <label className="kiosk-label" htmlFor="password">รหัสผ่าน</label>
                <input
                  id="password"
                  className="kiosk-input"
                  type="password"
                  autoComplete="current-password"
                  placeholder="กรอกรหัสผ่าน"
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  disabled={busy}
                />
              </div>

              <button
                type="submit"
                className="kiosk-btn kiosk-btn-primary"
                disabled={busy}
                style={{ alignSelf: "center", width: "100%" }}
              >
                {busy ? "กำลังเข้าสู่ระบบ..." : "เข้าสู่ระบบ"}
              </button>
            </div>
          </form>
        ) : (
          <div style={{ display: "flex", flexDirection: "column", gap: "var(--space-xl)" }}>
            <div
              className="card-scan-area"
              role="button"
              tabIndex={0}
              onClick={handleCardScan}
              onKeyDown={(e) => { if (e.key === "Enter" || e.key === " ") handleCardScan(); }}
            >
              <span className="card-scan-area__icon">🪪</span>
              <span>แตะเพื่อสแกนบัตร</span>
              <span style={{ fontSize: "1rem" }}>
                แตะบัตรพนักงานหรือสแกน QR Code
              </span>
            </div>
          </div>
        )}

        <div className="login-divider">หรือ</div>

        <div style={{ display: "flex", gap: "var(--space-md)", justifyContent: "center" }}>
          <button
            type="button"
            className="kiosk-btn kiosk-btn-outline"
            style={{ flex: 1, minHeight: "48px", fontSize: "1.125rem" }}
            onClick={() => { setMode("password"); setError(null); }}
            disabled={busy || mode === "password"}
          >
            🔑 รหัสผ่าน
          </button>
          <button
            type="button"
            className="kiosk-btn kiosk-btn-outline"
            style={{ flex: 1, minHeight: "48px", fontSize: "1.125rem" }}
            onClick={() => { setMode("card"); setError(null); }}
            disabled={busy || mode === "card"}
          >
            🪪 บัตร
          </button>
        </div>
      </div>
    </div>
  );
}
