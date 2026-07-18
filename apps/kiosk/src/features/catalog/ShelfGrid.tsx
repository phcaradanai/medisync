import {
  useMemo,
  useRef,
  useState,
  type KeyboardEvent as ReactKeyboardEvent,
} from "react";
import SlotCell, {
  ExpiryBadge,
  getSlotCellState,
  type SlotCellData,
} from "./SlotCell";

export interface ShelfGridProps {
  slots: readonly SlotCellData[];
  selectedSlotId?: string | null;
  initialShelf?: number;
  expiryWarningDays?: number;
  onSelect?: (slot: SlotCellData) => void;
  variant?: "detail" | "overview";
  requestedSlotIds?: readonly string[];
}

interface SlotPosition {
  shelf: number;
  row: number;
}

const SHELF_COUNT = 5;
const ROW_COUNT = 22;

function isValidPosition(shelf: number, row: number): boolean {
  return (
    Number.isInteger(shelf) &&
    Number.isInteger(row) &&
    shelf >= 1 &&
    shelf <= SHELF_COUNT &&
    row >= 1 &&
    row <= ROW_COUNT
  );
}

export function getSlotPosition(slot: SlotCellData): SlotPosition | null {
  const shelf = Number(slot.shelf);
  const row = Number(slot.rowNum);
  if (isValidPosition(shelf, row)) return { shelf, row };

  const physicalAddress = /^S([1-5])-R(\d{1,2})$/i.exec(slot.code.trim());
  if (physicalAddress) {
    const parsedShelf = Number(physicalAddress[1]);
    const parsedRow = Number(physicalAddress[2]);
    if (isValidPosition(parsedShelf, parsedRow)) {
      return { shelf: parsedShelf, row: parsedRow };
    }
  }

  const legacyAddress = /^([A-E])(\d{1,2})$/i.exec(slot.code.trim());
  if (legacyAddress) {
    const parsedShelf = legacyAddress[1].toUpperCase().charCodeAt(0) - 64;
    const parsedRow = Number(legacyAddress[2]);
    if (isValidPosition(parsedShelf, parsedRow)) {
      return { shelf: parsedShelf, row: parsedRow };
    }
  }

  const sequentialAddress = /^S(\d{1,3})$/i.exec(slot.code.trim());
  if (sequentialAddress) {
    const position = Number(sequentialAddress[1]);
    if (position >= 1 && position <= SHELF_COUNT * ROW_COUNT) {
      return {
        shelf: Math.ceil(position / ROW_COUNT),
        row: ((position - 1) % ROW_COUNT) + 1,
      };
    }
  }

  return null;
}

function clampShelf(shelf: number): number {
  return Math.max(1, Math.min(SHELF_COUNT, Math.round(shelf)));
}

export default function ShelfGrid({
  slots,
  selectedSlotId,
  initialShelf = 1,
  expiryWarningDays = 30,
  onSelect,
  variant = "detail",
  requestedSlotIds = [],
}: ShelfGridProps) {
  const [activeShelf, setActiveShelf] = useState(clampShelf(initialShelf));
  const [cabinetView, setCabinetView] = useState<"overview" | "shelf">("overview");
  const [focusedSlotId, setFocusedSlotId] = useState<string | null>(null);
  const [internalSelectedId, setInternalSelectedId] = useState<string | null>(
    null,
  );
  const tabRefs = useRef<Array<HTMLButtonElement | null>>([]);
  const resolvedSelectedId =
    selectedSlotId === undefined ? internalSelectedId : selectedSlotId;

  const slotsByPosition = useMemo(() => {
    const indexed = new Map<string, SlotCellData>();
    for (const slot of slots) {
      const position = getSlotPosition(slot);
      if (!position) continue;
      indexed.set(`${position.shelf}:${position.row}`, slot);
    }
    return indexed;
  }, [slots]);

  const selectedSlot = useMemo(
    () => slots.find((slot) => slot.id === resolvedSelectedId) ?? null,
    [resolvedSelectedId, slots],
  );
  const selectedPosition = selectedSlot ? getSlotPosition(selectedSlot) : null;
  const showSelectedDetail = selectedPosition?.shelf === activeShelf;

  const handleSelect = (slot: SlotCellData) => {
    if (variant === "overview") {
      setFocusedSlotId(slot.id);
      return;
    }
    setInternalSelectedId(slot.id);
    onSelect?.(slot);
  };

  const handleTabKeyDown = (
    event: ReactKeyboardEvent<HTMLButtonElement>,
    shelf: number,
  ) => {
    let nextShelf = shelf;
    if (event.key === "ArrowRight") nextShelf = (shelf % SHELF_COUNT) + 1;
    if (event.key === "ArrowLeft") {
      nextShelf = shelf === 1 ? SHELF_COUNT : shelf - 1;
    }
    if (event.key === "Home") nextShelf = 1;
    if (event.key === "End") nextShelf = SHELF_COUNT;
    if (nextShelf === shelf) return;

    event.preventDefault();
    setActiveShelf(nextShelf);
    tabRefs.current[nextShelf - 1]?.focus();
  };

  const renderCells = (shelf: number, firstRow = 1, lastRow = ROW_COUNT) => (
    <div
      className="shelf-grid__cells"
      role="grid"
      aria-label={`ช่องยาชั้น ${shelf}`}
      aria-colcount={ROW_COUNT}
      aria-rowcount={1}
    >
      {Array.from(
        { length: lastRow - firstRow + 1 },
        (_, index) => index + firstRow,
      ).map((row) => {
        const slot = slotsByPosition.get(`${shelf}:${row}`);
        return (
          <div
            className="shelf-grid__cell-wrapper"
            role="gridcell"
            aria-colindex={row}
            key={`${shelf}:${row}`}
          >
            <SlotCell
              slot={slot}
              shelfNumber={shelf}
              rowNumber={row}
              selected={slot?.id === resolvedSelectedId}
              expiryWarningDays={expiryWarningDays}
              onSelect={handleSelect}
              readOnly={false}
            />
          </div>
        );
      })}
    </div>
  );

  if (variant === "overview") {
    const openShelf = (shelf: number, slot?: SlotCellData) => {
      setActiveShelf(shelf);
      setFocusedSlotId(slot?.id ?? null);
      setCabinetView("shelf");
    };

    const compactState = (slot?: SlotCellData) => {
      if (!slot) return "empty";
      if (requestedSlotIds.includes(slot.id)) return "requested";
      const state = getSlotCellState(slot, expiryWarningDays);
      if (state === "low" || state === "expiring") return "warning";
      if (state === "expired") return "error";
      return "occupied";
    };

    return (
      <section
        className={`cabinet-browser cabinet-browser--${cabinetView}`}
        aria-label="ผังช่องยาในตู้"
      >
        <nav className="cabinet-browser__nav" aria-label="มุมมองตู้และชั้นยา">
          <button type="button" className={cabinetView === "overview" ? "is-active" : ""} onClick={() => setCabinetView("overview")}>▦ ภาพรวมตู้</button>
          {Array.from({ length: SHELF_COUNT }, (_, index) => index + 1).map((shelf) => (
            <button
              type="button"
              key={shelf}
              className={cabinetView === "shelf" && shelf === activeShelf ? "is-active" : ""}
              onClick={() => openShelf(shelf)}
            >
              ชั้น {shelf}
            </button>
          ))}
        </nav>

        {cabinetView === "overview" ? (
          <div className="cabinet-overview" aria-label="ภาพรวมตู้ยาครบ 5 ชั้น 110 ช่อง">
            {Array.from({ length: SHELF_COUNT }, (_, index) => index + 1).map((shelf) => (
              <div className="cabinet-overview__row" key={shelf}>
                <button type="button" className="cabinet-overview__shelf" onClick={() => openShelf(shelf)} aria-label={`เปิดรายละเอียดชั้น ${shelf}`}><span>ชั้น</span><strong>{shelf}</strong><small>{(shelf - 1) * 22 + 1}–{shelf * 22}</small></button>
                <div className="cabinet-overview__slots" role="grid" aria-label={`ช่องยาชั้น ${shelf}`}>
                  {Array.from({ length: ROW_COUNT }, (_, index) => index + 1).map((row) => {
                    const slot = slotsByPosition.get(`${shelf}:${row}`);
                    const physicalNumber = (shelf - 1) * ROW_COUNT + row;
                    const state = compactState(slot);
                    return (
                      <button
                        type="button"
                        key={`${shelf}:${row}`}
                        className={`cabinet-overview__slot is-${state}`}
                        onClick={() => openShelf(shelf, slot)}
                        aria-label={`ชั้น ${shelf} ช่อง ${physicalNumber} ${slot?.drugName || "ว่าง"}`}
                        title={slot ? `${slot.drugName || slot.displayName} · ${slot.code}` : `ชั้น ${shelf} ช่อง ${physicalNumber} · ว่าง`}
                      >
                        <span className="cabinet-overview__number">{physicalNumber}</span>
                        <span className="cabinet-overview__visual" aria-hidden="true">{slot ? "▰" : ""}</span>
                        {state === "requested" && <span className="cabinet-overview__status" aria-label="รายการที่ Sticker ร้องขอ">★</span>}
                        {state === "warning" && <span className="cabinet-overview__status" aria-label="คำเตือน">!</span>}
                        {state === "error" && <span className="cabinet-overview__status" aria-label="ใช้งานไม่ได้">×</span>}
                        <span className="cabinet-overview__code">{slot?.code || "ว่าง"}</span>
                      </button>
                    );
                  })}
                </div>
              </div>
            ))}
          </div>
        ) : (
          <div className="cabinet-detail" aria-label={`รายละเอียดชั้น ${activeShelf}`}>
            <header><div><span>รายละเอียดชั้น</span><strong>{activeShelf}</strong></div><p>ช่อง {(activeShelf - 1) * 22 + 1}–{activeShelf * 22} · ข้อมูลตำแหน่งแบบอ่านอย่างเดียว</p></header>
            <div className="cabinet-detail__grid">
              {Array.from({ length: ROW_COUNT }, (_, index) => index + 1).map((row) => {
                const slot = slotsByPosition.get(`${activeShelf}:${row}`);
                return <SlotCell key={`${activeShelf}:${row}`} slot={slot} shelfNumber={activeShelf} rowNumber={(activeShelf - 1) * ROW_COUNT + row} selected={Boolean(slot && requestedSlotIds.includes(slot.id))} expiryWarningDays={expiryWarningDays} readOnly />;
              })}
            </div>
            {focusedSlotId && (() => {
              const focused = slots.find((slot) => slot.id === focusedSlotId);
              const position = focused ? getSlotPosition(focused) : null;
              return focused && position ? <div className="cabinet-detail__focus" aria-live="polite"><strong>{focused.drugName || focused.displayName}</strong><span>ตู้ {focused.code} · ชั้น {position.shelf} · ช่อง {(position.shelf - 1) * 22 + position.row} · คงเหลือ {focused.quantity}/{focused.capacity}</span></div> : null;
            })()}
          </div>
        )}
      </section>
    );
  }

  return (
    <section className="shelf-grid" aria-label="ผังช่องยาในตู้">
      <div className="shelf-grid__tabs" role="tablist" aria-label="เลือกชั้นยา">
        {Array.from({ length: SHELF_COUNT }, (_, index) => index + 1).map(
          (shelf) => (
            <button
              key={shelf}
              type="button"
              className={`shelf-grid__tab${
                activeShelf === shelf ? " shelf-grid__tab--active" : ""
              }`}
              role="tab"
              aria-selected={activeShelf === shelf}
              aria-controls={`shelf-panel-${shelf}`}
              id={`shelf-tab-${shelf}`}
              tabIndex={activeShelf === shelf ? 0 : -1}
              ref={(element) => {
                tabRefs.current[shelf - 1] = element;
              }}
              onClick={() => setActiveShelf(shelf)}
              onKeyDown={(event) => handleTabKeyDown(event, shelf)}
            >
              ชั้น {shelf}
            </button>
          ),
        )}
      </div>

      <div
        className="shelf-grid__shelf"
        id={`shelf-panel-${activeShelf}`}
        role="tabpanel"
        aria-labelledby={`shelf-tab-${activeShelf}`}
      >
        <div className="shelf-grid__heading">
          <span>ชั้น {activeShelf}</span>
          <span className="shelf-grid__hint">เลื่อนซ้าย–ขวาเพื่อดูช่อง 1–22</span>
        </div>
        <div className="shelf-grid__viewport" tabIndex={0}>
          <div
            className="shelf-grid__cells"
            role="grid"
            aria-label={`ช่องยาชั้น ${activeShelf}`}
            aria-colcount={ROW_COUNT}
            aria-rowcount={1}
          >
            {renderCells(activeShelf).props.children}
          </div>
        </div>
      </div>

      {selectedSlot && showSelectedDetail && (
        <div className="shelf-grid__detail" aria-live="polite">
          <div className="shelf-grid__detail-heading">
            <div>
              <span className="shelf-grid__address">
                S{selectedPosition.shelf}-R{selectedPosition.row}
              </span>
              <h2>{
                selectedSlot.drugName ||
                selectedSlot.displayName ||
                "ช่องว่าง"
              }</h2>
            </div>
            <ExpiryBadge
              expiryDate={selectedSlot.expiryDate}
              warningDays={expiryWarningDays}
            />
          </div>
          <div className="shelf-grid__detail-meta">
            {selectedSlot.drugCode && (
              <span className="shelf-grid__drug-code">
                {selectedSlot.drugCode}
              </span>
            )}
            <span>
              คงเหลือ {selectedSlot.quantity} / {selectedSlot.capacity} หน่วย
            </span>
          </div>
        </div>
      )}
    </section>
  );
}
