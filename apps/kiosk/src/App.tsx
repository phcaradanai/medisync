import { useState } from "react";
import { AuthProvider, useAuth } from "./auth.tsx";
import LoginScreen from "./LoginScreen.tsx";
import WithdrawFlow from "./WithdrawFlow.tsx";
import RefillFlow from "./RefillFlow.tsx";

type Mode = "withdraw" | "refill";

function AppShell() {
  const { state, loading, logout } = useAuth();
  const [mode, setMode] = useState<Mode>("withdraw");

  if (loading) {
    return (
      <div className="kiosk-screen">
        <div className="kiosk-panel" style={{ alignItems: "center" }}>
          <div className="spinner" />
          <p className="kiosk-panel-subtitle">กำลังโหลด...</p>
        </div>
      </div>
    );
  }

  if (!state) {
    return <LoginScreen />;
  }

  const isRefill = mode === "refill";

  return (
    <>
      <header className={`kiosk-header${isRefill ? " kiosk-header--refill" : ""}`}>
        <div className="kiosk-header__user">
          <span>{state.kiosk.displayName}</span>
          <span style={{ fontSize: "0.8rem", opacity: 0.6 }}>
            {state.kiosk.code}
          </span>
        </div>
        <div style={{ display: "flex", gap: "var(--space-md)", alignItems: "center" }}>
          <button
            type="button"
            className={`kiosk-header__mode-btn${!isRefill ? " kiosk-header__mode-btn--active" : ""}`}
            onClick={() => setMode("withdraw")}
          >
            💊 เบิกยา
          </button>
          <button
            type="button"
            className={`kiosk-header__mode-btn${isRefill ? " kiosk-header__mode-btn--refill-active" : ""}`}
            onClick={() => setMode("refill")}
          >
            📦 เติมยา
          </button>
          <button
            type="button"
            className="kiosk-header__logout"
            onClick={logout}
          >
            ออกจากระบบ
          </button>
        </div>
      </header>
      {isRefill && (
        <div className="kiosk-refill-banner">🔄 โหมดเติมยา</div>
      )}
      {mode === "withdraw" ? <WithdrawFlow /> : <RefillFlow />}
    </>
  );
}

export default function App() {
  return (
    <AuthProvider>
      <AppShell />
    </AuthProvider>
  );
}
