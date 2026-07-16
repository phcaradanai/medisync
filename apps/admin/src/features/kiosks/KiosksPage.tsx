import { useState, useEffect, useCallback, type FormEvent } from "react";
import { create } from "@bufbuild/protobuf";
import {
  ListKiosksRequestSchema,
  CreateKioskRequestSchema,
  UpdateKioskRequestSchema,
  ResetKioskPinRequestSchema,
  type Kiosk,
} from "@medisync/proto/medisync/kiosk/v1/kiosk_pb";
import { kioskClient } from "../../api/client";

// ── Types ──────────────────────────────────────────────────────────

interface KioskFormData {
  code: string;
  displayName: string;
  pin: string;
}

const emptyForm: KioskFormData = {
  code: "",
  displayName: "",
  pin: "",
};

// ── Component ──────────────────────────────────────────────────────

export function KiosksPage() {
  const [kiosks, setKiosks] = useState<Kiosk[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  // Modal state
  const [showCreate, setShowCreate] = useState(false);
  const [showPin, setShowPin] = useState(false);
  const [pinResult, setPinResult] = useState<string | null>(null);
  const [form, setForm] = useState<KioskFormData>(emptyForm);
  const [saving, setSaving] = useState(false);
  const [formError, setFormError] = useState<string | null>(null);

  // ── Data fetching ──────────────────────────────────────────────

  const fetchKiosks = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const res = await kioskClient.listKiosks(create(ListKiosksRequestSchema, {}));
      setKiosks(res.kiosks);
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : "Failed to load kiosks");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchKiosks();
  }, [fetchKiosks]);

  // ── Create Kiosk ───────────────────────────────────────────────

  function openCreate() {
    setForm(emptyForm);
    setFormError(null);
    setPinResult(null);
    setShowCreate(true);
  }

  async function handleCreate(e: FormEvent) {
    e.preventDefault();
    setFormError(null);

    if (!form.code.trim()) {
      setFormError("Kiosk code is required");
      return;
    }
    if (!form.displayName.trim()) {
      setFormError("Display name is required");
      return;
    }
    if (!form.pin) {
      setFormError("PIN is required");
      return;
    }
    if (form.pin.length < 4) {
      setFormError("PIN must be at least 4 characters");
      return;
    }

    setSaving(true);
    try {
      const res = await kioskClient.createKiosk(create(CreateKioskRequestSchema, {
        code: form.code.trim(),
        displayName: form.displayName.trim(),
        pin: form.pin,
      }));
      if (res.kiosk?.pin) {
        setPinResult(res.kiosk.pin);
      }
      await fetchKiosks();
    } catch (e: unknown) {
      setFormError(e instanceof Error ? e.message : "Create failed");
    } finally {
      setSaving(false);
    }
  }

  // ── Toggle active ──────────────────────────────────────────────

  async function handleToggleActive(k: Kiosk) {
    try {
      await kioskClient.updateKiosk(create(UpdateKioskRequestSchema, {
        id: k.id,
        active: !k.active,
      }));
      await fetchKiosks();
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : "Update failed");
    }
  }

  // ── Reset PIN ──────────────────────────────────────────────────

  function openPinReset(k: Kiosk) {
    setForm({ code: k.code, displayName: k.displayName, pin: "" });
    setFormError(null);
    setPinResult(null);
    setShowPin(true);
  }

  async function handlePinReset(e: FormEvent) {
    e.preventDefault();
    setFormError(null);

    if (!form.pin || form.pin.length < 4) {
      setFormError("New PIN must be at least 4 characters");
      return;
    }

    setSaving(true);
    try {
      const res = await kioskClient.resetKioskPin(create(ResetKioskPinRequestSchema, {
        id: kiosks.find(k => k.code === form.code)?.id ?? "",
        newPin: form.pin,
      }));
      if (res.kiosk?.pin) {
        setPinResult(res.kiosk.pin);
      }
    } catch (e: unknown) {
      setFormError(e instanceof Error ? e.message : "PIN reset failed");
    } finally {
      setSaving(false);
    }
  }

  function closeModal() {
    setShowCreate(false);
    setShowPin(false);
    setPinResult(null);
    setForm(emptyForm);
    setFormError(null);
  }

  // ── Render ─────────────────────────────────────────────────────

  return (
    <div>
      <div className="page-header">
        <h1>Kiosks</h1>
        <div className="page-header-actions">
          <button className="btn-primary" onClick={openCreate}>
            + Add Kiosk
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
        {loading && kiosks.length === 0 ? (
          <div className="empty-state">Loading…</div>
        ) : kiosks.length === 0 ? (
          <div className="empty-state">No kiosks yet. Click + Add Kiosk to register a kiosk terminal.</div>
        ) : (
          <table>
            <thead>
              <tr>
                <th>Code</th>
                <th>Name</th>
                <th>PIN</th>
                <th>Status</th>
                <th style={{ width: 160 }}>Actions</th>
              </tr>
            </thead>
            <tbody>
              {kiosks.map((k) => (
                <tr key={k.id}>
                  <td className="mono">{k.code}</td>
                  <td>{k.displayName || "—"}</td>
                  <td className="text-muted">********</td>
                  <td>
                    <span className={`badge ${k.active ? "badge-active" : "badge-inactive"}`}>
                      {k.active ? "Active" : "Inactive"}
                    </span>
                  </td>
                  <td>
                    <div className="inline-actions">
                      <button
                        className="btn-ghost btn-sm"
                        onClick={() => handleToggleActive(k)}
                        style={{ color: k.active ? "var(--semantic-error)" : "var(--semantic-success)" }}
                      >
                        {k.active ? "Deactivate" : "Activate"}
                      </button>
                      <button
                        className="btn-ghost btn-sm"
                        onClick={() => openPinReset(k)}
                      >
                        Reset PIN
                      </button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>

      {/* ── Create Kiosk Modal ────────────────────────────────── */}
      {showCreate && (
        <div className="overlay" onClick={(e) => { if (e.target === e.currentTarget) closeModal(); }}>
          <form className="modal" onSubmit={handleCreate}>
            <h2>Register Kiosk</h2>
            {formError && <div className="login-error">{formError}</div>}

            {pinResult ? (
              <div className="rx-detail" style={{ padding: "var(--sp-lg)", background: "var(--semantic-warning)", borderRadius: "var(--radius-md)" }}>
                <div className="rx-detail__heading" style={{ color: "var(--text-on-warning)" }}>
                  ⚠️ Save this PIN — it will not be shown again
                </div>
                <div className="rx-detail__value" style={{ fontFamily: "var(--font-mono)", fontSize: "1.5rem", textAlign: "center", padding: "var(--sp-md)", background: "#fff", borderRadius: "var(--radius-sm)", marginTop: "var(--sp-sm)" }}>
                  {pinResult}
                </div>
              </div>
            ) : (
              <>
                <div className="form-group">
                  <label>Kiosk Code *</label>
                  <input
                    type="text"
                    value={form.code}
                    onChange={(e) => setForm({ ...form, code: e.target.value })}
                    placeholder="e.g. KIOSK-1"
                    required
                  />
                </div>

                <div className="form-group">
                  <label>Display Name *</label>
                  <input
                    type="text"
                    value={form.displayName}
                    onChange={(e) => setForm({ ...form, displayName: e.target.value })}
                    placeholder="e.g. ตู้ชั้น 1 โถงผู้ป่วยนอก"
                    required
                  />
                </div>

                <div className="form-group">
                  <label>PIN * (min 4 characters)</label>
                  <input
                    type="password"
                    value={form.pin}
                    onChange={(e) => setForm({ ...form, pin: e.target.value })}
                    placeholder="At least 4 characters"
                    required
                    minLength={4}
                  />
                </div>
              </>
            )}

            <div className="form-actions">
              <button type="button" className="btn-secondary" onClick={closeModal}>Close</button>
              {!pinResult && (
                <button type="submit" className="btn-primary" disabled={saving}>
                  {saving ? "Creating…" : "Create Kiosk"}
                </button>
              )}
            </div>
          </form>
        </div>
      )}

      {/* ── Reset PIN Modal ───────────────────────────────────── */}
      {showPin && (
        <div className="overlay" onClick={(e) => { if (e.target === e.currentTarget) closeModal(); }}>
          <form className="modal" onSubmit={handlePinReset}>
            <h2>Reset PIN — {form.code}</h2>
            {formError && <div className="login-error">{formError}</div>}

            {pinResult ? (
              <div className="rx-detail" style={{ padding: "var(--sp-lg)", background: "var(--semantic-warning)", borderRadius: "var(--radius-md)" }}>
                <div className="rx-detail__heading" style={{ color: "var(--text-on-warning)" }}>
                  ⚠️ New PIN — save it now
                </div>
                <div className="rx-detail__value" style={{ fontFamily: "var(--font-mono)", fontSize: "1.5rem", textAlign: "center", padding: "var(--sp-md)", background: "#fff", borderRadius: "var(--radius-sm)", marginTop: "var(--sp-sm)" }}>
                  {pinResult}
                </div>
              </div>
            ) : (
              <div className="form-group">
                <label>New PIN * (min 4 characters)</label>
                <input
                  type="password"
                  value={form.pin}
                  onChange={(e) => setForm({ ...form, pin: e.target.value })}
                  placeholder="At least 4 characters"
                  required
                  minLength={4}
                  autoFocus
                />
              </div>
            )}

            <div className="form-actions">
              <button type="button" className="btn-secondary" onClick={closeModal}>Close</button>
              {!pinResult && (
                <button type="submit" className="btn-primary" disabled={saving}>
                  {saving ? "Resetting…" : "Reset PIN"}
                </button>
              )}
            </div>
          </form>
        </div>
      )}
    </div>
  );
}
