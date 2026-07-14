// WCAG 2.1 AA contrast ratio calculator.
// Computes relative luminance and contrast ratio per WCAG 2.1 §1.4.3.
// Returns both the numeric ratio and a pass/fail at AA level.

interface RGB {
  r: number; // 0-255
  g: number;
  b: number;
}

/** Parse a hex color (e.g. "#1e66f5" or "#fff") into RGB. */
export function hexToRgb(hex: string): RGB {
  let h = hex.replace("#", "");
  if (h.length === 3) {
    h = h[0] + h[0] + h[1] + h[1] + h[2] + h[2];
  }
  if (h.length !== 6) {
    throw new Error(`Invalid hex color: ${hex}`);
  }
  const num = parseInt(h, 16);
  return {
    r: (num >> 16) & 255,
    g: (num >> 8) & 255,
    b: num & 255,
  };
}

/** Convert sRGB channel (0-255) to linear 0-1. */
function srgbToLinear(c: number): number {
  const s = c / 255;
  return s <= 0.04045 ? s / 12.92 : Math.pow((s + 0.055) / 1.055, 2.4);
}

/** WCAG relative luminance. */
function luminance(rgb: RGB): number {
  return (
    0.2126 * srgbToLinear(rgb.r) +
    0.7152 * srgbToLinear(rgb.g) +
    0.0722 * srgbToLinear(rgb.b)
  );
}

/** WCAG contrast ratio between two hex colors. */
export function contrastRatio(hex1: string, hex2: string): number {
  const l1 = luminance(hexToRgb(hex1));
  const l2 = luminance(hexToRgb(hex2));
  const lighter = Math.max(l1, l2);
  const darker = Math.min(l1, l2);
  return (lighter + 0.05) / (darker + 0.05);
}

export interface ContrastResult {
  foreground: string;
  background: string;
  ratio: number;
  aaNormal: boolean; // ≥ 4.5:1
  aaLarge: boolean;  // ≥ 3:1
  aaUI: boolean;     // ≥ 3:1 for UI components
}

/** Check a foreground/background pair against WCAG 2.1 AA thresholds. */
export function checkContrast(
  foreground: string,
  background: string,
): ContrastResult {
  const ratio = contrastRatio(foreground, background);
  return {
    foreground,
    background,
    ratio: Math.round(ratio * 100) / 100,
    aaNormal: ratio >= 4.5,
    aaLarge: ratio >= 3,
    aaUI: ratio >= 3,
  };
}
