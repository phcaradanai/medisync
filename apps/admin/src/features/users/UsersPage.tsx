import { useState, useEffect, useCallback, type FormEvent } from "react";
import { create } from "@bufbuild/protobuf";
import {
  Role,
  ListUsersRequestSchema,
  CreateUserRequestSchema,
  UpdateUserRequestSchema,
} from "@medisync/proto/medisync/identity/v1/identity_pb";
import type { User } from "@medisync/proto/medisync/identity/v1/identity_pb";
import { identityClient } from "../../api/client";

// ── Types ──────────────────────────────────────────────────────────

interface UserFormData {
  username: string;
  password: string;
  displayName: string;
  role: Role;
  wardIds: string;
}

const emptyForm: UserFormData = {
  username: "",
  password: "",
  displayName: "",
  role: Role.NURSE,
  wardIds: "",
};

const ROLE_OPTIONS: { value: Role; label: string }[] = [
  { value: Role.ADMIN, label: "Admin" },
  { value: Role.PHARMACIST, label: "Pharmacist" },
  { value: Role.NURSE, label: "Nurse" },
  { value: Role.REFILLER, label: "Refiller" },
];

// ── Component ──────────────────────────────────────────────────────

export function UsersPage() {
  const [users, setUsers] = useState<User[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [query, setQuery] = useState("");

  // Modal state
  const [showModal, setShowModal] = useState(false);
  const [editingId, setEditingId] = useState<string | null>(null);
  const [form, setForm] = useState<UserFormData>(emptyForm);
  const [saving, setSaving] = useState(false);
  const [formError, setFormError] = useState<string | null>(null);

  // ── Data fetching ──────────────────────────────────────────────

  const fetchUsers = useCallback(async (searchQuery?: string) => {
    setLoading(true);
    setError(null);
    try {
      const res = await identityClient.listUsers(
        create(ListUsersRequestSchema, { query: searchQuery ?? "" }),
      );
      setUsers(res.users);
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : "Failed to load users");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchUsers();
  }, [fetchUsers]);

  // ── Search ─────────────────────────────────────────────────────

  function handleSearch(e: FormEvent) {
    e.preventDefault();
    fetchUsers(query);
  }

  // ── CRUD operations ────────────────────────────────────────────

  function openCreate() {
    setEditingId(null);
    setForm(emptyForm);
    setFormError(null);
    setShowModal(true);
  }

  function openEdit(u: User) {
    setEditingId(u.id);
    setForm({
      username: u.username,
      password: "",
      displayName: u.displayName,
      role: u.role as Role,
      wardIds: u.wardIds.join(", "),
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

    if (!form.username.trim()) {
      setFormError("Username is required");
      return;
    }
    if (!editingId && !form.password) {
      setFormError("Password is required for new users");
      return;
    }
    if (!form.displayName.trim()) {
      setFormError("Display name is required");
      return;
    }

    const wardIds = form.wardIds
      .split(",")
      .map((w) => w.trim())
      .filter(Boolean);

    setSaving(true);
    try {
      if (editingId) {
        await identityClient.updateUser(
          create(UpdateUserRequestSchema, {
            id: editingId,
            displayName: form.displayName.trim(),
            role: form.role,
            active: true,
            wardIds,
          }),
        );
      } else {
        await identityClient.createUser(
          create(CreateUserRequestSchema, {
            username: form.username.trim(),
            password: form.password,
            displayName: form.displayName.trim(),
            role: form.role,
            wardIds,
          }),
        );
      }
      closeModal();
      await fetchUsers();
    } catch (e: unknown) {
      setFormError(e instanceof Error ? e.message : "Save failed");
    } finally {
      setSaving(false);
    }
  }

  async function handleToggleActive(u: User) {
    try {
      await identityClient.updateUser(
        create(UpdateUserRequestSchema, {
          id: u.id,
          active: !u.active,
        }),
      );
      await fetchUsers();
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : "Toggle failed");
    }
  }

  // ── Role label helper ──────────────────────────────────────────

  function roleLabel(r: Role): string {
    const opt = ROLE_OPTIONS.find((o) => o.value === r);
    return opt?.label ?? "Unknown";
  }

  // ── Render ─────────────────────────────────────────────────────

  return (
    <div>
      <div className="page-header">
        <h1>Users & Permissions</h1>
        <div className="page-header-actions">
          <button className="btn-primary" onClick={openCreate}>
            + Add User
          </button>
        </div>
      </div>

      {/* Search */}
      <form className="search-bar" onSubmit={handleSearch}>
        <input
          type="text"
          placeholder="Search by username or display name…"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
        />
      </form>

      {/* Error banner */}
      {error && (
        <div className="login-error mb-md" style={{ marginBottom: "var(--sp-lg)" }}>
          {error}
          <button className="btn-ghost btn-sm" style={{ marginLeft: "var(--sp-md)" }} onClick={() => setError(null)}>Dismiss</button>
        </div>
      )}

      {/* Table */}
      <div className="table-wrap">
        {loading && users.length === 0 ? (
          <div className="empty-state">Loading…</div>
        ) : users.length === 0 ? (
          <div className="empty-state">
            {query ? "No users match your search." : "No users yet. Click + Add User to create one."}
          </div>
        ) : (
          <table>
            <thead>
              <tr>
                <th>Username</th>
                <th>Display Name</th>
                <th>Role</th>
                <th>Wards</th>
                <th>Status</th>
                <th style={{ width: 160 }}>Actions</th>
              </tr>
            </thead>
            <tbody>
              {users.map((u) => (
                <tr key={u.id}>
                  <td className="mono">{u.username}</td>
                  <td>{u.displayName || "—"}</td>
                  <td>
                    <span className="badge">{roleLabel(u.role as Role)}</span>
                  </td>
                  <td className="text-muted">{u.wardIds?.join(", ") || "All"}</td>
                  <td>
                    <span className={`badge ${u.active ? "badge-active" : "badge-inactive"}`}>
                      {u.active ? "Active" : "Inactive"}
                    </span>
                  </td>
                  <td>
                    <div className="inline-actions">
                      <button className="btn-ghost btn-sm" onClick={() => openEdit(u)}>Edit</button>
                      <button
                        className="btn-ghost btn-sm"
                        onClick={() => handleToggleActive(u)}
                        style={{ color: u.active ? "var(--semantic-error)" : "var(--semantic-success)" }}
                      >
                        {u.active ? "Deactivate" : "Activate"}
                      </button>
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
        <div className="overlay" onClick={(e) => { if (e.target === e.currentTarget) closeModal(); }}>
          <form className="modal" onSubmit={handleSave}>
            <h2>{editingId ? "Edit User" : "Add User"}</h2>
            {formError && <div className="login-error">{formError}</div>}

            <div className="form-group">
              <label>Username *</label>
              <input
                type="text"
                value={form.username}
                onChange={(e) => setForm({ ...form, username: e.target.value })}
                placeholder="e.g. nurse1"
                disabled={!!editingId}
                required
              />
            </div>

            <div className="form-group">
              <label>Password {!editingId && "*"}</label>
              <input
                type="password"
                value={form.password}
                onChange={(e) => setForm({ ...form, password: e.target.value })}
                placeholder={editingId ? "(leave blank to keep current)" : "Enter password"}
              />
            </div>

            <div className="form-group">
              <label>Display Name *</label>
              <input
                type="text"
                value={form.displayName}
                onChange={(e) => setForm({ ...form, displayName: e.target.value })}
                placeholder="e.g. Nurse Joy"
                required
              />
            </div>

            <div className="form-group">
              <label>Role</label>
              <select value={form.role} onChange={(e) => setForm({ ...form, role: Number(e.target.value) as Role })}>
                {ROLE_OPTIONS.map((opt) => (
                  <option key={opt.value} value={opt.value}>{opt.label}</option>
                ))}
              </select>
            </div>

            <div className="form-group">
              <label>Ward IDs</label>
              <input
                type="text"
                value={form.wardIds}
                onChange={(e) => setForm({ ...form, wardIds: e.target.value })}
                placeholder="e.g. WARD-3A, WARD-9Z (comma-separated, leave empty for all)"
              />
              <span className="text-muted" style={{ fontSize: "0.8rem" }}>
                Empty = all wards (for ADMIN role). Separate wards with commas.
              </span>
            </div>

            <div className="form-actions">
              <button type="button" className="btn-secondary" onClick={closeModal}>Cancel</button>
              <button type="submit" className="btn-primary" disabled={saving}>
                {saving ? "Saving…" : editingId ? "Save Changes" : "Create User"}
              </button>
            </div>
          </form>
        </div>
      )}
    </div>
  );
}
