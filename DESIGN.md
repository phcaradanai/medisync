---
name: MediSync
description: Hospital medication dispensing — kiosk + admin. Clinical, calm, precise; kiosk adds big-and-obvious.
colors:
  primary: "#1e66f5"
  neutral-deep: "#1e1e2e"
  neutral-page: "#f5f5f5"
  neutral-surface: "#ffffff"
  neutral-subtle: "#f0f0f0"
  neutral-border: "#e5e7eb"
  neutral-border-strong: "#d1d5db"
  neutral-text: "#374151"
  neutral-text-muted: "#6b7280"
  neutral-inverse: "#111827"
  semantic-success: "#a6e3a1"
  semantic-warning: "#f9e2af"
  semantic-error: "#f38ba8"
  semantic-info: "#89b4fa"
  semantic-neutral: "#9399b2"
  semantic-progress: "#fab387"
  nav-bg: "#1e1e2e"
  nav-text: "#cdd6f4"
  nav-active: "#89b4fa"
  nav-button: "#313244"
typography:
  body:
    fontFamily: "system-ui, 'Noto Sans Thai', sans-serif"
    fontSize: "0.875rem"
    fontWeight: 400
    lineHeight: 1.5
  mono:
    fontFamily: "ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace"
    fontSize: "0.8rem"
    fontWeight: 400
    lineHeight: 1.35
  heading:
    fontFamily: "system-ui, 'Noto Sans Thai', sans-serif"
    fontSize: "1.5rem"
    fontWeight: 600
    lineHeight: 1.3
  stat:
    fontFamily: "system-ui, 'Noto Sans Thai', sans-serif"
    fontSize: "2rem"
    fontWeight: 700
    lineHeight: 1.2
  label:
    fontFamily: "system-ui, 'Noto Sans Thai', sans-serif"
    fontSize: "0.75rem"
    fontWeight: 600
    lineHeight: 1.4
    letterSpacing: "0.05em"
    textTransform: "uppercase"
  kiosk-title:
    fontFamily: "system-ui, 'Noto Sans Thai', sans-serif"
    fontSize: "2rem"
    fontWeight: 700
    lineHeight: 1.25
  kiosk-body:
    fontFamily: "system-ui, 'Noto Sans Thai', sans-serif"
    fontSize: "1.25rem"
    fontWeight: 400
    lineHeight: 1.45
  kiosk-action:
    fontFamily: "system-ui, 'Noto Sans Thai', sans-serif"
    fontSize: "1.5rem"
    fontWeight: 700
    lineHeight: 1.2
rounded:
  sm: "4px"
  md: "6px"
  lg: "8px"
spacing:
  xs: "0.35rem"
  sm: "0.5rem"
  md: "0.75rem"
  lg: "1rem"
  xl: "1.25rem"
  "2xl": "1.5rem"
  "3xl": "2rem"
components:
  button-primary:
    backgroundColor: "{colors.neutral-deep}"
    textColor: "{colors.neutral-surface}"
    rounded: "{rounded.md}"
    padding: "0.7rem 0.9rem"
  kiosk-action-button:
    backgroundColor: "{colors.neutral-deep}"
    textColor: "{colors.neutral-surface}"
    rounded: "{rounded.lg}"
    padding: "1.25rem 2rem"
    typography: kiosk-action
    minHeight: "64px"
  stat-card:
    backgroundColor: "{colors.neutral-surface}"
    rounded: "{rounded.lg}"
    padding: "{spacing.2xl}"
  status-badge:
    rounded: "{rounded.sm}"
    padding: "2px 8px"
  nav-link:
    textColor: "{colors.nav-text}"
    typography: body
  nav-link-active:
    textColor: "{colors.nav-active}"
    typography: body
---

# Design System: MediSync

Seed document — written pre-implementation, inheriting the proven PrintOps system ("The Lab Notebook"). Re-run `/impeccable document` once real components exist to capture actual tokens.

## 1. Overview

**Creative North Star: "The Lab Notebook, at arm's length."**

MediSync shares PrintOps' visual language — clinical, calm, precise, light-first, flat-by-default — because the same hospital staff will use both. It splits into two surfaces with different distances:

- **Admin app (desk distance):** identical register to PrintOps. Dense-enough tables, quiet colors, one accent blue, system fonts.
- **Kiosk app (arm's length, touch):** the same tokens scaled up. Type starts at 1.25rem, primary actions ≥64px tall, one primary action per screen. A nurse in gloves must never mis-tap.

Explicitly rejected: SaaS gradients, data walls, generic Material Design, dark-by-default, consumer-kiosk playfulness (mascots, confetti, oversized illustration).

**Key Characteristics:**
- One token set, two scales: `body`/`label` for admin, `kiosk-*` for the cabinet screen
- Light-first for fluorescent-lit wards; WCAG 2.1 AA minimum
- Six-color semantic status palette (success/warning/error/info/progress/neutral) shared with PrintOps
- Flat at rest; shadows only on floating surfaces
- Motion limited to state-change fades ≤200ms; `prefers-reduced-motion` respected
- Thai-first text: include `'Noto Sans Thai'` in the stack for consistent Thai rendering

## 2. Colors

Same palette as PrintOps (see `print_ops/DESIGN.md` for full rationale). Rules that carry over verbatim:

**The One Accent Rule.** Action Blue (#1e66f5) is the only non-semantic accent. ≤5% of any screen.

**The Six-Color Status Rule.** Dispense/prescription states map to exactly six semantic colors:
- RECEIVED / READY → Info (#89b4fa)
- DISPENSING / PRINTING → Progress (#fab387)
- DISPENSED / REFILLED → Success (#a6e3a1)
- TIMEOUT / STOCK-LOW → Warning (#f9e2af)
- FAILED / JAMMED / UNAUTHORIZED → Error (#f38ba8)
- CANCELLED / EXPIRED → Neutral (#9399b2)

Status is always color + icon/shape + text. Never a bare colored dot.

## 3. Typography

System-native stack with `'Noto Sans Thai'` inserted for Thai glyph coverage: `system-ui, 'Noto Sans Thai', sans-serif`. Monospace reserved for IDs, HN numbers, slot codes, payload hex.

Admin hierarchy = PrintOps (heading 1.5rem/600, body 0.875rem, label 0.75rem uppercase, stat 2rem/700, mono 0.8rem).

Kiosk hierarchy (arm's-length additions):
- **kiosk-title** (700, 2rem): screen purpose — one per screen ("สแกนบัตรเพื่อเบิกยา")
- **kiosk-body** (400, 1.25rem): instructions, drug names, patient identifiers
- **kiosk-action** (700, 1.5rem): button labels

**The Single Family Rule** holds: one family for everything except code/IDs. No web-font display faces; Noto Sans Thai loads locally/with `font-display: swap` and falls back to system Thai fonts without layout shift.

## 4. Elevation

Flat-by-default, identical vocabulary to PrintOps:
- **Card Rest** `0 1px 3px rgba(0,0,0,0.1)` — stat cards
- **Panel Float** `0 8px 24px rgba(0,0,0,0.08)` — login/dialog panels
- **Popover** `0 10px 24px rgba(0,0,0,0.18)` — tooltips/popovers on dark surface

Nothing else casts a shadow.

## 5. Components

Admin app inherits PrintOps components as-is: buttons, stat cards, status badges, tables, inputs, dark left nav (Deep Navy #1e1e2e, 200px), login panel.

Kiosk-specific:

### Kiosk Action Button
- Deep Navy fill, white text, 8px radius, min-height 64px, padding 1.25rem 2rem, kiosk-action type
- One per screen as the primary action; secondary actions are outlined (1px #d1d5db border, navy text), same height
- Disabled = opacity 0.7; pressed = opacity shift, no color transform

### Kiosk Step Screen
- Page Gray background, single centered White panel (Panel Float shadow), max-width ~720px for cabinet touchscreen
- kiosk-title at top, kiosk-body instruction, primary action pinned at bottom of panel
- Progress through a flow shown as plain step text ("ขั้นตอน 2 จาก 3"), not a decorative stepper

### Dispense Status Card
- Full-width panel showing hardware truth: slot, drug, quantity, live state badge (six-color rule)
- Terminal states (DISPENSED / FAILED) render large: icon + kiosk-title verdict + plain next step
- Never auto-dismiss a FAILED state; require acknowledgment

### Refill Mode
- Same components, Warm Peach (#fab387) header band signals "refill mode active" (color + label + icon)
- Slot list as table with big touch rows (min 56px), stock count in stat type

## 6. Do's and Don'ts

Inherit every Do/Don't from `print_ops/DESIGN.md`, plus:

### Do:
- **Do** keep kiosk touch targets ≥48px (primary ≥64px) and one primary action per screen
- **Do** show hardware-confirmed state, with explicit acknowledgment on failures
- **Do** write kiosk copy in Thai first, short imperative sentences
- **Do** use the Warm Peach band to make refill mode unmistakable from withdraw mode

### Don't:
- **Don't** put more than one flow decision on a kiosk screen
- **Don't** auto-advance past a failed or ambiguous hardware result
- **Don't** shrink kiosk type below 1.25rem body to fit more content — cut content instead
- **Don't** add celebratory animation to successful dispense; a calm success state is the brand
