import type { ReactNode } from "react";
import { useNavigate, useLocation, Outlet } from "react-router-dom";
import { useAuth } from "../auth/AuthContext";
import { Icon } from "../features/masterdata/icons";

// ── Section registry: rail order + top-bar title ───────────────────────
interface Section {
  path: string;
  title: string;
  icon: (p: { size?: number }) => ReactNode;
}

const SECTIONS: Section[] = [
  { path: "/dashboard", title: "ภาพรวมระบบ · Dashboard", icon: Icon.grid },
  { path: "/drugs", title: "ยา · Master Data", icon: Icon.pill },
  { path: "/projects", title: "โครงการ · Master Data", icon: Icon.folder },
  { path: "/devices", title: "อุปกรณ์ · ตู้ยา & Kiosk", icon: Icon.cabinet },
  { path: "/users", title: "ผู้ใช้งาน · Master Data", icon: Icon.users },
  { path: "/inventory", title: "คลังยา · ดูอย่างเดียว", icon: Icon.inventory },
];

function initials(name: string): string {
  const parts = name.trim().split(/\s+/).filter(Boolean);
  if (parts.length === 0) return "?";
  if (parts.length === 1) return parts[0].slice(0, 2).toUpperCase();
  return (parts[0][0] + parts[1][0]).toUpperCase();
}

export function AppChrome() {
  const { user, logout } = useAuth();
  const navigate = useNavigate();
  const location = useLocation();

  const active = SECTIONS.find((s) => location.pathname.startsWith(s.path)) ?? SECTIONS[0];
  const userName = user?.displayName || user?.username || "ผู้ใช้งาน";

  return (
    <div className="md-app">
      {/* ── Top bar ─────────────────────────────────────────────── */}
      <header className="md-topbar">
        <div className="md-brand" onClick={() => navigate("/master-data")} style={{ cursor: "pointer" }}>
          <span className="md-brand-logo"><Icon.database size={20} /></span>
          MediSync
        </div>
        <div className="md-topbar-title">
          <active.icon size={22} /> {active.title}
        </div>
        <div className="md-topbar-actions">
          <button className="md-icon-btn" title="การแจ้งเตือน"><Icon.bell size={20} /></button>
          <button className="md-icon-btn" title="ช่วยเหลือ"><Icon.help size={20} /></button>
          <div className="md-avatar" title={userName}>{initials(userName)}</div>
        </div>
      </header>

      <div className="md-body">
        {/* ── Icon rail ─────────────────────────────────────────── */}
        <nav className="md-rail">
          {SECTIONS.map((s) => (
            <button
              key={s.path}
              className={`md-rail-item${s.path === active.path ? " active" : ""}`}
              onClick={() => navigate(s.path)}
              title={s.title}
            >
              <s.icon size={22} />
            </button>
          ))}
          <div className="md-rail-spacer" />
          <button className="md-rail-item md-rail-collapse" onClick={logout} title="ออกจากระบบ">
            <Icon.logout size={20} />
          </button>
        </nav>

        {/* ── Content ───────────────────────────────────────────── */}
        <main className="md-content">
          <Outlet />
        </main>
      </div>
    </div>
  );
}
