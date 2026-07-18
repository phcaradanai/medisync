import { useState, useEffect, useCallback, useMemo, useRef, type FormEvent } from "react";
import { create } from "@bufbuild/protobuf";
import {
  ListProjectsRequestSchema,
  CreateProjectRequestSchema,
  UpdateProjectRequestSchema,
  type Project,
} from "@medisync/proto/medisync/identity/v1/identity_pb";
import { projectClient } from "../../api/client";
import { useAuth } from "../../auth/AuthContext";
import { Icon } from "../masterdata/icons";
import {
  MasterHeader, ListHeading, SearchInput, StatusBadge, MasterTable,
  MasterDrawer, DrawerSection, Field, formatThaiDateTime, type Column, type Step,
} from "../masterdata/kit";

const STEPS: Step[] = [
  { num: "01", label: "ข้อมูลโครงการ", icon: Icon.folder },
  { num: "02", label: "สถานะ", icon: Icon.checkCircle },
];

interface ProjectFormData { code: string; name: string; displayName: string; slug: string; active: boolean; }
const emptyForm: ProjectFormData = { code: "", name: "", displayName: "", slug: "", active: true };
function toForm(p: Project): ProjectFormData {
  return { code: p.code, name: p.name, displayName: p.displayName, slug: p.slug, active: p.active };
}

export function ProjectsPage() {
  const { user } = useAuth();
  const [projects, setProjects] = useState<Project[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [query, setQuery] = useState("");
  const [statusFilter, setStatusFilter] = useState<"all" | "active">("all");

  const [drawerOpen, setDrawerOpen] = useState(false);
  const [editingId, setEditingId] = useState<string | null>(null);
  const [form, setForm] = useState<ProjectFormData>(emptyForm);
  const [dirty, setDirty] = useState(false);
  const [activeStep, setActiveStep] = useState(0);
  const [saving, setSaving] = useState(false);
  const [formError, setFormError] = useState<string | null>(null);
  const [editedAt, setEditedAt] = useState<Date>(new Date());
  const sectionRefs = [useRef<HTMLDivElement>(null), useRef<HTMLDivElement>(null)];

  const load = useCallback(async () => {
    setLoading(true); setError(null);
    try {
      const res = await projectClient.listProjects(create(ListProjectsRequestSchema, {}));
      setProjects(res.projects);
    } catch (e: unknown) { setError(e instanceof Error ? e.message : "โหลดโครงการไม่สำเร็จ"); }
    finally { setLoading(false); }
  }, []);
  useEffect(() => { load(); }, [load]);

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    return projects.filter((p) => {
      if (statusFilter === "active" && !p.active) return false;
      if (!q) return true;
      return p.code.toLowerCase().includes(q) || p.name.toLowerCase().includes(q) || p.slug.toLowerCase().includes(q);
    });
  }, [projects, query, statusFilter]);
  const activeCount = useMemo(() => projects.filter((p) => p.active).length, [projects]);
  const userName = user?.displayName || user?.username || "ผู้ใช้งาน";

  function openCreate() { setEditingId(null); setForm(emptyForm); setDirty(false); setActiveStep(0); setFormError(null); setEditedAt(new Date()); setDrawerOpen(true); }
  function openEdit(p: Project) {
    setEditingId(p.id); setForm(toForm(p)); setDirty(false); setActiveStep(0); setFormError(null);
    setEditedAt(p.updatedAt ? new Date(Number(p.updatedAt.seconds) * 1000) : new Date()); setDrawerOpen(true);
  }
  function closeDrawer() { if (dirty && !confirm("มีการแก้ไขที่ยังไม่บันทึก ต้องการปิดหรือไม่?")) return; setDrawerOpen(false); }
  function setField<K extends keyof ProjectFormData>(k: K, v: ProjectFormData[K]) { setForm((f) => ({ ...f, [k]: v })); setDirty(true); }
  function goToStep(i: number) { setActiveStep(i); sectionRefs[i].current?.scrollIntoView({ behavior: "smooth", block: "start" }); }
  function restore() {
    if (editingId) { const p = projects.find((x) => x.id === editingId); if (p) { setForm(toForm(p)); setDirty(false); } }
    else { setForm(emptyForm); setDirty(false); }
  }

  async function handleSave(e: FormEvent) {
    e.preventDefault(); setFormError(null);
    if (!form.name.trim()) { setFormError("กรุณากรอกชื่อโครงการ"); return; }
    setSaving(true);
    try {
      if (editingId) {
        await projectClient.updateProject(create(UpdateProjectRequestSchema, {
          id: editingId, name: form.name.trim(), active: form.active,
        }));
      } else {
        await projectClient.createProject(create(CreateProjectRequestSchema, {
          name: form.name.trim(), slug: form.slug.trim() || undefined, code: form.code.trim() || undefined,
        }));
      }
      setDrawerOpen(false); await load();
    } catch (err: unknown) { setFormError(err instanceof Error ? err.message : "บันทึกไม่สำเร็จ"); }
    finally { setSaving(false); }
  }
  async function handleArchive(p: Project) {
    if (!confirm(`${p.active ? "ปิด" : "เปิด"}การใช้งานโครงการ "${p.name}" หรือไม่?`)) return;
    try { await projectClient.updateProject(create(UpdateProjectRequestSchema, { id: p.id, active: !p.active })); await load(); }
    catch (err: unknown) { setError(err instanceof Error ? err.message : "อัปเดตไม่สำเร็จ"); }
  }

  const columns: Column<Project>[] = [
    { key: "code", header: "รหัส", render: (p) => <span className="md-code">{p.code || "—"}</span> },
    { key: "name", header: "ชื่อโครงการ", render: (p) => p.name },
    { key: "display", header: "ชื่อที่แสดง", render: (p) => p.displayName || "—" },
    { key: "slug", header: "Slug", render: (p) => <span className="md-cell-muted">{p.slug || "—"}</span> },
    { key: "status", header: "สถานะ", render: (p) => <StatusBadge active={p.active} /> },
  ];

  return (
    <>
      <MasterHeader icon={Icon.folder} title="โครงการ" subtitle="จัดการโครงการ/หน่วยงานในระบบ (Master Data)">
        <button className="md-btn md-btn-primary" onClick={openCreate}><Icon.plus size={18} /> เพิ่มโครงการ</button>
      </MasterHeader>

      {error && <div className="md-err">{error}</div>}

      <div className="md-panel">
        <ListHeading icon={Icon.folder} title="รายการโครงการ" count={filtered.length} />
        <div className="md-toolbar">
          <SearchInput value={query} onChange={setQuery} placeholder="ค้นหารหัส ชื่อ หรือ slug" />
          <div className="md-segment">
            <button className={`md-seg-btn${statusFilter === "all" ? " active" : ""}`} onClick={() => setStatusFilter("all")}>ทั้งหมด <span className="md-seg-num">{projects.length}</span></button>
            <button className={`md-seg-btn${statusFilter === "active" ? " active" : ""}`} onClick={() => setStatusFilter("active")}>ใช้งาน <span className="md-seg-num">{activeCount}</span></button>
          </div>
          <button className="md-btn md-btn-primary" onClick={openCreate}><Icon.plus size={18} /> เพิ่มโครงการ</button>
        </div>
        <MasterTable rows={filtered} columns={columns} getId={(p) => p.id} loading={loading}
          onEdit={openEdit} onArchive={handleArchive} emptyText="ไม่พบโครงการ" />
      </div>

      <MasterDrawer
        open={drawerOpen} icon={Icon.folder}
        title={editingId ? "แก้ไขโครงการ" : "เพิ่มโครงการ"}
        entityLabel="โครงการ" code={form.code} dirty={dirty}
        steps={STEPS} activeStep={activeStep} onStep={goToStep}
        onClose={closeDrawer} onSubmit={handleSave} onRestore={restore}
        saving={saving} timeLabel={formatThaiDateTime(editedAt)}
      >
        {formError && <div className="md-err" style={{ margin: "0 0 16px" }}>{formError}</div>}

        <DrawerSection num="01" icon={Icon.folder} title="ข้อมูลโครงการ" refEl={sectionRefs[0]}>
          <div className="md-grid2">
            <Field label="รหัสโครงการ" lead={<Icon.hash size={18} />}>
              <input value={form.code} onChange={(e) => setField("code", e.target.value)} placeholder="CHULA-A" disabled={!!editingId} />
            </Field>
            <Field label="ชื่อโครงการ" required lead={<span style={{ fontWeight: 700 }}>T</span>}>
              <input value={form.name} onChange={(e) => setField("name", e.target.value)} placeholder="Chula Ward A" />
            </Field>
            <Field label="ชื่อที่แสดง" lead={<Icon.monitor size={18} />}>
              <input value={form.displayName} onChange={(e) => setField("displayName", e.target.value)} placeholder="หอผู้ป่วยจุฬา A" disabled={!!editingId} />
            </Field>
            <Field label="Slug" lead={<Icon.link size={18} />}>
              <input value={form.slug} onChange={(e) => setField("slug", e.target.value)} placeholder="chula-ward-a" disabled={!!editingId} />
            </Field>
          </div>
        </DrawerSection>

        <DrawerSection num="02" icon={Icon.checkCircle} title="สถานะ" green refEl={sectionRefs[1]}>
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
