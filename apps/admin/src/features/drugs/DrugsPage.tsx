import { useState, useEffect, useCallback, type FormEvent } from "react";
import { create } from "@bufbuild/protobuf";
import {
  DrugSchema,
  CreateDrugRequestSchema,
  UpdateDrugRequestSchema,
  DeactivateDrugRequestSchema,
  ListDrugsRequestSchema,
} from "@medisync/proto/medisync/catalog/v1/catalog_pb";
import type { Drug } from "@medisync/proto/medisync/catalog/v1/catalog_pb";
import {
  ListProjectsRequestSchema,
} from "@medisync/proto/medisync/identity/v1/identity_pb";
import type { Project } from "@medisync/proto/medisync/identity/v1/identity_pb";
import { catalogClient, projectClient } from "../../api/client";

// ── Types ──────────────────────────────────────────────────────────

interface DrugFormData {
  code: string;
  name: string;
  displayName: string;
  genericName: string;
  form: string;
  strength: string;
  unit: string;
  stickerNote: string;
  projectId: string;
  active: boolean;
}

const emptyForm: DrugFormData = {
  code: "",
  name: "",
  displayName: "",
  genericName: "",
  form: "",
  strength: "",
  unit: "",
  stickerNote: "",
  projectId: "",
  active: true,
};

// ── Component ──────────────────────────────────────────────────────

export function DrugsPage() {
  const [drugs, setDrugs] = useState<Drug[]>([]);
  const [projects, setProjects] = useState<Project[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [query, setQuery] = useState("");

  // Modal state
  const [showModal, setShowModal] = useState(false);
  const [editingId, setEditingId] = useState<string | null>(null);
  const [form, setForm] = useState<DrugFormData>(emptyForm);
  const [saving, setSaving] = useState(false);
  const [formError, setFormError] = useState<string | null>(null);

  // ── Data fetching ──────────────────────────────────────────────

  const fetchDrugs = useCallback(
    async (searchQuery?: string) => {
      setLoading(true);
      setError(null);
      try {
        const res = await catalogClient.listDrugs(
          create(ListDrugsRequestSchema, {
            query: searchQuery ?? "",
            pageSize: 100,
            includeInactive: true,
          }),
        );
        setDrugs(res.drugs);
      } catch (e: unknown) {
        setError(e instanceof Error ? e.message : "Failed to load drugs");
      } finally {
        setLoading(false);
      }
    },
    [],
  );

  // ── Initial load ───────────────────────────────────────────────

  useEffect(() => {
    fetchDrugs();
    fetchProjects();
  }, [fetchDrugs]);

  // ── Fetch projects ─────────────────────────────────────────────

  const fetchProjects = useCallback(async () => {
    try {
      const res = await projectClient.listProjects(create(ListProjectsRequestSchema, {}));
      setProjects(res.projects.filter(p => p.active));
    } catch (_) {
      // projects list is non-critical for drug page
    }
  }, []);

  // ── Search ─────────────────────────────────────────────────────

  function handleSearch(e: FormEvent) {
    e.preventDefault();
    fetchDrugs(query);
  }

  // ── CRUD operations ────────────────────────────────────────────

  function openCreate() {
    setEditingId(null);
    setForm({ ...emptyForm, projectId: projects[0]?.id ?? "" });
    setFormError(null);
    setShowModal(true);
  }

  function openEdit(drug: Drug) {
    setEditingId(drug.id);
    setForm({
      code: drug.code,
      name: drug.name,
      displayName: drug.displayName,
      genericName: drug.genericName,
      form: drug.form,
      strength: drug.strength,
      unit: drug.unit,
      stickerNote: drug.stickerNote,
      projectId: drug.projectId,
      active: drug.active,
    });
    setFormError(null);
    setShowModal(true);
  }

  function closeModal() {
    setShowModal(false);
    setEditingId(null);
    setForm(emptyForm);
    setFormError(null);
  }

  async function handleSave(e: FormEvent) {
    e.preventDefault();
    setFormError(null);

    if (!form.code.trim()) {
      setFormError("Drug code is required");
      return;
    }
    if (!form.name.trim()) {
      setFormError("Drug name is required");
      return;
    }

    setSaving(true);
    try {
      if (editingId) {
        const req = create(UpdateDrugRequestSchema, {
          drug: create(DrugSchema, {
            id: editingId,
            code: form.code.trim(),
            name: form.name.trim(),
            genericName: form.genericName.trim(),
            form: form.form.trim(),
            strength: form.strength.trim(),
            unit: form.unit.trim(),
            stickerNote: form.stickerNote.trim(),
            active: form.active,
          }),
        });
        await catalogClient.updateDrug(req);
      } else {
        const req = create(CreateDrugRequestSchema, {
          code: form.code.trim(),
          name: form.name.trim(),
          displayName: form.displayName.trim() || form.name.trim(),
          genericName: form.genericName.trim(),
          form: form.form.trim(),
          strength: form.strength.trim(),
          unit: form.unit.trim(),
          stickerNote: form.stickerNote.trim(),
          projectId: form.projectId.trim() || undefined,
        });
        await catalogClient.createDrug(req);
      }
      closeModal();
      await fetchDrugs();
    } catch (e: unknown) {
      setFormError(e instanceof Error ? e.message : "Save failed");
    } finally {
      setSaving(false);
    }
  }

  async function handleDeactivate(id: string) {
    if (
      !confirm(
        "Deactivate this drug? It will no longer appear in dispensing lists.",
      )
    ) {
      return;
    }
    try {
      await catalogClient.deactivateDrug(
        create(DeactivateDrugRequestSchema, { id }),
      );
      await fetchDrugs();
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : "Deactivate failed");
    }
  }

  // ── Render helpers ─────────────────────────────────────────────

  function field(
    label: string,
    value: string,
    setter: (v: string) => void,
    opts?: { required?: boolean; placeholder?: string },
  ) {
    return (
      <div className="form-group">
        <label>{label}</label>
        <input
          type="text"
          value={value}
          onChange={(e) => setter(e.target.value)}
          placeholder={opts?.placeholder}
          required={opts?.required}
        />
      </div>
    );
  }

  // ── Render ─────────────────────────────────────────────────────

  return (
    <div>
      {/* Header */}
      <div className="page-header">
        <h1>Drug Catalog</h1>
        <div className="page-header-actions">
          <button className="btn-primary" onClick={openCreate}>
            + Add Drug
          </button>
        </div>
      </div>

      {/* Search */}
      <form className="search-bar" onSubmit={handleSearch}>
        <input
          type="text"
          placeholder="Search by code, name, or generic name…"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
        />
      </form>

      {/* Error banner */}
      {error && (
        <div
          className="login-error mb-md"
          style={{ marginBottom: "var(--sp-lg)" }}
        >
          {error}
          <button
            className="btn-ghost btn-sm"
            style={{ marginLeft: "var(--sp-md)" }}
            onClick={() => setError(null)}
          >
            Dismiss
          </button>
        </div>
      )}

      {/* Table */}
      <div className="table-wrap">
        {loading && drugs.length === 0 ? (
          <div className="empty-state">Loading…</div>
        ) : drugs.length === 0 ? (
          <div className="empty-state">
            {query
              ? "No drugs match your search."
              : "No drugs in the catalog yet. Click + Add Drug to create one."}
          </div>
        ) : (
          <table>
            <thead>
              <tr>
                <th>Code</th>
                <th>Name</th>
                <th>Generic Name</th>
                <th>Form</th>
                <th>Strength</th>
                <th>Unit</th>
                <th>Status</th>
                <th style={{ width: 120 }}>Actions</th>
              </tr>
            </thead>
            <tbody>
              {drugs.map((d) => (
                <tr key={d.id}>
                  <td className="mono">{d.code}</td>
                  <td>{d.name}</td>
                  <td className="text-muted">{d.genericName || "—"}</td>
                  <td>{d.form || "—"}</td>
                  <td className="mono">{d.strength || "—"}</td>
                  <td>{d.unit || "—"}</td>
                  <td>
                    <span
                      className={`badge ${d.active ? "badge-active" : "badge-inactive"}`}
                    >
                      {d.active ? "Active" : "Inactive"}
                    </span>
                  </td>
                  <td>
                    <div className="inline-actions">
                      <button
                        className="btn-ghost btn-sm"
                        onClick={() => openEdit(d)}
                        title="Edit"
                      >
                        Edit
                      </button>
                      {d.active && (
                        <button
                          className="btn-ghost btn-sm"
                          onClick={() => handleDeactivate(d.id)}
                          style={{ color: "var(--semantic-error)" }}
                          title="Deactivate"
                        >
                          Deactivate
                        </button>
                      )}
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>

      {/* ── Create / Edit modal ────────────────────────────────── */}
      {showModal && (
        <div
          className="overlay"
          onClick={(e) => {
            if (e.target === e.currentTarget) closeModal();
          }}
        >
          <form className="modal" onSubmit={handleSave}>
            <h2>{editingId ? "Edit Drug" : "Add Drug"}</h2>

            {formError && <div className="login-error">{formError}</div>}

            {field("Code *", form.code, (v) =>
              setForm((f) => ({ ...f, code: v })),
              { required: true, placeholder: "e.g. PARA500" },
            )}

            {field("Name *", form.name, (v) =>
              setForm((f) => ({ ...f, name: v })),
              { required: true, placeholder: "e.g. Paracetamol 500mg" },
            )}

            <div className="form-row">
              {field("Generic Name", form.genericName, (v) =>
                setForm((f) => ({ ...f, genericName: v })),
                { placeholder: "e.g. Acetaminophen" },
              )}
              {field("Form", form.form, (v) =>
                setForm((f) => ({ ...f, form: v })),
                { placeholder: "e.g. tablet" },
              )}
            </div>

            <div className="form-row">
              {field("Strength", form.strength, (v) =>
                setForm((f) => ({ ...f, strength: v })),
                { placeholder: "e.g. 500 mg" },
              )}
              {field("Unit", form.unit, (v) =>
                setForm((f) => ({ ...f, unit: v })),
                { placeholder: "e.g. tab" },
              )}
            </div>

            {field("Sticker Note", form.stickerNote, (v) =>
              setForm((f) => ({ ...f, stickerNote: v })),
              { placeholder: "e.g. Take with food. Avoid alcohol." },
            )}

            {field("Display Name (Thai)", form.displayName, (v) =>
              setForm((f) => ({ ...f, displayName: v })),
              { placeholder: "e.g. พาราเซตามอล 500 มก." },
            )}

            <div className="form-group">
              <label>Project *</label>
              <select
                value={form.projectId}
                onChange={(e) => setForm((f) => ({ ...f, projectId: e.target.value }))}
                required
              >
                <option value="">-- Select Project --</option>
                {projects.map((p) => (
                  <option key={p.id} value={p.id}>
                    {p.name}
                  </option>
                ))}
              </select>
            </div>

            <div className="form-actions">
              <button
                type="button"
                className="btn-secondary"
                onClick={closeModal}
              >
                Cancel
              </button>
              <button type="submit" className="btn-primary" disabled={saving}>
                {saving
                  ? "Saving…"
                  : editingId
                    ? "Save Changes"
                    : "Create Drug"}
              </button>
            </div>
          </form>
        </div>
      )}
    </div>
  );
}
