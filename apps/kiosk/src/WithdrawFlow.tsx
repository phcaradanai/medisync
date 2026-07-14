import { useState, useEffect, useCallback, useRef } from "react";
import { createClient } from "@connectrpc/connect";
import { create } from "@bufbuild/protobuf";
import {
  DispensingService,
  ListPrescriptionsRequestSchema,
  GetPrescriptionRequestSchema,
  DispenseRequestSchema,
  PrescriptionState,
  type Prescription,
} from "@medisync/proto/medisync/dispensing/v1/dispensing_pb";
import { transport } from "./transport.ts";
import { useAuth } from "./auth.tsx";

const dispensingClient = createClient(DispensingService, transport);

type Step = "list" | "confirm" | "dispensing" | "done";

export default function WithdrawFlow() {
  const { state, logout } = useAuth();
  const user = state!.user;

  const [step, setStep] = useState<Step>("list");
  const [prescriptions, setPrescriptions] = useState<Prescription[]>([]);
  const [selected, setSelected] = useState<Prescription | null>(null);
  const [result, setResult] = useState<Prescription | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [dismissed, setDismissed] = useState(false);

  // Polling ref for dispensing status.
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null);

  // Load ready prescriptions for the user's wards.
  useEffect(() => {
    if (step !== "list") return;
    setBusy(true);
    setError(null);

    // List READY prescriptions. ward_id is empty; server scopes by JWT wards.
    const req = create(ListPrescriptionsRequestSchema, {
      wardId: "",
      states: [PrescriptionState.READY],
      pageSize: 50,
    });

    dispensingClient.listPrescriptions(req)
      .then((res) => setPrescriptions(res.prescriptions ?? []))
      .catch((err: unknown) => {
        const msg = err instanceof Error ? err.message : "เกิดข้อผิดพลาด";
        setError(`ไม่สามารถโหลดรายการยาได้: ${msg}`);
      })
      .finally(() => setBusy(false));
  }, [step]);

  // Stop polling on unmount.
  useEffect(() => {
    return () => {
      if (pollRef.current) clearInterval(pollRef.current);
    };
  }, []);

  const handleSelect = (rx: Prescription) => {
    setSelected(rx);
    setError(null);
    setStep("confirm");
  };

  const handleBack = () => {
    setSelected(null);
    setResult(null);
    setError(null);
    setDismissed(false);
    setStep("list");
  };

  const handleConfirm = useCallback(async () => {
    if (!selected) return;
    setError(null);
    setBusy(true);
    setStep("dispensing");

    const traceId = crypto.randomUUID();
    try {
      const req = create(DispenseRequestSchema, {
        prescriptionId: selected.prescriptionId,
        traceId,
      });
      const res = await dispensingClient.dispense(req);
      const rx = res.prescription;
      if (!rx) {
        setError("ไม่ได้รับข้อมูลยืนยันจากเซิร์ฟเวอร์");
        setStep("confirm");
        setBusy(false);
        return;
      }

      // DISPENSING is the current terminal state the server returns.
      // The fulfillment bridge (M3) will transition to DISPENSED/FAILED later.
      // We observe DISPENSING → poll for terminal state.
      if (rx.state === PrescriptionState.DISPENSING) {
        setResult(rx);
        // Poll for terminal state every 2 seconds.
        pollRef.current = setInterval(async () => {
          try {
            const gr = create(GetPrescriptionRequestSchema, { id: rx.id });
            const fres = await dispensingClient.getPrescription(gr);
            const updated = fres.prescription;
            if (!updated) return;
            setResult(updated);
            if (
              updated.state === PrescriptionState.DISPENSED ||
              updated.state === PrescriptionState.FAILED
            ) {
              if (pollRef.current) clearInterval(pollRef.current);
              setStep("done");
              setBusy(false);
            }
          } catch {
            // Polling failure is non-fatal; keep trying.
          }
        }, 2000);
      } else if (
        rx.state === PrescriptionState.DISPENSED ||
        rx.state === PrescriptionState.FAILED
      ) {
        setResult(rx);
        setStep("done");
        setBusy(false);
      } else {
        setResult(rx);
        setStep("done");
        setBusy(false);
      }
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : "เกิดข้อผิดพลาด";
      if (msg.includes("FailedPrecondition")) {
        setError("ใบสั่งยานี้ไม่พร้อมสำหรับการเบิก กรุณาลองใหม่");
      } else if (msg.includes("NotFound")) {
        setError("ไม่พบใบสั่งยานี้");
      } else if (msg.includes("Unauthenticated")) {
        setError("เซสชันหมดอายุ กรุณาเข้าสู่ระบบใหม่");
        logout();
      } else {
        setError(`เกิดข้อผิดพลาดในการเบิกจ่าย: ${msg}`);
      }
      setStep("confirm");
      setBusy(false);
    }
  }, [selected, logout]);

  const handleDone = () => {
    if (pollRef.current) clearInterval(pollRef.current);
    if (step === "done" && result?.state === PrescriptionState.FAILED && !dismissed) {
      // User must acknowledge failure.
      setDismissed(true);
      return;
    }
    handleBack();
  };

  // ── List screen ────────────────────────────────────────────
  if (step === "list") {
    return (
      <div className="kiosk-screen">
        <div className="kiosk-panel">
          <h1 className="kiosk-panel-title">รายการยาที่รอเบิก</h1>
          <p className="kiosk-panel-subtitle">
            {user.displayName} — เลือกรายการเพื่อเริ่มเบิกยา
          </p>

          {error && <div className="kiosk-error" role="alert">{error}</div>}

          {busy && (
            <div style={{ display: "flex", justifyContent: "center", padding: "var(--space-2xl)" }}>
              <div className="spinner" />
            </div>
          )}

          {!busy && prescriptions.length === 0 && !error && (
            <div className="kiosk-message">
              ไม่มีใบสั่งยาที่รอเบิกอยู่ในขณะนี้
            </div>
          )}

          {!busy && prescriptions.length > 0 && (
            <ul className="rx-list" role="listbox">
              {prescriptions.map((rx) => (
                <li
                  key={rx.id}
                  className="rx-card"
                  role="option"
                  tabIndex={0}
                  aria-selected={false}
                  onClick={() => handleSelect(rx)}
                  onKeyDown={(e) => { if (e.key === "Enter" || e.key === " ") handleSelect(rx); }}
                >
                  <div className="rx-card__info">
                    <div className="rx-card__patient">{rx.patientName}</div>
                    <div className="rx-card__drugs">
                      {rx.items?.map((it) => it.drugName).join(", ")}
                    </div>
                  </div>
                  <div>
                    <span className="rx-card__hn">HN {rx.hn}</span>
                  </div>
                </li>
              ))}
            </ul>
          )}

          <button
            type="button"
            className="kiosk-btn kiosk-btn-outline"
            style={{ alignSelf: "center" }}
            onClick={logout}
          >
            ออกจากระบบ
          </button>
        </div>
      </div>
    );
  }

  // ── Confirm screen ─────────────────────────────────────────
  if (step === "confirm" && selected) {
    return (
      <div className="kiosk-screen">
        <div className="kiosk-panel">
          <div className="step-indicator">ขั้นตอน 1 จาก 2</div>
          <h1 className="kiosk-panel-title">ยืนยันการเบิกยา</h1>

          {error && <div className="kiosk-error" role="alert">{error}</div>}

          <div className="rx-detail">
            <div className="rx-detail__section">
              <div className="rx-detail__heading">ผู้ป่วย</div>
              <div className="rx-detail__value">{selected.patientName}</div>
              <div className="rx-card__hn">HN {selected.hn}</div>
            </div>

            <div className="rx-detail__section">
              <div className="rx-detail__heading">รายการยา</div>
              <ul className="rx-detail__items">
                {selected.items?.map((item, i) => (
                  <li key={i}>
                    <div className="rx-detail__item">
                      <div style={{ display: "flex", flexDirection: "column", gap: "var(--space-xs)" }}>
                        <span className="rx-detail__item-name">{item.drugName}</span>
                        {item.dosageText && (
                          <span className="rx-detail__item-dosage">{item.dosageText}</span>
                        )}
                      </div>
                      <span className="rx-detail__item-qty">×{item.quantity}</span>
                    </div>
                  </li>
                ))}
              </ul>
            </div>

            <div className="rx-detail__section">
              <div className="rx-detail__heading">เลขที่ใบสั่งยา</div>
              <div className="rx-detail__value" style={{ fontFamily: "var(--font-mono)", fontSize: "1rem" }}>
                {selected.prescriptionId}
              </div>
            </div>
          </div>

          <div style={{ display: "flex", gap: "var(--space-md)" }}>
            <button
              type="button"
              className="kiosk-btn kiosk-btn-outline"
              style={{ flex: 1 }}
              onClick={handleBack}
              disabled={busy}
            >
              กลับ
            </button>
            <button
              type="button"
              className="kiosk-btn kiosk-btn-primary"
              style={{ flex: 1 }}
              onClick={handleConfirm}
              disabled={busy}
            >
              {busy ? "กำลังดำเนินการ..." : "เบิกยา"}
            </button>
          </div>
        </div>
      </div>
    );
  }

  // ── Dispensing / Done screens ──────────────────────────────
  if ((step === "dispensing" || step === "done") && result) {
    const rx = result;
    const isDispensing = rx.state === PrescriptionState.DISPENSING;
    const isDispensed = rx.state === PrescriptionState.DISPENSED;
    const isFailed = rx.state === PrescriptionState.FAILED;

    const statusClass = isDispensed
      ? "dispense-status--success"
      : isFailed
        ? "dispense-status--failed"
        : "dispense-status--dispensing";

    return (
      <div className="kiosk-screen">
        <div className="kiosk-panel">
          <div className={`dispense-status ${statusClass}`}>
            {isDispensing && (
              <>
                <div className="spinner" />
                <div className="dispense-status__verdict">กำลังจ่ายยา...</div>
                <div className="dispense-status__detail">
                  {rx.items?.[0]?.drugName ?? rx.prescriptionId}
                </div>
                <div className="dispense-status__detail" style={{ fontSize: "1rem", color: "var(--neutral-text-muted)" }}>
                  กรุณารอสักครู่ ระบบกำลังทำงาน
                </div>
              </>
            )}

            {isDispensed && (
              <>
                <div className="dispense-status__icon">✅</div>
                <div className="dispense-status__verdict">จ่ายยาสำเร็จ</div>
                <div className="dispense-status__detail">
                  {rx.patientName} — HN {rx.hn}
                </div>
                <div className="rx-detail__section" style={{ textAlign: "left", width: "100%" }}>
                  <ul className="rx-detail__items">
                    {rx.items?.map((item, i) => (
                      <li key={i}>
                        <div className="rx-detail__item">
                          <span className="rx-detail__item-name">{item.drugName}</span>
                          <span className="rx-detail__item-qty">×{item.quantity}</span>
                        </div>
                      </li>
                    ))}
                  </ul>
                </div>
                <button
                  type="button"
                  className="kiosk-btn kiosk-btn-primary"
                  onClick={handleDone}
                >
                  เบิกยาลำดับถัดไป
                </button>
              </>
            )}

            {isFailed && (
              <>
                <div className="dispense-status__icon">⚠️</div>
                <div className="dispense-status__verdict">การจ่ายยาล้มเหลว</div>
                {rx.failureReason && (
                  <div className="dispense-status__detail">
                    {rx.failureReason}
                  </div>
                )}
                <div className="dispense-status__detail" style={{ fontSize: "1rem", color: "var(--neutral-text-muted)" }}>
                  กรุณาติดต่อเภสัชกรเพื่อดำเนินการต่อ
                </div>
                {!dismissed ? (
                  <button
                    type="button"
                    className="kiosk-btn kiosk-btn-danger"
                    onClick={handleDone}
                  >
                    รับทราบ
                  </button>
                ) : (
                  <button
                    type="button"
                    className="kiosk-btn kiosk-btn-primary"
                    onClick={handleDone}
                  >
                    กลับสู่รายการ
                  </button>
                )}
              </>
            )}
          </div>
        </div>
      </div>
    );
  }

  // Fallback: shouldn't reach here.
  return (
    <div className="kiosk-screen">
      <div className="kiosk-panel">
        <h1 className="kiosk-panel-title">เกิดข้อผิดพลาด</h1>
        <button type="button" className="kiosk-btn kiosk-btn-outline" onClick={handleBack}>
          กลับ
        </button>
      </div>
    </div>
  );
}
