import { useState, useEffect, useCallback, useMemo, useRef, type FormEvent } from "react";
import { create } from "@bufbuild/protobuf";
import {
  ListKiosksRequestSchema, CreateKioskRequestSchema, UpdateKioskRequestSchema, ResetKioskPinRequestSchema,
} from "@medisync/proto/medisync/kiosk/v1/kiosk_pb";
import type { Kiosk } from "@medisync/proto/medisync/kiosk/v1/kiosk_pb";
import { ListProjectsRequestSchema } from "@medisync/proto/medisync/identity/v1/identity_pb";
import type { Project } from "@medisync/proto/medisync/identity/v1/identity_pb";
import { kioskClient, projectClient } from "../../api/client";
import { useAuth } from "../../auth/AuthContext";
import { Icon } from "../masterdata/icons";
import {
  MasterHeader, ListHeading, SearchInput, StatusBadge, MasterTable,
  MasterDrawer, DrawerSection, Field, SaveNotice, formatThaiDateTime, type Column, type Step,
} from "../masterdata/kit";

const KIO_STEPS: Step[] = [
  { num: "01", label: "ข้อมูลตู้ยา", icon: Icon.cabinet },
  { num: "02", label: "ความปลอดภัย", icon: Icon.link },
  { num: "03", label: "สถานะ", icon: Icon.checkCircle },
];

interface KioForm { code: string; name: string; displayName: string; projectId: string; pin: string; active: boolean; }
const emptyKio: KioForm = { code: "", name: "", displayName: "", projectId: "", pin: "", active: true };

export function DevicesPage() {
  const { user } = useAuth();
  const [kiosks, setKiosks] = useState<Kiosk[]>([]);
  const [projects, setProjects] = useState<Project[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [query, setQuery] = useState("");

  const [drawerOpen, setDrawerOpen] = useState(false);
  const [editingId, setEditingId] = useState<string | null>(null);
  const [kioForm, setKioForm] = useState<KioForm>(emptyKio);
  const [dirty, setDirty] = useState(false);
  const [activeStep, setActiveStep] = useState(0);
  const [saving, setSaving] = useState(false);
  const [formError, setFormError] = useState<string | null>(null);
  const [saveNotice, setSaveNotice] = useState<string | null>(null);
  const [editedAt, setEditedAt] = useState<Date>(new Date());
  const [pinResult, setPinResult] = useState<string | null>(null);
  const kioRefs = [useRef<HTMLDivElement>(null), useRef<HTMLDivElement>(null), useRef<HTMLDivElement>(null)];

  const load = useCallback(async () => {
    setLoading(true); setError(null);
    try {
      const k = await kioskClient.listKiosks(create(ListKiosksRequestSchema, {}));
      setKiosks(k.kiosks);
    } catch (e: unknown) { setError(e instanceof Error ? e.message : "โหลดข้อมูลอุปกรณ์ไม่สำเร็จ"); }
    finally { setLoading(false); }
  }, []);
  const loadProjects = useCallback(async () => {
    try { const res = await projectClient.listProjects(create(ListProjectsRequestSchema, {})); setProjects(res.projects.filter((p) => p.active)); }
    catch { /* non-critical */ }
  }, []);
  useEffect(() => { load(); loadProjects(); }, [load, loadProjects]);

  const projectName = useCallback((id: string) => projects.find((p) => p.id === id)?.name ?? "—", [projects]);
  const userName = user?.displayName || user?.username || "ผู้ใช้งาน";

  const kioFiltered = useMemo(() => {
    const q = query.trim().toLowerCase();
    return kiosks.filter((k) => !q || k.code.toLowerCase().includes(q) || (k.displayName || "").toLowerCase().includes(q) || (k.name || "").toLowerCase().includes(q));
  }, [kiosks, query]);

  function closeDrawer() { if (dirty && !confirm("มีการแก้ไขที่ยังไม่บันทึก ต้องการปิดหรือไม่?")) return; setDrawerOpen(false); setPinResult(null); }
  function goToStep(refs: React.RefObject<HTMLDivElement | null>[], i: number) { setActiveStep(i); refs[i].current?.scrollIntoView({ behavior: "smooth", block: "start" }); }

  function openKioCreate() { setDrawerOpen(true); setEditingId(null); setKioForm({ ...emptyKio, projectId: projects[0]?.id ?? "" }); reset(); }
  function openKioEdit(k: Kiosk) {
    setDrawerOpen(true); setEditingId(k.id);
    setKioForm({ code: k.code, name: k.name || "", displayName: k.displayName, projectId: k.projectId, pin: "", active: k.active });
    setEditedAt(new Date()); reset();
  }
  function reset() { setDirty(false); setActiveStep(0); setFormError(null); setPinResult(null); }

  function setKio<K extends keyof KioForm>(k: K, v: KioForm[K]) { setKioForm((f) => ({ ...f, [k]: v })); setDirty(true); }

  async function saveKiosk(e: FormEvent) {
    e.preventDefault(); setFormError(null);
    if (!kioForm.code.trim()) { setFormError("กรุณากรอกรหัสตู้ยา"); return; }
    if (!kioForm.displayName.trim()) { setFormError("กรุณากรอกชื่อที่แสดง"); return; }
    if (!editingId) {
      if (!kioForm.projectId) { setFormError("กรุณาเลือกโครงการ"); return; }
      if (!kioForm.pin || kioForm.pin.length < 4) { setFormError("PIN ต้องมีอย่างน้อย 4 หลัก"); return; }
    } else if (kioForm.pin && kioForm.pin.length < 4) {
      setFormError("PIN ใหม่ต้องมีอย่างน้อย 4 หลัก"); return;
    }
    setSaving(true);
    try {
      if (editingId) {
        await kioskClient.updateKiosk(create(UpdateKioskRequestSchema, { id: editingId, displayName: kioForm.displayName.trim(), name: kioForm.name.trim(), active: kioForm.active }));
        if (kioForm.pin) {
          const res = await kioskClient.resetKioskPin(create(ResetKioskPinRequestSchema, { id: editingId, newPin: kioForm.pin }));
          if (res.kiosk?.pin) { setPinResult(res.kiosk.pin); setSaving(false); await load(); return; }
        }
      } else {
        const res = await kioskClient.createKiosk(create(CreateKioskRequestSchema, {
          code: kioForm.code.trim(), displayName: kioForm.displayName.trim(), name: kioForm.name.trim(), pin: kioForm.pin, projectId: kioForm.projectId,
        }));
        if (res.kiosk?.pin) { setPinResult(res.kiosk.pin); setEditingId(res.kiosk.id); setSaving(false); await load(); return; }
      }
      setDrawerOpen(false); await load();
      setSaveNotice(editingId ? "บันทึกข้อมูลตู้ยาเรียบร้อยแล้ว" : "เพิ่มตู้ยาเรียบร้อยแล้ว");
      window.setTimeout(() => setSaveNotice(null), 4000);
    } catch (err: unknown) { setFormError(err instanceof Error ? err.message : "บันทึกไม่สำเร็จ"); }
    finally { setSaving(false); }
  }

  async function toggleKio(k: Kiosk) {
    if (!confirm(`${k.active ? "ปิด" : "เปิด"}การใช้งานตู้ยา "${k.displayName}" หรือไม่?`)) return;
    try { await kioskClient.updateKiosk(create(UpdateKioskRequestSchema, { id: k.id, active: !k.active })); await load(); }
    catch (err: unknown) { setError(err instanceof Error ? err.message : "อัปเดตไม่สำเร็จ"); }
  }

  const kioColumns: Column<Kiosk>[] = [
    { key: "code", header: "รหัสตู้ยา", render: (k) => <span className="md-code">{k.code}</span> },
    { key: "name", header: "ชื่อตู้", render: (k) => k.name || "—" },
    { key: "display", header: "ชื่อที่แสดง", render: (k) => k.displayName || "—" },
    { key: "project", header: "โครงการ", render: (k) => <span className="md-cell-muted">{projectName(k.projectId)}</span> },
    { key: "status", header: "สถานะ", render: (k) => <StatusBadge active={k.active} /> },
  ];

  const pinBanner = pinResult && (
    <div className="md-pin-banner">
      <Icon.checkCircle size={18} /> PIN คือ <strong>{pinResult}</strong> — แสดงเพียงครั้งเดียว โปรดบันทึกไว้
    </div>
  );

  return (
    <>
      <MasterHeader icon={Icon.cabinet} title="อุปกรณ์" subtitle="ตู้ยา (Master Data)">
        <button className="md-btn md-btn-primary" onClick={openKioCreate}><Icon.plus size={18} /> เพิ่มตู้ยา</button>
      </MasterHeader>
      <SaveNotice message={saveNotice} onDismiss={() => setSaveNotice(null)} />

      {error && <div className="md-err">{error}</div>}

      <div className="md-panel">
        <ListHeading icon={Icon.cabinet} title="รายการตู้ยา" count={kioFiltered.length} />
        <div className="md-toolbar">
          <SearchInput value={query} onChange={setQuery} placeholder="ค้นหารหัส หรือชื่อตู้ยา" />
          <button className="md-btn md-btn-primary" onClick={openKioCreate}><Icon.plus size={18} /> เพิ่มตู้ยา</button>
        </div>
        <MasterTable rows={kioFiltered} columns={kioColumns} getId={(k) => k.id} loading={loading} onEdit={openKioEdit} onArchive={toggleKio} emptyText="ไม่พบตู้ยา" />
      </div>

      {/* Kiosk drawer */}
      <MasterDrawer
        open={drawerOpen} icon={Icon.cabinet}
        title={editingId ? "แก้ไขตู้ยา" : "เพิ่มตู้ยา"} entityLabel="ตู้ยา" code={kioForm.code} dirty={dirty}
        steps={KIO_STEPS} activeStep={activeStep} onStep={(i) => goToStep(kioRefs, i)}
        onClose={closeDrawer} onSubmit={saveKiosk}
        onRestore={() => { setKioForm(editingId ? { ...kioForm, pin: "" } : { ...emptyKio, projectId: projects[0]?.id ?? "" }); setDirty(false); }}
        saving={saving} timeLabel={formatThaiDateTime(editedAt)}
      >
        {formError && <div className="md-err" style={{ margin: "0 0 16px" }}>{formError}</div>}
        {pinBanner}
        <DrawerSection num="01" icon={Icon.cabinet} title="ข้อมูลตู้ยา" refEl={kioRefs[0]}>
          <div className="md-grid2">
            <Field label="รหัสตู้ยา" required lead={<Icon.hash size={18} />}>
              <input value={kioForm.code} onChange={(e) => setKio("code", e.target.value)} placeholder="CAB-A01" disabled={!!editingId} />
            </Field>
            <Field label="ชื่อตู้ยา" lead={<span style={{ fontWeight: 700 }}>T</span>}>
              <input value={kioForm.name} onChange={(e) => setKio("name", e.target.value)} placeholder="ตู้ยาหอผู้ป่วย A" />
            </Field>
            <Field label="ชื่อที่แสดง" required lead={<Icon.monitor size={18} />}>
              <input value={kioForm.displayName} onChange={(e) => setKio("displayName", e.target.value)} placeholder="ตู้ A ชั้น 1" />
            </Field>
            <Field label="โครงการ" required lead={<Icon.folder size={18} />} trailingChevron>
              <select value={kioForm.projectId} onChange={(e) => setKio("projectId", e.target.value)} disabled={!!editingId}>
                <option value="">— เลือกโครงการ —</option>
                {projects.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
              </select>
            </Field>
          </div>
        </DrawerSection>
        <DrawerSection num="02" icon={Icon.link} title="ความปลอดภัย" refEl={kioRefs[1]}>
          <div className="md-grid2">
            <Field label={editingId ? "PIN ใหม่ (เว้นว่าง = ไม่เปลี่ยน)" : "PIN"} required={!editingId} lead={<Icon.link size={18} />}>
              <input value={kioForm.pin} onChange={(e) => setKio("pin", e.target.value)} placeholder="อย่างน้อย 4 หลัก" inputMode="numeric" />
            </Field>
          </div>
        </DrawerSection>
        <DrawerSection num="03" icon={Icon.checkCircle} title="สถานะ" green refEl={kioRefs[2]}>
          <div className="md-status-row">
            <div className={`md-toggle${kioForm.active ? " on" : ""}`} onClick={() => setKio("active", !kioForm.active)}>
              <span className="md-switch" /><span className="md-toggle-label">{kioForm.active ? "ใช้งาน" : "ปิดใช้งาน"}</span>
            </div>
            {kioForm.active && <span className="md-badge md-badge-soft"><Icon.checkCircle size={15} /> พร้อมใช้งาน</span>}
            <span className="md-status-meta">แก้ไขโดย <strong>{userName}</strong></span>
          </div>
        </DrawerSection>
      </MasterDrawer>
    </>
  );
}
