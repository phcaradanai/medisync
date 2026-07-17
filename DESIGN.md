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
  text-on-success: "#0d6b0d"
  text-on-warning: "#6b5500"
  text-on-error: "#7a001e"
  text-on-info: "#003399"
  text-on-neutral: "#2d313b"
  text-on-progress: "#7a3d00"
  danger-deep: "#c41e3a"
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
  kiosk-stat:
    fontFamily: "system-ui, 'Noto Sans Thai', sans-serif"
    fontSize: "2.5rem"
    fontWeight: 700
    lineHeight: 1.15
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
  button-secondary:
    backgroundColor: "{colors.neutral-subtle}"
    textColor: "{colors.neutral-text}"
    rounded: "{rounded.md}"
    padding: "0.7rem 0.9rem"
  button-danger:
    backgroundColor: "{colors.semantic-error}"
    textColor: "{colors.neutral-deep}"
    rounded: "{rounded.md}"
    padding: "0.7rem 0.9rem"
  button-ghost:
    backgroundColor: "transparent"
    textColor: "{colors.neutral-text-muted}"
    rounded: "{rounded.md}"
    padding: "0.35rem 0.5rem"
  kiosk-action-button:
    backgroundColor: "{colors.neutral-deep}"
    textColor: "{colors.neutral-surface}"
    rounded: "{rounded.lg}"
    padding: "1.25rem 2rem"
    typography: kiosk-action
    height: "64px"
  kiosk-action-button-outline:
    backgroundColor: "transparent"
    textColor: "{colors.neutral-deep}"
    rounded: "{rounded.lg}"
    padding: "1.25rem 2rem"
    typography: kiosk-action
    height: "64px"
  kiosk-action-button-danger:
    backgroundColor: "{colors.danger-deep}"
    textColor: "{colors.neutral-surface}"
    rounded: "{rounded.lg}"
    padding: "1.25rem 2rem"
    typography: kiosk-action
    height: "64px"
  kiosk-input:
    backgroundColor: "{colors.neutral-surface}"
    textColor: "{colors.neutral-deep}"
    rounded: "{rounded.md}"
    padding: "0.75rem 1rem"
    typography: kiosk-body
    height: "56px"
  admin-input:
    backgroundColor: "{colors.neutral-surface}"
    textColor: "{colors.neutral-text}"
    rounded: "{rounded.md}"
    padding: "0.5rem 0.75rem"
    typography: body
  status-badge:
    rounded: "{rounded.sm}"
    padding: "4px 12px"
  admin-badge:
    rounded: "{rounded.sm}"
    padding: "2px 8px"
    typography: label
  stat-card:
    backgroundColor: "{colors.neutral-surface}"
    rounded: "{rounded.lg}"
    padding: "{spacing.2xl}"
  rx-card:
    backgroundColor: "{colors.neutral-surface}"
    rounded: "{rounded.lg}"
    padding: "{spacing.xl}"
    height: "80px"
  nav-link:
    textColor: "{colors.nav-text}"
    typography: body
    padding: "0.5rem 1rem"
  nav-link-active:
    textColor: "{colors.nav-active}"
    typography: body
    padding: "0.5rem 1rem"
---

# Design System: MediSync

Scan-generated from `apps/kiosk/src/index.css` and `apps/admin/src/index.css` (2026-07-17). Both apps implement the same token set; this file is the unified source. The four components marked **Specified** at the end of §5 are designed here ahead of implementation (the DB migration for barcode / shelf / row / expiry already landed).

## 1. Overview

**Creative North Star: "The Lab Notebook, at arm's length."**

MediSync shares PrintOps' visual language — clinical, calm, precise, light-first, flat-by-default — because the same hospital staff use both. One token set, two viewing distances:

- **Admin app (desk distance, English-first):** identical register to PrintOps. Data tables, quiet forms, one accent blue, system fonts, dark left nav (200px). Density is a feature: pharmacists scan 110 slots and hundreds of catalog rows.
- **Kiosk app (arm's length, touch, Thai-first):** the same tokens scaled up. Body type starts at 1.25rem, primary actions ≥64px tall, all touch targets ≥48px, one primary action per screen. A nurse in gloves must never mis-tap.

Explicitly rejected: SaaS gradients, terminal-dense ops walls, generic Material Design, dark-by-default, consumer-kiosk playfulness (mascots, confetti, oversized illustration).

Motion is state-change only: 150ms transitions on admin, 200ms on kiosk (bigger surfaces read slower), fades and border-color shifts, nothing choreographed. Both apps carry a global `prefers-reduced-motion` kill switch. Layout is structural, not fluid: admin is a fixed 200px sidebar + fluid content; kiosk is a single centered panel, max-width 720px, on the Page Gray background.

**Key Characteristics:**
- One token set, two scales: `body`/`heading`/`label` for admin, `kiosk-*` for the cabinet screen
- Light-first for fluorescent-lit wards; WCAG 2.1 AA minimum
- Six-color semantic status palette with paired AA text inks (`text-on-*`)
- Flat at rest; three shadows total, only on floating surfaces
- Motion limited to state-change fades ≤200ms; `prefers-reduced-motion` respected
- Thai-first kiosk text: `'Noto Sans Thai'` in the stack for consistent Thai rendering; admin bilingual where useful
- Monospace reserved for machine identifiers: HN numbers, drug codes, slot codes, barcodes

## 2. Colors

Quiet warm-free neutrals, one action blue, and a six-color pastel status family — each pastel paired with a dark ink computed for AA contrast.

### Primary
- **Action Blue** (`primary`): the only non-semantic accent. Focus rings (3px outline on kiosk, 2px glow on admin), active nav item, selected mode button, hover borders on touch cards. Never decoration.

### Neutral
- **Deep Navy** (`neutral-deep`): headings, primary button fill, kiosk header, admin nav background. The "ink" of the lab notebook.
- **Page Gray** (`neutral-page`): app/body background on both surfaces.
- **White** (`neutral-surface`): panels, cards, tables, inputs.
- **Subtle Gray** (`neutral-subtle`): inset sections (kiosk detail blocks), secondary button fill, table row hover.
- **Border / Border Strong** (`neutral-border`, `neutral-border-strong`): 1px hairlines everywhere; strong variant for outlined kiosk buttons.
- **Body Ink / Muted Ink** (`neutral-text`, `neutral-text-muted`): body copy and secondary copy. Muted ink is for labels and metadata on white/gray surfaces only — never on the semantic pastels.
- **Crimson** (`danger-deep`): kiosk destructive action fill (cancel dispense). Deliberately *not* the pastel error — a destructive button must look heavier than a status badge.

### Semantic (the six-color status family)
Dispense/prescription/stock states map to exactly six pastels, each with a paired ink:

| State examples | Background | Text |
|---|---|---|
| RECEIVED / READY | `semantic-info` | `text-on-info` |
| DISPENSING / PRINTING | `semantic-progress` | `text-on-progress` |
| DISPENSED / REFILLED | `semantic-success` | `text-on-success` |
| TIMEOUT / STOCK-LOW / EXPIRING | `semantic-warning` | `text-on-warning` |
| FAILED / JAMMED / EXPIRED / UNAUTHORIZED | `semantic-error` | `text-on-error` |
| CANCELLED / NO-DATA | `semantic-neutral` | `text-on-neutral` |

`semantic-progress` (Warm Peach) doubles as the **refill-mode signal**: header band, active refill mode button.

### Nav (admin sidebar + kiosk header)
- **Nav Ink** (`nav-text`) on **Deep Navy** (`nav-bg`); **Nav Active** (`nav-active`) marks the current page; **Nav Button** (`nav-button`) is the raised control fill on dark.

### Named Rules
**The One Accent Rule.** Action Blue is the only non-semantic accent, on ≤5% of any screen. If blue is decorating rather than indicating focus/selection/action, remove it.

**The Six-Color Status Rule.** Every domain state maps to one of the six semantic pastels. No seventh status color, ever. Status is always color + icon/shape + text — never a bare colored dot.

**The Paired Ink Rule.** Text on a semantic pastel always uses its `text-on-*` partner. Gray text on a pastel is prohibited — it fails contrast and looks washed out.

### Known drift (captured, not endorsed)
- Admin badges (`badge-active` / `badge-inactive`) use a tonal treatment — `rgba` pastel at 20% + one-off inks (`#2d7a3a`, `#5a5f7a`) — while kiosk badges use solid pastel + `text-on-*`. Two treatments, one system: when touching admin badges next, migrate them to the solid pastel + paired ink convention.
- Refill peach (`#fab387`/`#7a3d00`) is hardcoded in kiosk CSS instead of referencing `--semantic-progress`/`--text-on-progress`. Same values; use the variables in new code.

## 3. Typography

**UI Font:** `system-ui, 'Noto Sans Thai', sans-serif` — everything.
**Mono Font:** `ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace` — machine identifiers only.

**Character:** invisible, native, fast. The system face renders Thai and English at equal quality with zero web-font load; Noto Sans Thai fills Thai glyph coverage where the platform face falls short. No display faces anywhere.

### Hierarchy — admin (desk distance)
- **Heading** (600, 1.5rem, 1.3): page titles. h2 = 1.25rem, h3 = 1rem, same weight.
- **Body** (400, 0.875rem, 1.5): tables, forms, prose.
- **Label** (600, 0.75rem, 0.05em tracking, uppercase): form labels, table headers, nav section labels, badges.
- **Stat** (700, 2rem, 1.2): dashboard numbers.
- **Mono** (400, 0.8rem, 1.35): IDs, HN numbers, drug codes, slot codes.

### Hierarchy — kiosk (arm's length)
- **kiosk-title** (700, 2rem, 1.25): screen purpose, one per screen ("สแกนบัตรเพื่อเบิกยา").
- **kiosk-body** (400, 1.25rem, 1.45): instructions, drug names, patient identifiers. The kiosk floor — nothing user-critical renders smaller.
- **kiosk-action** (700, 1.5rem, 1.2): button labels.
- **kiosk-stat** (700, 2.5rem, 1.15): stock counts, quantities — readable while standing.

### Named Rules
**The Single Family Rule.** One family for everything except code/IDs. No web-font display faces; Thai falls back through the system stack without layout shift.

**The Arm's-Length Floor Rule.** Kiosk body text never drops below 1.25rem to fit more content. Cut content instead.

## 4. Elevation

Flat-by-default. Surfaces sit on Page Gray separated by 1px hairline borders and background steps (White → Subtle Gray), not depth. Exactly three shadows exist, and each means "this floats":

### Shadow Vocabulary
- **Card Rest** (`box-shadow: 0 1px 3px rgba(0,0,0,0.1)`): stat cards, table wrappers, hover lift on kiosk touch cards.
- **Panel Float** (`box-shadow: 0 8px 24px rgba(0,0,0,0.08)`): login panels, kiosk step panel, modals.
- **Popover** (`box-shadow: 0 10px 24px rgba(0,0,0,0.18)`): tooltips/popovers over dark or busy surfaces.

### Named Rules
**The Flat-By-Default Rule.** Nothing else casts a shadow. If a new component seems to need a fourth shadow, it doesn't — pick the closest of the three or use a border.

## 5. Components

Shared vocabulary first, then per-surface components, then the four **Specified** components (designed here, not yet implemented — build them from these tokens, not new primitives).

### Buttons — admin
Quiet, rectangular, text-first. All: radius 6px, 150ms opacity/background transitions, `disabled` = opacity 0.55 + `not-allowed`.
- **Primary:** Deep Navy fill, white text, padding 0.7rem 0.9rem. Hover = opacity 0.9.
- **Secondary:** Subtle Gray fill, body ink, 1px border. Hover = border-gray fill.
- **Danger:** pastel error fill, Deep Navy text. Hover = opacity 0.85.
- **Ghost:** transparent, muted ink, compact padding (0.35rem 0.5rem). Hover = body ink on Subtle Gray.
- **Small** (`btn-sm`): 0.35rem 0.6rem padding, 0.8rem type — inline table actions.

### Buttons — kiosk
Big, obvious, gloved-finger safe. All: min-height 64px, min-width 200px, radius 8px, kiosk-action type, `touch-action: manipulation`, pressed = opacity 0.7, focus = 3px Action Blue outline.
- **Primary:** Deep Navy fill, white text. One per screen.
- **Outline:** transparent, Deep Navy text, 1px Border Strong. Secondary/back actions, same height as primary.
- **Danger:** Crimson (`danger-deep`) fill, white text. Destructive only (cancel mid-flow).
- **Warning:** Warm Peach fill + `text-on-progress` ink. Refill-mode actions.

### Inputs / Fields
- **Admin:** 1px Border stroke, White fill, radius 6px, padding 0.5rem 0.75rem. Focus = Action Blue border + 2px blue glow (`rgba(30,102,245,0.15)`). Labels are uppercase Label type, muted ink.
- **Kiosk:** same anatomy scaled up — min-height 56px, kiosk-body type, padding 0.75rem 1rem. Placeholder = muted ink (large text, ≥3:1 holds).

### Tables (admin)
White wrapper card (Card Rest shadow, radius 8px, `overflow-x: auto`). Uppercase Label headers in muted ink over a 1px border; rows separated by Subtle Gray hairlines; row hover = Subtle Gray fill. IDs and codes in mono. Tables may run dense and wide — that's the register.

### Navigation
- **Admin sidebar:** 200px Deep Navy column. Brand at top (700 weight), uppercase section labels, nav links in Nav Ink. Hover = Nav Button fill + white text. Active = Nav Active text + 3px left inset marker + 8% Nav Active tint. User/role footer pinned at bottom.
- **Kiosk header:** sticky Deep Navy bar, min-height 56px: user identity left, mode-switch buttons + logout right (all ≥48px). Active withdraw mode = Action Blue fill; active refill mode = Warm Peach fill + `text-on-progress` ink.

### Badges
- **Kiosk status badge:** solid semantic pastel + paired ink, radius 4px, padding 4px 12px, 600 weight 0.875rem, icon slot before the text.
- **Admin badge:** radius 4px, padding 2px 8px, uppercase Label type. (Currently tonal-treatment drift — see §2; converge on solid pastel + paired ink.)

### Modal / Overlay (admin)
35% black fixed overlay (z-index 100), centered White modal: radius 8px, Panel Float shadow, padding 1.5rem, max-width 540px, max-height 90vh scroll. Footer actions right-aligned above a Subtle Gray hairline. Modals are for create/edit forms only — prefer inline editing where the table supports it.

### Kiosk Step Screen
Page Gray background, single centered White panel (Panel Float, radius 8px, max-width 720px, padding 2rem, 1.5rem vertical gap). kiosk-title at top, kiosk-body instruction, primary action at bottom. Flow position is plain step text ("ขั้นตอน 2 จาก 3"), never a decorative stepper.

### Prescription / Touch Card (`rx-card`)
Min-height 80px two-column grid row: patient name (700, 1.25rem) + mono HN + drug summary left, status/meta right. 1px Border, radius 8px, White fill. Hover/focus = Action Blue border + Card Rest shadow; focus adds the 3px outline. The touch-list unit for prescriptions and refill slots.

### Dispense Status Card
Hardware truth, centered: 4rem icon, kiosk-title verdict tinted by state ink (`text-on-success` / `text-on-error` / `text-on-progress`), kiosk-body detail. Terminal FAILED states never auto-dismiss — they require an explicit acknowledgment tap.

### Refill Mode
Warm Peach banner (700, 1.25rem, `text-on-progress` ink) makes refill mode unmistakable from withdraw. Slot rows reuse `rx-card` with a stock badge (700, 1.25rem; low stock switches to `text-on-warning`) and an 8px progress bar (Subtle Gray track, Success fill, Warm Peach fill when low, 300ms width ease).

### Card Scan Area
2px dashed Border on Subtle Gray, radius 8px, min-height 160px, centered 3rem icon + kiosk-body muted instruction. The "present a physical thing here" affordance — reused by the barcode dialog below.

---

The four components below are **Specified — not yet implemented.** The `drug.barcode` + `slot.shelf/row/expiry` migration is live; these are the UI contracts for it.

### ShelfGrid (Specified)
The cabinet map: 5 shelves × 22 rows = 110 slots. Serves both surfaces at two scales:
- **Admin (inventory page):** one CSS Grid row per shelf, `grid-template-columns: repeat(22, minmax(40px, 1fr))`, 2px gap, shelf label (Label type, "SHELF 2") in a leading gutter. Whole cabinet visible at once on desktop; the wrapper card scrolls horizontally below ~1100px.
- **Kiosk (refill slot picker):** same structure with SlotCells ≥48px square and 4px gaps; one shelf per screen-width with plain shelf tabs ("ชั้น 1–5") — never shrink cells to fit all 110.
- Slot addressing is always mono (`S2-R14`); shelf/row numbers 1-based, matching the physical cabinet labels. Selecting a cell is the only interaction; everything else lives in the detail panel it opens.

### SlotCell (Specified)
One cell of the ShelfGrid. Radius 4px, 1px Border, content = mono row number plus a fill indicator.
- **States:** empty (Subtle Gray fill, muted ink), assigned (White fill, body ink), low (Warning pastel + `text-on-warning`), expiring (Warning pastel + clock glyph), expired (Error pastel + `text-on-error` + strike glyph), selected (2px Action Blue border + 8% blue tint).
- Per the Six-Color Status Rule, state is never color alone: low/expiring/expired each add a glyph, and the admin hover tooltip (Popover shadow) spells out drug, quantity, expiry date.
- Expired beats expiring beats low when states stack. An expired slot renders its state in both surfaces and blocks dispensing — the kiosk shows it as non-selectable.

### BarcodeScanDialog (Specified)
Kiosk withdraw entry point: scan a drug barcode to jump to matching prescriptions. Native `<dialog>` (escapes stacking contexts; no z-index games), White panel, Panel Float, radius 8px, max-width 720px.
- **Idle:** Card Scan Area affordance with barcode glyph, kiosk-title "สแกนบาร์โค้ดยา", kiosk-body instruction. Hardware scanner input is captured globally while open.
- **Matched:** drug name (kiosk-title) + mono barcode + Info badge, primary 64px confirm button. Confirmation stays a deliberate tap — a scan alone never fires a dispense (Design Principle 3).
- **Not found:** Error pastel message block + `text-on-error` ink, rescan (primary) and manual entry (outline) actions. Manual fallback is a kiosk-input (56px) with the mono barcode echoed as typed.
- Dismiss = outline button + Escape; never dismiss on backdrop tap alone (gloved palms brush screens).

### ExpiryBadge (Specified)
A `status-badge` variant, not a new primitive. Same anatomy (radius 4px, 4px 12px padding, 600 weight, icon + text); date rendered mono inside.
- **Expiring soon** (within the configured warning window; default 30 days): Warning pastel + `text-on-warning` + clock glyph, label "ใกล้หมดอายุ" on kiosk / "EXPIRING" on admin, with the date.
- **Expired:** Error pastel + `text-on-error` + strike glyph, "หมดอายุ" / "EXPIRED".
- **No expiry data:** Neutral pastel + `text-on-neutral`, shown only in admin (kiosk omits the badge entirely when there's nothing to warn about).
- Appears in: admin inventory table rows, SlotCell tooltips, kiosk refill slot rows, and the dispense confirm screen when any line item is expiring. It is an alert, not decoration — a fresh slot gets no badge.

## 6. Do's and Don'ts

### Do:
- **Do** keep kiosk touch targets ≥48px (primary actions ≥64px) and one primary action per screen.
- **Do** show hardware-confirmed state, with explicit acknowledgment on FAILED — never auto-dismiss or auto-advance past a failed or ambiguous hardware result.
- **Do** write kiosk copy in Thai first, short imperative sentences; admin copy in English.
- **Do** use Warm Peach (`semantic-progress`) as the refill-mode signal — banner + mode button + label + icon, never color alone.
- **Do** pair every semantic pastel background with its `text-on-*` ink (The Paired Ink Rule).
- **Do** render every machine identifier — HN, drug code, slot code (`S2-R14`), barcode — in the mono stack.
- **Do** treat expiry as a first-class alert: expired slots block dispensing and render in Error pastel with a glyph on both surfaces.
- **Do** require a deliberate confirm tap after every barcode/QR scan; a scan is input, never authorization to move medication.

### Don't:
- **Don't** use flashy SaaS gradients — no purple-to-blue heroes, no landing-page animation patterns.
- **Don't** build terminal-dense ops walls — nurses and pharmacists are not SREs; no Grafana sprawl.
- **Don't** default to generic Material Design — no default MUI look; purpose-built for medication workflows.
- **Don't** ship dark-mode-by-default — hospital environments are brightly lit; light mode primary.
- **Don't** add consumer-kiosk playfulness — no mascots, oversized illustrations, or celebratory confetti; a calm success state is the brand.
- **Don't** put more than one flow decision on a kiosk screen.
- **Don't** shrink kiosk body type below 1.25rem or touch targets below 48px to fit content — cut content or paginate (ShelfGrid shows one shelf per kiosk screen, never all 110 cells).
- **Don't** invent a seventh status color or a fourth shadow. The vocabulary is closed.
- **Don't** use gray/muted ink on semantic pastel backgrounds — always the paired `text-on-*` ink.
- **Don't** hardcode palette values in component CSS (the `#fab387` refill drift is the cautionary tale) — reference the custom properties.
