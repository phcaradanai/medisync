import { Routes, Route, Navigate, useLocation, useNavigate } from "react-router-dom";
import { AuthProvider, useAuth } from "./auth.tsx";
import LoginScreen from "./LoginScreen.tsx";
import WithdrawFlow from "./features/withdraw/WithdrawFlow";
import RefillFlow from "./features/refill/RefillFlow";

function KioskShell() {
  const { state, loading, logout } = useAuth();
  const navigate = useNavigate();
  const location = useLocation();
  const isRefill = location.pathname.startsWith("/refill");

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

  return (
    <>
      <header className="kiosk-header">
        <div className="flex flex-col">
          <span className="text-white text-lg font-bold">{state.kiosk.displayName}</span>
          <span className="text-sm text-gray-400">{state.kiosk.code}</span>
        </div>
        <div className="flex gap-3 items-center">
          <button
            className={`kiosk-header__mode-btn ${!isRefill ? "kiosk-header__mode-btn--active" : ""}`}
            onClick={() => navigate("/withdraw")}
          >
            💊 เบิกยา
          </button>
          <button
            className={`kiosk-header__mode-btn ${isRefill ? "kiosk-header__mode-btn--refill-active" : ""}`}
            onClick={() => navigate("/refill")}
          >
            📦 เติมยา
          </button>
          <button className="text-sm text-gray-400 hover:text-white transition-colors" onClick={logout}>
            ออกจากระบบ
          </button>
        </div>
      </header>
      {isRefill && <div className="kiosk-refill-banner">🔄 โหมดเติมยา</div>}
      <Routes>
        <Route index element={<Navigate to="/withdraw" replace />} />
        <Route path="withdraw" element={<WithdrawFlow />} />
        <Route path="refill" element={<RefillFlow />} />
      </Routes>
    </>
  );
}

export default function App() {
  return (
    <AuthProvider>
      <KioskShell />
    </AuthProvider>
  );
}
