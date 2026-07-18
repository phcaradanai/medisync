/* Shared Master-Data UI kit — the list/table/drawer design language used
   by every master-data configuration page (Drugs, Projects, Devices,
   Users). Presentational only; each page supplies its own data + fields. */
import { useState, useEffect, type ReactNode, type FormEvent } from "react";
import { Icon } from "./icons";

// ── Thai date helper ─────────────────────────────────────────────────
const TH_MONTHS = ["ม.ค.", "ก.พ.", "มี.ค.", "เม.ย.", "พ.ค.", "มิ.ย.", "ก.ค.", "ส.ค.", "ก.ย.", "ต.ค.", "พ.ย.", "ธ.ค."];
export function formatThaiDateTime(d: Date): string {
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${d.getDate()} ${TH_MONTHS[d.getMonth()]} ${d.getFullYear()} • ${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

type IconCmp = (p: { size?: number }) => ReactNode;

// ── Page header ──────────────────────────────────────────────────────
export function MasterHeader({ icon: I, title, subtitle, children }: {
  icon: IconCmp; title: string; subtitle?: ReactNode; children?: ReactNode;
}) {
  return (
    <div className="md-page-head">
      <div className="md-page-head-left">
        <div className="md-page-icon"><I size={28} /></div>
        <div>
          <div className="md-page-title">{title}</div>
          {subtitle != null && <div className="md-page-sub">{subtitle}</div>}
        </div>
      </div>
      {children && <div className="md-page-head-actions">{children}</div>}
    </div>
  );
}

export function ListHeading({ icon: I, title, count }: { icon: IconCmp; title: string; count: number }) {
  return (
    <div className="md-list-head">
      <div className="md-list-title">
        <span className="md-list-ico"><I size={20} /></span>
        {title}
        <span className="md-chip-count">{count} รายการ</span>
      </div>
    </div>
  );
}

export function SearchInput({ value, onChange, placeholder }: {
  value: string; onChange: (v: string) => void; placeholder?: string;
}) {
  return (
    <div className="md-search">
      <Icon.search size={18} />
      <input value={value} onChange={(e) => onChange(e.target.value)} placeholder={placeholder} />
    </div>
  );
}

export function Select({ value, onChange, children, minWidth }: {
  value: string; onChange: (v: string) => void; children: ReactNode; minWidth?: number;
}) {
  return (
    <div className="md-select">
      <select value={value} onChange={(e) => onChange(e.target.value)} style={minWidth ? { minWidth } : undefined}>
        {children}
      </select>
      <Icon.chevronDown size={18} />
    </div>
  );
}

export function StatusBadge({ active, onLabel = "ใช้งาน", offLabel = "ปิดใช้งาน" }: {
  active: boolean; onLabel?: string; offLabel?: string;
}) {
  return active
    ? <span className="md-badge md-badge-on"><Icon.checkCircle size={14} /> {onLabel}</span>
    : <span className="md-badge md-badge-off">{offLabel}</span>;
}

// ── Generic table with selection + pagination ────────────────────────
export interface Column<T> {
  key: string;
  header: ReactNode;
  render: (row: T) => ReactNode;
  width?: number | string;
}

const PAGE_SIZES = [10, 25, 50];

export function MasterTable<T>({
  rows, columns, getId, onEdit, onDuplicate, onArchive,
  loading, emptyText = "ไม่พบรายการ", defaultPageSize = 10, selectable = true,
}: {
  rows: T[];
  columns: Column<T>[];
  getId: (row: T) => string;
  onEdit?: (row: T) => void;
  onDuplicate?: (row: T) => void;
  onArchive?: (row: T) => void;
  loading?: boolean;
  emptyText?: string;
  defaultPageSize?: number;
  selectable?: boolean;
}) {
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(defaultPageSize);

  useEffect(() => { setPage(1); }, [rows.length, pageSize]);

  const totalPages = Math.max(1, Math.ceil(rows.length / pageSize));
  const pageClamped = Math.min(page, totalPages);
  const pageRows = rows.slice((pageClamped - 1) * pageSize, pageClamped * pageSize);
  const hasActions = !!(onEdit || onDuplicate || onArchive);
  const colCount = columns.length + (selectable ? 1 : 0) + (hasActions ? 1 : 0);

  function toggle(id: string) {
    setSelected((s) => { const n = new Set(s); n.has(id) ? n.delete(id) : n.add(id); return n; });
  }
  function toggleAll() {
    setSelected((s) => {
      if (pageRows.every((r) => s.has(getId(r)))) {
        const n = new Set(s); pageRows.forEach((r) => n.delete(getId(r))); return n;
      }
      const n = new Set(s); pageRows.forEach((r) => n.add(getId(r))); return n;
    });
  }

  return (
    <>
      <div className="md-table-wrap">
        <table className="md-table">
          <thead>
            <tr>
              {selectable && (
                <th style={{ width: 44 }}>
                  <span
                    className={`md-check${pageRows.length > 0 && pageRows.every((r) => selected.has(getId(r))) ? " checked" : ""}`}
                    onClick={toggleAll}
                  ><Icon.check size={13} /></span>
                </th>
              )}
              {columns.map((c) => (
                <th key={c.key} style={c.width ? { width: c.width } : undefined}>{c.header}</th>
              ))}
              {hasActions && <th>จัดการ</th>}
            </tr>
          </thead>
          <tbody>
            {loading && rows.length === 0 ? (
              <tr><td colSpan={colCount}><div className="md-empty">กำลังโหลด…</div></td></tr>
            ) : pageRows.length === 0 ? (
              <tr><td colSpan={colCount}><div className="md-empty">{emptyText}</div></td></tr>
            ) : (
              pageRows.map((row) => {
                const id = getId(row);
                return (
                  <tr key={id} className={selected.has(id) ? "selected" : ""}>
                    {selectable && (
                      <td>
                        <span className={`md-check${selected.has(id) ? " checked" : ""}`} onClick={() => toggle(id)}>
                          <Icon.check size={13} />
                        </span>
                      </td>
                    )}
                    {columns.map((c) => <td key={c.key}>{c.render(row)}</td>)}
                    {hasActions && (
                      <td>
                        <div className="md-row-actions">
                          {onEdit && <button type="button" className="md-act" title="แก้ไข" onClick={() => onEdit(row)}><Icon.edit size={17} /></button>}
                          {onDuplicate && <button type="button" className="md-act" title="ทำสำเนา" onClick={() => onDuplicate(row)}><Icon.copy size={17} /></button>}
                          {onArchive && <button type="button" className="md-act" title="เก็บเข้าคลัง" onClick={() => onArchive(row)}><Icon.archive size={17} /></button>}
                        </div>
                      </td>
                    )}
                  </tr>
                );
              })
            )}
          </tbody>
        </table>
      </div>

      <div className="md-list-foot">
        <div className="md-foot-sel">
          {selectable ? (
            <>เลือก {selected.size} รายการ
              {selected.size > 0 && <a onClick={() => setSelected(new Set())}>ล้างการเลือก</a>}</>
          ) : <>ทั้งหมด {rows.length} รายการ</>}
        </div>
        <div className="md-pager">
          <button type="button" className="md-page-btn nav" disabled={pageClamped <= 1} onClick={() => setPage(pageClamped - 1)}><Icon.chevronLeft size={16} /></button>
          {Array.from({ length: totalPages }, (_, i) => i + 1).slice(0, 5).map((n) => (
            <button type="button" key={n} className={`md-page-btn${n === pageClamped ? " active" : ""}`} onClick={() => setPage(n)}>{n}</button>
          ))}
          <button type="button" className="md-page-btn nav" disabled={pageClamped >= totalPages} onClick={() => setPage(pageClamped + 1)}><Icon.chevronRight size={16} /></button>
        </div>
        <div className="md-foot-right">
          <Select value={String(pageSize)} onChange={(v) => setPageSize(Number(v))}>
            {PAGE_SIZES.map((n) => <option key={n} value={n}>{n} แถว</option>)}
          </Select>
          <div className="md-foot-note">
            แสดง {rows.length === 0 ? 0 : (pageClamped - 1) * pageSize + 1}–{Math.min(pageClamped * pageSize, rows.length)} จาก {rows.length}
          </div>
        </div>
      </div>
    </>
  );
}

// ── Drawer editor ────────────────────────────────────────────────────
export interface Step { num: string; label: string; icon: IconCmp; }

export function MasterDrawer({
  open, icon: I, title, entityLabel, code, dirty, steps, activeStep, onStep,
  onClose, onSubmit, onRestore, saving, timeLabel, saveLabel = "บันทึกการแก้ไข", children,
}: {
  open: boolean;
  icon: IconCmp;
  title: string;
  entityLabel: string;
  code?: string;
  dirty: boolean;
  steps: Step[];
  activeStep: number;
  onStep: (i: number) => void;
  onClose: () => void;
  onSubmit: (e: FormEvent) => void;
  onRestore: () => void;
  saving: boolean;
  timeLabel: string;
  saveLabel?: string;
  children: ReactNode;
}) {
  if (!open) return null;
  return (
    <div className="md-drawer-scrim" onClick={(e) => { if (e.target === e.currentTarget) onClose(); }}>
      <form className="md-drawer" onSubmit={onSubmit}>
        <div className="md-drawer-head">
          <div className="md-drawer-head-ico"><I size={24} /></div>
          <div className="md-drawer-head-title">{title}</div>
          <div className="md-drawer-tags">
            <span className="md-tag"><Icon.database size={14} /> {entityLabel}</span>
            {code && <span className="md-tag">{code}</span>}
            {dirty
              ? <span className="md-tag md-tag-dirty"><span className="md-dot" /> ยังไม่บันทึก</span>
              : <span className="md-tag md-tag-clean"><span className="md-dot" /> บันทึกแล้ว</span>}
          </div>
          <button type="button" className="md-drawer-close" onClick={onClose}><Icon.x size={22} /></button>
        </div>

        <div className="md-drawer-body">
          <div className="md-stepper">
            {steps.map((s, i) => {
              const cls = i === activeStep ? "active" : i < activeStep ? "done" : "";
              return (
                <div key={s.num} className={`md-step ${cls}`} onClick={() => onStep(i)}>
                  <div className="md-step-num">{s.num}</div>
                  <div className="md-step-ico"><s.icon size={22} /></div>
                  <div className="md-step-label">{s.label}</div>
                </div>
              );
            })}
          </div>
          <div className="md-form">{children}</div>
        </div>

        <div className="md-drawer-foot">
          <div className="md-foot-time"><Icon.clock size={18} /> {timeLabel}</div>
          <div className="md-foot-actions">
            <button type="button" className="md-btn md-btn-ghost" onClick={onRestore}><Icon.undo size={18} /> คืนค่า</button>
            <button type="button" className="md-btn md-btn-ghost" onClick={onClose}><Icon.x size={18} /> ยกเลิก</button>
            <button type="submit" className="md-btn md-btn-primary" disabled={saving}><Icon.check size={18} /> {saving ? "กำลังบันทึก…" : saveLabel}</button>
          </div>
        </div>
      </form>
    </div>
  );
}

export function DrawerSection({ num, icon: I, title, green, refEl, children }: {
  num: string; icon: IconCmp; title: string; green?: boolean; refEl?: React.Ref<HTMLDivElement>; children: ReactNode;
}) {
  return (
    <section className="md-section" ref={refEl}>
      <div className="md-section-head">
        <span className="md-section-num">{num}</span>
        <span className={`md-section-ico${green ? " green" : ""}`}><I size={18} /></span>
        <span className="md-section-title">{title}</span>
      </div>
      {children}
    </section>
  );
}

export function Field({ label, required, lead, trailingChevron, highlight, children }: {
  label: string; required?: boolean; lead?: ReactNode; trailingChevron?: boolean; highlight?: boolean; children: ReactNode;
}) {
  return (
    <div className={`md-field${lead ? "" : " no-icon"}${highlight ? " highlight" : ""}`}>
      <label>{label}{required && <span className="req">*</span>}</label>
      <div className="md-input-wrap">
        {lead && <span className="md-lead">{lead}</span>}
        {children}
        {trailingChevron && <Icon.chevronDown className="md-trail" size={18} />}
      </div>
    </div>
  );
}
