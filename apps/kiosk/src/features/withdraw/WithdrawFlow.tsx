import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { create } from "@bufbuild/protobuf";
import { Code, ConnectError, createClient, type Interceptor } from "@connectrpc/connect";
import { createConnectTransport } from "@connectrpc/connect-web";
import {
  CancelDispenseRequestSchema,
  ConfirmDispenseRequestSchema,
  DispensingService,
  DispenseTransactionStatus,
  GetDispenseTransactionRequestSchema,
  PrepareDispenseRequestSchema,
  PrescriptionSchema,
  type DispenseTransaction,
  type Prescription,
} from "@medisync/proto/medisync/dispensing/v1/dispensing_pb";
import { IdentityService, CardLoginRequestSchema, type User } from "@medisync/proto/medisync/identity/v1/identity_pb";
import { InventoryService, ListSlotsRequestSchema, type Slot } from "@medisync/proto/medisync/inventory/v1/inventory_pb";
import { transport } from "../../transport.ts";
import { useAuth } from "../../auth.tsx";
import { parseKioskTesterCommand, subscribeToKioskTester, type KioskTesterCommand } from "../../kiosktesterBridge.ts";
import ShelfGrid, { getSlotPosition } from "../catalog/ShelfGrid.tsx";
import DispensingStatusModal from "../dispensing/DispensingStatusModal.tsx";
import DispenseProcessingOverlay from "../dispensing/DispenseProcessingOverlay.tsx";
import EmergencyDispenseModal from "../emergency/EmergencyDispenseModal.tsx";

export type ScannerState = "awaiting_scan" | "validating_sticker" | "scan_failed" | "request_loaded" | "awaiting_identity" | "verifying_identity" | "submitting_to_hardware" | "submission_uncertain" | "identity_failed" | "unauthorized";
type QueueState = "queued" | "dispensing" | "completed" | "failed" | "unknown";
type QueueTransaction = { id: string; requestId: string; prescription: Prescription; operator: string; acceptedAt: number; state: QueueState; reconnecting?: boolean; message?: string };
type PersistedQueueSummary = Pick<QueueTransaction, "id" | "requestId" | "operator" | "acceptedAt">;

const dispensingClient = createClient(DispensingService, transport);
const identityClient = createClient(IdentityService, transport);
const inventoryClient = createClient(InventoryService, transport);
const ACTIVE_QUEUE_KEY = "medisync.active-withdrawals.v1";

function staffDispensingClient(token: string, kioskToken: string) {
  const staffAuth: Interceptor = (next) => (req) => {
    req.header.set("Authorization", `Bearer ${token}`);
    req.header.set("X-Kiosk-Authorization", `Bearer ${kioskToken}`);
    return next(req);
  };
  return createClient(DispensingService, createConnectTransport({ baseUrl: "/", interceptors: [staffAuth] }));
}
function stateStep(state: ScannerState) { return ["awaiting_scan", "validating_sticker", "scan_failed"].includes(state) ? 1 : state === "request_loaded" ? 2 : 3; }
function queueState(tx: DispenseTransaction): QueueState {
  if (tx.status === DispenseTransactionStatus.DISPENSED) return "completed";
  if ([DispenseTransactionStatus.FAILED, DispenseTransactionStatus.CANCELLED, DispenseTransactionStatus.EXPIRED].includes(tx.status)) return "failed";
  if (tx.status === DispenseTransactionStatus.DISPENSING) return "dispensing";
  if (tx.status === DispenseTransactionStatus.QUEUED) return "queued";
  return "unknown";
}
function prescriptionFromTransaction(tx: DispenseTransaction): Prescription {
  return create(PrescriptionSchema, {
    prescriptionId: tx.prescriptionId,
    failureReason: tx.failureDetail,
    items: tx.items.map((item) => ({
      drugCode: item.drugCode,
      drugName: item.drugName,
      quantity: item.requestedQuantity,
    })),
  });
}
function loadQueueSummaries(): PersistedQueueSummary[] { try { return JSON.parse(localStorage.getItem(ACTIVE_QUEUE_KEY) || "[]") as PersistedQueueSummary[]; } catch { return []; } }

export default function WithdrawFlow() {
  const { state: auth } = useAuth();
  const kiosk = auth!.kiosk;
  const [workflow, setWorkflow] = useState<ScannerState>("awaiting_scan");
  const [prescription, setPrescription] = useState<Prescription | null>(null);
  const [preparedTransaction, setPreparedTransaction] = useState<DispenseTransaction | null>(null);
  const [slots, setSlots] = useState<Slot[]>([]);
  const [queue, setQueue] = useState<QueueTransaction[]>([]);
  const [cartOpen, setCartOpen] = useState(false);
  const [statusOpen, setStatusOpen] = useState(false);
  const [emergencyOpen, setEmergencyOpen] = useState(false);
  const [emergencyCardToken, setEmergencyCardToken] = useState("");
  const [activeTxnId, setActiveTxnId] = useState<string | null>(null);
  const [message, setMessage] = useState("");
  const [notice, setNotice] = useState("");
  const [now, setNow] = useState(() => new Date());
  const [online, setOnline] = useState(() => navigator.onLine);
  const scannerBuffer = useRef("");
  const scannerTimer = useRef<number | null>(null);
  const scanLock = useRef(false);
  const identityLock = useRef(false);
  const submitLock = useRef(false);
  const requestGeneration = useRef(0);
  const persistedQueue = useRef(new Map(loadQueueSummaries().map((item) => [item.id, item])));

  useEffect(() => {
    const clock = window.setInterval(() => setNow(new Date()), 1_000);
    const updateConnection = () => setOnline(navigator.onLine);
    window.addEventListener("online", updateConnection);
    window.addEventListener("offline", updateConnection);
    return () => {
      window.clearInterval(clock);
      window.removeEventListener("online", updateConnection);
      window.removeEventListener("offline", updateConnection);
    };
  }, []);

  useEffect(() => { inventoryClient.listSlots(create(ListSlotsRequestSchema, { cabinetId: kiosk.code, lowOnly: false })).then((res) => setSlots(res.slots)).catch(() => setMessage("ไม่สามารถโหลดผังช่องยาได้")); }, [kiosk.code]);

  const reconcile = useCallback(async (id: string) => {
    try {
      const res = await dispensingClient.getDispenseTransaction(create(GetDispenseTransactionRequestSchema, { dispenseId: id }));
      if (!res.transaction) return null;
      const tx = res.transaction;
      setQueue((items) => {
        const existing = items.find((item) => item.id === id);
        const restored = persistedQueue.current.get(id);
        const next: QueueTransaction = { id, requestId: tx.prescriptionId, prescription: existing?.prescription || prescriptionFromTransaction(tx), operator: tx.operatorDisplayName || existing?.operator || restored?.operator || "เจ้าหน้าที่ที่ยืนยันแล้ว", acceptedAt: existing?.acceptedAt || restored?.acceptedAt || Date.now(), state: queueState(tx), message: tx.failureDetail };
        return existing ? items.map((item) => item.id === id ? next : item) : [next, ...items];
      });
      return tx;
    } catch {
      setQueue((items) => items.map((item) => item.id === id ? { ...item, reconnecting: true, state: item.state === "unknown" ? "unknown" : item.state } : item));
      return null;
    }
  }, []);

  useEffect(() => { for (const id of persistedQueue.current.keys()) void reconcile(id); }, [reconcile]);
  useEffect(() => {
    // Do not erase the recovery registry while the first reconciliation is still loading.
    if (queue.length === 0 && persistedQueue.current.size > 0) return;
    const activeItems = queue.filter((item) => !["completed", "failed"].includes(item.state));
    const active = activeItems.map(({ id, requestId, operator, acceptedAt }) => ({ id, requestId, operator, acceptedAt }));
    persistedQueue.current = new Map(active.map((item) => [item.id, item]));
    localStorage.setItem(ACTIVE_QUEUE_KEY, JSON.stringify(active));
    if (!activeItems.length) return;
    const timer = window.setInterval(() => { for (const item of activeItems) void reconcile(item.id); }, 2_000);
    return () => window.clearInterval(timer);
  }, [queue, reconcile]);

  const resetScanner = useCallback((announcement = "") => {
    requestGeneration.current += 1;
    setWorkflow("awaiting_scan"); setPrescription(null); setPreparedTransaction(null); setMessage(""); setCartOpen(false);
    scannerBuffer.current = ""; scanLock.current = false; identityLock.current = false; submitLock.current = false;
    if (announcement) { setNotice(announcement); window.setTimeout(() => setNotice(""), 7000); }
  }, []);

  const cancelPrepared = useCallback(async () => {
    if (!preparedTransaction) {
      resetScanner();
      return;
    }
    setMessage("กำลังคืนรายการยาที่จองไว้...");
    try {
      await dispensingClient.cancelDispense(create(CancelDispenseRequestSchema, {
        dispenseId: preparedTransaction.dispenseId,
        reason: "cancelled at kiosk before identity confirmation",
      }));
      resetScanner("ยกเลิกรายการและคืนจำนวนยาที่จองไว้แล้ว");
    } catch {
      setMessage("ยกเลิกรายการไม่ได้ กรุณาตรวจสอบเครือข่ายแล้วลองใหม่");
    }
  }, [preparedTransaction, resetScanner]);

  const validateSticker = useCallback(async (raw: string) => {
    const code = raw.trim(); if (!code || scanLock.current) return;
    scanLock.current = true; setWorkflow("validating_sticker"); setMessage("");
    const duplicate = queue.find((item) => item.requestId === code || item.id === code);
    if (duplicate) { setWorkflow("scan_failed"); setMessage(`รายการนี้ถูกส่งเข้าคิวแล้ว · สถานะ ${duplicate.state}`); scanLock.current = false; return; }
    try {
      const res = await dispensingClient.prepareDispense(create(PrepareDispenseRequestSchema, { stickerCode: code, traceId: crypto.randomUUID() }));
      if (!res.prescription || !res.transaction) throw new Error("not_found");
      setPrescription(res.prescription); setPreparedTransaction(res.transaction); setWorkflow("request_loaded"); setCartOpen(true);
    } catch (error) {
      const ce = ConnectError.from(error);
      if (ce.code === Code.NotFound) setMessage("ไม่พบรายการ READY จาก Sticker นี้ หรือรายการถูกใช้ไปแล้ว");
      else if (ce.code === Code.FailedPrecondition) setMessage("ยาในตู้นี้ไม่พอหรือรายการยังไม่พร้อม กรุณาติดต่อเภสัชกร");
      else setMessage("ไม่สามารถตรวจสอบ Sticker กับระบบได้ กรุณาตรวจสอบเครือข่ายแล้วลองใหม่");
      setWorkflow("scan_failed");
    } finally { scanLock.current = false; }
  }, [queue]);

  useEffect(() => {
    if (emergencyOpen || !["awaiting_scan", "scan_failed"].includes(workflow)) return;
    const onKey = (event: KeyboardEvent) => {
      if (event.target instanceof HTMLInputElement || event.target instanceof HTMLTextAreaElement) return;
      if (event.key === "Enter") { const value = scannerBuffer.current; scannerBuffer.current = ""; if (value) { event.preventDefault(); void validateSticker(value); } }
      else if (event.key.length === 1 && !event.ctrlKey && !event.altKey && !event.metaKey) { scannerBuffer.current += event.key; if (scannerTimer.current) clearTimeout(scannerTimer.current); scannerTimer.current = window.setTimeout(() => { scannerBuffer.current = ""; }, 150); }
    };
    window.addEventListener("keydown", onKey); return () => window.removeEventListener("keydown", onKey);
  }, [emergencyOpen, workflow, validateSticker]);

  const submitAfterIdentity = useCallback(async (token: string, user: User, request: Prescription, prepared: DispenseTransaction, generation: number) => {
    if (submitLock.current || generation !== requestGeneration.current) return;
    submitLock.current = true; setWorkflow("submitting_to_hardware"); setMessage("");
    try {
      const res = await staffDispensingClient(token, auth!.token).confirmDispense(create(ConfirmDispenseRequestSchema, { dispenseId: prepared.dispenseId }));
      if (!res.transaction || res.transaction.status !== DispenseTransactionStatus.QUEUED) throw new Error("uncertain");
      const accepted = res.transaction;
      setPreparedTransaction(null);
      setQueue((items) => [{ id: accepted.dispenseId, requestId: accepted.prescriptionId, prescription: request, operator: accepted.operatorDisplayName || user.displayName, acceptedAt: Date.now(), state: "queued" }, ...items.filter((item) => item.id !== accepted.dispenseId)]);
      setActiveTxnId(accepted.dispenseId);
      resetScanner(`ส่งรายการ ${accepted.prescriptionId} เข้าคิวตู้ ${accepted.kioskCode} สำเร็จ · สามารถสแกนรายการถัดไปได้`);
    } catch (error) {
      const ce = ConnectError.from(error);
      if (ce.code === Code.PermissionDenied || ce.code === Code.NotFound) { identityLock.current = false; submitLock.current = false; setWorkflow("unauthorized"); setMessage("ระบบไม่อนุญาตให้เบิกรายการนี้ และไม่ได้ส่งคำสั่งไปยังเครื่อง"); return; }
      setWorkflow("submission_uncertain"); setMessage("ยังยืนยันผลการส่งคำสั่งไม่ได้ ระบบกำลังตรวจสอบรายการเดิม ห้ามสแกนหรือส่งซ้ำ");
      setQueue((items) => items.some((item) => item.id === prepared.dispenseId) ? items : [{ id: prepared.dispenseId, requestId: request.prescriptionId, prescription: request, operator: user.displayName, acceptedAt: Date.now(), state: "unknown", reconnecting: true }, ...items]);
      const reconciled = await reconcile(prepared.dispenseId);
      if (reconciled && [DispenseTransactionStatus.QUEUED, DispenseTransactionStatus.DISPENSING].includes(reconciled.status)) {
        setPreparedTransaction(null);
        setQueue((items) => items.map((item) => item.id === reconciled.dispenseId ? { ...item, operator: user.displayName } : item));
        setActiveTxnId(reconciled.dispenseId);
        resetScanner(`ตรวจสอบแล้ว: รายการ ${reconciled.prescriptionId} ถูกส่งเข้าคิวตู้ ${reconciled.kioskCode}`);
      }
    }
  }, [auth, reconcile, resetScanner]);

  const verifyIdentity = useCallback(async (rawToken: string) => {
    const token = rawToken.trim();
    const request = prescription;
    const prepared = preparedTransaction;
    if (!token || !request || !prepared || identityLock.current || submitLock.current) return;
    identityLock.current = true;
    const generation = requestGeneration.current;
    setWorkflow("verifying_identity"); setMessage("");
    try {
      const res = await identityClient.cardLogin(create(CardLoginRequestSchema, { cardToken: token, projectId: kiosk.projectId }));
      if (generation !== requestGeneration.current) return;
      if (!res.user || !res.accessToken || !res.user.active) throw new Error("invalid_identity");
      if (!(res.user.role === 1 || res.user.wardIds.includes(request.wardId))) { identityLock.current = false; setWorkflow("unauthorized"); setMessage("ผู้ใช้นี้ไม่มีสิทธิ์เบิกรายการของหอผู้ป่วยนี้ กรุณาใช้บัตรเจ้าหน้าที่ที่ได้รับอนุญาต"); return; }
      await submitAfterIdentity(res.accessToken, res.user, request, prepared, generation);
    } catch (error) {
      identityLock.current = false;
      if (generation !== requestGeneration.current || submitLock.current) return;
      setWorkflow("identity_failed"); setMessage("ไม่สามารถอ่านหรือยืนยันบัตรเจ้าหน้าที่ได้ กรุณาลองใหม่");
    }
  }, [kiosk.projectId, prescription, preparedTransaction, submitAfterIdentity]);

  useEffect(() => {
    if (emergencyOpen || !["awaiting_identity", "identity_failed", "unauthorized"].includes(workflow)) return;
    const onKey = (event: KeyboardEvent) => {
      if (event.target instanceof HTMLInputElement || event.target instanceof HTMLTextAreaElement) return;
      if (event.key === "Enter") { const value = scannerBuffer.current; scannerBuffer.current = ""; if (value) { event.preventDefault(); void verifyIdentity(value); } }
      else if (event.key.length === 1 && !event.ctrlKey && !event.altKey && !event.metaKey) { scannerBuffer.current += event.key; if (scannerTimer.current) clearTimeout(scannerTimer.current); scannerTimer.current = window.setTimeout(() => { scannerBuffer.current = ""; }, 150); }
    };
    window.addEventListener("keydown", onKey); return () => window.removeEventListener("keydown", onKey);
  }, [emergencyOpen, workflow, verifyIdentity]);

  // ── kiosktester integration ────────────────────────────────────────
  // The SSE bridge works for a kiosk opened independently in any tab or
  // browser. postMessage remains as a compatibility path for older testers.
  useEffect(() => {
    const handleCommand = (command: KioskTesterCommand) => {
      if (command.type === "scan_sticker") {
        if (!emergencyOpen) void validateSticker(command.code);
      } else if (emergencyOpen) {
        setEmergencyCardToken(command.cardToken);
      } else {
        void verifyIdentity(command.cardToken);
      }
    };
    const onMessage = (event: MessageEvent) => {
      const command = parseKioskTesterCommand(event.data, kiosk.code);
      if (command) handleCommand(command);
    };
    const disconnectTester = subscribeToKioskTester(kiosk.code, handleCommand);
    window.addEventListener("message", onMessage);
    return () => {
      disconnectTester();
      window.removeEventListener("message", onMessage);
    };
  }, [emergencyOpen, kiosk.code, validateSticker, verifyIdentity]);

  const requestedSlotIds = useMemo(() => { if (!prescription) return []; const codes = new Set(prescription.items.map((item) => item.drugCode)); return slots.filter((slot) => codes.has(slot.drugCode)).map((slot) => slot.id); }, [prescription, slots]);
  // Keep terminal transactions in memory long enough for the completion
  // overlay and duplicate-scan protection, but never count them as a live
  // machine queue. Unknown/reconnecting entries are surfaced as attention,
  // not as a confirmed queue position.
  const machineQueue = useMemo(
    () => queue.filter((item) => item.state === "queued" || item.state === "dispensing"),
    [queue],
  );
  const attentionCount = queue.filter((item) => item.state === "failed" || item.reconnecting).length;
  const step = stateStep(workflow);
  const handleEmergencyDispensed = useCallback((slotCode: string, dispensed: number) => {
    setSlots((current) => current.map((slot) => slot.code === slotCode ? { ...slot, quantity: Math.max(0, slot.quantity - dispensed) } : slot));
  }, []);

  return <main className="withdraw-workflow withdraw-workflow--cabinet">
    {["awaiting_scan", "validating_sticker", "scan_failed"].includes(workflow) && <h1 className="sr-only">สแกน Sticker เบิกยา</h1>}
    <header className="medical-header" aria-label="สถานะเครื่องจ่ายยา">
      <section className="medical-header__card environment-card" aria-label="สภาพแวดล้อมและความพร้อมของระบบ"><span className="medical-header__icon is-neutral" aria-hidden="true"><StatusIcon name="sensor"/></span><div><strong>ไม่มีข้อมูลเซนเซอร์</strong><small>อุณหภูมิและความชื้น</small></div><span className={`medical-header__icon ${attentionCount ? "is-warning" : "is-ready"}`} aria-hidden="true"><StatusIcon name={attentionCount ? "warning" : "check"}/></span><div><strong>{attentionCount ? "ต้องตรวจสอบระบบ" : "ระบบพร้อมใช้งาน"}</strong><small>{slots.length ? `Hardware พร้อม · ${slots.length} ตำแหน่ง` : "กำลังตรวจสอบ Hardware"}</small></div></section>
      <section className="medical-header__brand" aria-label={`${kiosk.displayName} ${kiosk.code}`}><strong>ADM</strong><span>AUTOMATED DISPENSING MACHINE</span><b title={kiosk.displayName}>{kiosk.displayName}</b><small>{kiosk.code}</small></section>
      <section className="medical-header__card connection-card" aria-label="เครือข่าย วันและเวลา"><div className={`medical-header__network ${online ? "is-online" : "is-offline"}`}><span className="network-icon" aria-hidden="true"><StatusIcon name={online ? "network" : "offline"}/></span><div><strong>{online ? "ONLINE" : "OFFLINE"}</strong><small>{online ? "Connected" : "Disconnected"}</small></div></div><time dateTime={now.toISOString()}><strong>{now.toLocaleTimeString("th-TH", { hour: "2-digit", minute: "2-digit", hour12: false })}</strong><span>{now.toLocaleDateString("th-TH", { day: "numeric", month: "long", year: "numeric" })}</span></time></section>
    </header>
    <section className="cabinet-stage" aria-label="ผังตู้ยาสำหรับอ้างอิงตำแหน่งจริง"><header><div><strong>ผังตู้ยา</strong><span>อ้างอิงตำแหน่งยาจากตู้จริง · {kiosk.code}</span></div><div className="cabinet-stage__actions"><span className={attentionCount ? "cabinet-stage__attention" : "cabinet-stage__ready"}><StatusIcon name={attentionCount ? "warning" : "check"}/>{attentionCount ? `ต้องตรวจสอบ ${attentionCount} คิว` : "พร้อมใช้งาน"}</span>{machineQueue.length > 0 && <button type="button" className="cabinet-stage__queue-btn" onClick={() => setStatusOpen(true)}>📋 สถานะคิว ({machineQueue.length})</button>}<button type="button" className="cabinet-stage__emergency-btn" onClick={() => { setEmergencyCardToken(""); setEmergencyOpen(true); }} aria-label="เปิดเบิกยาฉุกเฉิน" title="เบิกยาฉุกเฉิน"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><path d="M13.2 2 5.8 13h5.4L10.8 22l7.4-12h-5.4l.4-8Z"/></svg></button></div></header>{notice && <div className="queue-toast" role="status"><StatusIcon name="check"/>{notice}</div>}<ShelfGrid slots={slots} kioskCode={kiosk.code} variant="overview" requestedSlotIds={requestedSlotIds}/>{prescription && <div className="request-location-banner" role="status"><strong>{prescription.prescriptionId}</strong><span>{prescription.items.length} รายการยา · ตำแหน่งที่ Sticker ร้องขอแสดงด้วยเครื่องหมาย ★</span></div>}</section>
    <section className="withdraw-dock"><section className="withdraw-context">
      {["awaiting_scan", "validating_sticker", "scan_failed"].includes(workflow) ? <div className="sticker-scanner" aria-live="polite"><span className="step-number">1</span><div><h1><span>SCAN QR LABEL</span>สแกน Sticker เบิกยา</h1><p>นำ Sticker เบิกยาไว้เหนือเครื่องสแกน</p></div><div className="sticker-scanner__frame">{workflow === "validating_sticker" ? <><span className="spinner"/><strong>กำลังตรวจสอบ Sticker กับระบบ...</strong></> : <><DecorativeQr/><strong>วาง Sticker ที่จุดสแกน</strong><small>ระบบจะอ่านข้อมูลอัตโนมัติ</small><span className="scanner-ready-pill"><StatusIcon name="check"/>เครื่องสแกนพร้อมใช้งาน</span></>}</div>{message && <div className="withdraw-alert" role="alert">⚠ {message}</div>}</div>
      : <div className="request-review request-review--compact"><header><div><span>รายการเบิกยาที่ตรวจสอบแล้ว</span><strong>{prescription?.prescriptionId}</strong></div><span className="status-badge status-badge--success">✓ Backend ยืนยันรายการ</span></header><div className="patient-context"><span>ผู้ป่วยที่กำลังทำรายการ</span><strong>{prescription?.patientName}</strong><b>HN {prescription?.hn}</b><small>หอผู้ป่วย {prescription?.wardId}</small></div><div className="cart-ready-summary"><div><strong>{prescription?.items.length || 0} รายการยา</strong><span>เปิดตะกร้าเพื่อตรวจสอบตำแหน่ง จำนวน และความพร้อมก่อนยืนยันตัวตน</span></div><button type="button" onClick={() => setCartOpen(true)}>เปิดตะกร้ารายการยา</button></div>{message && <div className="withdraw-alert" role="alert">⚠ {message}</div>}{workflow === "submission_uncertain" && <button type="button" className="reconcile-button" onClick={() => prescription && void reconcile(prescription.id)}>ตรวจสอบสถานะอีกครั้ง</button>}</div>}
    </section><div className="dock-steps"><header className="identity-step-heading"><span className="step-number !text-white" style={{ fontSize: "x-large" }}>2</span><div><strong>ขั้นตอนการเบิกยา</strong><span>สแกนและทำตามลำดับ</span></div></header><ol className="withdraw-steps withdraw-steps--compact" aria-label={`ขั้นตอนที่ ${step} จาก 3`}>{["สแกน Sticker", "ตรวจสอบรายการ", "ยืนยันตัวตน"].map((label, index) => <li key={label} className={index + 1 === step ? "is-current" : index + 1 < step ? "is-done" : ""}><b>{index + 1}</b><span>{label}</span></li>)}</ol></div>
    <footer className="withdraw-footer"><section className="footer-system-status" aria-label="สถานะความพร้อมของเครื่อง"><span className="queue-safety-card__icon" aria-hidden="true"><StatusIcon name="check"/></span><span><b>ระบบพร้อมทำงาน</b><small>HARDWARE STATUS</small></span></section><small className="footer-hardware-meta">Hardware พร้อม · APP v0.1.0 · {kiosk.code}</small>
    <section className={`withdraw-actionbar${["awaiting_scan", "validating_sticker", "scan_failed"].includes(workflow) ? " withdraw-actionbar--ready" : ""}`}>
      {workflow === "request_loaded" && <><div><strong>ตะกร้ายา {prescription?.items.length || 0} รายการพร้อมตรวจสอบ</strong><span className="immediate-warning">ตู้ {kiosk.code} จองยาไว้แล้ว ตรวจสอบก่อนสแกนบัตรยืนยัน</span></div><div className="action-group"><button type="button" className="secondary" onClick={() => void cancelPrepared()}>ยกเลิกรายการ</button><button type="button" onClick={() => setCartOpen(true)}>เปิดตะกร้ายา</button></div></>}
      {["awaiting_identity", "identity_failed", "unauthorized", "verifying_identity"].includes(workflow) && <IdentityScanner busy={workflow === "verifying_identity"} onScan={verifyIdentity} onCancel={() => void cancelPrepared()}/>}
      {["submitting_to_hardware", "submission_uncertain"].includes(workflow) && <><div><strong>{workflow === "submission_uncertain" ? "กำลังตรวจสอบผลการส่งคำสั่ง" : "กำลังส่งคำสั่งเข้าคิวเครื่อง"}</strong><span>รายการถูกล็อกแล้ว ห้ามสแกนซ้ำหรือปิดหน้าจอ</span></div><span className="spinner"/></>}
      {["awaiting_scan", "validating_sticker", "scan_failed"].includes(workflow) && <><div><strong>{workflow === "validating_sticker" ? "กำลังตรวจสอบรายการ" : "พร้อมสแกนรายการถัดไป"}</strong>{workflow === "validating_sticker" && <span>กำลังอ่านข้อมูลจาก Sticker</span>}</div>{workflow === "scan_failed" && <button type="button" onClick={() => resetScanner()}>สแกนใหม่</button>}</>}
    </section></footer></section>
    {prescription && cartOpen && (
      <MedicationCartModal
        prescription={prescription}
        slots={slots}
        kioskCode={kiosk.code}
        onClose={() => setCartOpen(false)}
        onConfirm={() => { setCartOpen(false); setWorkflow("awaiting_identity"); }}
      />
    )}
    {statusOpen && (
      <DispensingStatusModal
        queue={machineQueue}
        now={now}
        onClose={() => setStatusOpen(false)}
      />
    )}
    {emergencyOpen && <EmergencyDispenseModal kioskCode={kiosk.code} projectId={kiosk.projectId} kioskToken={auth!.token} externalCardToken={emergencyCardToken} onExternalCardConsumed={() => setEmergencyCardToken("")} onClose={() => setEmergencyOpen(false)} onDispensed={handleEmergencyDispensed}/>}
    {/* Live dispense status — only on the idle scan screen so it never covers
        the next scan/cart/identity step; the dispense keeps polling in the
        queue while hidden. Driven purely by real backend state. */}
    {workflow === "awaiting_scan" && (() => {
      const activeTxn = queue.find((item) => item.id === activeTxnId);
      return activeTxn ? (
        <DispenseProcessingOverlay txn={activeTxn} onDismiss={() => setActiveTxnId(null)} />
      ) : null;
    })()}
  </main>;
}
function DecorativeQr() {
  return <span className="decorative-qr" aria-label="สัญลักษณ์ตำแหน่งสแกน QR ไม่ใช่ QR สำหรับสแกน">{Array.from({ length: 25 }, (_, index) => <i key={index}/>)}</span>;
}

function StatusIcon({ name }: { name: "sensor" | "check" | "warning" | "network" | "offline" }) {
  const paths = {
    sensor: <><path d="M9 4a3 3 0 0 1 6 0v9.2a5 5 0 1 1-6 0V4Z"/><path d="M12 7v8"/></>,
    check: <><path d="M12 3 4.5 6v5.5c0 4.6 3.2 7.8 7.5 9.5 4.3-1.7 7.5-4.9 7.5-9.5V6L12 3Z"/><path d="m8.5 12 2.2 2.2 4.8-5"/></>,
    warning: <><path d="M12 3 2.8 20h18.4L12 3Z"/><path d="M12 9v5M12 17.2v.1"/></>,
    network: <><path d="M4 9a12 12 0 0 1 16 0M7 12.5a7.5 7.5 0 0 1 10 0M10.2 16a2.8 2.8 0 0 1 3.6 0"/><circle cx="12" cy="19" r="1"/></>,
    offline: <><path d="M4 9a12 12 0 0 1 16 0M7 12.5a7.5 7.5 0 0 1 4-1.8M14.5 11.1a7 7 0 0 1 2.5 1.4M10.5 16a2.8 2.8 0 0 1 3 0M3 3l18 18"/></>,
  };
  return <svg className="status-icon-svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.9" strokeLinecap="round" strokeLinejoin="round" focusable="false">{paths[name]}</svg>;
}

function IdentityScanner({ busy, onScan, onCancel }: { busy: boolean; onScan: (token: string) => void; onCancel: () => void }) {
  const [value, setValue] = useState("");
  return <><div><strong>โหมดยืนยันตัวตนผู้เบิก · ยืนยันตัวตนสำหรับรายการนี้</strong><span className="immediate-warning">เมื่อยืนยันสำเร็จ ระบบจะส่งเข้าคิวทันทีและไม่สามารถยกเลิกได้</span></div><form className="identity-form" onSubmit={(e) => { e.preventDefault(); onScan(value); setValue(""); }}><input type="password" aria-label="รหัสบัตรเจ้าหน้าที่" value={value} onChange={(e) => setValue(e.target.value)} placeholder="รหัสบัตร (fallback)" autoComplete="off" disabled={busy}/><button type="submit" disabled={busy || !value.trim()}>{busy ? "กำลังตรวจสอบและส่ง..." : "ยืนยันตัวตนและส่งเข้าคิว"}</button><button type="button" className="secondary" onClick={onCancel} disabled={busy}>ยกเลิกรายการ</button></form></>;
}

function MedicationCartModal({ prescription, slots, kioskCode, onClose, onConfirm }: { prescription: Prescription; slots: Slot[]; kioskCode: string; onClose: () => void; onConfirm: () => void }) {
  const items = prescription.items.map((item, index) => {
    const slot = slots.find((candidate) => candidate.drugCode === item.drugCode);
    const position = slot ? getSlotPosition(slot) : null;
    return {
      key: `${item.drugCode}-${index}`,
      item,
      slot,
      position,
      shortage: !slot || slot.quantity < item.quantity,
    };
  });
  const shortageCount = items.filter((item) => item.shortage).length;

  return <div className="medication-cart-backdrop"><section className="medication-cart" role="dialog" aria-modal="true" aria-labelledby="medication-cart-title" aria-describedby="medication-cart-hint"><header><span aria-hidden="true">Rx</span><div><small>MEDICATION CART</small><h2 id="medication-cart-title">ตรวจสอบรายการยาที่จะเบิก</h2><p>{prescription.prescriptionId} · {prescription.patientName} · HN {prescription.hn}</p></div><button type="button" onClick={onClose} aria-label="ปิดตะกร้ารายการยา">×</button></header><div className="medication-cart__summary"><strong>{items.length} รายการยา</strong><span className={shortageCount ? "has-shortage" : "is-ready"}>{shortageCount ? `⚠ ต้องตรวจสอบ ${shortageCount} รายการ` : "✓ ยาพร้อมจ่ายครบทุกช่อง"}</span></div><div className="medication-cart__items">{items.map(({ key, item, slot, position, shortage }) => <article key={key} className={shortage ? "has-shortage" : "is-ready"}><div className="medication-cart__drug"><strong>{item.drugName}</strong><span>{item.drugCode}</span></div><div className="medication-cart__location"><small>ตำแหน่งยา</small><strong>{slot && position ? `ชั้น ${position.shelf} · ช่อง ${(position.shelf - 1) * 22 + position.row}` : "ไม่พบตำแหน่ง"}</strong><span>{slot ? `${kioskCode} · ${slot.code}` : "กรุณาติดต่อเภสัชกร"}</span></div><div className="medication-cart__quantity"><small>จำนวนที่เบิก</small><strong>{item.quantity}</strong><span>{slot ? `คงเหลือ ${slot.quantity}` : "ไม่มีข้อมูลคงเหลือ"}</span></div><b>{shortage ? slot ? `ไม่เพียงพอ · ขาด ${item.quantity - slot.quantity}` : "ไม่พบยาในตู้" : "พร้อมจ่าย"}</b></article>)}</div><aside id="medication-cart-hint" className={shortageCount ? "medication-cart__hint has-shortage" : "medication-cart__hint"}><strong>{shortageCount ? "ตรวจพบรายการที่ต้องตรวจสอบ" : "ขั้นตอนถัดไป"}</strong><span>{shortageCount ? "ปริมาณยาบางรายการไม่เพียงพอหรือไม่พบตำแหน่ง กรุณาตรวจสอบกับเภสัชกรก่อนดำเนินการ" : "สแกนบัตรเจ้าหน้าที่เพื่อยืนยันรายการและส่งเข้าคิวจ่ายยา"}</span></aside><footer><button type="button" className="secondary" onClick={onClose}>กลับไปตรวจสอบ</button><button type="button" onClick={onConfirm}>ไปสแกนบัตรยืนยัน</button></footer></section></div>;
}
