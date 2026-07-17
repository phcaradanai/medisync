import {
  useMemo,
  useRef,
  useState,
  type KeyboardEvent as ReactKeyboardEvent,
} from "react";
import SlotCell, {
  ExpiryBadge,
  type SlotCellData,
} from "./SlotCell";

export interface ShelfGridProps {
  slots: readonly SlotCellData[];
  selectedSlotId?: string | null;
  initialShelf?: number;
  expiryWarningDays?: number;
  onSelect?: (slot: SlotCellData) => void;
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
}: ShelfGridProps) {
  const [activeShelf, setActiveShelf] = useState(clampShelf(initialShelf));
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
            {Array.from({ length: ROW_COUNT }, (_, index) => index + 1).map(
              (row) => {
                const slot = slotsByPosition.get(`${activeShelf}:${row}`);
                return (
                  <div
                    className="shelf-grid__cell-wrapper"
                    role="gridcell"
                    aria-colindex={row}
                    key={`${activeShelf}:${row}`}
                  >
                    <SlotCell
                      slot={slot}
                      shelfNumber={activeShelf}
                      rowNumber={row}
                      selected={slot?.id === resolvedSelectedId}
                      expiryWarningDays={expiryWarningDays}
                      onSelect={handleSelect}
                    />
                  </div>
                );
              },
            )}
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
