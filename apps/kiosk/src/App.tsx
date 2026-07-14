import { AuthProvider, useAuth } from "./auth.tsx";
import LoginScreen from "./LoginScreen.tsx";
import WithdrawFlow from "./WithdrawFlow.tsx";

function AppShell() {
  const { state, loading, logout } = useAuth();

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
        <div className="kiosk-header__user">
          <span>{state.user.displayName}</span>
          <span style={{ fontSize: "0.8rem", opacity: 0.6 }}>
            {state.user.wardIds?.join(", ")}
          </span>
        </div>
        <button
          type="button"
          className="kiosk-header__logout"
          onClick={logout}
        >
          ออกจากระบบ
        </button>
      </header>
      <WithdrawFlow />
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
