// SlotDetailPopup — detailed drug info popup when clicking a slot in ShelfGrid.
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
  const [loading, setLoading] = useState(true);

  // Load same drug in other slots
  useEffect(() => {
    if (!slot.drugCode) { setLoading(false); return; }
    const req = create(ListSlotsRequestSchema, { pageSize: 100 } as any);
    inventoryClient.listSlots(req).then((res) => {
      const others = (res.slots || []).filter(
        (s) => s.drugCode === slot.drugCode && s.code !== slot.code
      );
      setSameDrugSlots(others);
    }).catch(() => {}).finally(() => setLoading(false));
  }, [slot.drugCode, slot.code]);

  const expirySec = (slot.expiryDate as any)?.seconds;
  const hasExpiry = !!expirySec;
  const daysLeft = hasExpiry
    ? Math.ceil((Number(expirySec) * 1000 - Date.now()) / 86400000)
    : null;

  const stockPct = slot.capacity > 0 ? Math.round((slot.quantity / slot.capacity) * 100) : 0;

  return (
    <div className="popup-overlay" onClick={onClose}>
      <div className="popup" onClick={(e) => e.stopPropagation()}>
        {/* Header */}
        <div className="popup-header">
          <div>
            <h2>🔬 รายละเอียดช่องยา</h2>
            <span className="popup-header__addr">{slot.code} · ตู้ {slot.cabinetId?.slice(0, 8)}</span>
          </div>
          <button className="popup-close" onClick={onClose}>✕</button>
        </div>

        {/* Body */}
        <div className="popup-body">
          {/* Drug Image */}
          <div className="drug-image-area">
            <div className="drug-image">
              {slot.imageUrl ? <img src={slot.imageUrl} alt={slot.drugName} /> : "💊"}
            </div>
            <div className="drug-stock-bar">
              <div className="drug-stock-num">{slot.quantity}</div>
              <div className="drug-stock-label">{slot.unit || "หน่วย"} / {slot.capacity}</div>
              <div className="stock-bar-track">
                <div className="stock-bar-fill" style={{ width: `${stockPct}%`, background: stockPct < 20 ? "#dc3545" : "#28a745" }} />
              </div>
            </div>
          </div>

          {/* Drug Info */}
          <div className="drug-info">
            <div className="drug-name">{slot.drugName || "ช่องว่าง"}</div>
            <div className="drug-type-row">
              <span className="tag tag-code">{slot.drugCode}</span>
              {slot.drugType && <span className="tag tag-form">💊 {slot.drugType}</span>}
              {slot.lotNumber && <span className="tag tag-manufacturer">🏷️ Lot: {slot.lotNumber}</span>}
            </div>

            {/* LASA Warning */}
            {slot.lasaGroup && (
              <div className="alert-box alert-lasa">
                ⚠️ <b>LASA:</b> {slot.lasaGroup} — ตรวจสอบชื่อยาก่อนจ่าย
              </div>
            )}

            {/* High Alert */}
            {slot.highAlert && (
              <div className="alert-box alert-danger">
                🔴 <b>HIGH ALERT:</b> ยาที่มีความเสี่ยงสูง — ต้องตรวจสอบซ้ำก่อนจ่าย
              </div>
            )}
          </div>
        </div>

        {/* Detail Grid */}
        <div className="popup-detail-grid">
          <div className="detail-item">
            <span className="detail-label">📅 วันหมดอายุ</span>
            <span className={`detail-value ${daysLeft !== null && daysLeft < 30 ? "expiry-warning" : "expiry-safe"}`}>
              {hasExpiry
                ? `${new Date(Number(expirySec) * 1000).toLocaleDateString("th-TH")} (${daysLeft! > 0 ? `อีก ${daysLeft} วัน` : "หมดอายุแล้ว"})`
                : "—"}
            </span>
          </div>
          <div className="detail-item">
            <span className="detail-label">🏷️ Lot Number</span>
            <span className="detail-value">{slot.lotNumber || "—"}</span>
          </div>
          <div className="detail-item">
            <span className="detail-label">📍 ตำแหน่ง</span>
            <span className="detail-value">ชั้น {slot.shelf || 1} · แถว {slot.rowNum || 1}</span>
          </div>
          <div className="detail-item">
            <span className="detail-label">📦 คงเหลือ</span>
            <span className="detail-value">{slot.quantity} / {slot.capacity} ({stockPct}%)</span>
          </div>
          {(slot as any).widthCm > 0 && (
            <div className="detail-item">
              <span className="detail-label">📐 ขนาดช่อง (cm)</span>
              <span className="detail-value">{slot.widthCm} × {slot.depthCm} × {slot.heightCm}</span>
            </div>
          )}
          {slot.drugType && (
            <div className="detail-item">
              <span className="detail-label">💊 ประเภทยา</span>
              <span className="detail-value">{slot.drugType}</span>
            </div>
          )}
        </div>

        {/* Carousel: Same drug in other slots */}
        {sameDrugSlots.length > 0 && (
          <div className="popup-carousel-section">
            <div className="carousel-title">📍 ตำแหน่งยาตัวเดียวกันในตู้นี้ ({sameDrugSlots.length})</div>
            <div className="carousel">
              {!loading && sameDrugSlots.map((s) => (
                <div key={s.id} className="carousel-card">
                  <div className="carousel-card-addr">{s.code}</div>
                  <div className="carousel-card-qty">{s.quantity}</div>
                  <div className="carousel-card-label">{s.unit || "หน่วย"}</div>
                </div>
              ))}
            </div>
          </div>
        )}

        {/* Footer */}
        <div className="popup-footer">
          <button className="btn btn-outline" onClick={onClose}>ปิด</button>
          {onRefill && <button className="btn btn-outline" onClick={() => onRefill(slot)}>📦 เติมยา</button>}
          {onDispense && slot.quantity > 0 && (
            <button className="btn btn-primary" onClick={() => onDispense(slot)}>💊 เบิกยาจากช่องนี้</button>
          )}
        </div>
      </div>
    </div>
  );
}
