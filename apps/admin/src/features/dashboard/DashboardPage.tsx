import { useState, useEffect, useCallback, useMemo } from "react";
import { useNavigate } from "react-router-dom";
import { create } from "@bufbuild/protobuf";
import { ListDrugsRequestSchema } from "@medisync/proto/medisync/catalog/v1/catalog_pb";
import type { Drug } from "@medisync/proto/medisync/catalog/v1/catalog_pb";
import { ListProjectsRequestSchema, ListUsersRequestSchema } from "@medisync/proto/medisync/identity/v1/identity_pb";
import { ListCabinetsRequestSchema } from "@medisync/proto/medisync/cabinet/v1/cabinet_pb";
import { ListKiosksRequestSchema } from "@medisync/proto/medisync/kiosk/v1/kiosk_pb";
import { ListSlotsRequestSchema } from "@medisync/proto/medisync/inventory/v1/inventory_pb";
import type { Slot } from "@medisync/proto/medisync/inventory/v1/inventory_pb";
import type { Cabinet } from "@medisync/proto/medisync/cabinet/v1/cabinet_pb";
import { catalogClient, projectClient, cabinetClient, kioskClient, identityClient, inventoryClient } from "../../api/client";
import { Icon } from "../masterdata/icons";
import { MasterHeader } from "../masterdata/kit";

interface Counts { drugs: number; drugsActive: number; projects: number; cabinets: number; kiosks: number; users: number; }

export function DashboardPage() {
  const navigate = useNavigate();
  const [counts, setCounts] = useState<Counts>({ drugs: 0, drugsActive: 0, projects: 0, cabinets: 0, kiosks: 0, users: 0 });
  const [slots, setSlots] = useState<Slot[]>([]);
  const [cabinets, setCabinets] = useState<Cabinet[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);

  const load = useCallback(async () => {
    setLoading(true); setError(null);
    const next: Counts = { drugs: 0, drugsActive: 0, projects: 0, cabinets: 0, kiosks: 0, users: 0 };
    try {
      const d = await catalogClient.listDrugs(create(ListDrugsRequestSchema, { query: "", pageSize: 500, includeInactive: true }));
      next.drugs = d.drugs.length;
      next.drugsActive = d.drugs.filter((x: Drug) => x.active).length;
    } catch (e: unknown) { setError(e instanceof Error ? e.message : "โหลดข้อมูลไม่สำเร็จ"); }
    try { const r = await projectClient.listProjects(create(ListProjectsRequestSchema, {})); next.projects = r.projects.length; } catch { /* */ }
    try { const r = await cabinetClient.listCabinets(create(ListCabinetsRequestSchema, {})); next.cabinets = r.cabinets.length; setCabinets(r.cabinets); } catch { /* */ }
    try { const r = await kioskClient.listKiosks(create(ListKiosksRequestSchema, {})); next.kiosks = r.kiosks.length; } catch { /* */ }
    try { const r = await identityClient.listUsers(create(ListUsersRequestSchema, { query: "" })); next.users = r.users.length; } catch { /* */ }
    try { const r = await inventoryClient.listSlots(create(ListSlotsRequestSchema, {})); setSlots(r.slots); } catch { /* */ }
    setCounts(next); setLoading(false);
  }, []);
  useEffect(() => { load(); }, [load]);

  const cabinetCode = useCallback((id: string) => cabinets.find((c) => c.id === id)?.code ?? "—", [cabinets]);

  const totalItems = counts.drugs + counts.projects + counts.cabinets + counts.kiosks + counts.users;
  const lowSlots = useMemo(() => slots.filter((s) => s.drugId && s.quantity <= s.lowThreshold), [slots]);
  const outSlots = useMemo(() => lowSlots.filter((s) => s.quantity <= 0), [lowSlots]);

  const kpis = [
    { label: "รายการข้อมูลหลักทั้งหมด", value: totalItems, icon: Icon.database, tint: "md-tint-drug", sub: "5 หมวด" },
    { label: "ยาที่พร้อมใช้งาน", value: counts.drugsActive, icon: Icon.pill, tint: "md-tint-proj", sub: <>จาก <strong>{counts.drugs}</strong> รายการ</> },
    { label: "ช่องเก็บยาทั้งหมด", value: slots.length, icon: Icon.inventory, tint: "md-tint-cab", sub: `${cabinets.length} ตู้` },
  ];

  const cards = [
    { key: "drug", label: "ยา", count: counts.drugs, icon: Icon.pill, tint: "md-tint-drug", to: "/drugs" },
    { key: "proj", label: "โครงการ", count: counts.projects, icon: Icon.folder, tint: "md-tint-proj", to: "/projects" },
    { key: "cab", label: "ตู้ยา", count: counts.cabinets, icon: Icon.cabinet, tint: "md-tint-cab", to: "/devices" },
    { key: "kiosk", label: "Kiosk", count: counts.kiosks, icon: Icon.monitor, tint: "md-tint-kiosk", to: "/devices" },
    { key: "user", label: "ผู้ใช้งาน", count: counts.users, icon: Icon.users, tint: "md-tint-user", to: "/users" },
  ];

  return (
    <>
      <MasterHeader icon={Icon.grid} title="ภาพรวมระบบ" subtitle={loading ? "กำลังโหลด…" : `ข้อมูลหลัก 5 หมวด • ${totalItems} รายการ`}>
        <button className="md-btn md-btn-ghost" onClick={load}><Icon.undo size={18} /> รีเฟรช</button>
      </MasterHeader>

      {error && <div className="md-err">{error}</div>}

      {/* KPI overview */}
      <div className="md-kpis">
        {kpis.map((k, i) => (
          <div className="md-kpi" key={i}>
            <div className="md-kpi-top">
              <div>
                <div className="md-kpi-label">{k.label}</div>
                <div className="md-kpi-value" style={{ marginTop: 8 }}>{k.value}</div>
              </div>
              <div className={`md-kpi-ico ${k.tint}`}><k.icon size={22} /></div>
            </div>
            <div className="md-kpi-sub">{k.sub}</div>
          </div>
        ))}
        <div className={`md-kpi${lowSlots.length ? " warn" : ""}`}>
          <div className="md-kpi-top">
            <div>
              <div className="md-kpi-label">สต็อกที่ต้องดูแล</div>
              <div className="md-kpi-value" style={{ marginTop: 8 }}>{lowSlots.length}</div>
            </div>
            <div className="md-kpi-ico md-tint-kiosk" style={lowSlots.length ? { background: "#fef3e2", color: "#b45309" } : undefined}><Icon.bell size={22} /></div>
          </div>
          <div className="md-kpi-sub">{outSlots.length > 0 ? <>หมด <strong style={{ color: "#c0304f" }}>{outSlots.length}</strong> · ใกล้หมด {lowSlots.length - outSlots.length}</> : "สต็อกปกติทั้งหมด"}</div>
        </div>
      </div>

      {/* Master-data categories */}
      <div className="md-section-label"><span className="md-slabel-ico"><Icon.database size={18} /></span> ข้อมูลหลัก (Master Data)</div>
      <div className="md-cards">
        {cards.map((c) => (
          <div key={c.key} className={`md-card tint-${c.key}`} onClick={() => navigate(c.to)}>
            <div className="md-card-top">
              <div className={`md-card-ico ${c.tint}`}><c.icon size={24} /></div>
              <div className="md-card-count">{c.count}</div>
            </div>
            <div className="md-card-label">{c.label}</div>
            <div className="md-card-status"><Icon.checkCircle size={15} /> พร้อมใช้</div>
          </div>
        ))}
      </div>

      {/* Low-stock overview */}
      <div className="md-section-label" style={{ marginTop: 26 }}><span className="md-slabel-ico"><Icon.inventory size={18} /></span> แจ้งเตือนสต็อก</div>
      <div className="md-panel">
        <div className="md-table-wrap">
          <table className="md-table">
            <thead>
              <tr>
                <th>ช่อง</th><th>ตู้</th><th>ยา</th><th>คงเหลือ</th><th>ขั้นต่ำ</th><th>สถานะ</th>
              </tr>
            </thead>
            <tbody>
              {loading ? (
                <tr><td colSpan={6}><div className="md-empty">กำลังโหลด…</div></td></tr>
              ) : lowSlots.length === 0 ? (
                <tr><td colSpan={6}><div className="md-empty">สต็อกปกติทั้งหมด ไม่มีรายการใกล้หมด 🎉</div></td></tr>
              ) : (
                lowSlots.slice(0, 8).map((s) => (
                  <tr key={s.id}>
                    <td><span className="md-code">{s.code}</span></td>
                    <td className="md-cell-muted">{cabinetCode(s.cabinetId)}</td>
                    <td>{s.drugName} <span className="md-cell-muted">· {s.drugCode}</span></td>
                    <td><strong>{s.quantity}</strong></td>
                    <td className="md-cell-muted">{s.lowThreshold}</td>
                    <td>{s.quantity <= 0 ? <span className="md-badge md-badge-error">หมด</span> : <span className="md-badge md-badge-warn">ใกล้หมด</span>}</td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>
        {lowSlots.length > 8 && (
          <div className="md-list-foot">
            <div className="md-foot-sel">แสดง 8 จาก {lowSlots.length} รายการ</div>
            <button className="md-btn md-btn-ghost" onClick={() => navigate("/inventory")}>ดูคลังยาทั้งหมด <Icon.chevronRight size={16} /></button>
          </div>
        )}
      </div>
    </>
  );
}
