import { useAuth } from "../auth/AuthContext";
import { Role } from "@medisync/proto/medisync/identity/v1/identity_pb";

interface NavSidebarProps {
  page: string;
  onNavigate: (page: string) => void;
}

function roleLabel(role: Role): string {
  switch (role) {
    case Role.ADMIN:
      return "Admin";
    case Role.PHARMACIST:
      return "Pharmacist";
    case Role.NURSE:
      return "Nurse";
    case Role.REFILLER:
      return "Refiller";
    default:
      return "Unknown";
  }
}

export function NavSidebar({ page, onNavigate }: NavSidebarProps) {
  const { user, logout } = useAuth();

  return (
    <nav className="nav-sidebar">
      <div className="nav-brand">MediSync</div>

      <div className="nav-section-label">Management</div>
      <button
        className={`nav-link${page === "drugs" ? " active" : ""}`}
        onClick={() => onNavigate("drugs")}
      >
        Drugs
      </button>
      <button
        className={`nav-link${page === "inventory" ? " active" : ""}`}
        onClick={() => onNavigate("inventory")}
      >
        Inventory
      </button>
      <button
        className={`nav-link${page === "users" ? " active" : ""}`}
        onClick={() => onNavigate("users")}
      >
        Users
      </button>

      <div className="nav-footer">
        {user && (
          <>
            <div className="nav-user">{user.displayName || user.username}</div>
            <div className="nav-role">{roleLabel(user.role)}</div>
          </>
        )}
        <button
          className="nav-link"
          onClick={logout}
          style={{ paddingLeft: 0 }}
        >
          Sign Out
        </button>
      </div>
    </nav>
  );
}
