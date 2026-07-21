import { useState, useEffect, useCallback, type FormEvent } from "react";
import { useSearchParams } from "react-router-dom";
import { create } from "@bufbuild/protobuf";
import {
  ListSlotsRequestSchema,
  AssignDrugRequestSchema,
  RefillRequestSchema,
  AdjustStockRequestSchema,
  CreateSlotRequestSchema,
} from "@medisync/proto/medisync/inventory/v1/inventory_pb";
import type { Slot } from "@medisync/proto/medisync/inventory/v1/inventory_pb";
import type { Drug } from "@medisync/proto/medisync/catalog/v1/catalog_pb";
import { ListProjectsRequestSchema } from "@medisync/proto/medisync/identity/v1/identity_pb";
import type { Project } from "@medisync/proto/medisync/identity/v1/identity_pb";
import { inventoryClient, catalogClient, kioskClient, projectClient } from "../../api/client";
import { ListDrugsRequestSchema } from "@medisync/proto/medisync/catalog/v1/catalog_pb";
import { ListKiosksRequestSchema } from "@medisync/proto/medisync/kiosk/v1/kiosk_pb";
import type { Kiosk } from "@medisync/proto/medisync/kiosk/v1/kiosk_pb";

// ── Types ──────────────────────────────────────────────────────────

interface AssignForm {
  drugId: string;
  drugCode: string;
  drugName: string;
  capacity: number;
  lowThreshold: number;
}

interface CreateSlotForm {
  cabinetId: string;
  code: string;
  displayName: string;
  capacity: number;
  lowThreshold: number;
  projectId: string;
  shelf: number;
  rowNum: number;
}

const emptyAssign: AssignForm = { drugId: "", drugCode: "", drugName: "", capacity: 50, lowThreshold: 5 };

const emptySlotForm: CreateSlotForm = {
  cabinetId: "", code: "", displayName: "", capacity: 100, lowThreshold: 10,
  projectId: "", shelf: 1, rowNum: 1,
};

// ── Component ──────────────────────────────────────────────────────

export function InventoryPage() {
  const [searchParams, setSearchParams] = useSearchParams();
  const linkedDrugId = searchParams.get("drugId") ?? "";
  const linkedDrugCode = searchParams.get("drugCode") ?? "";
  const [slots, setSlots] = useState<Slot[]>([]);
  const [drugs, setDrugs] = useState<Drug[]>([]);
  const [kiosks, setKiosks] = useState<Kiosk[]>([]);
  const [projects, setProjects] = useState<Project[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  // Modal state
  const [showCreate, setShowCreate] = useState(false);
  const [showAssign, setShowAssign] = useState(false);
  const [showRefill, setShowRefill] = useState(false);
  const [showAdjust, setShowAdjust] = useState(false);
  const [selectedSlot, setSelectedSlot] = useState<Slot | null>(null);
  const [assignForm, setAssignForm] = useState<AssignForm>(emptyAssign);
  const [slotForm, setSlotForm] = useState<CreateSlotForm>(emptySlotForm);
  const [refillQty, setRefillQty] = useState(0);
  const [adjustQty, setAdjustQty] = useState(0);
  const [adjustReason, setAdjustReason] = useState("");
  const [saving, setSaving] = useState(false);
  const [formError, setFormError] = useState<string | null>(null);
  const [drugFilter, setDrugFilter] = useState("");

  // ── Data fetching ──────────────────────────────────────────────

  const fetchAll = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const [slotsRes, drugsRes, kioRes, projRes] = await Promise.all([
        inventoryClient.listSlots(create(ListSlotsRequestSchema, {})),
        catalogClient.listDrugs(create(ListDrugsRequestSchema, { query: "", includeInactive: false, pageSize: 200 })),
        kioskClient.listKiosks(create(ListKiosksRequestSchema, {})),
        projectClient.listProjects(create(ListProjectsRequestSchema, {})),
      ]);
      setSlots(slotsRes.slots);
      setDrugs(drugsRes.drugs);
      setKiosks(kioRes.kiosks);
      setProjects(projRes.projects.filter(p => p.active));
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : "Failed to load data");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => { fetchAll(); }, [fetchAll]);

  // ── Create Slot ─────────────────────────────────────────────────

  function openCreate() {
    setSlotForm({
      ...emptySlotForm,
      projectId: projects[0]?.id ?? "",
      cabinetId: kiosks[0]?.code ?? "",
    });
    setFormError(null);
    setShowCreate(true);
  }

  async function handleCreateSlot(e: FormEvent) {
    e.preventDefault();
    setFormError(null);
    if (!slotForm.code.trim()) { setFormError("Code is required"); return; }
    if (!slotForm.cabinetId) { setFormError("Cabinet is required"); return; }
    if (!slotForm.projectId) { setFormError("Project is required"); return; }
    setSaving(true);
    try {
      await inventoryClient.createSlot(create(CreateSlotRequestSchema, {
        cabinetId: slotForm.cabinetId,
        code: slotForm.code.trim(),
        displayName: slotForm.displayName.trim() || slotForm.code.trim(),
        capacity: slotForm.capacity,
        lowThreshold: slotForm.lowThreshold,
        projectId: slotForm.projectId,
      }));
      setShowCreate(false);
      await fetchAll();
    } catch (e: unknown) {
      setFormError(e instanceof Error ? e.message : "Create slot failed");
    } finally { setSaving(false); }
  }

  // ── Assign drug to slot ────────────────────────────────────────

  function openAssign(slot: Slot) {
    setSelectedSlot(slot);
    const linkedDrug = linkedDrugId ? drugs.find((drug) => drug.id === linkedDrugId) : undefined;
    setAssignForm(linkedDrug ? {
      drugId: linkedDrug.id, drugCode: linkedDrug.code, drugName: linkedDrug.name,
      capacity: linkedDrug.defaultSlotCapacity || slot.capacity || 50, lowThreshold: slot.lowThreshold,
    } : slot.drugId ? {
      drugId: slot.drugId, drugCode: slot.drugCode, drugName: slot.drugName,
      capacity: slot.capacity, lowThreshold: slot.lowThreshold,
    } : emptyAssign);
    setDrugFilter("");
    setFormError(null);
    setShowAssign(true);
  }

  async function handleAssign(e: FormEvent) {
    e.preventDefault(); setFormError(null);
    if (!assignForm.drugId) { setFormError("Please select a drug"); return; }
    if (!selectedSlot) return;
    setSaving(true);
    try {
      await inventoryClient.assignDrug(create(AssignDrugRequestSchema, {
        slotId: selectedSlot.id, drugId: assignForm.drugId,
        capacity: assignForm.capacity, lowThreshold: assignForm.lowThreshold,
      }));
      setShowAssign(false); await fetchAll();
    } catch (e: unknown) {
      setFormError(e instanceof Error ? e.message : "Assign failed");
    } finally { setSaving(false); }
  }

  // ── Refill ─────────────────────────────────────────────────────

  function openRefill(slot: Slot) { setSelectedSlot(slot); setRefillQty(0); setFormError(null); setShowRefill(true); }

  async function handleRefill(e: FormEvent) {
    e.preventDefault(); setFormError(null);
    if (refillQty <= 0) { setFormError("Quantity must be positive"); return; }
    if (!selectedSlot) return;
    setSaving(true);
    try {
      await inventoryClient.refill(create(RefillRequestSchema, {
        slotId: selectedSlot.id, quantityAdded: refillQty, traceId: crypto.randomUUID(),
      }));
      setShowRefill(false); await fetchAll();
    } catch (e: unknown) {
      setFormError(e instanceof Error ? e.message : "Refill failed");
    } finally { setSaving(false); }
  }

  // ── Adjust stock ──────────────────────────────────────────────

  function openAdjust(slot: Slot) { setSelectedSlot(slot); setAdjustQty(slot.quantity); setAdjustReason(""); setFormError(null); setShowAdjust(true); }

  async function handleAdjust(e: FormEvent) {
    e.preventDefault(); setFormError(null);
    if (adjustQty < 0) { setFormError("Quantity must not be negative"); return; }
    if (!adjustReason.trim()) { setFormError("Reason is required"); return; }
    if (!selectedSlot) return;
    setSaving(true);
    try {
      await inventoryClient.adjustStock(create(AdjustStockRequestSchema, {
        slotId: selectedSlot.id, newQuantity: adjustQty, reason: adjustReason.trim(), traceId: crypto.randomUUID(),
      }));
      setShowAdjust(false); await fetchAll();
    } catch (e: unknown) {
      setFormError(e instanceof Error ? e.message : "Adjust failed");
    } finally { setSaving(false); }
  }

  // ── Drug selector ─────────────────────────────────────────────

  function selectDrug(drug: Drug) {
    setAssignForm({
      drugId: drug.id,
      drugCode: drug.code,
      drugName: drug.name,
      capacity: drug.defaultSlotCapacity || assignForm.capacity,
      lowThreshold: assignForm.lowThreshold,
    });
    setDrugFilter("");
  }

  const filteredDrugs = drugFilter
    ? drugs.filter(d => d.code.toLowerCase().includes(drugFilter.toLowerCase()) || d.name.toLowerCase().includes(drugFilter.toLowerCase()))
    : drugs;

  const cabinetCode = (code: string) => kiosks.find(k => k.code === code)?.code || code || "—";

  const toggleEmergency = useCallback((slot: Slot) => {
    // Update local state and call backend
    setSlots(prev => prev.map(s => s.id === slot.id ? { ...s, emergencyDrug: !(s as any).emergencyDrug } as any : s));
    // TODO: add UpdateSlotEmergency RPC when available
  }, []);

  // ── Render ─────────────────────────────────────────────────────

  return (
    <div>
      <div className="page-header">
        <h1>Inventory — Slot Management</h1>
        <div className="page-header-actions">
          <button className="btn-primary" onClick={openCreate}>+ Create Slot</button>
          <button className="btn-ghost btn-sm" onClick={fetchAll}>Refresh</button>
        </div>
      </div>

      {linkedDrugId && <div className="md-link-context" role="status"><div><strong>กำลังจัดการการเชื่อมโยงยา {linkedDrugCode || linkedDrugId}</strong><span>เลือก “Assign” หรือ “Reassign” ที่ช่องจริง ระบบจะเตรียมยานี้ให้โดยอัตโนมัติ</span></div><button type="button" className="md-btn md-btn-ghost" onClick={() => setSearchParams({})}>ล้างบริบท</button></div>}

      {error && <div className="login-error mb-md" style={{ marginBottom: "var(--sp-lg)" }}>{error}<button className="btn-ghost btn-sm" style={{ marginLeft: "var(--sp-md)" }} onClick={() => setError(null)}>Dismiss</button></div>}

      <div className="table-wrap">
        {loading && slots.length === 0 ? (
          <div className="empty-state">Loading…</div>
        ) : slots.length === 0 ? (
          <div className="empty-state">No slots yet. Click + Create Slot.</div>
        ) : (
          <table>
            <thead>
              <tr>
                <th>Cabinet</th>
                <th>Code</th>
                <th>Shelf-Row</th>
                <th>Drug</th>
                <th>Stock</th>
                <th>Expiry</th>
                <th style={{ width: 60 }}>🚨 Emerg</th>
                <th style={{ width: 200 }}>Actions</th>
              </tr>
            </thead>
            <tbody>
              {slots.map((s) => (
                <tr key={s.id} className={linkedDrugId && s.drugId === linkedDrugId ? "md-linked-slot" : ""}>
                  <td className="text-muted">{cabinetCode(s.cabinetId)}</td>
                  <td className="mono">{s.code}</td>
                  <td>{s.shelf || 1}-{s.rowNum || 1}</td>
                  <td>{s.drugCode ? <><strong>{s.drugName}</strong><br/><span className="text-muted mono">{s.drugCode}</span></> : <span className="text-muted">Unassigned</span>}</td>
                  <td><span className={s.quantity <= s.lowThreshold ? "badge badge-error" : ""}>{s.quantity}</span>{s.quantity <= s.lowThreshold && s.quantity > 0 && <span className="badge badge-warning" style={{ marginLeft: 4 }}>Low</span>}</td>
                  <td className="text-muted">{s.expiryDate ? new Date(Number(s.expiryDate.seconds) * 1000).toLocaleDateString() : "—"}</td>
                  <td>
                    <input type="checkbox" checked={(s as any).emergencyDrug || false} onChange={() => toggleEmergency(s)} title="Emergency drug access" style={{ width: 18, height: 18, cursor: "pointer" }} />
                  </td>
                  <td>
                    <div className="inline-actions">
                      <button className="btn-ghost btn-sm" onClick={() => openAssign(s)}>{s.drugId ? "Reassign" : "Assign"}</button>
                      {s.drugId && <><button className="btn-ghost btn-sm" onClick={() => openRefill(s)}>Refill</button><button className="btn-ghost btn-sm" onClick={() => openAdjust(s)}>Adjust</button></>}
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>

      {/* ── Create Slot Modal ──────────────────────────────────── */}
      {showCreate && (
        <div className="overlay" onClick={(e) => { if (e.target === e.currentTarget) setShowCreate(false); }}>
          <form className="modal" onSubmit={handleCreateSlot}>
            <h2>Create Slot</h2>
            {formError && <div className="login-error">{formError}</div>}

            <div className="form-group">
              <label>Cabinet *</label>
              <select value={slotForm.cabinetId} onChange={(e) => setSlotForm(f => ({ ...f, cabinetId: e.target.value }))} required>
                <option value="">-- Select Cabinet --</option>
                {kiosks.filter(k => k.active).map(k => <option key={k.id} value={k.code}>{k.code} — {k.displayName}</option>)}
              </select>
            </div>

            <div className="form-row">
              <div className="form-group">
                <label>Code *</label>
                <input type="text" value={slotForm.code} onChange={(e) => setSlotForm(f => ({ ...f, code: e.target.value }))} placeholder="e.g. A01" required />
              </div>
              <div className="form-group">
                <label>Display Name</label>
                <input type="text" value={slotForm.displayName} onChange={(e) => setSlotForm(f => ({ ...f, displayName: e.target.value }))} placeholder="e.g. Slot A01" />
              </div>
            </div>

            <div className="form-row">
              <div className="form-group">
                <label>Shelf</label>
                <input type="number" min={1} max={5} value={slotForm.shelf} onChange={(e) => setSlotForm(f => ({ ...f, shelf: parseInt(e.target.value) || 1 }))} />
              </div>
              <div className="form-group">
                <label>Row</label>
                <input type="number" min={1} max={22} value={slotForm.rowNum} onChange={(e) => setSlotForm(f => ({ ...f, rowNum: parseInt(e.target.value) || 1 }))} />
              </div>
            </div>

            <div className="form-row">
              <div className="form-group">
                <label>Capacity</label>
                <input type="number" min={1} value={slotForm.capacity} onChange={(e) => setSlotForm(f => ({ ...f, capacity: parseInt(e.target.value) || 0 }))} />
              </div>
              <div className="form-group">
                <label>Low Threshold</label>
                <input type="number" min={0} value={slotForm.lowThreshold} onChange={(e) => setSlotForm(f => ({ ...f, lowThreshold: parseInt(e.target.value) || 0 }))} />
              </div>
            </div>

            <div className="form-group">
              <label>Project *</label>
              <select value={slotForm.projectId} onChange={(e) => setSlotForm(f => ({ ...f, projectId: e.target.value }))} required>
                <option value="">-- Select Project --</option>
                {projects.map(p => <option key={p.id} value={p.id}>{p.name}</option>)}
              </select>
            </div>

            <div className="form-actions">
              <button type="button" className="btn-secondary" onClick={() => setShowCreate(false)}>Cancel</button>
              <button type="submit" className="btn-primary" disabled={saving}>{saving ? "Creating…" : "Create Slot"}</button>
            </div>
          </form>
        </div>
      )}

      {/* ── Assign Drug Modal ──────────────────────────────────── */}
      {showAssign && selectedSlot && (
        <div className="overlay" onClick={(e) => { if (e.target === e.currentTarget) setShowAssign(false); }}>
          <form className="modal" onSubmit={handleAssign}>
            <h2>{selectedSlot.drugId ? "Reassign Drug" : "Assign Drug"} — Slot {selectedSlot.code}</h2>
            {formError && <div className="login-error">{formError}</div>}
            <div className="form-group">
              <label>Search Drug</label>
              <input type="text" value={assignForm.drugId ? `${assignForm.drugCode} — ${assignForm.drugName}` : ""}
                onChange={(e) => { setDrugFilter(e.target.value); setAssignForm({ ...assignForm, drugId: "", drugCode: "", drugName: "" }); }}
                placeholder="Type to search…" />
            </div>
            {!assignForm.drugId && drugFilter && (
              <div style={{ maxHeight: 200, overflowY: "auto", marginBottom: "var(--sp-md)", border: "1px solid var(--semantic-border)", borderRadius: 6 }}>
                {filteredDrugs.length === 0 ? <div style={{ padding: "var(--sp-md)", color: "var(--semantic-muted)" }}>No drugs match</div> :
                  filteredDrugs.slice(0, 20).map(d => (
                    <button key={d.id} type="button" className="btn-ghost" style={{ display: "block", width: "100%", textAlign: "left", padding: "var(--sp-sm) var(--sp-md)" }}
                      onClick={() => selectDrug(d)}><span className="mono">{d.code}</span> — {d.name}</button>))
                }
              </div>
            )}
            <div className="form-row">
              <div className="form-group"><label>Capacity</label><input type="number" min={1} value={assignForm.capacity} onChange={(e) => setAssignForm({ ...assignForm, capacity: parseInt(e.target.value) || 0 })} /></div>
              <div className="form-group"><label>Low Threshold</label><input type="number" min={0} value={assignForm.lowThreshold} onChange={(e) => setAssignForm({ ...assignForm, lowThreshold: parseInt(e.target.value) || 0 })} /></div>
            </div>
            <div className="form-actions">
              <button type="button" className="btn-secondary" onClick={() => setShowAssign(false)}>Cancel</button>
              <button type="submit" className="btn-primary" disabled={saving}>{saving ? "Saving…" : selectedSlot.drugId ? "Reassign Drug" : "Assign Drug"}</button>
            </div>
          </form>
        </div>
      )}

      {/* ── Refill Modal ───────────────────────────────────────── */}
      {showRefill && selectedSlot && (
        <div className="overlay" onClick={(e) => { if (e.target === e.currentTarget) setShowRefill(false); }}>
          <form className="modal" onSubmit={handleRefill}>
            <h2>Refill — {selectedSlot.drugName || selectedSlot.code}</h2>
            <p className="text-muted">Current stock: {selectedSlot.quantity} / {selectedSlot.capacity}</p>
            {formError && <div className="login-error">{formError}</div>}
            <div className="form-group"><label>Quantity to Add</label><input type="number" min={1} value={refillQty || ""} onChange={(e) => setRefillQty(parseInt(e.target.value) || 0)} autoFocus /></div>
            <div className="form-actions"><button type="button" className="btn-secondary" onClick={() => setShowRefill(false)}>Cancel</button><button type="submit" className="btn-primary" disabled={saving}>{saving ? "Saving…" : "Refill"}</button></div>
          </form>
        </div>
      )}

      {/* ── Adjust Stock Modal ─────────────────────────────────── */}
      {showAdjust && selectedSlot && (
        <div className="overlay" onClick={(e) => { if (e.target === e.currentTarget) setShowAdjust(false); }}>
          <form className="modal" onSubmit={handleAdjust}>
            <h2>Adjust Stock — {selectedSlot.drugName || selectedSlot.code}</h2>
            <p className="text-muted">Current stock: {selectedSlot.quantity}</p>
            {formError && <div className="login-error">{formError}</div>}
            <div className="form-group"><label>New Quantity</label><input type="number" min={0} value={adjustQty} onChange={(e) => setAdjustQty(parseInt(e.target.value) || 0)} autoFocus /></div>
            <div className="form-group"><label>Reason *</label><input type="text" value={adjustReason} onChange={(e) => setAdjustReason(e.target.value)} placeholder="e.g. Physical count mismatch…" required /></div>
            <div className="form-actions"><button type="button" className="btn-secondary" onClick={() => setShowAdjust(false)}>Cancel</button><button type="submit" className="btn-primary" disabled={saving}>{saving ? "Saving…" : "Adjust Stock"}</button></div>
          </form>
        </div>
      )}
    </div>
  );
}
