import { useState, useEffect, useCallback, useMemo, useRef, type FormEvent } from "react";
import { create } from "@bufbuild/protobuf";
import {
  ListCabinetsRequestSchema, CreateCabinetRequestSchema, UpdateCabinetRequestSchema,
} from "@medisync/proto/medisync/cabinet/v1/cabinet_pb";
import type { Cabinet } from "@medisync/proto/medisync/cabinet/v1/cabinet_pb";
import {
  ListKiosksRequestSchema, CreateKioskRequestSchema, UpdateKioskRequestSchema, ResetKioskPinRequestSchema,
} from "@medisync/proto/medisync/kiosk/v1/kiosk_pb";
import type { Kiosk } from "@medisync/proto/medisync/kiosk/v1/kiosk_pb";
import { ListProjectsRequestSchema } from "@medisync/proto/medisync/identity/v1/identity_pb";
import type { Project } from "@medisync/proto/medisync/identity/v1/identity_pb";
import { cabinetClient, kioskClient, projectClient } from "../../api/client";
import { useAuth } from "../../auth/AuthContext";
import { Icon } from "../masterdata/icons";
import {
  MasterHeader, ListHeading, SearchInput, StatusBadge, MasterTable,
  MasterDrawer, DrawerSection, Field, SaveNotice, formatThaiDateTime, type Column, type Step,
} from "../masterdata/kit";

type Tab = "cabinets" | "kiosks";

const CAB_STEPS: Step[] = [
  { num: "01", label: "ข้อมูลตู้ยา", icon: Icon.cabinet },
  { num: "02", label: "สถานะ", icon: Icon.checkCircle },
];
const KIO_STEPS: Step[] = [
  { num: "01", label: "ข้อมูล Kiosk", icon: Icon.monitor },
  { num: "02", label: "ความปลอดภัย", icon: Icon.link },
  { num: "03", label: "สถานะ", icon: Icon.checkCircle },
];

interface CabForm { code: string; name: string; displayName: string; projectId: string; active: boolean; }
interface KioForm { code: string; displayName: string; projectId: string; pin: string; active: boolean; }
const emptyCab: CabForm = { code: "", name: "", displayName: "", projectId: "", active: true };
const emptyKio: KioForm = { code: "", displayName: "", projectId: "", pin: "", active: true };

export function DevicesPage() {
  const { user } = useAuth();
  const [tab, setTab] = useState<Tab>("cabinets");
  const [cabinets, setCabinets] = useState<Cabinet[]>([]);
  const [kiosks, setKiosks] = useState<Kiosk[]>([]);
  const [projects, setProjects] = useState<Project[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [query, setQuery] = useState("");

  // drawer (one at a time)
  const [drawerKind, setDrawerKind] = useState<Tab | null>(null);
  const [editingId, setEditingId] = useState<string | null>(null);
  const [cabForm, setCabForm] = useState<CabForm>(emptyCab);
  const [kioForm, setKioForm] = useState<KioForm>(emptyKio);
  const [dirty, setDirty] = useState(false);
  const [activeStep, setActiveStep] = useState(0);
  const [saving, setSaving] = useState(false);
  const [formError, setFormError] = useState<string | null>(null);
  const [saveNotice, setSaveNotice] = useState<string | null>(null);
  const [editedAt, setEditedAt] = useState<Date>(new Date());
  const [pinResult, setPinResult] = useState<string | null>(null);
  const cabRefs = [useRef<HTMLDivElement>(null), useRef<HTMLDivElement>(null)];
  const kioRefs = [useRef<HTMLDivElement>(null), useRef<HTMLDivElement>(null), useRef<HTMLDivElement>(null)];

  const load = useCallback(async () => {
    setLoading(true); setError(null);
    try {
      const [c, k] = await Promise.all([
        cabinetClient.listCabinets(create(ListCabinetsRequestSchema, {})),
        kioskClient.listKiosks(create(ListKiosksRequestSchema, {})),
      ]);
      setCabinets(c.cabinets); setKiosks(k.kiosks);
    } catch (e: unknown) { setError(e instanceof Error ? e.message : "โหลดข้อมูลอุปกรณ์ไม่สำเร็จ"); }
    finally { setLoading(false); }
  }, []);
  const loadProjects = useCallback(async () => {
    try { const res = await projectClient.listProjects(create(ListProjectsRequestSchema, {})); setProjects(res.projects.filter((p) => p.active)); }
    catch { /* non-critical */ }
  }, []);
  useEffect(() => { load(); loadProjects(); }, [load, loadProjects]);

  useEffect(() => { setQuery(""); }, [tab]);

  const projectName = useCallback((id: string) => projects.find((p) => p.id === id)?.name ?? "—", [projects]);
  const userName = user?.displayName || user?.username || "ผู้ใช้งาน";

  const cabFiltered = useMemo(() => {
    const q = query.trim().toLowerCase();
    return cabinets.filter((c) => !q || c.code.toLowerCase().includes(q) || c.name.toLowerCase().includes(q) || c.displayName.toLowerCase().includes(q));
  }, [cabinets, query]);
  const kioFiltered = useMemo(() => {
    const q = query.trim().toLowerCase();
    return kiosks.filter((k) => !q || k.code.toLowerCase().includes(q) || k.displayName.toLowerCase().includes(q));
  }, [kiosks, query]);

  function closeDrawer() { if (dirty && !confirm("มีการแก้ไขที่ยังไม่บันทึก ต้องการปิดหรือไม่?")) return; setDrawerKind(null); setPinResult(null); }
  function goToStep(refs: React.RefObject<HTMLDivElement | null>[], i: number) { setActiveStep(i); refs[i].current?.scrollIntoView({ behavior: "smooth", block: "start" }); }

  // ── Cabinet drawer ──────────────────────────────────────────────
  function openCabCreate() { setDrawerKind("cabinets"); setEditingId(null); setCabForm({ ...emptyCab, projectId: projects[0]?.id ?? "" }); reset(); }
  function openCabEdit(c: Cabinet) {
    setDrawerKind("cabinets"); setEditingId(c.id);
    setCabForm({ code: c.code, name: c.name, displayName: c.displayName, projectId: c.projectId, active: c.active });
    setEditedAt(c.updatedAt ? new Date(Number(c.updatedAt.seconds) * 1000) : new Date()); reset();
  }
  // ── Kiosk drawer ────────────────────────────────────────────────
  function openKioCreate() { setDrawerKind("kiosks"); setEditingId(null); setKioForm({ ...emptyKio, projectId: projects[0]?.id ?? "" }); reset(); }
  function openKioEdit(k: Kiosk) {
    setDrawerKind("kiosks"); setEditingId(k.id);
    setKioForm({ code: k.code, displayName: k.displayName, projectId: k.projectId, pin: "", active: k.active });
    setEditedAt(new Date()); reset();
  }
  function reset() { setDirty(false); setActiveStep(0); setFormError(null); setPinResult(null); }

  function setCab<K extends keyof CabForm>(k: K, v: CabForm[K]) { setCabForm((f) => ({ ...f, [k]: v })); setDirty(true); }
  function setKio<K extends keyof KioForm>(k: K, v: KioForm[K]) { setKioForm((f) => ({ ...f, [k]: v })); setDirty(true); }

  async function saveCabinet(e: FormEvent) {
    e.preventDefault(); setFormError(null);
    if (!cabForm.code.trim()) { setFormError("กรุณากรอกรหัสตู้"); return; }
    if (!cabForm.name.trim()) { setFormError("กรุณากรอกชื่อตู้"); return; }
    if (!editingId && !cabForm.projectId) { setFormError("กรุณาเลือกโครงการ"); return; }
    setSaving(true);
    try {
      if (editingId) {
        await cabinetClient.updateCabinet(create(UpdateCabinetRequestSchema, { id: editingId, name: cabForm.name.trim(), active: cabForm.active }));
      } else {
        await cabinetClient.createCabinet(create(CreateCabinetRequestSchema, {
          code: cabForm.code.trim(), name: cabForm.name.trim(), displayName: cabForm.displayName.trim() || cabForm.name.trim(), projectId: cabForm.projectId,
        }));
      }
      setDrawerKind(null); await load();
      setSaveNotice(editingId ? "บันทึกข้อมูลตู้ยาเรียบร้อยแล้ว" : "เพิ่มตู้ยาเรียบร้อยแล้ว");
      window.setTimeout(() => setSaveNotice(null), 4000);
    } catch (err: unknown) { setFormError(err instanceof Error ? err.message : "บันทึกไม่สำเร็จ"); }
    finally { setSaving(false); }
  }
  async function saveKiosk(e: FormEvent) {
    e.preventDefault(); setFormError(null);
    if (!kioForm.code.trim()) { setFormError("กรุณากรอกรหัส Kiosk"); return; }
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
        await kioskClient.updateKiosk(create(UpdateKioskRequestSchema, { id: editingId, displayName: kioForm.displayName.trim(), active: kioForm.active }));
        if (kioForm.pin) {
          const res = await kioskClient.resetKioskPin(create(ResetKioskPinRequestSchema, { id: editingId, newPin: kioForm.pin }));
          if (res.kiosk?.pin) { setPinResult(res.kiosk.pin); setSaving(false); await load(); return; }
        }
      } else {
        const res = await kioskClient.createKiosk(create(CreateKioskRequestSchema, {
          code: kioForm.code.trim(), displayName: kioForm.displayName.trim(), pin: kioForm.pin, projectId: kioForm.projectId,
        }));
        if (res.kiosk?.pin) { setPinResult(res.kiosk.pin); setEditingId(res.kiosk.id); setSaving(false); await load(); return; }
      }
      setDrawerKind(null); await load();
      setSaveNotice(editingId ? "บันทึกข้อมูล Kiosk เรียบร้อยแล้ว" : "เพิ่ม Kiosk เรียบร้อยแล้ว");
      window.setTimeout(() => setSaveNotice(null), 4000);
    } catch (err: unknown) { setFormError(err instanceof Error ? err.message : "บันทึกไม่สำเร็จ"); }
    finally { setSaving(false); }
  }

  async function toggleCab(c: Cabinet) {
    if (!confirm(`${c.active ? "ปิด" : "เปิด"}การใช้งานตู้ "${c.name}" หรือไม่?`)) return;
    try { await cabinetClient.updateCabinet(create(UpdateCabinetRequestSchema, { id: c.id, active: !c.active })); await load(); }
    catch (err: unknown) { setError(err instanceof Error ? err.message : "อัปเดตไม่สำเร็จ"); }
  }
  async function toggleKio(k: Kiosk) {
    if (!confirm(`${k.active ? "ปิด" : "เปิด"}การใช้งาน Kiosk "${k.displayName}" หรือไม่?`)) return;
    try { await kioskClient.updateKiosk(create(UpdateKioskRequestSchema, { id: k.id, active: !k.active })); await load(); }
    catch (err: unknown) { setError(err instanceof Error ? err.message : "อัปเดตไม่สำเร็จ"); }
  }

  const cabColumns: Column<Cabinet>[] = [
    { key: "code", header: "รหัสตู้", render: (c) => <span className="md-code">{c.code}</span> },
    { key: "name", header: "ชื่อตู้", render: (c) => c.name },
    { key: "display", header: "ชื่อที่แสดง", render: (c) => c.displayName || "—" },
    { key: "project", header: "โครงการ", render: (c) => <span className="md-cell-muted">{projectName(c.projectId)}</span> },
    { key: "status", header: "สถานะ", render: (c) => <StatusBadge active={c.active} /> },
  ];
  const kioColumns: Column<Kiosk>[] = [
    { key: "code", header: "รหัส Kiosk", render: (k) => <span className="md-code">{k.code}</span> },
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
      <MasterHeader icon={Icon.cabinet} title="อุปกรณ์" subtitle="ตู้ยา และ Kiosk (Master Data)">
        {tab === "cabinets"
          ? <button className="md-btn md-btn-primary" onClick={openCabCreate}><Icon.plus size={18} /> เพิ่มตู้ยา</button>
          : <button className="md-btn md-btn-primary" onClick={openKioCreate}><Icon.plus size={18} /> เพิ่ม Kiosk</button>}
      </MasterHeader>
      <SaveNotice message={saveNotice} onDismiss={() => setSaveNotice(null)} />

      {error && <div className="md-err">{error}</div>}

      <div className="md-panel">
        <ListHeading icon={tab === "cabinets" ? Icon.cabinet : Icon.monitor}
          title={tab === "cabinets" ? "รายการตู้ยา" : "รายการ Kiosk"}
          count={tab === "cabinets" ? cabFiltered.length : kioFiltered.length} />
        <div className="md-toolbar">
          <div className="md-segment">
            <button className={`md-seg-btn${tab === "cabinets" ? " active" : ""}`} onClick={() => setTab("cabinets")}><Icon.cabinet size={15} /> ตู้ยา <span className="md-seg-num">{cabinets.length}</span></button>
            <button className={`md-seg-btn${tab === "kiosks" ? " active" : ""}`} onClick={() => setTab("kiosks")}><Icon.monitor size={15} /> Kiosk <span className="md-seg-num">{kiosks.length}</span></button>
          </div>
          <SearchInput value={query} onChange={setQuery} placeholder={tab === "cabinets" ? "ค้นหารหัส หรือชื่อตู้" : "ค้นหารหัส หรือชื่อ Kiosk"} />
          {tab === "cabinets"
            ? <button className="md-btn md-btn-primary" onClick={openCabCreate}><Icon.plus size={18} /> เพิ่มตู้ยา</button>
            : <button className="md-btn md-btn-primary" onClick={openKioCreate}><Icon.plus size={18} /> เพิ่ม Kiosk</button>}
        </div>
        {tab === "cabinets" ? (
          <MasterTable rows={cabFiltered} columns={cabColumns} getId={(c) => c.id} loading={loading} onEdit={openCabEdit} onArchive={toggleCab} emptyText="ไม่พบตู้ยา" />
        ) : (
          <MasterTable rows={kioFiltered} columns={kioColumns} getId={(k) => k.id} loading={loading} onEdit={openKioEdit} onArchive={toggleKio} emptyText="ไม่พบ Kiosk" />
        )}
      </div>

      {/* Cabinet drawer */}
      <MasterDrawer
        open={drawerKind === "cabinets"} icon={Icon.cabinet}
        title={editingId ? "แก้ไขตู้ยา" : "เพิ่มตู้ยา"} entityLabel="ตู้ยา" code={cabForm.code} dirty={dirty}
        steps={CAB_STEPS} activeStep={activeStep} onStep={(i) => goToStep(cabRefs, i)}
        onClose={closeDrawer} onSubmit={saveCabinet}
        onRestore={() => { setCabForm(editingId ? cabForm : { ...emptyCab, projectId: projects[0]?.id ?? "" }); setDirty(false); }}
        saving={saving} timeLabel={formatThaiDateTime(editedAt)}
      >
        {formError && <div className="md-err" style={{ margin: "0 0 16px" }}>{formError}</div>}
        <DrawerSection num="01" icon={Icon.cabinet} title="ข้อมูลตู้ยา" refEl={cabRefs[0]}>
          <div className="md-grid2">
            <Field label="รหัสตู้" required lead={<Icon.hash size={18} />}>
              <input value={cabForm.code} onChange={(e) => setCab("code", e.target.value)} placeholder="CAB-A01" disabled={!!editingId} />
            </Field>
            <Field label="ชื่อตู้" required lead={<span style={{ fontWeight: 700 }}>T</span>}>
              <input value={cabForm.name} onChange={(e) => setCab("name", e.target.value)} placeholder="ตู้ยาหอผู้ป่วย A" />
            </Field>
            <Field label="ชื่อที่แสดง" lead={<Icon.monitor size={18} />}>
              <input value={cabForm.displayName} onChange={(e) => setCab("displayName", e.target.value)} placeholder="ตู้ A ชั้น 1" disabled={!!editingId} />
            </Field>
            <Field label="โครงการ" required lead={<Icon.folder size={18} />} trailingChevron>
              <select value={cabForm.projectId} onChange={(e) => setCab("projectId", e.target.value)} disabled={!!editingId}>
                <option value="">— เลือกโครงการ —</option>
                {projects.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
              </select>
            </Field>
          </div>
        </DrawerSection>
        <DrawerSection num="02" icon={Icon.checkCircle} title="สถานะ" green refEl={cabRefs[1]}>
          <div className="md-status-row">
            <div className={`md-toggle${cabForm.active ? " on" : ""}`} onClick={() => setCab("active", !cabForm.active)}>
              <span className="md-switch" /><span className="md-toggle-label">{cabForm.active ? "ใช้งาน" : "ปิดใช้งาน"}</span>
            </div>
            {cabForm.active && <span className="md-badge md-badge-soft"><Icon.checkCircle size={15} /> พร้อมใช้งาน</span>}
            <span className="md-status-meta">แก้ไขโดย <strong>{userName}</strong></span>
          </div>
        </DrawerSection>
      </MasterDrawer>

      {/* Kiosk drawer */}
      <MasterDrawer
        open={drawerKind === "kiosks"} icon={Icon.monitor}
        title={editingId ? "แก้ไข Kiosk" : "เพิ่ม Kiosk"} entityLabel="Kiosk" code={kioForm.code} dirty={dirty}
        steps={KIO_STEPS} activeStep={activeStep} onStep={(i) => goToStep(kioRefs, i)}
        onClose={closeDrawer} onSubmit={saveKiosk}
        onRestore={() => { setKioForm(editingId ? { ...kioForm, pin: "" } : { ...emptyKio, projectId: projects[0]?.id ?? "" }); setDirty(false); }}
        saving={saving} timeLabel={formatThaiDateTime(editedAt)}
      >
        {formError && <div className="md-err" style={{ margin: "0 0 16px" }}>{formError}</div>}
        {pinBanner}
        <DrawerSection num="01" icon={Icon.monitor} title="ข้อมูล Kiosk" refEl={kioRefs[0]}>
          <div className="md-grid2">
            <Field label="รหัส Kiosk" required lead={<Icon.hash size={18} />}>
              <input value={kioForm.code} onChange={(e) => setKio("code", e.target.value)} placeholder="KIOSK-01" disabled={!!editingId} />
            </Field>
            <Field label="ชื่อที่แสดง" required lead={<span style={{ fontWeight: 700 }}>T</span>}>
              <input value={kioForm.displayName} onChange={(e) => setKio("displayName", e.target.value)} placeholder="Kiosk หน้าห้องยา" />
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
