import { useState, useEffect, useRef, type FormEvent } from "react";
import { useAuth } from "./auth.tsx";

export default function LoginScreen() {
  const { login } = useAuth();
  const [code, setCode] = useState("");
  const [pin, setPin] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const codeRef = useRef<HTMLInputElement>(null);

  // Auto-focus code field on mount.
  useEffect(() => {
    codeRef.current?.focus();
  }, []);

  const handleLogin = async (e: FormEvent) => {
    e.preventDefault();
    if (!code.trim() || !pin.trim()) {
      setError("กรุณากรอกรหัสเครื่องและ PIN");
      return;
    }
    setError(null);
    setBusy(true);
    const err = await login(code.trim(), pin);
    setBusy(false);
    if (err) setError(err);
  };

  return (
    <div className="kiosk-screen">
      <div className="kiosk-panel">
        <h1 className="kiosk-panel-title">ระบบเบิกจ่ายยา</h1>
        <p className="kiosk-panel-subtitle">MediSync — กรุณาเข้าสู่ระบบเครื่องจ่ายยา</p>

        {error && <div className="kiosk-error" role="alert">{error}</div>}

        <form onSubmit={handleLogin}>
          <div style={{ display: "flex", flexDirection: "column", gap: "var(--space-xl)" }}>
            <div className="kiosk-input-group">
              <label className="kiosk-label" htmlFor="code">รหัสเครื่อง (Kiosk Code)</label>
              <input
                ref={codeRef}
                id="code"
                className="kiosk-input"
                type="text"
                autoComplete="off"
                placeholder="กรอกรหัสเครื่อง"
                value={code}
                onChange={(e) => setCode(e.target.value)}
                disabled={busy}
              />
            </div>

            <div className="kiosk-input-group">
              <label className="kiosk-label" htmlFor="pin">PIN</label>
              <input
                id="pin"
                className="kiosk-input"
                type="password"
                autoComplete="off"
                inputMode="numeric"
                placeholder="กรอก PIN"
                value={pin}
                onChange={(e) => setPin(e.target.value)}
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
      </div>
    </div>
  );
}
