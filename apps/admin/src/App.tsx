import { useState, useEffect } from "react";
import { AuthProvider, useAuth } from "./auth/AuthContext";
import { LoginPage } from "./components/LoginPage";
import { NavSidebar } from "./components/NavSidebar";
import { DrugsPage } from "./pages/DrugsPage";
import { InventoryPage } from "./pages/InventoryPage";
import { KiosksPage } from "./pages/KiosksPage";
import { UsersPage } from "./pages/UsersPage";
import { checkContrast } from "./utils/contrast";

// ── WCAG contrast verification ────────────────────────────────────
// Runs once at module load — logs computed ratios for DESIGN.md pairs.
function runContrastVerification() {
  const pairs: [fg: string, bg: string, label: string][] = [
    ["#374151", "#f5f5f5", "body text / page bg"],
    ["#1e1e2e", "#f5f5f5", "headings / page bg"],
    ["#6b7280", "#f5f5f5", "muted text / page bg"],
    ["#374151", "#ffffff", "body text / surface"],
    ["#cdd6f4", "#1e1e2e", "nav text / nav bg"],
    ["#89b4fa", "#1e1e2e", "nav active / nav bg"],
    ["#ffffff", "#1e1e2e", "white text / nav bg (brand)"],
    ["#ffffff", "#1e66f5", "white on primary"],
    ["#374151", "#ffffff", "text on btn-primary (dark btn)"],
    ["#ffffff", "#1e1e2e", "btn-primary text / Deep Navy"],
    ["#374151", "#f0f0f0", "btn-secondary text / subtle"],
    ["#1e1e2e", "#e5e7eb", "section heading / border context"],
    ["#5a5f7a", "#ffffff", "status-badge inactive / surface"],
    ["#2d7a3a", "#ffffff", "status-badge active text / surface"],
    ["#2d7a3a", "#f5f5f5", "status-badge active / page"],
    ["#c94a6d", "#f5f5f5", "error text / page"],
  ];

  const failures: string[] = [];
  const results: string[] = [];

  for (const [fg, bg, label] of pairs) {
    const r = checkContrast(fg, bg);
    const status = r.aaNormal ? "PASS" : r.aaLarge ? "PASS (large)" : "FAIL";
    results.push(`  ${status.padEnd(13)} ${r.ratio.toFixed(2).padEnd(6)}  ${label.padEnd(40)}  (${fg} on ${bg})`);
    if (!r.aaNormal && !r.aaLarge) {
      failures.push(`FAIL ${label}: ${r.ratio.toFixed(2)} (${fg} on ${bg})`);
    }
  }

  // Log to console so it's visible during dev/building.
  console.group("WCAG 2.1 AA Contrast Verification (computed ratios)");
  console.log("Foreground / Background pairs from DESIGN.md");
  console.log("Thresholds: AA Normal ≥ 4.5, AA Large ≥ 3.0");
  console.log("");
  for (const line of results) console.log(line);
  console.log("");
  if (failures.length > 0) {
    console.warn(`${failures.length} FAILURES:`);
    for (const f of failures) console.warn(f);
  } else {
    console.log("✅ All pairs meet WCAG 2.1 AA contrast thresholds.");
  }
  console.groupEnd();

  return { results, failures };
}

let contrastResults: ReturnType<typeof runContrastVerification> | null = null;

// ── App shell ──────────────────────────────────────────────────────

function AppShell() {
  const { user, loading } = useAuth();
  const [page, setPage] = useState("drugs");

  // Run contrast check once.
  useEffect(() => {
    if (!contrastResults) {
      contrastResults = runContrastVerification();
    }
  }, []);

  if (loading) {
    return (
      <div className="login-page">
        <div className="login-panel" style={{ textAlign: "center" }}>
          <p>Loading…</p>
        </div>
      </div>
    );
  }

  if (!user) {
    return <LoginPage />;
  }

  const PageComponent =
    page === "drugs"
      ? DrugsPage
      : page === "inventory"
        ? InventoryPage
        : page === "kiosks"
          ? KiosksPage
          : page === "users"
            ? UsersPage
            : DrugsPage;

  return (
    <div className="app-shell">
      <NavSidebar page={page} onNavigate={setPage} />
      <div className="app-main">
        <div className="app-content">
          <PageComponent />
        </div>
      </div>
    </div>
  );
}

// ── Root export ────────────────────────────────────────────────────

export default function App() {
  return (
    <AuthProvider>
      <AppShell />
    </AuthProvider>
  );
}
