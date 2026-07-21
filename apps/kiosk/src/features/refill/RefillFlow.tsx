import { useState, useEffect, useCallback, useMemo, useRef } from "react";
import { useNavigate } from "react-router-dom";
import { createClient } from "@connectrpc/connect";
import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import {
  InventoryService,
  ListSlotsRequestSchema,
  RefillRequestSchema,
  type Slot,
} from "@medisync/proto/medisync/inventory/v1/inventory_pb";
import { transport } from "../../transport.ts";
import { useAuth } from "../../auth.tsx";
import { getSlotPosition } from "../catalog/ShelfGrid";
import { expiryValueToDate, type SlotCellData } from "../catalog/SlotCell";

const inventoryClient = createClient(InventoryService, transport);

const SHELF_COUNT = 5;
const ROW_COUNT = 22;

// ── Status model ─────────────────────────────────────────────
// Real classification derived entirely from ListSlots data.
type SlotStatus = "ok" | "low" | "out" | "empty";

const STATUS_META: Record<
  SlotStatus,
  { label: string; dot: string; token: string }
> = {
  ok: { label: "เพียงพอ", dot: "🟢", token: "ok" },
  low: { label: "ใกล้หมด", dot: "🟠", token: "low" },
  out: { label: "หมด/ไม่พอ", dot: "🔴", token: "out" },
  empty: { label: "ว่าง", dot: "⚪", token: "empty" },
};

function statusOf(slot?: Slot): SlotStatus {
  if (!slot || (!slot.drugName && !slot.drugCode)) return "empty";
  if (slot.quantity <= 0) return "out";
  if (slot.quantity <= slot.lowThreshold) return "low";
  return "ok";
}

// Unit noun. Core has no unit field, so infer from catalog category when
// present; otherwise fall back to a neutral "หน่วย".
function unitLabel(slot?: Slot): string {
  const c = (slot?.category ?? "").toLowerCase();
  if (c.includes("cap") || c.includes("แคปซูล")) return "แคปซูล";
  if (c.includes("tab") || c.includes("เม็ด")) return "เม็ด";
  if (c.includes("syr") || c.includes("น้ำ") || c.includes("ml")) return "ขวด";
  return "หน่วย";
}

const THAI_MONTHS = [
  "มกราคม", "กุมภาพันธ์", "มีนาคม", "เมษายน", "พฤษภาคม", "มิถุนายน",
  "กรกฎาคม", "สิงหาคม", "กันยายน", "ตุลาคม", "พฤศจิกายน", "ธันวาคม",
];

function thaiDate(d: Date): string {
  return `${d.getDate()} ${THAI_MONTHS[d.getMonth()]} ${d.getFullYear() + 543}`;
}

function thaiTime(d: Date): string {
  return `${String(d.getHours()).padStart(2, "0")}:${String(d.getMinutes()).padStart(2, "0")}`;
}

function shortThaiDate(value?: unknown): string {
  const d = expiryValueToDate(value as never);
  if (!d) return "—";
  return `${String(d.getDate()).padStart(2, "0")}/${String(d.getMonth() + 1).padStart(2, "0")}/${d.getFullYear() + 543}`;
}

// A refill performed in this session. There is no ListRefillHistory RPC yet,
// so this panel reflects the terminal's own confirmed refills. Lot number and
// note are captured here but not yet persisted server-side (proto RefillRequest
// carries slot_id, quantity_added, trace_id, expiry_date only).
interface RefillEvent {
  id: string;
  code: string;
  at: Date;
  by: string;
  added: number;
  unit: string;
  lot: string;
  note: string;
  expiry?: string;
}

export default function RefillFlow() {
  const { state } = useAuth();
  const kiosk = state!.kiosk;
  const navigate = useNavigate();

  const [slots, setSlots] = useState<Slot[]>([]);
  const [loading, setLoading] = useState(true);
  const [online, setOnline] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const [activeShelf, setActiveShelf] = useState(1);
  const [statusFilter, setStatusFilter] = useState<SlotStatus | "all">("all");
  const [search, setSearch] = useState("");
  const [selectedId, setSelectedId] = useState<string | null>(null);

  const [qty, setQty] = useState(0);
  const [lot, setLot] = useState("");
  const [expiry, setExpiry] = useState("");
  const [note, setNote] = useState("");
  const [saving, setSaving] = useState(false);
  const [formError, setFormError] = useState<string | null>(null);
  const [toast, setToast] = useState<string | null>(null);

  const [history, setHistory] = useState<RefillEvent[]>([]);
  const [now, setNow] = useState(() => new Date());

  // Live clock.
  useEffect(() => {
    const t = setInterval(() => setNow(new Date()), 1000);
    return () => clearInterval(t);
  }, []);

  // Load every slot once; the grid, filters and summary all derive from this.
  const fetchSlots = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const res = await inventoryClient.listSlots(
        create(ListSlotsRequestSchema, { cabinetId: kiosk.code, lowOnly: false }),
      );
      setSlots(res.slots);
      setOnline(true);
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : "เกิดข้อผิดพลาด";
      setError(`ไม่สามารถโหลดข้อมูลช่องยาได้: ${msg}`);
      setOnline(false);
    } finally {
      setLoading(false);
    }
  }, [kiosk.code]);

  useEffect(() => {
    void fetchSlots();
  }, [fetchSlots]);

  // Index slots by shelf/row using the shared position resolver.
  const byPosition = useMemo(() => {
    const map = new Map<string, Slot>();
    for (const slot of slots) {
      const pos = getSlotPosition(slot as unknown as SlotCellData);
      if (pos) map.set(`${pos.shelf}:${pos.row}`, slot);
    }
    return map;
  }, [slots]);

  const shelfRows = useMemo(
    () =>
      Array.from({ length: ROW_COUNT }, (_, i) => {
        const row = i + 1;
        return { row, slot: byPosition.get(`${activeShelf}:${row}`) };
      }),
    [byPosition, activeShelf],
  );

  const matchesFilters = useCallback(
    (slot?: Slot) => {
      if (statusFilter !== "all" && statusOf(slot) !== statusFilter) return false;
      const q = search.trim().toLowerCase();
      if (!q) return true;
      return (
        (slot?.drugName ?? "").toLowerCase().includes(q) ||
        (slot?.drugCode ?? "").toLowerCase().includes(q) ||
        (slot?.code ?? "").toLowerCase().includes(q)
      );
    },
    [statusFilter, search],
  );

  const summary = useMemo(() => {
    const counts: Record<SlotStatus, number> = { ok: 0, low: 0, out: 0, empty: 0 };
    for (const { slot } of shelfRows) counts[statusOf(slot)]++;
    return counts;
  }, [shelfRows]);

  const selected = useMemo(
    () => slots.find((s) => s.id === selectedId) ?? null,
    [slots, selectedId],
  );
  const selectedPos = selected ? getSlotPosition(selected as unknown as SlotCellData) : null;

  const handleSelect = (slot?: Slot) => {
    if (!slot) return;
    setSelectedId(slot.id);
    setQty(0);
    setLot("");
    setNote("");
    setExpiry("");
    setFormError(null);
  };

  const maxAdd = selected ? Math.max(0, selected.capacity - selected.quantity) : 0;
  const canRefill = selected != null && statusOf(selected) !== "empty";

  const bump = (n: number) => setQty((q) => Math.max(0, Math.min(maxAdd, q + n)));

  const toastTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const flash = (msg: string) => {
    setToast(msg);
    if (toastTimer.current) clearTimeout(toastTimer.current);
    toastTimer.current = setTimeout(() => setToast(null), 3200);
  };

  const handleSave = async () => {
    if (!selected) return;
    if (!canRefill) {
      setFormError("ช่องนี้ยังไม่ได้กำหนดยา ไม่สามารถเติมได้");
      return;
    }
    if (qty <= 0) {
      setFormError("กรุณาระบุจำนวนที่ต้องการเติม");
      return;
    }
    if (qty > maxAdd) {
      setFormError(`เติมเกินความจุไม่ได้ (เหลือพื้นที่ ${maxAdd} ${unitLabel(selected)})`);
      return;
    }

    setFormError(null);
    setSaving(true);
    try {
      const res = await inventoryClient.refill(
        create(RefillRequestSchema, {
          slotId: selected.id,
          quantityAdded: qty,
          traceId: crypto.randomUUID(),
          expiryDate: expiry
            ? timestampFromDate(new Date(`${expiry}T00:00:00`))
            : undefined,
        }),
      );
      if (!res.slot) {
        setFormError("ไม่ได้รับการยืนยันจากเซิร์ฟเวอร์");
        return;
      }
      const updated = res.slot;
      setSlots((prev) => prev.map((s) => (s.id === updated.id ? updated : s)));
      setOnline(true);
      setHistory((prev) => [
        {
          id: crypto.randomUUID(),
          code: updated.code,
          at: new Date(),
          by: kiosk.displayName,
          added: qty,
          unit: unitLabel(updated),
          lot: lot.trim(),
          note: note.trim(),
          expiry: expiry || undefined,
        },
        ...prev,
      ]);
      flash(`เติม ${updated.drugName || updated.code} +${qty} ${unitLabel(updated)} สำเร็จ`);
      setQty(0);
      setLot("");
      setNote("");
      setExpiry("");
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : "เกิดข้อผิดพลาด";
      setFormError(`เติมยาไม่สำเร็จ: ${msg}`);
      setOnline(false);
    } finally {
      setSaving(false);
    }
  };

  const cabinetTitle = kiosk.displayName || "AUTOMATED DISPENSING MACHINE";

  return (
    <div className="refill-dash">
      {/* ── Cabinet status header ─────────────────────────────── */}
      <header className="rd-cabinet">
        <div className="rd-cabinet__side">
          {/* Temperature and door state are hardware telemetry; no core RPC
              exposes them to the kiosk yet, so these show provisioned defaults. */}
          <div className="rd-chip" title="อุณหภูมิภายในตู้ (ยังไม่เชื่อมเซนเซอร์)">
            <span className="rd-chip__icon">🌡️</span>
            <span className="rd-chip__val">24.2°C</span>
            <span className="rd-chip__cap">อุณหภูมิภายในตู้</span>
          </div>
          <div className="rd-chip" title="สถานะประตูตู้ (ยังไม่เชื่อมเซนเซอร์)">
            <span className="rd-chip__icon">🔒</span>
            <span className="rd-chip__val">ปิดประตูแล้ว</span>
            <span className="rd-chip__cap">สถานะตู้</span>
          </div>
        </div>

        <div className="rd-cabinet__brand">
          <div className="rd-brand__logo">ADM</div>
          <div className="rd-brand__sub">{cabinetTitle}</div>
          <div className="rd-brand__code">{kiosk.code}</div>
        </div>

        <div className="rd-cabinet__side rd-cabinet__side--right">
          <div className={`rd-net ${online ? "is-online" : "is-offline"}`}>
            <span className="rd-net__icon">{online ? "📶" : "⚠️"}</span>
            <div>
              <div className="rd-net__label">{online ? "ONLINE" : "OFFLINE"}</div>
              <div className="rd-net__sub">
                {online ? "เชื่อมต่อพร้อมใช้งาน" : "ขาดการเชื่อมต่อ"}
              </div>
            </div>
          </div>
          <div className="rd-clock">
            <div className="rd-clock__time">{thaiTime(now)}</div>
            <div className="rd-clock__date">{thaiDate(now)}</div>
          </div>
        </div>
      </header>

      {/* ── Toolbar ───────────────────────────────────────────── */}
      <div className="rd-toolbar">
        <div>
          <h1 className="rd-toolbar__title">การเติมยา (Refill)</h1>
          <p className="rd-toolbar__sub">จัดการสต็อกยาในแต่ละช่อง</p>
        </div>
        <div className="rd-toolbar__actions">
          <button
            type="button"
            className="rd-btn rd-btn--ghost"
            onClick={() => navigate("/withdraw")}
          >
            💊 เบิกยา
          </button>
          <button
            type="button"
            className="rd-btn rd-btn--ghost"
            onClick={() => navigate("/catalog")}
          >
            ▦ ผังช่องยา
          </button>
          <button
            type="button"
            className="rd-btn rd-btn--ghost"
            onClick={() => void fetchSlots()}
            disabled={loading}
          >
            🔄 รีเฟรชข้อมูล
          </button>
          <button
            type="button"
            className="rd-btn rd-btn--primary"
            onClick={() => void fetchSlots()}
            disabled={loading}
          >
            ⟳ สแกนทั้งหมด
          </button>
        </div>
      </div>

      {error && (
        <div className="rd-error" role="alert">
          {error}
          <button type="button" className="rd-btn rd-btn--ghost" onClick={() => void fetchSlots()}>
            ลองใหม่
          </button>
        </div>
      )}

      {/* ── Shelf tabs ────────────────────────────────────────── */}
      <div className="rd-shelftabs" role="tablist" aria-label="เลือกชั้น">
        <span className="rd-shelftabs__overview">▦ ภาพรวมสต็อก</span>
        {Array.from({ length: SHELF_COUNT }, (_, i) => i + 1).map((shelf) => (
          <button
            key={shelf}
            type="button"
            role="tab"
            aria-selected={activeShelf === shelf}
            className={`rd-shelftab ${activeShelf === shelf ? "is-active" : ""}`}
            onClick={() => {
              setActiveShelf(shelf);
              setSelectedId(null);
            }}
          >
            ชั้น {shelf}
          </button>
        ))}
      </div>

      {/* ── Filter row ────────────────────────────────────────── */}
      <div className="rd-filters">
        <div className="rd-legend">
          {(["ok", "low", "out", "empty"] as SlotStatus[]).map((s) => (
            <button
              key={s}
              type="button"
              className={`rd-legend__item ${statusFilter === s ? "is-active" : ""}`}
              data-token={STATUS_META[s].token}
              onClick={() => setStatusFilter((cur) => (cur === s ? "all" : s))}
            >
              <span className={`rd-dot rd-dot--${STATUS_META[s].token}`} />
              {STATUS_META[s].label}
            </button>
          ))}
        </div>
        <div className="rd-filters__right">
          <div className="rd-search">
            <span aria-hidden="true">🔍</span>
            <input
              type="search"
              placeholder="ค้นหายา / รหัสช่อง"
              value={search}
              onChange={(e) => setSearch(e.target.value)}
            />
          </div>
          {statusFilter !== "all" && (
            <button
              type="button"
              className="rd-btn rd-btn--ghost rd-btn--sm"
              onClick={() => setStatusFilter("all")}
            >
              ล้างตัวกรอง
            </button>
          )}
        </div>
      </div>

      {/* ── Slot grid ─────────────────────────────────────────── */}
      <div className="rd-gridwrap">
        <div className="rd-shelfrail">
          <span>ชั้น</span>
          <strong>{activeShelf}</strong>
          <small>{ROW_COUNT} ช่อง</small>
        </div>
        {loading ? (
          <div className="rd-grid-loading">
            <div className="spinner" />
            <span>กำลังโหลดผังช่องยา...</span>
          </div>
        ) : (
          <div className="rd-grid">
            {shelfRows.map(({ row, slot }) => {
              const status = statusOf(slot);
              const dimmed = !matchesFilters(slot);
              const isSelected = slot?.id === selectedId;
              const pct =
                slot && slot.capacity > 0
                  ? Math.round((slot.quantity / slot.capacity) * 100)
                  : 0;
              return (
                <button
                  key={row}
                  type="button"
                  className={`rd-cell rd-cell--${status} ${isSelected ? "is-selected" : ""} ${dimmed ? "is-dimmed" : ""}`}
                  onClick={() => handleSelect(slot)}
                  disabled={!slot}
                  aria-label={
                    slot
                      ? `${slot.drugName || "ช่องว่าง"} ช่อง ${slot.code} ${STATUS_META[status].label}`
                      : `ช่องว่าง ${row}`
                  }
                >
                  <span className="rd-cell__num">{String(row).padStart(2, "0")}</span>
                  {slot && (slot.drugName || slot.drugCode) ? (
                    <>
                      <span className="rd-cell__name">{slot.drugName || slot.displayName}</span>
                      {slot.drugCode && <span className="rd-cell__dose">{slot.drugCode}</span>}
                      <span className="rd-cell__icon" aria-hidden="true">💊</span>
                      <span className={`rd-cell__qty rd-cell__qty--${status}`}>
                        {status === "out" && "⊘ "}
                        {status === "ok" && "✓ "}
                        {status === "low" && "! "}
                        {slot.quantity} {unitLabel(slot)}
                      </span>
                      <span className="rd-cell__bar" aria-hidden="true">
                        <span
                          className={`rd-cell__bar-fill rd-cell__bar-fill--${status}`}
                          style={{ width: `${pct}%` }}
                        />
                      </span>
                    </>
                  ) : (
                    <span className="rd-cell__empty" aria-hidden="true">
                      <span className="rd-cell__plus">＋</span>
                      ว่าง
                    </span>
                  )}
                  <span className="rd-cell__code">{slot?.code ?? `— ${row} —`}</span>
                </button>
              );
            })}
          </div>
        )}
      </div>

      {/* ── Detail / Refill / History ─────────────────────────── */}
      <div className="rd-panels">
        {/* Detail */}
        <section className="rd-panel">
          <div className="rd-panel__head">
            <span className="rd-panel__head-icon">🔎</span>
            <h2>รายละเอียดช่อง {selected?.code ?? "—"}</h2>
            {selected && (
              <span className={`rd-badge rd-badge--${statusOf(selected)}`}>
                {STATUS_META[statusOf(selected)].label}
              </span>
            )}
          </div>

          {selected ? (
            <div className="rd-detail">
              <div className="rd-detail__hero">
                <div className="rd-detail__img" aria-hidden="true">💊</div>
                <div>
                  <div className="rd-detail__name">
                    {selected.drugName || selected.displayName || "ช่องว่าง"}
                  </div>
                  {selected.drugCode && (
                    <div className="rd-detail__code">{selected.drugCode}</div>
                  )}
                </div>
              </div>

              <div className="rd-detail__chips">
                <div className="rd-info">
                  <span className="rd-info__k">ประเภท</span>
                  <span className="rd-info__v">{selected.category || unitLabel(selected)}</span>
                </div>
                <div className="rd-info">
                  <span className="rd-info__k">ความจุ</span>
                  <span className="rd-info__v">{selected.capacity} {unitLabel(selected)}</span>
                </div>
                <div className="rd-info">
                  <span className="rd-info__k">ตำแหน่ง</span>
                  <span className="rd-info__v">
                    {selectedPos ? `ชั้น ${selectedPos.shelf} / ช่อง ${selectedPos.row}` : selected.code}
                  </span>
                </div>
                <div className="rd-info">
                  <span className="rd-info__k">ขั้นต่ำที่ควรมี</span>
                  <span className="rd-info__v">{selected.lowThreshold} {unitLabel(selected)}</span>
                </div>
                <div className="rd-info">
                  <span className="rd-info__k">ปัจจุบัน</span>
                  <span className="rd-info__v">{selected.quantity} {unitLabel(selected)}</span>
                </div>
                <div className="rd-info">
                  <span className="rd-info__k">วันหมดอายุ</span>
                  <span className="rd-info__v">{shortThaiDate(selected.expiryDate)}</span>
                </div>
              </div>

              {(() => {
                const denom = Math.max(selected.lowThreshold, selected.quantity, 1);
                const pct = Math.min(100, Math.round((selected.quantity / denom) * 100));
                return (
                  <div className="rd-detail__progress">
                    <div className="rd-detail__progress-head">
                      <span>
                        {selected.quantity} / {selected.lowThreshold}
                      </span>
                      <span>{pct}%</span>
                    </div>
                    <div className="rd-progress">
                      <div
                        className={`rd-progress__fill rd-progress__fill--${statusOf(selected)}`}
                        style={{ width: `${pct}%` }}
                      />
                    </div>
                  </div>
                );
              })()}

              <div className="rd-detail__updated">
                อัปเดตล่าสุด: {shortThaiDate(selected.updatedAt)}
              </div>
            </div>
          ) : (
            <div className="rd-empty">เลือกช่องจากผังด้านบนเพื่อดูรายละเอียด</div>
          )}
        </section>

        {/* Refill form */}
        <section className="rd-panel">
          <div className="rd-panel__head">
            <span className="rd-panel__head-icon">📥</span>
            <h2>เติมยาเข้าช่อง</h2>
          </div>

          {formError && <div className="rd-error rd-error--inline">{formError}</div>}

          <div className={`rd-form ${canRefill ? "" : "is-disabled"}`}>
            <label className="rd-field">
              <span className="rd-field__label">จำนวนที่เติม</span>
              <div className="rd-stepper">
                <button type="button" onClick={() => bump(-1)} disabled={!canRefill || qty <= 0}>
                  −
                </button>
                <input
                  type="number"
                  min={0}
                  max={maxAdd}
                  value={qty}
                  disabled={!canRefill}
                  onChange={(e) => {
                    const v = parseInt(e.target.value, 10);
                    setQty(Number.isNaN(v) ? 0 : Math.max(0, Math.min(maxAdd, v)));
                  }}
                />
                <button type="button" onClick={() => bump(1)} disabled={!canRefill || qty >= maxAdd}>
                  ＋
                </button>
                <span className="rd-stepper__unit">{unitLabel(selected ?? undefined)}</span>
              </div>
              <div className="rd-quick">
                {[10, 20, 50].map((n) => (
                  <button
                    key={n}
                    type="button"
                    className="rd-quick__btn"
                    disabled={!canRefill || qty + n > maxAdd}
                    onClick={() => bump(n)}
                  >
                    +{n}
                  </button>
                ))}
              </div>
            </label>

            <div className="rd-total">
              <span>รวมเป็น</span>
              <strong>
                {(selected?.quantity ?? 0) + qty} {unitLabel(selected ?? undefined)}
              </strong>
            </div>

            <label className="rd-field">
              <span className="rd-field__label">ล็อตที่เติม (Lot No.)</span>
              <input
                type="text"
                className="rd-input"
                placeholder="LOT20240724-01"
                value={lot}
                disabled={!canRefill}
                onChange={(e) => setLot(e.target.value)}
              />
            </label>

            <label className="rd-field">
              <span className="rd-field__label">วันหมดอายุ</span>
              <input
                type="date"
                className="rd-input"
                value={expiry}
                disabled={!canRefill}
                onChange={(e) => setExpiry(e.target.value)}
              />
            </label>

            <label className="rd-field">
              <span className="rd-field__label">หมายเหตุ (ถ้ามี)</span>
              <input
                type="text"
                className="rd-input"
                placeholder="เช่น สภาพยา, ผู้เติม"
                value={note}
                disabled={!canRefill}
                onChange={(e) => setNote(e.target.value)}
              />
            </label>

            <button
              type="button"
              className="rd-save"
              onClick={() => void handleSave()}
              disabled={!canRefill || saving || qty <= 0}
            >
              {saving ? "กำลังบันทึก..." : "✔ บันทึกการเติม"}
            </button>
            {!canRefill && selected && (
              <p className="rd-form__hint">ช่องนี้ยังไม่ได้กำหนดยา</p>
            )}
          </div>
        </section>

        {/* History */}
        <section className="rd-panel">
          <div className="rd-panel__head">
            <span className="rd-panel__head-icon">🕑</span>
            <h2>ประวัติการเติม</h2>
            <span className="rd-panel__head-note">เซสชันนี้</span>
          </div>

          {history.length === 0 ? (
            <div className="rd-empty">ยังไม่มีการเติมในเซสชันนี้</div>
          ) : (
            <ul className="rd-history">
              {history.map((h) => (
                <li key={h.id} className="rd-history__item">
                  <div className="rd-history__top">
                    <span className="rd-history__date">
                      {shortThaiDate(h.at)} {thaiTime(h.at)}
                    </span>
                    <span className="rd-history__added">+{h.added} {h.unit}</span>
                  </div>
                  <div className="rd-history__meta">
                    <span>ช่อง {h.code}</span>
                    <span>โดย: {h.by}</span>
                  </div>
                  {(h.lot || h.expiry) && (
                    <div className="rd-history__lot">
                      {h.lot && <span>{h.lot}</span>}
                      {h.expiry && <span>EXP: {shortThaiDate(`${h.expiry}T00:00:00`)}</span>}
                    </div>
                  )}
                  {h.note && <div className="rd-history__note">📝 {h.note}</div>}
                </li>
              ))}
            </ul>
          )}
        </section>
      </div>

      {/* ── Shelf summary ─────────────────────────────────────── */}
      <div className="rd-summary">
        <h3 className="rd-summary__title">สต็อกยาในชั้น {activeShelf} (สรุป)</h3>
        <div className="rd-summary__grid">
          {(["ok", "low", "out", "empty"] as SlotStatus[]).map((s) => {
            const n = summary[s];
            const pct = Math.round((n / ROW_COUNT) * 100);
            return (
              <div key={s} className="rd-stat" data-token={STATUS_META[s].token}>
                <div className="rd-stat__head">
                  <span className={`rd-dot rd-dot--${STATUS_META[s].token}`} />
                  {STATUS_META[s].label}
                </div>
                <div className="rd-stat__row">
                  <strong>{n}</strong>
                  <span>ช่อง</span>
                  <em>{pct}%</em>
                </div>
                <div className="rd-progress rd-progress--sm">
                  <div
                    className={`rd-progress__fill rd-progress__fill--${s}`}
                    style={{ width: `${pct}%` }}
                  />
                </div>
              </div>
            );
          })}
          <div className="rd-stat" data-token="total">
            <div className="rd-stat__head">รวมทั้งหมด</div>
            <div className="rd-stat__row">
              <strong>{ROW_COUNT}</strong>
              <span>ช่อง</span>
              <em>100%</em>
            </div>
            <div className="rd-progress rd-progress--sm">
              <div className="rd-progress__fill rd-progress__fill--total" style={{ width: "100%" }} />
            </div>
          </div>
        </div>
      </div>

      {/* ── Status footer ─────────────────────────────────────── */}
      <footer className="rd-statusbar">
        <span className="rd-statusbar__left">🛡️ ระบบพร้อมทำงาน</span>
        <span className="rd-statusbar__mid">Hardware พร้อม · APP v1.0.0 · {kiosk.code}</span>
        <span className={`rd-statusbar__right ${online ? "is-online" : "is-offline"}`}>
          ● {online ? "พร้อมสนับสนุนรายการถัดไป" : "กำลังเชื่อมต่อใหม่"}
        </span>
      </footer>

      {toast && <div className="rd-toast" role="status">{toast}</div>}
    </div>
  );
}
