import { useEffect } from "react";
import { Routes, Route, Navigate, Outlet } from "react-router-dom";
import { AuthProvider, useAuth } from "./auth/AuthContext";
import { LoginPage } from "./components/LoginPage";
import { NavSidebar } from "./components/NavSidebar";
import { DrugsPage } from "./features/drugs/DrugsPage";
import { InventoryPage } from "./features/inventory/InventoryPage";
import { UsersPage } from "./features/users/UsersPage";
import { KiosksPage } from "./features/kiosks/KiosksPage";
import { CabinetsPage } from "./features/cabinets/CabinetsPage";
import { checkContrast } from "./utils/contrast";

let contrastChecked = false;

function AppShell() {
  const { user, loading } = useAuth();

  useEffect(() => {
    if (!contrastChecked) {
      contrastChecked = true;
      const pairs: [string, string, string][] = [
        ["#374151", "#f5f5f5", "body text / page bg"],
        ["#ffffff", "#1e66f5", "white on primary"],
        ["#ffffff", "#1e1e2e", "white on nav bg"],
      ];
      for (const [fg, bg, label] of pairs) {
        const r = checkContrast(fg, bg);
        if (!r.aaNormal && !r.aaLarge) {
          console.warn(`WCAG FAIL ${label}: ${r.ratio.toFixed(2)} (${fg} on ${bg})`);
        }
      }
    }
  }, []);

  if (loading) {
    return <div className="login-page"><div className="login-panel" style={{ textAlign: "center" }}><p>Loading…</p></div></div>;
  }

  if (!user) {
    return <LoginPage />;
  }

  return (
    <div className="app-shell">
      <NavSidebar />
      <div className="app-main">
        <div className="app-content">
          <Outlet />
        </div>
      </div>
    </div>
  );
}

export default function App() {
  return (
    <AuthProvider>
      <Routes>
        <Route element={<AppShell />}>
          <Route index element={<Navigate to="/drugs" replace />} />
          <Route path="drugs" element={<DrugsPage />} />
          <Route path="inventory" element={<InventoryPage />} />
          <Route path="users" element={<UsersPage />} />
          <Route path="kiosks" element={<KiosksPage />} />
          <Route path="cabinets" element={<CabinetsPage />} />
        </Route>
      </Routes>
    </AuthProvider>
  );
}
