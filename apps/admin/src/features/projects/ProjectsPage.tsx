import { useState, useEffect, useCallback, type FormEvent } from "react";
import { create } from "@bufbuild/protobuf";
import {
  ListProjectsRequestSchema,
  CreateProjectRequestSchema,
  UpdateProjectRequestSchema,
  type Project,
} from "@medisync/proto/medisync/identity/v1/identity_pb";
import { projectClient } from "../../api/client";

interface ProjectFormData {
  name: string;
  slug: string;
  code: string;
}

const emptyForm: ProjectFormData = { name: "", slug: "", code: "" };

export function ProjectsPage() {
  const [projects, setProjects] = useState<Project[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [form, setForm] = useState<ProjectFormData>(emptyForm);
  const [editingId, setEditingId] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  const loadProjects = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const res = await projectClient.listProjects(create(ListProjectsRequestSchema, {}));
      setProjects(res.projects);
    } catch (e: any) {
      setError(e.message ?? "Failed to load projects");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => { loadProjects(); }, [loadProjects]);

  const resetForm = () => {
    setForm(emptyForm);
    setEditingId(null);
  };

  const handleSubmit = async (e: FormEvent) => {
    e.preventDefault();
    if (!form.name.trim()) return;
    setSubmitting(true);
    setError(null);
    try {
      if (editingId) {
        await projectClient.updateProject(create(UpdateProjectRequestSchema, {
          id: editingId,
          name: form.name.trim(),
        }));
      } else {
        await projectClient.createProject(create(CreateProjectRequestSchema, {
          name: form.name.trim(),
          slug: form.slug.trim() || undefined,
          code: form.code.trim() || undefined,
        }));
      }
      resetForm();
      await loadProjects();
    } catch (e: any) {
      setError(e.message ?? "Failed to save project");
    } finally {
      setSubmitting(false);
    }
  };

  const handleToggleActive = async (p: Project) => {
    try {
      await projectClient.updateProject(create(UpdateProjectRequestSchema, {
        id: p.id,
        active: !p.active,
      }));
      await loadProjects();
    } catch (e: any) {
      setError(e.message ?? "Failed to update project");
    }
  };

  const startEdit = (p: Project) => {
    setEditingId(p.id);
    setForm({ name: p.name, slug: p.slug, code: p.code });
  };

  if (loading) {
    return <div className="page"><div className="page-header"><h1>Projects</h1></div><p>Loading…</p></div>;
  }

  return (
    <div className="page">
      <div className="page-header">
        <h1>Projects ({projects.length})</h1>
      </div>

      {error && <div className="error-banner">{error}</div>}

      <form onSubmit={handleSubmit} className="create-form">
        <h3>{editingId ? "Edit Project" : "New Project"}</h3>
        <div className="form-row">
          <input
            type="text"
            placeholder="Project Name"
            value={form.name}
            onChange={e => setForm(f => ({ ...f, name: e.target.value }))}
            required
          />
          {!editingId && (
            <>
              <input
                type="text"
                placeholder="Slug (auto-generated)"
                value={form.slug}
                onChange={e => setForm(f => ({ ...f, slug: e.target.value }))}
              />
              <input
                type="text"
                placeholder="Code (e.g. BKK-HOSP)"
                value={form.code}
                onChange={e => setForm(f => ({ ...f, code: e.target.value }))}
              />
            </>
          )}
          <button type="submit" disabled={submitting || !form.name.trim()}>
            {editingId ? "Save" : "Create"}
          </button>
          {editingId && <button type="button" onClick={resetForm}>Cancel</button>}
        </div>
      </form>

      <table className="data-table">
        <thead>
          <tr>
            <th>Code</th>
            <th>Name</th>
            <th>Slug</th>
            <th>Active</th>
            <th>Actions</th>
          </tr>
        </thead>
        <tbody>
          {projects.length === 0 && (
            <tr><td colSpan={5} style={{ textAlign: "center", color: "#888" }}>No projects yet</td></tr>
          )}
          {projects.map(p => (
            <tr key={p.id} className={!p.active ? "inactive-row" : ""}>
              <td><code>{p.code || "—"}</code></td>
              <td>{p.name}</td>
              <td><code>{p.slug}</code></td>
              <td>{p.active ? "✅" : "❌"}</td>
              <td className="actions-cell">
                <button onClick={() => startEdit(p)} disabled={editingId === p.id}>Edit</button>
                <button onClick={() => handleToggleActive(p)}>
                  {p.active ? "Deactivate" : "Activate"}
                </button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
