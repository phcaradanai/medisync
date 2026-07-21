import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { create } from "@bufbuild/protobuf";
import { Code, ConnectError, createClient, type Interceptor } from "@connectrpc/connect";
import { createConnectTransport } from "@connectrpc/connect-web";
import {
  DispensingService,
  EmergencyDispenseRequestSchema,
  EmergencyDispenseStatus,
  EmergencyOperatorAuthMethod,
  GetEmergencyDispenseTransactionRequestSchema,
  ListEmergencyDrugsRequestSchema,
  type EmergencyDispenseTransaction,
  type EmergencyDrug,
} from "@medisync/proto/medisync/dispensing/v1/dispensing_pb";
import { CardLoginRequestSchema, IdentityService } from "@medisync/proto/medisync/identity/v1/identity_pb";
import { transport } from "../../transport.ts";

const client = createClient(DispensingService, transport);
const identityClient = createClient(IdentityService, transport);

function staffEmergencyClient(staffToken: string, kioskToken: string) {
  const staffAuth: Interceptor = (next) => (request) => {
    request.header.set("Authorization", `Bearer ${staffToken}`);
    request.header.set("X-Kiosk-Authorization", `Bearer ${kioskToken}`);
    return next(request);
  };
  return createClient(DispensingService, createConnectTransport({ baseUrl: "/", interceptors: [staffAuth] }));
}

type Props = {
  kioskCode: string;
  projectId: string;
  kioskToken: string;
  externalCardToken: string;
  onExternalCardConsumed: () => void;
  onClose: () => void;
  onDispensed: (slotCode: string, quantity: number) => void;
};

function activeKey(kioskCode: string) {
  return `medisync.active-emergency.${kioskCode}`;
}

function Icon({ name }: { name: "emergency" | "identity" | "patient" | "medicine" | "clock" | "check" | "fail" }) {
  const paths = {
    emergency: <><path d="M13.2 2 5.8 13h5.4L10.8 22l7.4-12h-5.4l.4-8Z"/></>,
    identity: <><circle cx="12" cy="8" r="3.2"/><path d="M5.5 20c.7-4 3-6 6.5-6s5.8 2 6.5 6"/></>,
    patient: <><path d="M5 5h14v15H5z"/><path d="M9 2v6m6-6v6M8 12h8m-4-4v8"/></>,
    medicine: <><path d="m8.2 4.2 11.6 11.6a2.8 2.8 0 0 1-4 4L4.2 8.2a2.8 2.8 0 0 1 4-4Z"/><path d="m8 12 4-4"/></>,
    clock: <><circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/></>,
    check: <><circle cx="12" cy="12" r="9"/><path d="m8 12 2.6 2.6L16.5 9"/></>,
    fail: <><circle cx="12" cy="12" r="9"/><path d="m9 9 6 6m0-6-6 6"/></>,
  };
  return <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.9" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">{paths[name]}</svg>;
}

function errorMessage(error: unknown): string {
  if (error instanceof ConnectError) {
    if (error.code === Code.PermissionDenied) return "ไม่พบรหัสพนักงานที่ใช้งานได้ในโครงการของตู้นี้";
    if (error.code === Code.FailedPrecondition) return "ยาไม่พร้อมจ่าย จำนวนไม่พอ หรือไม่ได้ตั้งค่าเป็นยาฉุกเฉินแล้ว";
  }
  return "ไม่สามารถสร้างรายการเบิกฉุกเฉินได้ กรุณาตรวจสอบระบบและลองอีกครั้ง";
}

function cardErrorMessage(error: unknown): string {
  if (error instanceof ConnectError) {
    if (error.code === Code.PermissionDenied) return "บัตรนี้ไม่อยู่ในโครงการเดียวกับตู้ หรือบัญชีถูกระงับ";
    if (error.code === Code.Unauthenticated) return "ไม่พบบัตรเจ้าหน้าที่นี้ในระบบ กรุณาลองอีกครั้ง";
  }
  return "ยืนยันบัตรเจ้าหน้าที่ไม่สำเร็จ กรุณาตรวจสอบการเชื่อมต่อแล้วลองอีกครั้ง";
}

export default function EmergencyDispenseModal({ kioskCode, projectId, kioskToken, externalCardToken, onExternalCardConsumed, onClose, onDispensed }: Props) {
  const [drugs, setDrugs] = useState<EmergencyDrug[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState("");
  const [hn, setHn] = useState("");
  const [identityMethod, setIdentityMethod] = useState<"card" | "employee_code">("card");
  const [employeeCode, setEmployeeCode] = useState("");
  const [cardInput, setCardInput] = useState("");
  const [cardAccessToken, setCardAccessToken] = useState("");
  const [cardOperatorName, setCardOperatorName] = useState("");
  const [cardStatus, setCardStatus] = useState<"idle" | "verifying" | "verified" | "failed">("idle");
  const [cardError, setCardError] = useState("");
  const cardInputRef = useRef<HTMLInputElement>(null);
  const [selectedSlot, setSelectedSlot] = useState("");
  const [quantity, setQuantity] = useState(1);
  const [reason, setReason] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [submitError, setSubmitError] = useState("");
  const [transaction, setTransaction] = useState<EmergencyDispenseTransaction | null>(null);
  const selected = useMemo(() => drugs.find((drug) => drug.slotCode === selectedSlot) ?? null, [drugs, selectedSlot]);
  const identityReady = hn.trim().length > 0 && employeeCode.trim().length > 0
    && (identityMethod === "employee_code" || cardAccessToken.length > 0);
  const isActive = transaction?.status === EmergencyDispenseStatus.QUEUED || transaction?.status === EmergencyDispenseStatus.DISPENSING;
  const isDone = transaction?.status === EmergencyDispenseStatus.DISPENSED;
  const isFailed = transaction?.status === EmergencyDispenseStatus.FAILED;

  const resetIdentity = (method: "card" | "employee_code") => {
    setIdentityMethod(method);
    setEmployeeCode("");
    setCardInput("");
    setCardAccessToken("");
    setCardOperatorName("");
    setCardStatus("idle");
    setCardError("");
    setSubmitError("");
    if (method === "card") window.setTimeout(() => cardInputRef.current?.focus(), 0);
  };

  const verifyCard = useCallback(async (rawToken: string) => {
    const token = rawToken.trim();
    if (!token || cardStatus === "verifying") return;
    setCardStatus("verifying");
    setCardError("");
    setCardInput("");
    try {
      const response = await identityClient.cardLogin(create(CardLoginRequestSchema, { cardToken: token, projectId }));
      if (!response.user || !response.accessToken || !response.user.active || !response.employeeCode) {
        throw new Error("card identity is incomplete");
      }
      setEmployeeCode(response.employeeCode.toUpperCase());
      setCardAccessToken(response.accessToken);
      setCardOperatorName(response.user.displayName);
      setCardStatus("verified");
    } catch (error) {
      setEmployeeCode("");
      setCardAccessToken("");
      setCardOperatorName("");
      setCardStatus("failed");
      setCardError(cardErrorMessage(error));
    }
  }, [cardStatus, projectId]);

  useEffect(() => {
    if (!externalCardToken) return;
    setIdentityMethod("card");
    void verifyCard(externalCardToken);
    onExternalCardConsumed();
  }, [externalCardToken, onExternalCardConsumed, verifyCard]);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setLoadError("");
    client.listEmergencyDrugs(create(ListEmergencyDrugsRequestSchema, { kioskCode, pageSize: 100 }))
      .then((response) => {
        if (cancelled) return;
        setDrugs(response.drugs);
        setSelectedSlot((current) => current || response.drugs[0]?.slotCode || "");
      })
      .catch(() => { if (!cancelled) setLoadError("โหลดรายการยาฉุกเฉินของตู้นี้ไม่สำเร็จ"); })
      .finally(() => { if (!cancelled) setLoading(false); });

    const activeID = localStorage.getItem(activeKey(kioskCode));
    if (activeID) {
      client.getEmergencyDispenseTransaction(create(GetEmergencyDispenseTransactionRequestSchema, { dispenseId: activeID }))
        .then((response) => {
          if (cancelled || !response.transaction) return;
          setTransaction(response.transaction);
          if (response.transaction.status === EmergencyDispenseStatus.DISPENSED) {
            localStorage.removeItem(activeKey(kioskCode));
            onDispensed(response.transaction.slotCode, response.transaction.dispensedQuantity);
          } else if (response.transaction.status === EmergencyDispenseStatus.FAILED) {
            localStorage.removeItem(activeKey(kioskCode));
          }
        })
        .catch(() => { if (!cancelled) setSubmitError("ไม่สามารถตรวจสอบรายการฉุกเฉินที่กำลังทำงานได้ ระบบจะเก็บเลขรายการไว้เพื่อลองใหม่"); });
    }
    return () => { cancelled = true; };
  }, [kioskCode, onDispensed]);

  useEffect(() => {
    if (!transaction || !isActive) return;
    const timer = window.setTimeout(async () => {
      try {
        const response = await client.getEmergencyDispenseTransaction(create(GetEmergencyDispenseTransactionRequestSchema, { dispenseId: transaction.dispenseId }));
        if (!response.transaction) return;
        setTransaction(response.transaction);
        if (response.transaction.status === EmergencyDispenseStatus.DISPENSED) {
          localStorage.removeItem(activeKey(kioskCode));
          onDispensed(response.transaction.slotCode, response.transaction.dispensedQuantity);
        } else if (response.transaction.status === EmergencyDispenseStatus.FAILED) {
          localStorage.removeItem(activeKey(kioskCode));
        }
      } catch {
        setSubmitError("ขาดการเชื่อมต่อชั่วคราว ระบบจะตรวจสอบสถานะอีกครั้ง");
      }
    }, 1_500);
    return () => window.clearTimeout(timer);
  }, [isActive, kioskCode, onDispensed, transaction]);

  useEffect(() => {
    if (!selected) return;
    setQuantity((value) => Math.min(Math.max(value, 1), Math.max(selected.maxDispense, 1)));
  }, [selected]);

  const submit = async () => {
    if (!identityReady || !selected || selected.maxDispense < 1 || submitting) return;
    setSubmitting(true);
    setSubmitError("");
    try {
      const emergencyClient = identityMethod === "card"
        ? staffEmergencyClient(cardAccessToken, kioskToken)
        : client;
      const response = await emergencyClient.emergencyDispense(create(EmergencyDispenseRequestSchema, {
        kioskCode,
        hn: hn.trim(),
        employeeCode: employeeCode.trim().toUpperCase(),
        slotCode: selected.slotCode,
        drugCode: selected.drugCode,
        quantity,
        reason: reason.trim(),
        traceId: globalThis.crypto?.randomUUID?.() ?? `${Date.now()}-${Math.random()}`,
      }));
      if (!response.transaction) throw new Error("missing transaction");
      if (response.transaction.status === EmergencyDispenseStatus.QUEUED || response.transaction.status === EmergencyDispenseStatus.DISPENSING) {
        localStorage.setItem(activeKey(kioskCode), response.transaction.dispenseId);
      } else {
        localStorage.removeItem(activeKey(kioskCode));
        if (response.transaction.status === EmergencyDispenseStatus.DISPENSED) {
          onDispensed(response.transaction.slotCode, response.transaction.dispensedQuantity);
        }
      }
      setTransaction(response.transaction);
    } catch (error) {
      setSubmitError(errorMessage(error));
    } finally {
      setSubmitting(false);
    }
  };

  const reset = () => {
    setTransaction(null);
    setHn("");
    setIdentityMethod("card");
    setEmployeeCode("");
    setCardInput("");
    setCardAccessToken("");
    setCardOperatorName("");
    setCardStatus("idle");
    setCardError("");
    setQuantity(1);
    setReason("");
    setSubmitError("");
  };

  return <div className="emergency-modal-backdrop">
    <section className="emergency-modal" role="dialog" aria-modal="true" aria-labelledby="emergency-modal-title">
      <header className="emergency-modal__header">
        <span className="emergency-modal__symbol"><Icon name="emergency"/></span>
        <div><small>EMERGENCY WITHDRAWAL</small><h2 id="emergency-modal-title">เบิกยาฉุกเฉิน</h2><p>สำหรับรายการที่ไม่มี Prescription จึงไม่มี Sticker เบิกยาให้สแกน</p></div>
        <span className="emergency-modal__kiosk">ตู้ <strong>{kioskCode}</strong></span>
        <button type="button" onClick={onClose} aria-label="ปิดรายการเบิกฉุกเฉิน">×</button>
      </header>

      {transaction ? <div className={`emergency-result ${isDone ? "is-success" : isFailed ? "is-failed" : "is-active"}`} aria-live="polite">
        <span className="emergency-result__icon"><Icon name={isDone ? "check" : isFailed ? "fail" : "clock"}/></span>
        <div className="emergency-result__lead">
          <small>{isDone ? "HARDWARE CONFIRMED" : isFailed ? "DISPENSE FAILED" : "HARDWARE IN PROGRESS"}</small>
          <h3>{isDone ? "จ่ายยาฉุกเฉินสำเร็จ" : isFailed ? "จ่ายยาไม่สำเร็จ" : transaction.status === EmergencyDispenseStatus.DISPENSING ? "เครื่องกำลังจ่ายยา" : "รายการอยู่ในคิวตู้ยา"}</h3>
          <p>{isDone ? "นำยาออกจากช่องรับยาและตรวจสอบจำนวนก่อนออกจากหน้าตู้" : isFailed ? transaction.failureDetail || "เครื่องไม่สามารถจ่ายยาได้ กรุณาติดต่อเภสัชกร" : "โปรดรอที่หน้าตู้ ห้ามสร้างรายการซ้ำ"}</p>
        </div>
        <dl>
          <div><dt>HN</dt><dd>{transaction.hn}</dd></div>
          <div><dt>ผู้เบิก</dt><dd>{transaction.operatorDisplayName} · {transaction.employeeCode}</dd></div>
          <div><dt>วิธียืนยัน</dt><dd>{transaction.operatorAuthMethod === EmergencyOperatorAuthMethod.CARD ? "สแกนบัตรเจ้าหน้าที่" : "กรอกรหัสพนักงาน"}</dd></div>
          <div><dt>ยา</dt><dd>{transaction.drugName}</dd></div>
          <div><dt>ตำแหน่ง / จำนวน</dt><dd>{transaction.slotCode} · {transaction.requestedQuantity}</dd></div>
          <div><dt>เลขที่รายการ</dt><dd className="mono">{transaction.dispenseId}</dd></div>
          <div><dt>ตู้ยา</dt><dd className="mono">{transaction.kioskCode}</dd></div>
        </dl>
        {isActive && <div className="emergency-result__progress"><span/><b>กำลังรอผลยืนยันจาก Hardware</b></div>}
        {submitError && <p className="emergency-modal__error" role="alert">{submitError}</p>}
        <footer>{!isActive && <button type="button" className="secondary" onClick={reset}>ทำรายการฉุกเฉินใหม่</button>}<button type="button" onClick={onClose}>{isActive ? "ซ่อนสถานะ (รายการยังทำงานต่อ)" : "ปิด"}</button></footer>
      </div> : <>
        <div className="emergency-modal__notice"><Icon name="emergency"/><div><strong>ใช้เฉพาะการเบิกฉุกเฉินที่ไม่มี Prescription</strong><span>จึงไม่มี Sticker ให้สแกน กรุณาระบุ HN และรหัสพนักงานผู้เบิก ระบบจะบันทึกเป็น Emergency Transaction แยกจากการเบิกตาม Prescription</span></div></div>
        <form className="emergency-form" onSubmit={(event) => { event.preventDefault(); void submit(); }}>
          <fieldset className="emergency-form__identity">
            <legend>1 · ระบุผู้ป่วยและยืนยันผู้เบิก</legend>
            <label className="emergency-hn"><span><Icon name="patient"/>HN ผู้ป่วย <b>จำเป็น</b></span><input value={hn} onChange={(event) => setHn(event.target.value)} onKeyDown={(event) => { if (event.key === "Enter" && identityMethod === "card") { event.preventDefault(); cardInputRef.current?.focus(); } }} placeholder="เช่น HN100001" autoComplete="off" maxLength={64} autoFocus required/></label>
            <div className="emergency-identity-choice">
              <span className="emergency-identity-choice__label"><Icon name="identity"/>วิธียืนยันผู้เบิก <b>จำเป็น</b></span>
              <div className="emergency-identity-tabs" role="tablist" aria-label="เลือกวิธียืนยันผู้เบิก">
                <button type="button" role="tab" aria-selected={identityMethod === "card"} className={identityMethod === "card" ? "is-selected" : ""} onClick={() => resetIdentity("card")}>สแกนบัตร</button>
                <button type="button" role="tab" aria-selected={identityMethod === "employee_code"} className={identityMethod === "employee_code" ? "is-selected" : ""} onClick={() => resetIdentity("employee_code")}>กรอกรหัสพนักงาน</button>
              </div>
              {identityMethod === "card" ? cardStatus === "verified" ? <div className="emergency-card-status is-verified" role="status"><Icon name="check"/><div><strong>ยืนยันบัตรแล้ว</strong><span>{cardOperatorName} · {employeeCode}</span></div><button type="button" onClick={() => resetIdentity("card")}>เปลี่ยนบัตร</button></div> : <div className={`emergency-card-scan ${cardStatus === "failed" ? "has-error" : ""}`}>
                <input ref={cardInputRef} type="password" aria-label="สแกนบัตรเจ้าหน้าที่" value={cardInput} onChange={(event) => setCardInput(event.target.value)} onKeyDown={(event) => { if (event.key === "Enter") { event.preventDefault(); void verifyCard(cardInput); } }} placeholder={cardStatus === "verifying" ? "กำลังยืนยันบัตร..." : "แตะที่นี่ แล้วสแกนบัตรเจ้าหน้าที่"} autoComplete="off" disabled={cardStatus === "verifying"}/>
                <span>{cardStatus === "verifying" ? "กำลังตรวจสอบสิทธิ์กับระบบ" : "กรอก HN แล้วสแกนบัตรได้ตามปกติ"}</span>
                {cardError && <small role="alert">{cardError}</small>}
              </div> : <label className="emergency-employee-code"><span className="sr-only">รหัสพนักงานผู้เบิก</span><input value={employeeCode} onChange={(event) => setEmployeeCode(event.target.value.toUpperCase())} placeholder="รหัสพนักงาน เช่น EMP-NURSE-001" aria-label="รหัสพนักงานผู้เบิก" autoComplete="off" maxLength={64} required/></label>}
            </div>
          </fieldset>

          <fieldset className="emergency-form__medicines" disabled={!identityReady || loading || Boolean(loadError)}>
            <legend>2 · เลือกยาที่ตั้งค่าเบิกฉุกเฉินในตู้ {kioskCode}</legend>
            {loading ? <div className="emergency-form__state"><span className="spinner"/>กำลังโหลดรายการยา...</div>
              : loadError ? <div className="emergency-form__state is-error">{loadError}</div>
              : drugs.length === 0 ? <div className="emergency-form__state">ยังไม่มียาที่หลังบ้านตั้งค่าให้เบิกฉุกเฉินสำหรับตู้นี้</div>
              : <div className="emergency-drug-list">{drugs.map((drug) => <label key={drug.slotCode} className={selectedSlot === drug.slotCode ? "is-selected" : ""}>
                <input type="radio" name="emergency-drug" value={drug.slotCode} checked={selectedSlot === drug.slotCode} onChange={() => setSelectedSlot(drug.slotCode)} disabled={drug.maxDispense < 1}/>
                <span className="emergency-drug-list__icon"><Icon name="medicine"/></span>
                <span><strong>{drug.drugName}</strong><small>{drug.drugCode} · ช่อง {drug.slotCode}</small></span>
                <span><b>คงเหลือ {drug.quantity}</b><small>เบิกได้สูงสุด {drug.maxDispense}</small></span>
              </label>)}</div>}
          </fieldset>

          <fieldset className="emergency-form__confirm" disabled={!identityReady || !selected}>
            <legend>3 · จำนวนและยืนยัน</legend>
            <label className="emergency-quantity"><span>จำนวนที่เบิก</span><div><button type="button" onClick={() => setQuantity((value) => Math.max(1, value - 1))} disabled={quantity <= 1}>−</button><output>{quantity}</output><button type="button" onClick={() => setQuantity((value) => Math.min(selected?.maxDispense || 1, value + 1))} disabled={quantity >= (selected?.maxDispense || 1)}>+</button></div></label>
            <label className="emergency-reason"><span>เหตุผลเพิ่มเติม <small>(ไม่บังคับ)</small></span><input value={reason} onChange={(event) => setReason(event.target.value)} placeholder="ระบุเหตุผลสั้น ๆ" maxLength={500}/></label>
            <aside><strong>{selected?.drugName || "ยังไม่ได้เลือกยา"}</strong><span>{selected ? `${selected.drugCode} · ช่อง ${selected.slotCode} · ตู้ ${kioskCode}` : "กรอก HN และรหัสพนักงานก่อนเลือกยา"}</span></aside>
          </fieldset>
          {submitError && <p className="emergency-modal__error" role="alert">{submitError}</p>}
          <footer><button type="button" className="secondary" onClick={onClose}>ยกเลิก</button><button type="submit" disabled={!identityReady || !selected || selected.maxDispense < 1 || submitting}>{submitting ? "กำลังสร้าง transaction..." : "ยืนยันและเริ่มจ่ายยาฉุกเฉิน"}</button></footer>
        </form>
      </>}
    </section>
  </div>;
}
