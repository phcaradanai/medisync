// SlotDetailPopup — modern drug detail popup matching pharmacy kiosk design.
// Features: hero card, status banner, inline alerts, mini detail cards, carousel.
import { useEffect, useState } from "react";
import { createClient } from "@connectrpc/connect";
import { create } from "@bufbuild/protobuf";
import { InventoryService, ListSlotsRequestSchema } from "@medisync/proto/medisync/inventory/v1/inventory_pb";
import { transport } from "../../transport.ts";
import type { SlotCellData } from "./SlotCell";

const inventoryClient = createClient(InventoryService, transport);

interface Props {
  slot: SlotCellData;
  onClose: () => void;
  onDispense?: (slot: SlotCellData) => void;
  onRefill?: (slot: SlotCellData) => void;
}

export default function SlotDetailPopup({ slot, onClose, onDispense, onRefill }: Props) {
  const [sameDrugSlots, setSameDrugSlots] = useState<any[]>([]);
  const [_loading, setLoading] = useState(true);

  useEffect(() => {
    if (!slot.drugCode) { setLoading(false); return; }
    create(ListSlotsRequestSchema, { pageSize: 100 } as any);
    inventoryClient.listSlots(create(ListSlotsRequestSchema, { pageSize: 100 } as any))
      .then((res) => setSameDrugSlots((res.slots || []).filter((s: any) => s.drugCode === slot.drugCode && s.code !== slot.code)))
      .catch(() => {}).finally(() => setLoading(false));
  }, [slot.drugCode, slot.code]);

  const expirySec = (slot.expiryDate as any)?.seconds;
  const hasExpiry = !!expirySec;
  const daysLeft: number | null = hasExpiry ? Math.ceil((Number(expirySec) * 1000 - Date.now()) / 86400000) : null;
  const stockPct = slot.capacity > 0 ? Math.round((slot.quantity / slot.capacity) * 100) : 0;
  const isLow = slot.quantity <= slot.lowThreshold && slot.quantity > 0;
  const isExpired = daysLeft !== null && daysLeft <= 0;
  const isExpiring = daysLeft !== null && daysLeft > 0 && daysLeft <= 30;

  return (
    <div className="sdp-overlay" onClick={onClose}>
      <div className="sdp" onClick={(e) => e.stopPropagation()}>
        
        {/* ── Hero Card ── */}
        <div className="sdp-hero">
          <button className="sdp-close" onClick={onClose}>✕</button>
          <div className="sdp-hero__img">
            {slot.imageUrl ? <img src={slot.imageUrl} alt={slot.drugName} /> : "💊"}
          </div>
          <div className="sdp-hero__info">
            <div className="sdp-hero__name">{slot.drugName || "ช่องว่าง"}</div>
            <div className="sdp-hero__code">{slot.drugCode || "—"}</div>
            <div className="sdp-hero__tags">
              {slot.drugType && <span className="sdp-tag sdp-tag--type">{slot.drugType}</span>}
              {slot.lotNumber && <span className="sdp-tag sdp-tag--lot">Lot: {slot.lotNumber}</span>}
            </div>
          </div>
          <div className="sdp-hero__stock">
            <div className="sdp-hero__qty">{slot.quantity}<span>/{slot.capacity}</span></div>
            <div className="sdp-hero__stockbar"><div className="sdp-hero__stockfill" style={{width: `${stockPct}%`, background: stockPct < 20 ? "#dc3545" : stockPct < 50 ? "#ffc107" : "#28a745"}} /></div>
          </div>
        </div>

        {/* ── Status Banner ── */}
        {isExpired && <div className="sdp-banner sdp-banner--expired">⛔ ยาหมดอายุแล้ว — ห้ามจ่าย</div>}
        {isExpiring && !isExpired && <div className="sdp-banner sdp-banner--expiring">⚠️ ยาใกล้หมดอายุ — อีก {daysLeft} วัน</div>}
        {isLow && !isExpired && <div className="sdp-banner sdp-banner--low">📉 สต็อกเหลือน้อย ({slot.quantity} / {slot.capacity})</div>}
        {!isLow && !isExpired && !isExpiring && slot.quantity > 0 && <div className="sdp-banner sdp-banner--ok">✅ สต็อกปกติ พร้อมจ่าย</div>}

        {/* ── Alerts ── */}
        {slot.lasaGroup && (
          <div className="sdp-alert sdp-alert--lasa">
            <span className="sdp-alert__icon">⚠️</span>
            <div><b>LASA Warning</b><br/>{slot.lasaGroup} — ตรวจสอบชื่อยาก่อนจ่าย</div>
          </div>
        )}
        {slot.highAlert && (
          <div className="sdp-alert sdp-alert--danger">
            <span className="sdp-alert__icon">🔴</span>
            <div><b>HIGH ALERT</b><br/>ยาที่มีความเสี่ยงสูง ต้องตรวจสอบซ้ำก่อนจ่าย</div>
          </div>
        )}

        {/* ── Detail Cards ── */}
        <div className="sdp-details">
          <div className="sdp-detail">
            <div className="sdp-detail__icon">📅</div>
            <div className="sdp-detail__label">วันหมดอายุ</div>
            <div className={`sdp-detail__val ${isExpiring ? "text-red" : isExpired ? "text-red" : ""}`}>
              {hasExpiry ? new Date(Number(expirySec) * 1000).toLocaleDateString("th-TH") : "—"}
            </div>
          </div>
          <div className="sdp-detail">
            <div className="sdp-detail__icon">🏷️</div>
            <div className="sdp-detail__label">Lot</div>
            <div className="sdp-detail__val mono">{slot.lotNumber || "—"}</div>
          </div>
          <div className="sdp-detail">
            <div className="sdp-detail__icon">📍</div>
            <div className="sdp-detail__label">ตำแหน่ง</div>
            <div className="sdp-detail__val">ชั้น {slot.shelf || 1} · แถว {slot.rowNum || 1}</div>
          </div>
          <div className="sdp-detail">
            <div className="sdp-detail__icon">📐</div>
            <div className="sdp-detail__label">ขนาด (cm)</div>
            <div className="sdp-detail__val">{(slot as any).widthCm > 0 ? `${slot.widthCm}×${slot.depthCm}×${slot.heightCm}` : "—"}</div>
          </div>
        </div>

        {/* ── Carousel ── */}
        {sameDrugSlots.length > 0 && (
          <div className="sdp-carousel-sec">
            <div className="sdp-carousel-sec__title">ยาตัวเดียวกันในตู้อื่น ({sameDrugSlots.length})</div>
            <div className="sdp-carousel">
              {sameDrugSlots.map((s: any) => (
                <div key={s.id} className="sdp-carousel-card">
                  <div className="sdp-carousel-card__code">{s.code}</div>
                  <div className="sdp-carousel-card__qty">{s.quantity}</div>
                  <div className="sdp-carousel-card__unit">{s.unit || "หน่วย"}</div>
                </div>
              ))}
            </div>
          </div>
        )}

        {/* ── Footer ── */}
        <div className="sdp-footer">
          <button className="sdp-btn sdp-btn--ghost" onClick={onClose}>ปิด</button>
          {onRefill && <button className="sdp-btn sdp-btn--outline" onClick={() => onRefill(slot)}>📦 เติมยา</button>}
          {onDispense && slot.quantity > 0 && !isExpired && (
            <button className="sdp-btn sdp-btn--primary" onClick={() => onDispense(slot)}>💊 เบิกยา</button>
          )}
        </div>
      </div>
    </div>
  );
}
