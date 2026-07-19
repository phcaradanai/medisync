import { useEffect, useMemo, useRef, useState, type CSSProperties } from "react";
import {
  expiryValueToDate,
  getExpiryState,
  type ExpiryValue,
  type SlotCellData,
  type SlotLotInput,
} from "./SlotCell";

export interface SlotDetailModalProps {
  slot: SlotCellData;
  /** Other slots holding the same drug — rendered in the bottom carousel. */
  relatedSlots?: readonly SlotCellData[];
  expiryWarningDays?: number;
  now?: Date;
  onClose: () => void;
  onSelectRelated?: (slot: SlotCellData) => void;
}

interface LotView extends SlotLotInput {
  /** 1-based number of this lot's first piece in FEFO dispensing order. */
  startPiece: number;
}

// Theme palette cycled across lots so each batch reads as a distinct colour.
const LOT_TONES = ["#22c55e", "#1e66f5", "#7c3aed", "#0ea5e9"];

const thaiDate = new Intl.DateTimeFormat("th-TH-u-ca-gregory", {
  day: "numeric",
  month: "short",
  year: "numeric",
});

function formatThaiDate(value?: ExpiryValue): string | null {
  const date = expiryValueToDate(value);
  return date ? thaiDate.format(date) : null;
}

function pieceLabel(sequence: number): string {
  return `#${String(sequence).padStart(3, "0")}`;
}

/**
 * A single classification badge, highest-risk first: high-alert drugs outrank
 * look-alike/sound-alike (LASA), which outrank ordinary stock. Only one shows.
 */
function classify(slot: SlotCellData): {
  tone: "danger" | "lasa" | "normal";
  icon: string;
  label: string;
} {
  if (slot.highAlert) return { tone: "danger", icon: "🛡", label: "ความเสี่ยงสูง" };
  if (slot.lasa) return { tone: "lasa", icon: "⚠", label: "LASA" };
  return { tone: "normal", icon: "✓", label: slot.category || "ยาสามัญ" };
}

/**
 * Build the FEFO-ordered lot breakdown for a slot. Uses the real per-lot data
 * when the caller provides it; otherwise derives a single representative lot
 * from the aggregate slot fields so the channel still renders truthfully.
 */
export function deriveLots(slot: SlotCellData): LotView[] {
  const source: SlotLotInput[] =
    slot.lots && slot.lots.length > 0
      ? [...slot.lots]
      : [
          {
            lotNumber: slot.drugCode
              ? `LOT-${slot.code}-${slot.drugCode}`
              : `LOT-${slot.code}`,
            quantity: slot.quantity,
            expiryDate: slot.expiryDate,
            refillDate: slot.updatedAt,
          },
        ];

  source.sort((a, b) => {
    const aDate = expiryValueToDate(a.expiryDate)?.getTime() ?? Infinity;
    const bDate = expiryValueToDate(b.expiryDate)?.getTime() ?? Infinity;
    return aDate - bDate;
  });

  let cursor = 1;
  return source
    .filter((lot) => lot.quantity > 0)
    .map((lot) => {
      const view: LotView = { ...lot, startPiece: cursor };
      cursor += lot.quantity;
      return view;
    });
}

function CapsuleGlyph({ className }: { className?: string }) {
  return (
    <svg className={className} viewBox="0 0 40 22" aria-hidden="true" focusable="false">
      <rect x="1" y="1" width="38" height="20" rx="10" />
      <rect x="1" y="1" width="19" height="20" rx="10" className="slot-capsule__half" />
    </svg>
  );
}

export default function SlotDetailModal({
  slot,
  relatedSlots = [],
  expiryWarningDays = 30,
  now = new Date(),
  onClose,
  onSelectRelated,
}: SlotDetailModalProps) {
  const lots = useMemo(() => deriveLots(slot), [slot]);
  const [selectedLot, setSelectedLot] = useState(0);
  const closeRef = useRef<HTMLButtonElement | null>(null);
  const carouselRef = useRef<HTMLDivElement | null>(null);

  const activeLot = lots[selectedLot] ?? lots[0];
  const filled = Math.max(0, Math.min(slot.quantity, slot.capacity));
  const empty = Math.max(0, slot.capacity - filled);
  const fillPercent = slot.capacity ? (filled / slot.capacity) * 100 : 0;
  const drugName = slot.drugName || slot.displayName || "ช่องว่าง";
  const status = classify(slot);
  const activeExpiryState = activeLot
    ? getExpiryState(activeLot.expiryDate, expiryWarningDays, now)
    : "none";

  useEffect(() => {
    closeRef.current?.focus();
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") onClose();
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [onClose]);

  const ruler = useMemo(() => {
    const step = 50;
    const marks = new Set<number>([0, slot.capacity, filled]);
    for (let value = step; value < slot.capacity; value += step) marks.add(value);
    return [...marks].sort((a, b) => a - b);
  }, [slot.capacity, filled]);

  const scrollCarousel = (direction: 1 | -1) => {
    const node = carouselRef.current;
    if (!node) return;
    node.scrollBy({ left: direction * node.clientWidth * 0.8, behavior: "smooth" });
  };

  return (
    <div
      className="slot-detail__backdrop"
      role="dialog"
      aria-modal="true"
      aria-label={`รายละเอียดช่องยา ${slot.code}`}
      onClick={(event) => {
        if (event.target === event.currentTarget) onClose();
      }}
    >
      <div className="slot-detail">
        <button
          type="button"
          className="slot-detail__close"
          aria-label="ปิดหน้าต่างรายละเอียดช่องยา"
          ref={closeRef}
          onClick={onClose}
        >
          ✕
        </button>

        <header className="slot-detail__header">
          <div className="slot-detail__thumb" aria-hidden="true">
            <CapsuleGlyph className="slot-detail__thumb-capsule" />
          </div>
          <div className="slot-detail__identity">
            <h2 className="slot-detail__name">{drugName}</h2>
            <div className="slot-detail__location">
              <span className="slot-detail__pin" aria-hidden="true">
                📍
              </span>
              <span className="slot-detail__address">{slot.code}</span>
            </div>
            <div className="slot-detail__chips">
              {slot.form && (
                <span className="slot-detail__chip">
                  <span aria-hidden="true">💊</span>
                  {slot.form}
                </span>
              )}
              {slot.manufacturer && (
                <span className="slot-detail__chip">
                  <span aria-hidden="true">🏭</span>
                  {slot.manufacturer}
                </span>
              )}
              {slot.drugCode && (
                <span className="slot-detail__chip slot-detail__chip--code">
                  {slot.drugCode}
                </span>
              )}
            </div>
          </div>
          <div className={`slot-detail__status slot-detail__status--${status.tone}`}>
            <span className="slot-detail__status-icon" aria-hidden="true">
              {status.icon}
            </span>
            <span className="slot-detail__status-label">{status.label}</span>
          </div>
        </header>

        <div className="slot-detail__stats">
          <div className="slot-detail__count">
            <span className="slot-detail__count-value">{filled}</span>
            <span className="slot-detail__count-total">/ {slot.capacity}</span>
            <span className="slot-detail__count-unit">ชิ้น</span>
          </div>
          <div className="slot-detail__progress" aria-hidden="true">
            <span
              className="slot-detail__progress-fill"
              style={{ width: `${fillPercent}%` }}
            />
          </div>
          <div className="slot-detail__lot-count">
            <span aria-hidden="true">🗂</span>
            <strong>{lots.length}</strong>
            <span>LOT</span>
          </div>
        </div>

        <section className="slot-channel" aria-label="ผังชิ้นยาในช่อง ตามลำดับการจ่าย">
          <div className="slot-channel__ends">
            <span className="slot-channel__end">
              <span aria-hidden="true">⬇</span> หน้าตู้
            </span>
            <span className="slot-channel__direction">
              <span className="slot-channel__direction-text">ทิศทางจ่าย →</span>
            </span>
            <span className="slot-channel__end">
              ท้ายช่อง <span aria-hidden="true">⬆</span>
            </span>
          </div>

          <div className="slot-channel__rail">
            <div className="slot-channel__filled" style={{ flexGrow: filled }}>
              {lots.map((lot, index) => {
                const selected = index === selectedLot;
                const tone = LOT_TONES[index % LOT_TONES.length];
                // Standing "cards" that fill the segment, proportional to the
                // lot size so the FEFO channel reads at a glance.
                const cards = Math.max(
                  2,
                  Math.round((lot.quantity / Math.max(1, filled)) * 18),
                );
                return (
                  <button
                    type="button"
                    key={`${lot.lotNumber}-${lot.startPiece}`}
                    className={`slot-lot${selected ? " slot-lot--selected" : ""}${
                      index === 0 ? " slot-lot--lead" : ""
                    }`}
                    style={
                      {
                        flexGrow: lot.quantity,
                        flexBasis: 0,
                        "--lot": tone,
                      } as CSSProperties
                    }
                    aria-pressed={selected}
                    aria-label={`LOT ${lot.lotNumber} · ${lot.quantity} ชิ้น · เริ่มชิ้น ${pieceLabel(
                      lot.startPiece,
                    )}`}
                    onClick={() => setSelectedLot(index)}
                  >
                    <span className="slot-lot__cards" aria-hidden="true">
                      {Array.from({ length: cards }, (_, card) => (
                        <span
                          key={card}
                          className={`slot-lot__card${
                            index === 0 && card === 0 ? " slot-lot__card--head" : ""
                          }`}
                        >
                          <CapsuleGlyph className="slot-lot__capsule" />
                        </span>
                      ))}
                    </span>
                    <span className="slot-lot__tag">
                      {index === 0 && (
                        <span className="slot-lot__lead" aria-hidden="true">
                          1
                        </span>
                      )}
                      <span className="slot-lot__qty">{lot.quantity}</span>
                    </span>
                  </button>
                );
              })}
            </div>
            {empty > 0 && (
              <div
                className="slot-channel__empty"
                style={{ flexGrow: empty, flexBasis: 0 }}
              >
                <span className="slot-channel__empty-icon" aria-hidden="true">
                  ⬚
                </span>
                <span>ว่าง {empty}</span>
              </div>
            )}
          </div>

          {activeLot && (
            <div className="slot-piece" aria-live="polite">
              <div className="slot-piece__seq">
                <span aria-hidden="true">💊</span>
                <strong>{pieceLabel(activeLot.startPiece)}</strong>
              </div>
              <dl className="slot-piece__grid">
                <div className="slot-piece__field">
                  <span className="slot-piece__ico" aria-hidden="true">
                    🏷
                  </span>
                  <div>
                    <dt>LOT</dt>
                    <dd className="slot-piece__lot">{activeLot.lotNumber}</dd>
                  </div>
                </div>
                <div className="slot-piece__field">
                  <span className="slot-piece__ico" aria-hidden="true">
                    📅
                  </span>
                  <div>
                    <dt>หมดอายุ</dt>
                    <dd className={`slot-piece__exp slot-piece__exp--${activeExpiryState}`}>
                      {formatThaiDate(activeLot.expiryDate) ?? "—"}
                    </dd>
                  </div>
                </div>
                <div className="slot-piece__field">
                  <span className="slot-piece__ico" aria-hidden="true">
                    📦
                  </span>
                  <div>
                    <dt>เติมยา</dt>
                    <dd>{formatThaiDate(activeLot.refillDate) ?? "—"}</dd>
                  </div>
                </div>
              </dl>
              {selectedLot === 0 && (
                <div className="slot-piece__fefo">
                  <span aria-hidden="true">✓</span> พร้อมจ่าย
                </div>
              )}
            </div>
          )}

          <div className="slot-channel__ruler" aria-hidden="true">
            {ruler.map((value) => (
              <span
                key={value}
                className={`slot-channel__tick${
                  value === filled ? " slot-channel__tick--current" : ""
                }`}
                style={{
                  left: `${slot.capacity ? (value / slot.capacity) * 100 : 0}%`,
                }}
              >
                {value}
              </span>
            ))}
          </div>
        </section>

        {relatedSlots.length > 0 && (
          <section className="slot-related" aria-label="ยาชนิดเดียวกันในช่องอื่น">
            <div className="slot-related__head">
              <span aria-hidden="true">🗂</span>
              <h3>ยาชนิดเดียวกันในช่องอื่น</h3>
            </div>
            <div className="slot-related__row">
              <button
                type="button"
                className="slot-related__nav"
                aria-label="เลื่อนซ้าย"
                onClick={() => scrollCarousel(-1)}
              >
                ‹
              </button>
              <div className="slot-related__cards" ref={carouselRef}>
                {relatedSlots.map((related) => {
                  const expLabel = formatThaiDate(related.expiryDate);
                  const expState = getExpiryState(
                    related.expiryDate,
                    expiryWarningDays,
                    now,
                  );
                  return (
                    <button
                      type="button"
                      key={related.id}
                      className="slot-related__card"
                      onClick={() => onSelectRelated?.(related)}
                    >
                      <span className="slot-related__card-visual" aria-hidden="true">
                        <CapsuleGlyph className="slot-related__capsule" />
                      </span>
                      <span className="slot-related__card-body">
                        <strong>{related.code}</strong>
                        {expLabel && (
                          <span
                            className={`slot-related__exp slot-related__exp--${expState}`}
                          >
                            <span aria-hidden="true">📅</span>
                            {expLabel}
                          </span>
                        )}
                      </span>
                      <span className="slot-related__card-qty">{related.quantity}</span>
                    </button>
                  );
                })}
              </div>
              <button
                type="button"
                className="slot-related__nav"
                aria-label="เลื่อนขวา"
                onClick={() => scrollCarousel(1)}
              >
                ›
              </button>
            </div>
          </section>
        )}
      </div>
    </div>
  );
}
