import { useState, useEffect, useCallback, useMemo } from "react";
import { create } from "@bufbuild/protobuf";
import { timestampDate } from "@bufbuild/protobuf/wkt";
import { ListSlotsRequestSchema } from "@medisync/proto/medisync/inventory/v1/inventory_pb";
import type { Slot } from "@medisync/proto/medisync/inventory/v1/inventory_pb";
import { ListCabinetsRequestSchema } from "@medisync/proto/medisync/cabinet/v1/cabinet_pb";
import type { Cabinet } from "@medisync/proto/medisync/cabinet/v1/cabinet_pb";
import { ListProjectsRequestSchema } from "@medisync/proto/medisync/identity/v1/identity_pb";
import type { Project } from "@medisync/proto/medisync/identity/v1/identity_pb";
import { inventoryClient, cabinetClient, projectClient } from "../../api/client";
import { Icon } from "../masterdata/icons";
import { MasterHeader, ListHeading, SearchInput, Select, MasterTable, type Column } from "../masterdata/kit";

function stockBadge(s: Slot) {
  if (!s.drugId) return <span className="md-badge md-badge-off">ว่าง</span>;
  if (s.quantity <= 0) return <span className="md-badge md-badge-error">หมด</span>;
  if (s.quantity <= s.lowThreshold) return <span className="md-badge md-badge-warn">ใกล้หมด</span>;
  return <span className="md-badge md-badge-on"><Icon.checkCircle size={14} /> ปกติ</span>;
}

export function InventoryPage() {
  const [slots, setSlots] = useState<Slot[]>([]);
  const [cabinets, setCabinets] = useState<Cabinet[]>([]);
  const [projects, setProjects] = useState<Project[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [query, setQuery] = useState("");
  const [cabinetFilter, setCabinetFilter] = useState("");
  const [stockFilter, setStockFilter] = useState<"all" | "low">("all");

  const fetchAll = useCallback(async () => {
    setLoading(true); setError(null);
    try {
      const [s, c, p] = await Promise.all([
        inventoryClient.listSlots(create(ListSlotsRequestSchema, {})),
        cabinetClient.listCabinets(create(ListCabinetsRequestSchema, {})),
        projectClient.listProjects(create(ListProjectsRequestSchema, {})),
      ]);
      setSlots(s.slots); setCabinets(c.cabinets); setProjects(p.projects);
    } catch (e: unknown) { setError(e instanceof Error ? e.message : "โหลดข้อมูลคลังไม่สำเร็จ"); }
    finally { setLoading(false); }
  }, []);
  useEffect(() => { fetchAll(); }, [fetchAll]);

  const cabinetName = useCallback((id: string) => cabinets.find((c) => c.id === id)?.code ?? "—", [cabinets]);
  void projects;

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    return slots.filter((s) => {
      if (cabinetFilter && s.cabinetId !== cabinetFilter) return false;
      if (stockFilter === "low" && !(s.drugId && s.quantity <= s.lowThreshold)) return false;
      if (!q) return true;
      return s.code.toLowerCase().includes(q) || s.drugCode.toLowerCase().includes(q) || s.drugName.toLowerCase().includes(q);
    });
  }, [slots, query, cabinetFilter, stockFilter]);

  const lowCount = useMemo(() => slots.filter((s) => s.drugId && s.quantity <= s.lowThreshold).length, [slots]);

  const columns: Column<Slot>[] = [
    { key: "code", header: "ช่อง", render: (s) => <span className="md-code">{s.code}</span> },
    { key: "cabinet", header: "ตู้", render: (s) => <span className="md-cell-muted">{cabinetName(s.cabinetId)}</span> },
    { key: "pos", header: "ตำแหน่ง", render: (s) => <span className="md-cell-muted">{s.shelf || 1}-{s.rowNum || 1}</span> },
    { key: "drug", header: "ยา", render: (s) => s.drugId ? <span>{s.drugName} <span className="md-cell-muted">· {s.drugCode}</span></span> : <span className="md-cell-muted">ยังไม่กำหนด</span> },
    { key: "qty", header: "คงเหลือ", render: (s) => <strong>{s.quantity}</strong> },
    { key: "cap", header: "ความจุ", render: (s) => <span className="md-cell-muted">{s.capacity}</span> },
    { key: "low", header: "ขั้นต่ำ", render: (s) => <span className="md-cell-muted">{s.lowThreshold}</span> },
    { key: "expiry", header: "หมดอายุ", render: (s) => <span className="md-cell-muted">{s.expiryDate ? timestampDate(s.expiryDate).toLocaleDateString("th-TH") : "—"}</span> },
    { key: "status", header: "สถานะ", render: (s) => stockBadge(s) },
  ];

  return (
    <>
      <MasterHeader icon={Icon.inventory} title="คลังยา" subtitle="สต็อกยาในตู้ (ดูอย่างเดียว · อัปเดตตามการเติมยารายวัน)">
        <span className="md-badge md-badge-soft"><Icon.help size={15} /> ดูอย่างเดียว</span>
        <button className="md-btn md-btn-ghost" onClick={fetchAll}><Icon.undo size={18} /> รีเฟรช</button>
      </MasterHeader>

      {error && <div className="md-err">{error}</div>}

      <div className="md-panel">
        <ListHeading icon={Icon.inventory} title="ช่องเก็บยา" count={filtered.length} />
        <div className="md-toolbar">
          <SearchInput value={query} onChange={setQuery} placeholder="ค้นหาช่อง หรือชื่อ/รหัสยา" />
          <div className="md-segment">
            <button className={`md-seg-btn${stockFilter === "all" ? " active" : ""}`} onClick={() => setStockFilter("all")}>ทั้งหมด <span className="md-seg-num">{slots.length}</span></button>
            <button className={`md-seg-btn${stockFilter === "low" ? " active" : ""}`} onClick={() => setStockFilter("low")}>ใกล้หมด <span className="md-seg-num">{lowCount}</span></button>
          </div>
          <Select value={cabinetFilter} onChange={setCabinetFilter}>
            <option value="">ทุกตู้</option>
            {cabinets.map((c) => <option key={c.id} value={c.id}>{c.code}</option>)}
          </Select>
        </div>
        <MasterTable rows={filtered} columns={columns} getId={(s) => s.id} loading={loading} selectable={false} emptyText="ไม่พบช่องเก็บยา" />
      </div>
    </>
  );
}
