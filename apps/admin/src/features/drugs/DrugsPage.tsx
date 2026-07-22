import { useState, useEffect, useCallback, useMemo, useRef, type FormEvent } from "react";
import { useNavigate } from "react-router-dom";
import { create } from "@bufbuild/protobuf";
import {
  DrugSchema,
  CreateDrugRequestSchema,
  UpdateDrugRequestSchema,
  DeactivateDrugRequestSchema,
  ListDrugsRequestSchema,
} from "@medisync/proto/medisync/catalog/v1/catalog_pb";
import type { Drug } from "@medisync/proto/medisync/catalog/v1/catalog_pb";
import { ListProjectsRequestSchema } from "@medisync/proto/medisync/identity/v1/identity_pb";
import type { Project } from "@medisync/proto/medisync/identity/v1/identity_pb";
import { catalogClient, projectClient } from "../../api/client";
import { useAuth } from "../../auth/AuthContext";
import { Icon } from "../masterdata/icons";
import {
  MasterHeader, ListHeading, SearchInput, Select, StatusBadge, MasterTable,
  MasterDrawer, DrawerSection, Field, SaveNotice, formatThaiDateTime, type Column, type Step,
} from "../masterdata/kit";

const FORM_OPTIONS = ["Tablet", "Capsule", "Syrup", "Injection", "Cream", "Drops"];
const UNIT_OPTIONS = ["เม็ด", "แคปซูล", "ขวด", "หลอด", "หน่วย", "ampoule"];
const SAFETY_OPTIONS = [
  { value: "NORMAL", label: "ปกติ" },
  { value: "LASA", label: "LASA" },
  { value: "HIGH_ALERT", label: "ยาอันตราย / High Alert" },
] as const;

const STEPS: Step[] = [
  { num: "01", label: "ข้อมูลยา", icon: Icon.pill },
  { num: "02", label: "การแสดงผล", icon: Icon.monitor },
  { num: "03", label: "การเชื่อมโยง", icon: Icon.link },
  { num: "04", label: "สถานะ", icon: Icon.checkCircle },
];

interface DrugFormData {
  code: string; name: string; displayName: string; genericName: string;
  form: string; strength: string; unit: string; barcode: string;
  stickerNote: string; projectId: string; active: boolean;
  defaultSlotCapacity: number;
  category: string; manufacturer: string;
  safetyClassification: "NORMAL" | "LASA" | "HIGH_ALERT";
}
const emptyForm: DrugFormData = {
  code: "", name: "", displayName: "", genericName: "",
  form: "Tablet", strength: "", unit: "เม็ด", barcode: "",
  stickerNote: "", projectId: "", active: true,
  defaultSlotCapacity: 100,
  category: "", manufacturer: "", safetyClassification: "NORMAL",
};
function drugToForm(d: Drug): DrugFormData {
  return {
    code: d.code, name: d.name, displayName: d.displayName, genericName: d.genericName,
    form: d.form, strength: d.strength, unit: d.unit, barcode: d.barcode,
    stickerNote: d.stickerNote, projectId: d.projectId, active: d.active,
    defaultSlotCapacity: d.defaultSlotCapacity || 100,
    category: d.category, manufacturer: d.manufacturer,
    safetyClassification: (d.safetyClassification || "NORMAL") as DrugFormData["safetyClassification"],
  };
}

export function DrugsPage() {
  const navigate = useNavigate();
  const { user } = useAuth();
  const [drugs, setDrugs] = useState<Drug[]>([]);
  const [projects, setProjects] = useState<Project[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const [query, setQuery] = useState("");
  const [statusFilter, setStatusFilter] = useState<"all" | "active">("all");
  const [projectFilter, setProjectFilter] = useState("");

  const [drawerOpen, setDrawerOpen] = useState(false);
  const [editingId, setEditingId] = useState<string | null>(null);
  const [form, setForm] = useState<DrugFormData>(emptyForm);
  const [dirty, setDirty] = useState(false);
  const [activeStep, setActiveStep] = useState(0);
  const [saving, setSaving] = useState(false);
  const [formError, setFormError] = useState<string | null>(null);
  const [saveNotice, setSaveNotice] = useState<string | null>(null);
  const [editedAt, setEditedAt] = useState<Date>(new Date());
  const sectionRefs = [useRef<HTMLDivElement>(null), useRef<HTMLDivElement>(null), useRef<HTMLDivElement>(null), useRef<HTMLDivElement>(null)];

  const fetchDrugs = useCallback(async () => {
    setLoading(true); setError(null);
    try {
      const res = await catalogClient.listDrugs(create(ListDrugsRequestSchema, { query: "", pageSize: 500, includeInactive: true }));
      setDrugs(res.drugs);
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : "โหลดรายการยาไม่สำเร็จ");
    } finally { setLoading(false); }
  }, []);
  const fetchProjects = useCallback(async () => {
    try {
      const res = await projectClient.listProjects(create(ListProjectsRequestSchema, {}));
      setProjects(res.projects.filter((p) => p.active));
    } catch { /* non-critical */ }
  }, []);
  useEffect(() => { fetchDrugs(); fetchProjects(); }, [fetchDrugs, fetchProjects]);

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    return drugs.filter((d) => {
      if (statusFilter === "active" && !d.active) return false;
      if (projectFilter && d.projectId !== projectFilter) return false;
      if (!q) return true;
      return d.code.toLowerCase().includes(q) || d.name.toLowerCase().includes(q)
        || d.genericName.toLowerCase().includes(q) || d.displayName.toLowerCase().includes(q);
    });
  }, [drugs, query, statusFilter, projectFilter]);

  const activeCount = useMemo(() => drugs.filter((d) => d.active).length, [drugs]);
  const projectName = useCallback((id: string) => projects.find((p) => p.id === id)?.name ?? "—", [projects]);
  const userName = user?.displayName || user?.username || "ผู้ใช้งาน";

  function openCreate() {
    setEditingId(null); setForm({ ...emptyForm, projectId: projects[0]?.id ?? "" });
    setDirty(false); setActiveStep(0); setFormError(null); setEditedAt(new Date()); setDrawerOpen(true);
  }
  function openEdit(d: Drug) {
    setEditingId(d.id); setForm(drugToForm(d));
    setDirty(false); setActiveStep(0); setFormError(null);
    setEditedAt(d.updatedAt ? new Date(Number(d.updatedAt.seconds) * 1000) : new Date());
    setDrawerOpen(true);
  }
  function openDuplicate(d: Drug) { openEdit(d); setEditingId(null); setDirty(true); }
  function closeDrawer() {
    if (dirty && !confirm("มีการแก้ไขที่ยังไม่บันทึก ต้องการปิดหรือไม่?")) return;
    setDrawerOpen(false);
  }
  function setField<K extends keyof DrugFormData>(k: K, v: DrugFormData[K]) { setForm((f) => ({ ...f, [k]: v })); setDirty(true); }
  function goToStep(i: number) { setActiveStep(i); sectionRefs[i].current?.scrollIntoView({ behavior: "smooth", block: "start" }); }
  function restore() {
    if (editingId) { const d = drugs.find((x) => x.id === editingId); if (d) { setForm(drugToForm(d)); setDirty(false); } }
    else { setForm({ ...emptyForm, projectId: projects[0]?.id ?? "" }); setDirty(false); }
  }

  async function handleSave(e: FormEvent) {
    e.preventDefault(); setFormError(null);
    if (!form.code.trim()) { setFormError("กรุณากรอกรหัสยา"); return; }
    if (!form.name.trim()) { setFormError("กรุณากรอกชื่อยา"); return; }
    if (!form.displayName.trim()) { setFormError("กรุณากรอกชื่อที่แสดงบน Kiosk"); return; }
    if (!Number.isInteger(form.defaultSlotCapacity) || form.defaultSlotCapacity <= 0) {
      setFormError("ความจุมาตรฐานต่อช่องต้องเป็นจำนวนเต็มมากกว่า 0"); return;
    }
    setSaving(true);
    try {
      if (editingId) {
        await catalogClient.updateDrug(create(UpdateDrugRequestSchema, {
          drug: create(DrugSchema, {
            id: editingId, code: form.code.trim(), name: form.name.trim(), displayName: form.displayName.trim(),
            genericName: form.genericName.trim(), form: form.form.trim(), strength: form.strength.trim(),
            unit: form.unit.trim(), barcode: form.barcode.trim(), stickerNote: form.stickerNote.trim(), active: form.active,
            defaultSlotCapacity: form.defaultSlotCapacity,
            category: form.category.trim(), manufacturer: form.manufacturer.trim(),
            safetyClassification: form.safetyClassification,
          }),
        }));
      } else {
        await catalogClient.createDrug(create(CreateDrugRequestSchema, {
          code: form.code.trim(), name: form.name.trim(), displayName: form.displayName.trim() || form.name.trim(),
          genericName: form.genericName.trim(), form: form.form.trim(), strength: form.strength.trim(),
          unit: form.unit.trim(), barcode: form.barcode.trim(), stickerNote: form.stickerNote.trim(),
          projectId: form.projectId.trim() || undefined,
          defaultSlotCapacity: form.defaultSlotCapacity,
          category: form.category.trim(), manufacturer: form.manufacturer.trim(),
          safetyClassification: form.safetyClassification,
        }));
      }
      setDrawerOpen(false); await fetchDrugs();
      setSaveNotice(editingId ? "บันทึกข้อมูลยาเรียบร้อยแล้ว" : "เพิ่มยาใหม่เรียบร้อยแล้ว");
      window.setTimeout(() => setSaveNotice(null), 4000);
    } catch (err: unknown) {
      setFormError(err instanceof Error ? err.message : "บันทึกไม่สำเร็จ");
    } finally { 
      setSaving(false); 
      setDirty(false);
    }
  }
  async function handleArchive(d: Drug) {
    if (!confirm(`ปิดการใช้งาน "${d.name}" หรือไม่?`)) return;
    try { await catalogClient.deactivateDrug(create(DeactivateDrugRequestSchema, { id: d.id })); await fetchDrugs(); }
    catch (err: unknown) { setError(err instanceof Error ? err.message : "ปิดการใช้งานไม่สำเร็จ"); }
  }

  const columns: Column<Drug>[] = [
    { key: "code", header: "รหัส", render: (d) => <span className="md-code">{d.code}</span> },
    { key: "name", header: "ชื่อยา", render: (d) => d.name },
    { key: "display", header: "ชื่อที่แสดง", render: (d) => d.displayName || "—" },
    { key: "form", header: "รูปแบบ", render: (d) => d.form || "—" },
    { key: "strength", header: "ความแรง", render: (d) => d.strength || "—" },
    { key: "unit", header: "หน่วย", render: (d) => d.unit || "—" },
    { key: "capacity", header: "ความจุ/ช่อง", render: (d) => <strong>{d.defaultSlotCapacity || "—"}</strong> },
    { key: "safety", header: "ประเภทความเสี่ยง", render: (d) => {
      const safety = d.safetyClassification || "NORMAL";
      const label = safety === "LASA" ? "LASA" : safety === "HIGH_ALERT" ? "High Alert" : "ปกติ";
      return <span className={`md-safety-badge md-safety-badge--${safety.toLowerCase().replace("_", "-")}`}>{label}</span>;
    } },
    { key: "project", header: "โครงการ", render: (d) => <span className="md-cell-muted">{projectName(d.projectId)}</span> },
    { key: "status", header: "สถานะ", render: (d) => <StatusBadge active={d.active} /> },
  ];

  const previewName = form.displayName.trim() || form.name.trim() || "ชื่อยา";
  const previewMeta = [form.code || "รหัส", form.strength, form.unit].filter(Boolean).join(" · ");

  return (
    <>
      <MasterHeader icon={Icon.pill} title="ยา" subtitle="จัดการรายการยาในระบบ (Master Data)">
        <button className="md-btn md-btn-outline" disabled title="ฟังก์ชันนำเข้ายังไม่เปิดใช้งาน"><Icon.upload size={18} /> นำเข้า</button>
        <button className="md-btn md-btn-outline" disabled title="ฟังก์ชันส่งออกยังไม่เปิดใช้งาน"><Icon.download size={18} /> ส่งออก</button>
        <button className="md-btn md-btn-primary" onClick={openCreate}><Icon.plus size={18} /> เพิ่มยา</button>
      </MasterHeader>
      <SaveNotice message={saveNotice} onDismiss={() => setSaveNotice(null)} />

      {error && <div className="md-err">{error}</div>}

      <div className="md-panel">
        <ListHeading icon={Icon.pill} title="รายการยา" count={filtered.length} />
        <div className="md-toolbar">
          <SearchInput value={query} onChange={setQuery} placeholder="ค้นหารหัส ชื่อยา หรือชื่อสามัญ" />
          <div className="md-segment">
            <button className={`md-seg-btn${statusFilter === "all" ? " active" : ""}`} onClick={() => setStatusFilter("all")}>ทั้งหมด <span className="md-seg-num">{drugs.length}</span></button>
            <button className={`md-seg-btn${statusFilter === "active" ? " active" : ""}`} onClick={() => setStatusFilter("active")}>ใช้งาน <span className="md-seg-num">{activeCount}</span></button>
          </div>
          <Select value={projectFilter} onChange={setProjectFilter}>
            <option value="">ทุกโครงการ</option>
            {projects.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
          </Select>
          {/* <button className="md-btn md-btn-primary" onClick={openCreate}><Icon.plus size={18} /> เพิ่มยา</button> */}
        </div>
        <MasterTable rows={filtered} columns={columns} getId={(d) => d.id} loading={loading}
          onEdit={openEdit} onDuplicate={openDuplicate} onArchive={handleArchive} emptyText="ไม่พบรายการยา" />
      </div>

      <MasterDrawer
        open={drawerOpen} icon={Icon.pill}
        title={editingId ? "แก้ไขข้อมูลยา" : "เพิ่มข้อมูลยา"}
        entityLabel="ยา" code={form.code} dirty={dirty}
        steps={STEPS} activeStep={activeStep} onStep={goToStep}
        onClose={closeDrawer} onSubmit={handleSave} onRestore={restore}
        saving={saving} timeLabel={formatThaiDateTime(editedAt)}
      >
        {formError && <div className="md-err" style={{ margin: "0 0 16px" }}>{formError}</div>}

        <DrawerSection num="01" icon={Icon.pill} title="ข้อมูลยา" refEl={sectionRefs[0]}>
          <div className="md-grid3">
            <Field label="รหัสยา" required lead={<Icon.hash size={18} />}>
              <input value={form.code} onChange={(e) => setField("code", e.target.value)} placeholder="PARA500" />
            </Field>
            <Field label="ชื่อยา" required lead={<span style={{ fontWeight: 700 }}>T</span>}>
              <input value={form.name} onChange={(e) => setField("name", e.target.value)} placeholder="Paracetamol 500 mg" />
            </Field>
            <Field label="ชื่อสามัญ" lead={<Icon.flask size={18} />}>
              <input value={form.genericName} onChange={(e) => setField("genericName", e.target.value)} placeholder="Acetaminophen" />
            </Field>
            <Field label="รูปแบบยา" lead={<Icon.pill size={18} />} trailingChevron>
              <select value={form.form} onChange={(e) => setField("form", e.target.value)}>
                {(FORM_OPTIONS.includes(form.form) ? FORM_OPTIONS : [form.form, ...FORM_OPTIONS]).map((o) => <option key={o} value={o}>{o}</option>)}
              </select>
            </Field>
            <Field label="ความแรง" lead={<Icon.gauge size={18} />}>
              <input value={form.strength} onChange={(e) => setField("strength", e.target.value)} placeholder="500 mg" />
            </Field>
            <Field label="หน่วย" lead={<Icon.box size={18} />} trailingChevron>
              <select value={form.unit} onChange={(e) => setField("unit", e.target.value)}>
                {(UNIT_OPTIONS.includes(form.unit) ? UNIT_OPTIONS : [form.unit, ...UNIT_OPTIONS]).map((o) => <option key={o} value={o}>{o}</option>)}
              </select>
            </Field>
            <Field
              label="ความจุมาตรฐานต่อช่อง"
              required
              lead={<Icon.inventory size={18} />}
              helpId="default-slot-capacity-help"
              help="ใช้เป็นค่าเริ่มต้นเมื่อผูกยานี้กับช่องใหม่ ไม่เปลี่ยนช่องที่มีอยู่แล้ว"
            >
              <input
                type="number"
                min={1}
                step={1}
                value={form.defaultSlotCapacity}
                onChange={(e) => setField("defaultSlotCapacity", Number.parseInt(e.target.value, 10) || 0)}
                aria-describedby="default-slot-capacity-help"
              />
            </Field>
            <Field label="หมวดหมู่ยา" lead={<Icon.folder size={18} />}>
              <input value={form.category} onChange={(e) => setField("category", e.target.value)} placeholder="เช่น ยาแก้ปวด, ยาปฏิชีวนะ" />
            </Field>
            <Field label="ผู้ผลิต" lead={<Icon.box size={18} />}>
              <input value={form.manufacturer} onChange={(e) => setField("manufacturer", e.target.value)} placeholder="ชื่อบริษัทผู้ผลิต" />
            </Field>
            <Field label="ประเภทความเสี่ยง" required lead={<Icon.gauge size={18} />} trailingChevron>
              <select
                aria-label="ประเภทความเสี่ยง"
                value={form.safetyClassification}
                onChange={(e) => setField("safetyClassification", e.target.value as DrugFormData["safetyClassification"])}
              >
                {SAFETY_OPTIONS.map((option) => <option key={option.value} value={option.value}>{option.label}</option>)}
              </select>
            </Field>
          </div>
        </DrawerSection>

        <DrawerSection num="02" icon={Icon.monitor} title="การแสดงผล" refEl={sectionRefs[1]}>
          <div className="md-display-layout">
            <div>
              <Field label="ชื่อที่แสดงบน Kiosk" required highlight>
                <input value={form.displayName} onChange={(e) => setField("displayName", e.target.value)} placeholder="พาราเซตามอล 500 มก." />
              </Field>
              <div style={{ height: 16 }} />
              <div className="md-field no-icon" style={{ marginBottom: 16 }}>
                <label>บาร์โค้ด</label>
                <div className="md-input-inline">
                  <div className="md-input-wrap">
                    <span className="md-lead"><Icon.barcode size={18} /></span>
                    <input value={form.barcode} onChange={(e) => setField("barcode", e.target.value)} placeholder="8850000123456" />
                  </div>
                  <button type="button" className="md-btn md-btn-ghost" disabled title="ยังไม่ได้เชื่อมต่อเครื่องสแกนในหน้า Admin"><Icon.scan size={18} /> สแกน</button>
                </div>
              </div>
              <div className="md-field no-icon">
                <label>คำแนะนำบนฉลาก</label>
                <textarea value={form.stickerNote} onChange={(e) => setField("stickerNote", e.target.value)} placeholder="รับประทานครั้งละ 1 เม็ด หลังอาหาร" />
              </div>
            </div>
            <div className="md-preview">
              <div className="md-preview-head"><Icon.monitor size={16} /> ตัวอย่างหน้าจอ</div>
              <div className="md-preview-card">
                <div className="md-preview-name">{previewName}</div>
                <div className="md-preview-meta">{previewMeta}</div>
                <div className="md-preview-badge"><Icon.checkCircle size={16} /> {form.active ? "พร้อมใช้งาน" : "ปิดใช้งาน"}</div>
              </div>
            </div>
          </div>
        </DrawerSection>

        <DrawerSection num="03" icon={Icon.link} title="การเชื่อมโยง" refEl={sectionRefs[2]}>
          <div className="md-link-row">
            <Field label="โครงการ" required lead={<Icon.folder size={18} />} trailingChevron>
              <select value={form.projectId} onChange={(e) => setField("projectId", e.target.value)} disabled={!!editingId} style={{ paddingLeft: 40 }}>
                <option value="">— เลือกโครงการ —</option>
                {projects.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
              </select>
            </Field>
            <div className="md-field no-icon">
              <label>การผูกช่องยา</label>
              <div className="md-cabinet-box"><span className="md-cab-ico"><Icon.cabinet size={18} /></span> จัดการจากคลังยาและช่องจริง</div>
            </div>
            <button
              type="button"
              className="md-btn md-btn-primary"
              style={{ minHeight: 48 }}
              disabled={!editingId || !form.active}
              title={!editingId ? "กรุณาบันทึกยาก่อนจัดการการเชื่อมโยง" : !form.active ? "ต้องเปิดใช้งานยาก่อนจึงจะผูกกับช่องได้" : "ไปยังคลังยาเพื่อผูกยากับช่อง"}
              onClick={() => editingId && form.active && navigate(`/inventory?drugId=${encodeURIComponent(editingId)}&drugCode=${encodeURIComponent(form.code)}`)}
            ><Icon.link size={18} /> จัดการการเชื่อมโยง</button>
          </div>
        </DrawerSection>

        <DrawerSection num="04" icon={Icon.checkCircle} title="สถานะ" green refEl={sectionRefs[3]}>
          <div className="md-status-row">
            <div className={`md-toggle${form.active ? " on" : ""}`} onClick={() => setField("active", !form.active)}>
              <span className="md-switch" />
              <span className="md-toggle-label">{form.active ? "ใช้งาน" : "ปิดใช้งาน"}</span>
            </div>
            {form.active && <span className="md-badge md-badge-soft"><Icon.checkCircle size={15} /> พร้อมเผยแพร่</span>}
            <span className="md-status-meta">แก้ไขโดย <strong>{userName}</strong></span>
          </div>
        </DrawerSection>
      </MasterDrawer>
    </>
  );
}
