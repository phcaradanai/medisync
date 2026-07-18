export type ExpiryValue =
  | Date
  | string
  | number
  | {
      seconds: bigint | number | string;
      nanos?: number;
    };

export interface SlotCellData {
  id: string;
  code: string;
  displayName?: string;
  drugCode?: string;
  drugName?: string;
  quantity: number;
  capacity: number;
  lowThreshold: number;
  shelf?: number;
  rowNum?: number;
  expiryDate?: ExpiryValue;
}

export type SlotCellState =
  | "empty"
  | "assigned"
  | "low"
  | "expiring"
  | "expired";

export interface SlotCellProps {
  slot?: SlotCellData;
  shelfNumber?: number;
  rowNumber?: number;
  selected?: boolean;
  expiryWarningDays?: number;
  now?: Date;
  onSelect?: (slot: SlotCellData) => void;
  readOnly?: boolean;
}

export interface ExpiryBadgeProps {
  expiryDate?: ExpiryValue;
  warningDays?: number;
  now?: Date;
}

const DAY_MS = 24 * 60 * 60 * 1000;

export function expiryValueToDate(value?: ExpiryValue): Date | null {
  if (value === undefined || value === null || value === "") return null;

  if (value instanceof Date) {
    return Number.isNaN(value.getTime()) ? null : value;
  }

  if (typeof value === "object" && "seconds" in value) {
    const milliseconds =
      Number(value.seconds) * 1000 + Number(value.nanos ?? 0) / 1_000_000;
    const date = new Date(milliseconds);
    return Number.isNaN(date.getTime()) ? null : date;
  }

  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? null : date;
}

function startOfDay(date: Date): Date {
  return new Date(date.getFullYear(), date.getMonth(), date.getDate());
}

export function getExpiryState(
  expiryDate?: ExpiryValue,
  warningDays = 30,
  now = new Date(),
): "fresh" | "expiring" | "expired" | "none" {
  const expiry = expiryValueToDate(expiryDate);
  if (!expiry) return "none";

  const daysUntilExpiry = Math.round(
    (startOfDay(expiry).getTime() - startOfDay(now).getTime()) / DAY_MS,
  );

  if (daysUntilExpiry < 0) return "expired";
  if (daysUntilExpiry < warningDays) return "expiring";
  return "fresh";
}

export function getSlotCellState(
  slot?: SlotCellData,
  expiryWarningDays = 30,
  now = new Date(),
): SlotCellState {
  if (!slot || (!slot.drugCode && !slot.drugName)) return "empty";

  const expiryState = getExpiryState(
    slot.expiryDate,
    expiryWarningDays,
    now,
  );
  if (expiryState === "expired") return "expired";
  if (expiryState === "expiring") return "expiring";
  if (slot.quantity <= slot.lowThreshold) return "low";
  return "assigned";
}

function formatExpiryDate(value?: ExpiryValue): string | null {
  const date = expiryValueToDate(value);
  if (!date) return null;

  const year = date.getFullYear();
  const month = String(date.getMonth() + 1).padStart(2, "0");
  const day = String(date.getDate()).padStart(2, "0");
  return `${year}-${month}-${day}`;
}

export function ExpiryBadge({
  expiryDate,
  warningDays = 30,
  now = new Date(),
}: ExpiryBadgeProps) {
  const state = getExpiryState(expiryDate, warningDays, now);
  const formattedDate = formatExpiryDate(expiryDate);

  if (!formattedDate || state === "none" || state === "fresh") return null;

  const expired = state === "expired";
  return (
    <span
      className={`status-badge ${
        expired ? "status-badge--error" : "status-badge--warning"
      } expiry-badge`}
      aria-label={`${expired ? "หมดอายุ" : "ใกล้หมดอายุ"} ${formattedDate}`}
    >
      <span aria-hidden="true">{expired ? "⊘" : "◷"}</span>
      <span>{expired ? "หมดอายุ" : "ใกล้หมดอายุ"}</span>
      <time dateTime={formattedDate}>{formattedDate}</time>
    </span>
  );
}

function getStateLabel(state: SlotCellState): string {
  switch (state) {
    case "expired":
      return "หมดอายุ ไม่สามารถเลือกได้";
    case "expiring":
      return "ใกล้หมดอายุ";
    case "low":
      return "ยาเหลือน้อย";
    case "assigned":
      return "พร้อมใช้งาน";
    case "empty":
      return "ช่องว่าง";
  }
}

function getStateGlyph(state: SlotCellState): string | null {
  switch (state) {
    case "expired":
      return "⊘";
    case "expiring":
      return "◷";
    case "low":
      return "!";
    default:
      return null;
  }
}

export function SlotCell({
  slot,
  shelfNumber,
  rowNumber,
  selected = false,
  expiryWarningDays = 30,
  now = new Date(),
  onSelect,
  readOnly = false,
}: SlotCellProps) {
  const resolvedShelfNumber = shelfNumber ?? slot?.shelf ?? 1;
  const resolvedRowNumber = rowNumber ?? slot?.rowNum ?? 1;
  const state = getSlotCellState(slot, expiryWarningDays, now);
  const glyph = getStateGlyph(state);
  const address = `S${resolvedShelfNumber}-R${resolvedRowNumber}`;
  const disabled = !slot || state === "expired" || readOnly;
  const fillPercent = slot?.capacity
    ? Math.max(0, Math.min(100, (slot.quantity / slot.capacity) * 100))
    : 0;
  const drugName = slot?.drugName || slot?.displayName || "ช่องว่าง";
  const title = slot
    ? `${address} · ${drugName} · ${slot.quantity}/${slot.capacity} · ${getStateLabel(state)}`
    : `${address} · ช่องว่าง`;

  return (
    <button
      type="button"
      className={`slot-cell slot-cell--${state}${selected ? " slot-cell--selected" : ""}`}
      data-state={selected ? "selected" : state}
      aria-label={title}
      aria-pressed={slot && !readOnly ? selected : undefined}
      disabled={disabled}
      title={title}
      onClick={() => slot && !disabled && onSelect?.(slot)}
    >
      <span className="slot-cell__header">
        <span className="slot-cell__row">{resolvedRowNumber}</span>
        {glyph && (
          <span className="slot-cell__glyph" aria-label={getStateLabel(state)}>
            <span aria-hidden="true">{glyph}</span>
          </span>
        )}
      </span>
      <span className="slot-cell__body">
        <span className="slot-cell__drug-name">{drugName}</span>
        <span className="slot-cell__drug-visual" aria-hidden="true">
          {state === "empty" ? (
            <span className="slot-cell__empty-icon">
              <svg viewBox="0 0 40 40" fill="none" focusable="false">
                <rect x="5" y="8" width="30" height="24" rx="6" />
                <path d="M20 14v12M14 20h12" />
              </svg>
            </span>
          ) : (
            <svg viewBox="0 0 34 58" focusable="false">
              <path d="M11 2h12v8l4 6v32c0 5-3 8-8 8h-4c-5 0-8-3-8-8V16l4-6z" />
              <path d="M8 24h18v20H8z" className="slot-cell__vial-label" />
            </svg>
          )}
        </span>
      </span>
      <span className="slot-cell__meta">
        <span className="slot-cell__drug-code">
          {slot?.drugCode ? `รหัส ${slot.drugCode}` : "พร้อมกำหนดยา"}
        </span>
        {slot && <span className="slot-cell__stock">คงเหลือ {slot.quantity}</span>}
      </span>
      <span className="slot-cell__fill-track" aria-hidden="true">
        <span
          className="slot-cell__fill"
          style={{ width: `${fillPercent}%` }}
        />
      </span>
      <span className="slot-cell__position">{slot?.code || address}</span>
    </button>
  );
}

export default SlotCell;
