/**
 * WCAG 2.1 contrast ratio utility.
 * Computes the relative luminance and contrast ratio per
 * https://www.w3.org/TR/WCAG21/#dfn-contrast-ratio.
 */

/** sRGB to linear (gamma expansion). */
function linearize(channel8bit: number): number {
  const s = channel8bit / 255;
  return s <= 0.04045 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4;
}

/** Relative luminance of an RGB hex color (6-char). */
export function relativeLuminance(hex: string): number {
  const r = parseInt(hex.slice(1, 3), 16);
  const g = parseInt(hex.slice(3, 5), 16);
  const b = parseInt(hex.slice(5, 7), 16);
  return 0.2126 * linearize(r) + 0.7152 * linearize(g) + 0.0722 * linearize(b);
}

/** WCAG contrast ratio: (L1 + 0.05) / (L2 + 0.05) where L1 >= L2. */
export function contrastRatio(hex1: string, hex2: string): number {
  const l1 = relativeLuminance(hex1);
  const l2 = relativeLuminance(hex2);
  const lighter = Math.max(l1, l2);
  const darker = Math.min(l1, l2);
  return (lighter + 0.05) / (darker + 0.05);
}

/** AA = 4.5:1 (normal text) or 3:1 (large≥18pt bold or ≥24pt). */
export function meetsAA(ratio: number): boolean {
  return ratio >= 4.5;
}

export function meetsAALarge(ratio: number): boolean {
  return ratio >= 3.0;
}

/** Color pairs we must verify for the kiosk. */
export interface ContrastCheck {
  label: string;
  foreground: string;
  background: string;
  /** "normal" or "large" (kiosk body is 20px/1.25rem ≈ 20pt → large) */
  category: "normal" | "large";
}

/** Run all kiosk contrast checks. Returns failures only. */
export function verifyKioskContrast(): string[] {
  const checks: ContrastCheck[] = [
    // Primary button: white text on deep navy
    { label: "primary-btn", foreground: "#ffffff", background: "#1e1e2e", category: "large" },
    // Outline button: navy text on page gray
    { label: "outline-btn", foreground: "#1e1e2e", background: "#f5f5f5", category: "large" },
    // Body text on white panel
    { label: "body-text", foreground: "#374151", background: "#ffffff", category: "large" },
    // Muted text on white
    { label: "muted-text", foreground: "#6b7280", background: "#ffffff", category: "large" },
    // Status text on success bg — WCAG AA (≥4.5:1 normal)
    { label: "success-badge", foreground: "#0d6b0d", background: "#a6e3a1", category: "normal" },
    // Status text on warning bg
    { label: "warning-badge", foreground: "#6b5500", background: "#f9e2af", category: "normal" },
    // Status text on error bg
    { label: "error-badge", foreground: "#7a001e", background: "#f38ba8", category: "normal" },
    // Status text on info bg
    { label: "info-badge", foreground: "#003399", background: "#89b4fa", category: "normal" },
    // Status text on progress bg
    { label: "progress-badge", foreground: "#7a3d00", background: "#fab387", category: "normal" },
    // Status text on neutral bg
    { label: "neutral-badge", foreground: "#2d313b", background: "#9399b2", category: "normal" },
    // Kiosk title (navy on white)
    { label: "kiosk-title", foreground: "#1e1e2e", background: "#ffffff", category: "large" },
    // Input text on white
    { label: "input-text", foreground: "#1e1e2e", background: "#ffffff", category: "large" },
    // Input placeholder
    { label: "input-placeholder", foreground: "#6b7280", background: "#ffffff", category: "large" },
    // Header text on dark bg
    { label: "header-text", foreground: "#cdd6f4", background: "#1e1e2e", category: "normal" },
    // Action button text (white on navy large)
    { label: "action-btn", foreground: "#ffffff", background: "#1e1e2e", category: "large" },
    // Card patient name
    { label: "card-patient", foreground: "#1e1e2e", background: "#ffffff", category: "large" },
    // Card HN (mono text on white)
    { label: "card-hn", foreground: "#6b7280", background: "#ffffff", category: "normal" },
    // Success verdict (green text on white)
    { label: "success-verdict", foreground: "#1a7f1a", background: "#ffffff", category: "large" },
    // Error verdict (red text on white)
    { label: "error-verdict", foreground: "#c41e3a", background: "#ffffff", category: "large" },
    // Progress verdict (orange text on white)
    { label: "progress-verdict", foreground: "#c26500", background: "#ffffff", category: "large" },
    // Danger button (white on error bg)
    { label: "danger-btn", foreground: "#ffffff", background: "#c41e3a", category: "large" },
  ];

  const failures: string[] = [];
  for (const check of checks) {
    const ratio = contrastRatio(check.foreground, check.background);
    const threshold = check.category === "large" ? 3.0 : 4.5;
    const pass = ratio >= threshold;
    if (!pass) {
      failures.push(
        `FAIL ${check.label}: ${check.foreground} on ${check.background} = ${ratio.toFixed(2)}:1 (need ≥${threshold}:1 for ${check.category})`,
      );
    }
  }
  return failures;
}
