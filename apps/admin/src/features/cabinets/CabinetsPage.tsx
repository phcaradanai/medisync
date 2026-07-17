import { useState, useEffect, useCallback, type FormEvent } from "react";
import { create } from "@bufbuild/protobuf";
import {
  ListCabinetsRequestSchema,
  CreateCabinetRequestSchema,
  UpdateCabinetRequestSchema,
  type Cabinet,
} from "@medisync/proto/medisync/cabinet/v1/cabinet_pb";
import {
  ListProjectsRequestSchema,
} from "@medisync/proto/medisync/identity/v1/identity_pb";
import type { Project } from "@medisync/proto/medisync/identity/v1/identity_pb";
import { cabinetClient, projectClient } from "../../api/client";

// ── Component ──────────────────────────────────────────────────────

export function CabinetsPage() {
  const [cabinets, setCabinets] = useState<Cabinet[]>([]);
  const [projects, setProjects] = useState<Project[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  // Modal state
  const [showModal, setShowModal] = useState(false);
  const [editId, setEditId] = useState<string | null>(null);
  const [code, setCode] = useState("");
  const [name, setName] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [projectId, setProjectId] = useState("");
  const [saving, setSaving] = useState(false);
  const [formError, setFormError] = useState<string | null>(null);

  // ── Fetch ──────────────────────────────────────────────────────

  const fetchCabinets = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const res = await cabinetClient.listCabinets(create(ListCabinetsRequestSchema, {}));
      setCabinets(res.cabinets);
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : "Failed to load cabinets");
    } finally {
      setLoading(false);
    }
  }, []);

  const fetchProjects = useCallback(async () => {
    try {
      const res = await projectClient.listProjects(create(ListProjectsRequestSchema, {}));
      setProjects(res.projects.filter(p => p.active));
    } catch (_) {
      // non-critical
    }
  }, []);

  useEffect(() => {
    fetchCabinets();
    fetchProjects();
  }, [fetchCabinets, fetchProjects]);

  // ── CRUD ───────────────────────────────────────────────────────

  function openCreate() {
    setEditId(null);
    setCode("");
    setName("");
    setDisplayName("");
    setProjectId(projects[0]?.id ?? "");
    setFormError(null);
    setShowModal(true);
  }

  function openEdit(c: Cabinet) {
    setEditId(c.id);
    setCode(c.code);
    setName(c.name);
    setDisplayName(c.displayName);
    setProjectId(c.projectId);
    setFormError(null);
    setShowModal(true);
  }

  function closeModal() {
    setShowModal(false);
    setEditId(null);
    setCode("");
    setName("");
    setDisplayName("");
    setProjectId("");
    setFormError(null);
  }

  async function handleSave(e: FormEvent) {
    e.preventDefault();
    setFormError(null);

    if (!code.trim()) {
      setFormError("Code is required");
      return;
    }
    if (!name.trim()) {
      setFormError("Name is required");
      return;
    }
    if (!projectId) {
      setFormError("Project is required");
      return;
    }

    setSaving(true);
    try {
      if (editId) {
        await cabinetClient.updateCabinet(create(UpdateCabinetRequestSchema, {
          id: editId,
          name: name.trim(),
        }));
      } else {
        await cabinetClient.createCabinet(create(CreateCabinetRequestSchema, {
          code: code.trim(),
          name: name.trim(),
          displayName: displayName.trim() || name.trim(),
          projectId: projectId,
        }));
      }
      closeModal();
      await fetchCabinets();
    } catch (e: unknown) {
      setFormError(e instanceof Error ? e.message : "Save failed");
    } finally {
      setSaving(false);
    }
  }

  async function handleToggleActive(c: Cabinet) {
    try {
      await cabinetClient.updateCabinet(create(UpdateCabinetRequestSchema, {
        id: c.id,
        active: !c.active,
      }));
      await fetchCabinets();
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : "Update failed");
    }
  }

  // ── Render ─────────────────────────────────────────────────────

  return (
    <div>
      <div className="page-header">
        <h1>Cabinets</h1>
        <div className="page-header-actions">
          <button className="btn-primary" onClick={openCreate}>
            + Add Cabinet
          </button>
        </div>
      </div>

      {error && (
        <div className="login-error mb-md" style={{ marginBottom: "var(--sp-lg)" }}>
          {error}
          <button className="btn-ghost btn-sm" style={{ marginLeft: "var(--sp-md)" }} onClick={() => setError(null)}>Dismiss</button>
        </div>
      )}

      <div className="table-wrap">
        {loading && cabinets.length === 0 ? (
          <div className="empty-state">Loading…</div>
        ) : cabinets.length === 0 ? (
          <div className="empty-state">No cabinets yet. Click + Add Cabinet to register a vending machine.</div>
        ) : (
          <table>
            <thead>
              <tr>
                <th>Code</th>
                <th>Name</th>
                <th>Status</th>
                <th style={{ width: 120 }}>Actions</th>
              </tr>
            </thead>
            <tbody>
              {cabinets.map((c) => (
                <tr key={c.id}>
                  <td className="mono">{c.code}</td>
                  <td>{c.name}</td>
                  <td>
                    <span className={`badge ${c.active ? "badge-active" : "badge-inactive"}`}>
                      {c.active ? "Active" : "Inactive"}
                    </span>
                  </td>
                  <td>
                    <div className="inline-actions">
                      <button className="btn-ghost btn-sm" onClick={() => openEdit(c)}>Edit</button>
                      <button
                        className="btn-ghost btn-sm"
                        onClick={() => handleToggleActive(c)}
                        style={{ color: c.active ? "var(--semantic-error)" : "var(--semantic-success)" }}
                      >
                        {c.active ? "Deactivate" : "Activate"}
                      </button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>

      {/* ── Create / Edit Modal ────────────────────────────────── */}
      {showModal && (
        <div className="overlay" onClick={(e) => { if (e.target === e.currentTarget) closeModal(); }}>
          <form className="modal" onSubmit={handleSave}>
            <h2>{editId ? "Edit Cabinet" : "Add Cabinet"}</h2>
            {formError && <div className="login-error">{formError}</div>}

            <div className="form-group">
              <label>Code *</label>
              <input
                type="text"
                value={code}
                onChange={(e) => setCode(e.target.value)}
                placeholder="e.g. CAB-01"
                disabled={!!editId}
                required
              />
            </div>

            <div className="form-group">
              <label>Name *</label>
              <input
                type="text"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="e.g. ตู้จ่ายยาชั้น 1"
                required
              />
            </div>

            <div className="form-group">
              <label>Display Name (Thai)</label>
              <input
                type="text"
                value={displayName}
                onChange={(e) => setDisplayName(e.target.value)}
                placeholder="e.g. ตู้จ่ายยาหลัก ชั้น 1"
              />
            </div>

            <div className="form-group">
              <label>Project *</label>
              <select
                value={projectId}
                onChange={(e) => setProjectId(e.target.value)}
                required
              >
                <option value="">-- Select Project --</option>
                {projects.map((p) => (
                  <option key={p.id} value={p.id}>{p.name}</option>
                ))}
              </select>
            </div>

            <div className="form-actions">
              <button type="button" className="btn-secondary" onClick={closeModal}>Cancel</button>
              <button type="submit" className="btn-primary" disabled={saving}>
                {saving ? "Saving…" : editId ? "Save Changes" : "Create Cabinet"}
              </button>
            </div>
          </form>
        </div>
      )}
    </div>
  );
}
