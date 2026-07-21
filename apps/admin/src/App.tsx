import { Routes, Route, Navigate } from "react-router-dom";
import { AuthProvider, useAuth } from "./auth/AuthContext";
import { LoginPage } from "./components/LoginPage";
import { AppChrome } from "./components/AppChrome";
import { DashboardPage } from "./features/dashboard/DashboardPage";
import { DrugsPage } from "./features/drugs/DrugsPage";
import { InventoryPage } from "./features/inventory/InventoryPage";
import { UsersPage } from "./features/users/UsersPage";
import { DevicesPage } from "./features/devices/DevicesPage";
import { ProjectsPage } from "./features/projects/ProjectsPage";
import { DispenseTransactionsPage } from "./features/reports/DispenseTransactionsPage";

function AppShell() {
  const { user, loading } = useAuth();

  if (loading) {
    return <div className="login-page"><div className="login-panel" style={{ textAlign: "center" }}><p>Loading…</p></div></div>;
  }
  if (!user) {
    return <LoginPage />;
  }
  return <AppChrome />;
}

export default function App() {
  return (
    <AuthProvider>
      <Routes>
        <Route element={<AppShell />}>
          <Route index element={<Navigate to="/dashboard" replace />} />
          <Route path="dashboard" element={<DashboardPage />} />
          <Route path="drugs" element={<DrugsPage />} />
          <Route path="projects" element={<ProjectsPage />} />
          <Route path="devices" element={<DevicesPage />} />
          <Route path="users" element={<UsersPage />} />
          <Route path="inventory" element={<InventoryPage />} />
          <Route path="dispense-transactions" element={<DispenseTransactionsPage />} />
          {/* legacy redirects */}
          <Route path="master-data" element={<Navigate to="/dashboard" replace />} />
          <Route path="kiosks" element={<Navigate to="/devices" replace />} />
          <Route path="cabinets" element={<Navigate to="/devices" replace />} />
        </Route>
      </Routes>
    </AuthProvider>
  );
}
