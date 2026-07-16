import { useState, useEffect, useCallback } from "react";
import { createClient } from "@connectrpc/connect";
import { create } from "@bufbuild/protobuf";
import {
  InventoryService,
  ListSlotsRequestSchema,
  RefillRequestSchema,
  type Slot,
} from "@medisync/proto/medisync/inventory/v1/inventory_pb";
import { transport } from "../../transport.ts";
import { useAuth } from "../../auth.tsx";

const inventoryClient = createClient(InventoryService, transport);

type Step = "list" | "refill" | "done";

export default function RefillFlow() {
  const { state } = useAuth();
  const kiosk = state!.kiosk;

  const [step, setStep] = useState<Step>("list");
  const [slots, setSlots] = useState<Slot[]>([]);
  const [selected, setSelected] = useState<Slot | null>(null);
  const [refillQty, setRefillQty] = useState(0);
  const [result, setResult] = useState<Slot | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [showAll, setShowAll] = useState(false);

  // Load slots.
  const fetchSlots = useCallback(async (lowOnly: boolean) => {
    setBusy(true);
    setError(null);
    try {
      const req = create(ListSlotsRequestSchema, {
        cabinetId: "",
        lowOnly,
      });
      const res = await inventoryClient.listSlots(req);
      setSlots(res.slots);
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : "เกิดข้อผิดพลาด";
      setError(`ไม่สามารถโหลดข้อมูลช่องจ่ายยาได้: ${msg}`);
    } finally {
      setBusy(false);
    }
  }, []);

  useEffect(() => {
    fetchSlots(!showAll);
  }, [fetchSlots, showAll]);

  const handleSelect = (slot: Slot) => {
    setSelected(slot);
    setRefillQty(0);
    setError(null);
    setStep("refill");
  };

  const handleBack = () => {
    setSelected(null);
    setResult(null);
    setError(null);
    setStep("list");
  };

  const handleConfirm = async () => {
    if (!selected || refillQty <= 0) {
      setError("กรุณากรอกจำนวนที่ต้องการเติม");
      return;
    }

    const maxAdd = selected.capacity - selected.quantity;
    if (refillQty > maxAdd) {
      setError(`ไม่สามารถเติมเกินความจุได้ (เหลือ ${maxAdd} หน่วย)`);
      return;
    }

    setError(null);
    setBusy(true);

    try {
      const req = create(RefillRequestSchema, {
        slotId: selected.id,
        quantityAdded: refillQty,
        traceId: crypto.randomUUID(),
      });
      const res = await inventoryClient.refill(req);
      if (res.slot) {
        setResult(res.slot);
        setStep("done");
      } else {
        setError("ไม่ได้รับข้อมูลยืนยันจากเซิร์ฟเวอร์");
      }
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : "เกิดข้อผิดพลาด";
      setError(`ไม่สามารถเติมยาได้: ${msg}`);
    } finally {
      setBusy(false);
    }
  };

  const handleDone = () => {
    setSelected(null);
    setResult(null);
    setRefillQty(0);
    setStep("list");
    fetchSlots(!showAll);
  };

  // ── List screen ────────────────────────────────────────────
  if (step === "list") {
    const lowCount = slots.filter((s) => s.quantity <= s.lowThreshold).length;

    return (
      <div className="kiosk-screen">
        <div className="kiosk-panel">
          <h1 className="kiosk-panel-title">เติมยาเข้าตู้</h1>
          <p className="kiosk-panel-subtitle">
            {kiosk.displayName} — เลือกช่องเพื่อเริ่มเติมยา
          </p>

          <div style={{ display: "flex", gap: "var(--space-md)", marginBottom: "var(--space-xl)" }}>
            <button
              type="button"
              className={`kiosk-btn ${!showAll ? "kiosk-btn-warning" : "kiosk-btn-outline"}`}
              style={{ flex: 1, fontSize: "1.125rem" }}
              onClick={() => setShowAll(false)}
            >
              🔴 เหลือน้อย ({lowCount})
            </button>
            <button
              type="button"
              className={`kiosk-btn ${showAll ? "kiosk-btn-warning" : "kiosk-btn-outline"}`}
              style={{ flex: 1, fontSize: "1.125rem" }}
              onClick={() => setShowAll(true)}
            >
              📋 ทั้งหมด ({slots.length})
            </button>
          </div>

          {error && <div className="kiosk-error" role="alert">{error}</div>}

          {busy && (
            <div style={{ display: "flex", justifyContent: "center", padding: "var(--space-2xl)" }}>
              <div className="spinner" />
            </div>
          )}

          {!busy && slots.length === 0 && !error && (
            <div className="kiosk-message">
              {showAll ? "ไม่มีช่องจ่ายยาในระบบ" : "ไม่มีช่องที่สินค้าเหลือน้อย"}
            </div>
          )}

          {!busy && slots.length > 0 && (
            <ul className="rx-list" role="listbox">
              {slots.map((slot) => {
                const isLow = slot.quantity <= slot.lowThreshold;
                const pct = slot.capacity > 0 ? Math.round((slot.quantity / slot.capacity) * 100) : 0;
                return (
                  <li
                    key={slot.id}
                    className="rx-card"
                    role="option"
                    tabIndex={0}
                    onClick={() => handleSelect(slot)}
                    onKeyDown={(e) => { if (e.key === "Enter" || e.key === " ") handleSelect(slot); }}
                  >
                    <div className="rx-card__info" style={{ flex: 1 }}>
                      <div className="rx-card__patient">
                        {slot.drugName || <span style={{ color: "var(--neutral-text-muted)" }}>ช่องว่าง — {slot.code}</span>}
                      </div>
                      {slot.drugCode && (
                        <div className="rx-card__drugs" style={{ display: "flex", gap: "var(--space-md)", alignItems: "center" }}>
                          <span className="mono">{slot.drugCode}</span>
                          <span>ช่อง {slot.code}</span>
                        </div>
                      )}
                    </div>
                    <div style={{ textAlign: "right", minWidth: 80 }}>
                      <div className="refill-stock-badge" data-low={isLow}>
                        {slot.quantity}<span style={{ fontSize: "0.8rem", opacity: 0.6 }}>/{slot.capacity}</span>
                      </div>
                      <div className="refill-progress-bar">
                        <div
                          className="refill-progress-fill"
                          style={{ width: `${pct}%` }}
                          data-low={isLow}
                        />
                      </div>
                    </div>
                  </li>
                );
              })}
            </ul>
          )}
        </div>
      </div>
    );
  }

  // ── Refill entry screen ────────────────────────────────────
  if (step === "refill" && selected) {
    const maxAdd = selected.capacity - selected.quantity;
    const pct = selected.capacity > 0 ? Math.round((selected.quantity / selected.capacity) * 100) : 0;

    return (
      <div className="kiosk-screen">
        <div className="kiosk-panel">
          <div className="step-indicator">ขั้นตอน 1 จาก 2</div>
          <h1 className="kiosk-panel-title">กรอกจำนวนที่เติม</h1>

          {error && <div className="kiosk-error" role="alert">{error}</div>}

          <div className="rx-detail">
            <div className="rx-detail__section">
              <div className="rx-detail__heading">ยา</div>
              <div className="rx-detail__value">{selected.drugName || "(ช่องว่าง)"}</div>
              {selected.drugCode && (
                <div className="rx-card__drugs mono">{selected.drugCode}</div>
              )}
            </div>

            <div className="rx-detail__section">
              <div className="rx-detail__heading">ช่อง</div>
              <div className="rx-detail__value mono">{selected.code}</div>
            </div>

            <div className="rx-detail__section">
              <div className="rx-detail__heading">จำนวนปัจจุบัน</div>
              <div style={{ display: "flex", alignItems: "center", gap: "var(--space-md)", marginTop: "var(--space-sm)" }}>
                <div className="refill-stock-badge" style={{ fontSize: "1.5rem" }}>
                  {selected.quantity}<span style={{ fontSize: "0.9rem", opacity: 0.6 }}>/{selected.capacity}</span>
                </div>
                <div className="refill-progress-bar" style={{ flex: 1 }}>
                  <div className="refill-progress-fill" style={{ width: `${pct}%` }} data-low={selected.quantity <= selected.lowThreshold} />
                </div>
              </div>
            </div>

            <div className="rx-detail__section">
              <div className="rx-detail__heading" style={{ marginBottom: "var(--space-md)" }}>จำนวนที่เติม</div>
              <div style={{ display: "flex", gap: "var(--space-md)", alignItems: "center" }}>
                {[1, 5, 10, 20].map((n) => (
                  <button
                    key={n}
                    type="button"
                    className="kiosk-btn kiosk-btn-outline"
                    style={{ flex: 1, minHeight: 56, fontSize: "1.25rem" }}
                    onClick={() => setRefillQty(refillQty + n)}
                  >
                    +{n}
                  </button>
                ))}
              </div>
              <div style={{ display: "flex", gap: "var(--space-md)", alignItems: "center", marginTop: "var(--space-md)" }}>
                <input
                  type="number"
                  className="kiosk-input"
                  style={{ flex: 1, fontSize: "1.5rem", textAlign: "center" }}
                  min={0}
                  max={maxAdd}
                  value={refillQty}
                  onChange={(e) => setRefillQty(parseInt(e.target.value) || 0)}
                  placeholder="0"
                />
                <button
                  type="button"
                  className="kiosk-btn kiosk-btn-outline"
                  style={{ minHeight: 56, fontSize: "1.25rem", minWidth: 72 }}
                  onClick={() => setRefillQty(Math.max(0, refillQty - 1))}
                >
                  −1
                </button>
              </div>
              {refillQty > 0 && (
                <div className="text-muted" style={{ marginTop: "var(--space-sm)", fontSize: "1rem" }}>
                  จะเป็น {selected.quantity + refillQty} / {selected.capacity} หน่วย
                </div>
              )}
            </div>
          </div>

          <div style={{ display: "flex", gap: "var(--space-md)" }}>
            <button
              type="button"
              className="kiosk-btn kiosk-btn-outline"
              style={{ flex: 1 }}
              onClick={handleBack}
              disabled={busy}
            >
              กลับ
            </button>
            <button
              type="button"
              className="kiosk-btn kiosk-btn-primary"
              style={{ flex: 1 }}
              onClick={handleConfirm}
              disabled={busy || refillQty <= 0}
            >
              {busy ? "กำลังเติม..." : `เติม ${refillQty} หน่วย`}
            </button>
          </div>
        </div>
      </div>
    );
  }

  // ── Done screen ────────────────────────────────────────────
  if (step === "done" && result) {
    return (
      <div className="kiosk-screen">
        <div className="kiosk-panel">
          <div className="dispense-status dispense-status--success">
            <div className="dispense-status__icon">📦</div>
            <div className="dispense-status__verdict">เติมยาสำเร็จ</div>
            <div className="dispense-status__detail" style={{ fontSize: "1.25rem" }}>
              {result.drugName}
            </div>
            <div className="dispense-status__detail" style={{ fontSize: "1.125rem" }}>
              ช่อง {result.code}: {result.quantity} หน่วย
            </div>
            <button
              type="button"
              className="kiosk-btn kiosk-btn-primary"
              onClick={handleDone}
            >
              เติมต่อ
            </button>
          </div>
        </div>
      </div>
    );
  }

  // Fallback.
  return (
    <div className="kiosk-screen">
      <div className="kiosk-panel">
        <h1 className="kiosk-panel-title">เกิดข้อผิดพลาด</h1>
        <button type="button" className="kiosk-btn kiosk-btn-outline" onClick={handleBack}>
          กลับ
        </button>
      </div>
    </div>
  );
}
