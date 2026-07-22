# Product

## Register

product

## Users

- **Ward nurses / pharmacists** — stand at the dispensing cabinet (kiosk) to withdraw medication for patients. Context: mid-shift, time-pressured, gloved hands on a touchscreen, fluorescent lighting. Job: authenticate fast (QR/NFC/login), pick or confirm a prescription, receive the correct drug with a printed sticker, and walk away in under a minute.
- **Refill staff** — pharmacy personnel restocking the cabinet. Context: scheduled restock rounds with a cart of medication. Job: enter refill mode, see which slots are low, load drugs into the right slots, and confirm counts without ambiguity.
- **System administrators** — pharmacy/IT staff using the admin app. Context: desk work, periodic. Job: manage the drug catalog, map drugs to cabinet slots, manage user accounts, and assign ward-scoped permissions.
- **Integration systems** — an external team's service reads prescriptions from the hospital's central DB and publishes them into this system via NATS JetStream. Context: machine-to-machine; needs a stable, versioned contract.

## Product Purpose

MediSync is a hospital medication dispensing platform built around an automated vending cabinet. It receives prescriptions from the hospital (via a NATS JetStream stream fed by an external integration service), orchestrates the physical dispensing flow on the cabinet hardware, prints medication stickers, and gives pharmacy staff a kiosk UI for withdrawal/refill plus an admin app for catalog, users, and ward-based access control.

It composes two existing systems rather than replacing them:
- **vending-3d-ctl-agent** (existing, Node/Express) — low-level serial control of the cabinet (vending board, navigation lights, QR/NFC reader). MediSync's hardware bridge calls its HTTP API.
- **print_ops** (existing, print gateway) — sticker printing. MediSync submits print jobs via `POST /api/v1/print-jobs` with `X-Api-Key`.

Core capabilities:
- Consume prescription events from NATS JetStream and track them through a dispensing state machine (received → ready → dispensing → dispensed/failed), with full audit trail
- Command the cabinet through the vending agent and confirm physical dispense results
- Print medication stickers through print_ops at the right point in the flow
- Kiosk UI for withdraw and refill, optimized for touch and speed
- Admin app for drug catalog, slot mapping, stock levels, users, roles, and ward-scoped permissions
- Event-driven core: every state change is a domain event on JetStream; services stay decoupled

Success means: a nurse gets the right drug with the right sticker in under a minute; stock counts are always trustworthy; every dispense is auditable end-to-end; the whole system runs from one docker compose. Delivery target: working end-to-end system in 10 days.

## Brand Personality

**Clinical, calm, precise** — the same design family as PrintOps ("The Lab Notebook"). The interface is a trustworthy instrument, not a consumer app. Kiosk screens add one more trait: **big and obvious** — large touch targets, few steps, zero ambiguity about what the machine is doing with physical medication.

Emotional goals: confidence (the cabinet did exactly what it said), calm (no alarm-styling for normal states), speed (the UI never makes staff wait or hunt).

## Anti-references

- **No flashy SaaS gradients** — no purple-to-blue heroes, no landing-page animation patterns
- **No terminal-dense ops walls** — nurses and pharmacists are not SREs; no Grafana sprawl
- **No generic Material Design** — no default MUI look; purpose-built for medication workflows
- **No dark-mode-by-default** — hospital environments are brightly lit; light mode primary
- **No consumer-kiosk playfulness** — no oversized illustrations, mascots, or celebratory confetti; dispensing controlled medication is serious

## Design Principles

1. **One glance, one action.** Every kiosk screen has exactly one primary action, readable from arm's length. If a screen needs explanation, it's wrong.
2. **Hardware truth over app state.** Show what the cabinet actually reported (dispensed, jammed, timeout), never an optimistic guess. A dispense is done when the hardware confirms it.
3. **Irreversible actions are deliberate.** Dispensing drugs and committing refills touch physical inventory; confirm intent before firing and always show the outcome plainly.
4. **Audit is a feature, not a log.** Every state transition (who, what, which slot, which prescription) is recorded and visible to admins; trust in the numbers is the product.
5. **Speed is respect.** Sub-second screen transitions, no spinner where cached data suffices, workflows measured in taps.

## Accessibility & Inclusion

- **WCAG 2.1 AA** minimum across kiosk and admin
- **Kiosk touch targets ≥ 48px**, primary actions larger; usable with gloves
- **Color never the sole signal** — status pairs color with icon/shape/text (red/green color-blindness)
- **Prefers-reduced-motion** respected; motion limited to state-change fades
- **Light-first** for fluorescent-lit environments; contrast holds up under fatigue
- **Thai-first UI text** with an i18n layer (Thai/English) — staff-facing copy in Thai, technical/admin surfaces bilingual where useful
