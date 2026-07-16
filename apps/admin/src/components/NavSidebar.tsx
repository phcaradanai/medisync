import { useNavigate, useLocation } from "react-router-dom";
import { useAuth } from "../auth/AuthContext";
import { Role } from "@medisync/proto/medisync/identity/v1/identity_pb";

function roleLabel(role: Role): string {
  switch (role) {
    case Role.ADMIN: return "Admin";
    case Role.PHARMACIST: return "Pharmacist";
    case Role.NURSE: return "Nurse";
    case Role.REFILLER: return "Refiller";
    default: return "Unknown";
  }
}

const NAV_ITEMS = [
  { label: "Projects",  path: "/projects" },
  { label: "Drugs",     path: "/drugs" },
  { label: "Inventory", path: "/inventory" },
  { label: "Users",     path: "/users" },
  { label: "Kiosks",    path: "/kiosks" },
  { label: "Cabinets",  path: "/cabinets" },
] as const;

export function NavSidebar() {
  const { user, logout } = useAuth();
  const navigate = useNavigate();
  const location = useLocation();

  return (
    <nav className="nav-sidebar">
      <div className="nav-brand" onClick={() => navigate("/")} style={{ cursor: "pointer" }}>MediSync</div>

      <div className="nav-section-label">Management</div>
      {NAV_ITEMS.map(({ label, path }) => (
        <button
          key={path}
          className={`nav-link${location.pathname === path ? " active" : ""}`}
          onClick={() => navigate(path)}
        >
          {label}
        </button>
      ))}

      <div className="nav-footer">
        {user && (
          <>
            <div className="nav-user">{user.displayName || user.username}</div>
            <div className="nav-role">{roleLabel(user.role)}</div>
          </>
        )}
        <button className="nav-link" onClick={logout} style={{ paddingLeft: 0 }}>
          Sign Out
        </button>
      </div>
    </nav>
  );
}
