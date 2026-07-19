import { useCallback, useEffect, useState } from "react";
import { Routes, Route, Navigate, useLocation, useNavigate } from "react-router-dom";
import { create } from "@bufbuild/protobuf";
import { createClient } from "@connectrpc/connect";
import {
  InventoryService,
  ListSlotsRequestSchema,
  type Slot,
} from "@medisync/proto/medisync/inventory/v1/inventory_pb";
import { AuthProvider, useAuth } from "./auth.tsx";
import LoginScreen from "./LoginScreen.tsx";
import WithdrawFlow from "./features/withdraw/WithdrawFlow";
import RefillFlow from "./features/refill/RefillFlow";
import ShelfGrid from "./features/catalog/ShelfGrid";
import SlotDetailModal from "./features/catalog/SlotDetailModal";
import type { SlotCellData } from "./features/catalog/SlotCell";
import { transport } from "./transport.ts";

const inventoryClient = createClient(InventoryService, transport);

function ShelfGridScreen() {
  const { state } = useAuth();
  const [slots, setSlots] = useState<Slot[]>([]);
  const [selectedSlotId, setSelectedSlotId] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const fetchSlots = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const response = await inventoryClient.listSlots(
        create(ListSlotsRequestSchema, { cabinetId: "", lowOnly: false }),
      );
      setSlots(response.slots);
    } catch (caughtError: unknown) {
      const message =
        caughtError instanceof Error ? caughtError.message : "เกิดข้อผิดพลาด";
      setError(`ไม่สามารถโหลดผังช่องยาได้: ${message}`);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void fetchSlots();
  }, [fetchSlots]);

  const selectedProtoSlot = selectedSlotId
    ? slots.find((slot) => slot.id === selectedSlotId) ?? null
    : null;
  const selectedSlot = selectedProtoSlot as unknown as SlotCellData | null;

  // Same drug loaded in other slots — powers the "ยาชนิดเดียวกันในช่องอื่น" carousel.
  const relatedSlots: SlotCellData[] = selectedProtoSlot
    ? (slots.filter(
        (slot) =>
          slot.id !== selectedProtoSlot.id &&
          Boolean(selectedProtoSlot.drugId) &&
          slot.drugId === selectedProtoSlot.drugId,
      ) as unknown as SlotCellData[])
    : [];

  return (
    <div className="kiosk-screen">
      <div className="kiosk-panel kiosk-panel--shelf-grid">
        <div>
          <h1 className="kiosk-panel-title">ผังช่องยาในตู้</h1>
          <p className="kiosk-panel-subtitle">
            {state!.kiosk.displayName} — เลือกชั้นและช่องที่ต้องการตรวจสอบ
          </p>
        </div>

        {error && (
          <div className="kiosk-error" role="alert">
            {error}
            <button
              type="button"
              className="kiosk-btn kiosk-btn-outline shelf-grid-screen__retry"
              onClick={() => void fetchSlots()}
            >
              ลองใหม่
            </button>
          </div>
        )}

        {loading && !error && (
          <div className="shelf-grid-screen__loading" aria-live="polite">
            <div className="spinner" />
            <span>กำลังโหลดผังช่องยา...</span>
          </div>
        )}

        {!loading && !error && (
          <ShelfGrid
            slots={slots}
            selectedSlotId={selectedSlotId}
            onSelect={(slot) => setSelectedSlotId(slot.id)}
          />
        )}
      </div>

      {selectedSlot && (
        <SlotDetailModal
          slot={selectedSlot}
          relatedSlots={relatedSlots}
          onClose={() => setSelectedSlotId(null)}
          onSelectRelated={(slot) => setSelectedSlotId(slot.id)}
        />
      )}
    </div>
  );
}

function KioskShell() {
  const { state, loading } = useAuth();
  const navigate = useNavigate();
  const location = useLocation();
  const isRefill = location.pathname.startsWith("/refill");
  const isWithdraw = location.pathname.startsWith("/withdraw");
  const isCatalog = location.pathname.startsWith("/catalog");

  if (loading) {
    return (
      <div className="kiosk-screen">
        <div className="kiosk-panel" style={{ alignItems: "center" }}>
          <div className="spinner" />
          <p className="kiosk-panel-subtitle">กำลังโหลด...</p>
        </div>
      </div>
    );
  }

  if (!state) {
    return <LoginScreen />;
  }

  return (
    <>
      <header className={`kiosk-header${isWithdraw ? " kiosk-header--withdraw" : ""}`}>
        <div className="flex flex-col">
          <span className="text-white text-lg font-bold">{state.kiosk.displayName}</span>
          <span className="text-sm text-gray-400">{state.kiosk.code}</span>
        </div>
        <div className="flex gap-3 items-center">
          <button
            className={`kiosk-header__mode-btn ${isWithdraw ? "kiosk-header__mode-btn--active" : ""}`}
            onClick={() => navigate("/withdraw")}
          >
            💊 เบิกยา
          </button>
          <button
            className={`kiosk-header__mode-btn ${isRefill ? "kiosk-header__mode-btn--refill-active" : ""}`}
            onClick={() => navigate("/refill")}
          >
            📦 เติมยา
          </button>
          <button
            className={`kiosk-header__mode-btn ${isCatalog ? "kiosk-header__mode-btn--active" : ""}`}
            onClick={() => navigate("/catalog")}
          >
            ▦ ผังช่องยา
          </button>
        </div>
      </header>
      {isRefill && <div className="kiosk-refill-banner">🔄 โหมดเติมยา</div>}
      <Routes>
        <Route index element={<Navigate to="/withdraw" replace />} />
        <Route path="withdraw" element={<WithdrawFlow />} />
        <Route path="refill" element={<RefillFlow />} />
        <Route path="catalog" element={<ShelfGridScreen />} />
      </Routes>
    </>
  );
}

export default function App() {
  return (
    <AuthProvider>
      <KioskShell />
    </AuthProvider>
  );
}
