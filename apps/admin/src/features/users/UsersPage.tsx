import { useState, useEffect, useCallback, useMemo, useRef, type FormEvent } from "react";
import { create } from "@bufbuild/protobuf";
import {
  Role,
  ListUsersRequestSchema,
  CreateUserRequestSchema,
  UpdateUserRequestSchema,
  ListProjectsRequestSchema,
} from "@medisync/proto/medisync/identity/v1/identity_pb";
import type { User, Project } from "@medisync/proto/medisync/identity/v1/identity_pb";
import { identityClient, projectClient } from "../../api/client";
import { useAuth } from "../../auth/AuthContext";
import { Icon } from "../masterdata/icons";
import {
  MasterHeader, ListHeading, SearchInput, Select, StatusBadge, MasterTable,
  MasterDrawer, DrawerSection, Field, SaveNotice, formatThaiDateTime, type Column, type Step,
} from "../masterdata/kit";

const ROLE_OPTIONS: { value: Role; label: string }[] = [
  { value: Role.ADMIN, label: "ผู้ดูแลระบบ" },
  { value: Role.PHARMACIST, label: "เภสัชกร" },
  { value: Role.NURSE, label: "พยาบาล" },
  { value: Role.REFILLER, label: "ผู้เติมยา" },
];
function roleLabel(r: Role): string { return ROLE_OPTIONS.find((o) => o.value === r)?.label ?? "ไม่ระบุ"; }

const STEPS: Step[] = [
  { num: "01", label: "ข้อมูลบัญชี", icon: Icon.users },
  { num: "02", label: "สิทธิ์ & ขอบเขต", icon: Icon.link },
  { num: "03", label: "สถานะ", icon: Icon.checkCircle },
];

interface UserFormData {
  username: string; password: string; displayName: string;
  role: Role; projectId: string; wardIds: string; active: boolean;
}
const emptyForm: UserFormData = { username: "", password: "", displayName: "", role: Role.NURSE, projectId: "", wardIds: "", active: true };
function toForm(u: User): UserFormData {
  return { username: u.username, password: "", displayName: u.displayName, role: u.role as Role, projectId: u.projectId, wardIds: u.wardIds.join(", "), active: u.active };
}

export function UsersPage() {
  const { user } = useAuth();
  const [users, setUsers] = useState<User[]>([]);
  const [projects, setProjects] = useState<Project[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [query, setQuery] = useState("");
  const [roleFilter, setRoleFilter] = useState("");

  const [drawerOpen, setDrawerOpen] = useState(false);
  const [editingId, setEditingId] = useState<string | null>(null);
  const [form, setForm] = useState<UserFormData>(emptyForm);
  const [dirty, setDirty] = useState(false);
  const [activeStep, setActiveStep] = useState(0);
  const [saving, setSaving] = useState(false);
  const [formError, setFormError] = useState<string | null>(null);
  const [saveNotice, setSaveNotice] = useState<string | null>(null);
  const [editedAt, setEditedAt] = useState<Date>(new Date());
  const sectionRefs = [useRef<HTMLDivElement>(null), useRef<HTMLDivElement>(null), useRef<HTMLDivElement>(null)];

  const load = useCallback(async () => {
    setLoading(true); setError(null);
    try {
      const res = await identityClient.listUsers(create(ListUsersRequestSchema, { query: "" }));
      setUsers(res.users);
    } catch (e: unknown) { setError(e instanceof Error ? e.message : "โหลดผู้ใช้งานไม่สำเร็จ"); }
    finally { setLoading(false); }
  }, []);
  const loadProjects = useCallback(async () => {
    try { const res = await projectClient.listProjects(create(ListProjectsRequestSchema, {})); setProjects(res.projects.filter((p) => p.active)); }
    catch { /* non-critical */ }
  }, []);
  useEffect(() => { load(); loadProjects(); }, [load, loadProjects]);

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    return users.filter((u) => {
      if (roleFilter && String(u.role) !== roleFilter) return false;
      if (!q) return true;
      return u.username.toLowerCase().includes(q) || u.displayName.toLowerCase().includes(q);
    });
  }, [users, query, roleFilter]);
  const projectName = useCallback((id: string) => projects.find((p) => p.id === id)?.name ?? "—", [projects]);
  const userName = user?.displayName || user?.username || "ผู้ใช้งาน";

  function openCreate() { setEditingId(null); setForm({ ...emptyForm, projectId: projects[0]?.id ?? "" }); setDirty(false); setActiveStep(0); setFormError(null); setEditedAt(new Date()); setDrawerOpen(true); }
  function openEdit(u: User) {
    setEditingId(u.id); setForm(toForm(u)); setDirty(false); setActiveStep(0); setFormError(null);
    setEditedAt(u.createdAt ? new Date(Number(u.createdAt.seconds) * 1000) : new Date()); setDrawerOpen(true);
  }
  function closeDrawer() { if (dirty && !confirm("มีการแก้ไขที่ยังไม่บันทึก ต้องการปิดหรือไม่?")) return; setDrawerOpen(false); }
  function setField<K extends keyof UserFormData>(k: K, v: UserFormData[K]) { setForm((f) => ({ ...f, [k]: v })); setDirty(true); }
  function goToStep(i: number) { setActiveStep(i); sectionRefs[i].current?.scrollIntoView({ behavior: "smooth", block: "start" }); }
  function restore() {
    if (editingId) { const u = users.find((x) => x.id === editingId); if (u) { setForm(toForm(u)); setDirty(false); } }
    else { setForm({ ...emptyForm, projectId: projects[0]?.id ?? "" }); setDirty(false); }
  }

  async function handleSave(e: FormEvent) {
    e.preventDefault(); setFormError(null);
    if (!form.username.trim()) { setFormError("กรุณากรอกชื่อผู้ใช้"); return; }
    if (!editingId && !form.password) { setFormError("กรุณากำหนดรหัสผ่านสำหรับผู้ใช้ใหม่"); return; }
    if (!form.displayName.trim()) { setFormError("กรุณากรอกชื่อที่แสดง"); return; }
    const wardIds = form.wardIds.split(",").map((w) => w.trim()).filter(Boolean);
    setSaving(true);
    try {
      if (editingId) {
        await identityClient.updateUser(create(UpdateUserRequestSchema, {
          id: editingId, displayName: form.displayName.trim(), role: form.role, active: form.active, wardIds,
        }));
      } else {
        await identityClient.createUser(create(CreateUserRequestSchema, {
          username: form.username.trim(), password: form.password, displayName: form.displayName.trim(),
          role: form.role, wardIds, projectId: form.projectId || undefined,
        }));
      }
      setDrawerOpen(false); await load();
      setSaveNotice(editingId ? "บันทึกข้อมูลผู้ใช้งานเรียบร้อยแล้ว" : "เพิ่มผู้ใช้งานเรียบร้อยแล้ว");
      window.setTimeout(() => setSaveNotice(null), 4000);
    } catch (err: unknown) { setFormError(err instanceof Error ? err.message : "บันทึกไม่สำเร็จ"); }
    finally { setSaving(false); }
  }
  async function handleArchive(u: User) {
    if (!confirm(`${u.active ? "ปิด" : "เปิด"}การใช้งานผู้ใช้ "${u.displayName}" หรือไม่?`)) return;
    try { await identityClient.updateUser(create(UpdateUserRequestSchema, { id: u.id, active: !u.active })); await load(); }
    catch (err: unknown) { setError(err instanceof Error ? err.message : "อัปเดตไม่สำเร็จ"); }
  }

  const columns: Column<User>[] = [
    { key: "username", header: "ชื่อผู้ใช้", render: (u) => <span className="md-code">{u.username}</span> },
    { key: "name", header: "ชื่อที่แสดง", render: (u) => u.displayName },
    { key: "role", header: "บทบาท", render: (u) => <span className="md-badge md-badge-role">{roleLabel(u.role as Role)}</span> },
    { key: "project", header: "โครงการ", render: (u) => <span className="md-cell-muted">{u.projectId ? projectName(u.projectId) : "ทุกโครงการ"}</span> },
    { key: "wards", header: "Wards", render: (u) => <span className="md-cell-muted">{u.wardIds.length ? u.wardIds.join(", ") : "ทั้งหมด"}</span> },
    { key: "status", header: "สถานะ", render: (u) => <StatusBadge active={u.active} /> },
  ];

  return (
    <>
      <MasterHeader icon={Icon.users} title="ผู้ใช้งาน" subtitle="จัดการบัญชีและสิทธิ์ผู้ใช้ (Master Data)">
        <button className="md-btn md-btn-primary" onClick={openCreate}><Icon.plus size={18} /> เพิ่มผู้ใช้</button>
      </MasterHeader>
      <SaveNotice message={saveNotice} onDismiss={() => setSaveNotice(null)} />

      {error && <div className="md-err">{error}</div>}

      <div className="md-panel">
        <ListHeading icon={Icon.users} title="รายชื่อผู้ใช้งาน" count={filtered.length} />
        <div className="md-toolbar">
          <SearchInput value={query} onChange={setQuery} placeholder="ค้นหาชื่อผู้ใช้ หรือชื่อที่แสดง" />
          <Select value={roleFilter} onChange={setRoleFilter}>
            <option value="">ทุกบทบาท</option>
            {ROLE_OPTIONS.map((o) => <option key={o.value} value={String(o.value)}>{o.label}</option>)}
          </Select>
          <button className="md-btn md-btn-primary" onClick={openCreate}><Icon.plus size={18} /> เพิ่มผู้ใช้</button>
        </div>
        <MasterTable rows={filtered} columns={columns} getId={(u) => u.id} loading={loading}
          onEdit={openEdit} onArchive={handleArchive} emptyText="ไม่พบผู้ใช้งาน" />
      </div>

      <MasterDrawer
        open={drawerOpen} icon={Icon.users}
        title={editingId ? "แก้ไขผู้ใช้งาน" : "เพิ่มผู้ใช้งาน"}
        entityLabel="ผู้ใช้" code={form.username} dirty={dirty}
        steps={STEPS} activeStep={activeStep} onStep={goToStep}
        onClose={closeDrawer} onSubmit={handleSave} onRestore={restore}
        saving={saving} timeLabel={formatThaiDateTime(editedAt)}
      >
        {formError && <div className="md-err" style={{ margin: "0 0 16px" }}>{formError}</div>}

        <DrawerSection num="01" icon={Icon.users} title="ข้อมูลบัญชี" refEl={sectionRefs[0]}>
          <div className="md-grid3">
            <Field label="ชื่อผู้ใช้" required lead={<Icon.hash size={18} />}>
              <input value={form.username} onChange={(e) => setField("username", e.target.value)} placeholder="nurse01" disabled={!!editingId} />
            </Field>
            <Field label="ชื่อที่แสดง" required lead={<span style={{ fontWeight: 700 }}>T</span>}>
              <input value={form.displayName} onChange={(e) => setField("displayName", e.target.value)} placeholder="นภา วัฒนกุล" />
            </Field>
            {!editingId && (
              <Field label="รหัสผ่าน" required lead={<Icon.link size={18} />}>
                <input type="password" value={form.password} onChange={(e) => setField("password", e.target.value)} placeholder="••••••••" />
              </Field>
            )}
          </div>
        </DrawerSection>

        <DrawerSection num="02" icon={Icon.link} title="สิทธิ์ & ขอบเขต" refEl={sectionRefs[1]}>
          <div className="md-grid3">
            <Field label="บทบาท" required lead={<Icon.users size={18} />} trailingChevron>
              <select value={String(form.role)} onChange={(e) => setField("role", Number(e.target.value) as Role)}>
                {ROLE_OPTIONS.map((o) => <option key={o.value} value={String(o.value)}>{o.label}</option>)}
              </select>
            </Field>
            <Field label="โครงการ" lead={<Icon.folder size={18} />} trailingChevron>
              <select value={form.projectId} onChange={(e) => setField("projectId", e.target.value)} disabled={!!editingId}>
                <option value="">ทุกโครงการ</option>
                {projects.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
              </select>
            </Field>
            <Field label="Wards (คั่นด้วย ,)" lead={<Icon.cabinet size={18} />}>
              <input value={form.wardIds} onChange={(e) => setField("wardIds", e.target.value)} placeholder="ว่าง = ทุก ward" />
            </Field>
          </div>
        </DrawerSection>

        <DrawerSection num="03" icon={Icon.checkCircle} title="สถานะ" green refEl={sectionRefs[2]}>
          <div className="md-status-row">
            <div className={`md-toggle${form.active ? " on" : ""}`} onClick={() => setField("active", !form.active)}>
              <span className="md-switch" />
              <span className="md-toggle-label">{form.active ? "ใช้งาน" : "ปิดใช้งาน"}</span>
            </div>
            {form.active && <span className="md-badge md-badge-soft"><Icon.checkCircle size={15} /> พร้อมใช้งาน</span>}
            <span className="md-status-meta">แก้ไขโดย <strong>{userName}</strong></span>
          </div>
        </DrawerSection>
      </MasterDrawer>
    </>
  );
}
