import { useState, useEffect, useCallback, useRef } from "react";
import { createClient, Code } from "@connectrpc/connect";
import type { ConnectError } from "@connectrpc/connect";
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

// ── Constants ────────────────────────────────────────────────────

const POLL_INTERVAL_MS = 2000;
const MAX_RETRIES = 2;
const RETRY_DELAY_MS = 1000;

// ── Step type ────────────────────────────────────────────────────

type Step = "list" | "confirm" | "dispensing" | "done";

// ── Error kind enum ──────────────────────────────────────────────

type ErrorKind =
  | "timeout"
  | "offline"
  | "hardware_busy"
  | "cancelled"
  | "precondition"
  | "not_found"
  | "unauth"
  | "generic";

// ── Helpers ──────────────────────────────────────────────────────

/**
 * Categorize a caught error (ConnectError, Error, or unknown) into a
 * user-facing Thai message + machine-readable ErrorKind.
 */
function categorizeError(err: unknown): { kind: ErrorKind; message: string } {
  const msg = err instanceof Error ? err.message : "เกิดข้อผิดพลาด";
  const connectCode = (err as ConnectError)?.code;

  // Offline / network unreachable.
  if (
    (typeof navigator !== "undefined" && !navigator.onLine) ||
    /network error|failed to fetch|networkerror/i.test(msg)
  ) {
    return {
      kind: "offline",
      message: "ไม่สามารถเชื่อมต่อเซิร์ฟเวอร์ได้ กรุณาตรวจสอบเครือข่าย",
    };
  }

  // Timeout — deadline exceeded.
  if (connectCode === Code.DeadlineExceeded || msg.includes("timeout")) {
    return {
      kind: "timeout",
      message: "การเชื่อมต่อขัดข้อง กรุณาลองใหม่อีกครั้ง",
    };
  }

  // Hardware busy — resource exhausted.
  if (connectCode === Code.ResourceExhausted) {
    return {
      kind: "hardware_busy",
      message: "เครื่องกำลังทำงานอยู่ กรุณารอสักครู่แล้วลองใหม่",
    };
  }

  // Cancelled.
  if (connectCode === Code.Canceled) {
    return {
      kind: "cancelled",
      message: "คำขอยกเลิกแล้ว",
    };
  }

  // Failed precondition.
  if (connectCode === Code.FailedPrecondition) {
    return {
      kind: "precondition",
      message: "ใบสั่งยานี้ไม่พร้อมสำหรับการเบิก กรุณาลองใหม่",
    };
  }

  // Not found.
  if (connectCode === Code.NotFound) {
    return {
      kind: "not_found",
      message: "ไม่พบใบสั่งยานี้",
    };
  }

  // Unauthenticated.
  if (connectCode === Code.Unauthenticated) {
    return {
      kind: "unauth",
      message: "เซสชันหมดอายุ กรุณาเข้าสู่ระบบใหม่",
    };
  }

  return {
    kind: "generic",
    message: `เกิดข้อผิดพลาดในการเบิกจ่าย: ${msg}`,
  };
}

/**
 * True if the error is transient and worth retrying (timeout, offline, cancelled).
 * Note: hardware_busy is NOT transient — retrying when the hardware is busy
 * is pointless; the user should be shown the status and decide when to retry.
 */
function isTransient(kind: ErrorKind): boolean {
  return kind === "timeout" || kind === "offline" || kind === "cancelled";
}

// ── Component ────────────────────────────────────────────────────

export default function WithdrawFlow() {
  const { state, logout } = useAuth();
  const kiosk = state!.kiosk;

  const [step, setStep] = useState<Step>("list");
  const [prescriptions, setPrescriptions] = useState<Prescription[]>([]);
  const [selected, setSelected] = useState<Prescription | null>(null);
  const [result, setResult] = useState<Prescription | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [errorKind, setErrorKind] = useState<ErrorKind | null>(null);
  const [busy, setBusy] = useState(false);
  const [dismissed, setDismissed] = useState(false);
  const [listAttempt, setListAttempt] = useState(0);

  // Prevent duplicate dispense calls even if state races.
  const dispensingRef = useRef(false);

  // Polling ref for dispensing status.
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null);

  // ── Cleanup polling on unmount or step change ────────────────

  const stopPolling = useCallback(() => {
    if (pollRef.current) {
      clearInterval(pollRef.current);
      pollRef.current = null;
    }
  }, []);

  useEffect(() => {
    return () => stopPolling();
  }, [stopPolling]);

  // Stop polling when leaving the dispensing/done step.
  useEffect(() => {
    if (step !== "dispensing" && step !== "done") {
      stopPolling();
    }
  }, [step, stopPolling]);

  // ── Load ready prescriptions ─────────────────────────────────

  useEffect(() => {
    if (step !== "list") return;

    let cancelled = false;
    setBusy(true);
    setError(null);
    setErrorKind(null);

    const req = create(ListPrescriptionsRequestSchema, {
      wardId: "",
      states: [PrescriptionState.READY],
      pageSize: 50,
    });

    dispensingClient
      .listPrescriptions(req)
      .then((res) => {
        if (!cancelled) setPrescriptions(res.prescriptions ?? []);
      })
      .catch((err: unknown) => {
        if (!cancelled) {
          const { kind, message } = categorizeError(err);
          setErrorKind(kind);
          setError(message);
        }
      })
      .finally(() => {
        if (!cancelled) setBusy(false);
      });

    return () => {
      cancelled = true;
    };
  }, [step, listAttempt]);

  // ── Handlers ─────────────────────────────────────────────────

  const handleSelect = (rx: Prescription) => {
    setSelected(rx);
    setError(null);
    setErrorKind(null);
    setStep("confirm");
  };

  const handleBack = () => {
    setSelected(null);
    setResult(null);
    setError(null);
    setErrorKind(null);
    setDismissed(false);
    dispensingRef.current = false;
    setStep("list");
  };

  const handleConfirm = useCallback(async () => {
    // Guard: prevent duplicate taps.
    if (!selected || dispensingRef.current) return;
    dispensingRef.current = true;

    // Guard: prevent dispensing stale prescriptions.
    if (selected.state !== PrescriptionState.READY) {
      setError("ใบสั่งยานี้ไม่พร้อมสำหรับการเบิก กรุณาเลือกรายการใหม่");
      setErrorKind("precondition");
      dispensingRef.current = false;
      return;
    }

    setError(null);
    setErrorKind(null);
    setBusy(true);
    setStep("dispensing");

    const traceId = crypto.randomUUID();

    for (let attempt = 0; attempt <= MAX_RETRIES; attempt++) {
      if (attempt > 0) {
        // Wait before retry.
        await new Promise((r) => setTimeout(r, RETRY_DELAY_MS));
      }

      try {
        const req = create(DispenseRequestSchema, {
          prescriptionId: selected.prescriptionId,
          traceId,
        });
        const res = await dispensingClient.dispense(req);
        const rx = res.prescription;

        if (!rx) {
          setError("ไม่ได้รับข้อมูลยืนยันจากเซิร์ฟเวอร์");
          setErrorKind("generic");
          setStep("confirm");
          setBusy(false);
          dispensingRef.current = false;
          return;
        }

        // DISPENSING → poll for terminal state.
        if (rx.state === PrescriptionState.DISPENSING) {
          setResult(rx);
          startPolling(rx.id);
          return; // Busy stays true until polling finishes.
        }

        // Immediate terminal state.
        if (
          rx.state === PrescriptionState.DISPENSED ||
          rx.state === PrescriptionState.FAILED
        ) {
          setResult(rx);
          setStep("done");
          setBusy(false);
          dispensingRef.current = false;
          return;
        }

        // Unexpected state — treat as done.
        setResult(rx);
        setStep("done");
        setBusy(false);
        dispensingRef.current = false;
        return;
      } catch (err: unknown) {
        const { kind, message } = categorizeError(err);

        if (kind === "unauth") {
          setError(message);
          setErrorKind(kind);
          logout();
          dispensingRef.current = false;
          return;
        }

        if (!isTransient(kind) || attempt >= MAX_RETRIES) {
          setError(message);
          setErrorKind(kind);
          setStep("confirm");
          setBusy(false);
          dispensingRef.current = false;
          return;
        }

        // Transient error — will retry.
      }
    }

    // Should not reach here (loop handles all exits), but safety net:
    setError("เกิดข้อผิดพลาด กรุณาลองใหม่อีกครั้ง");
    setErrorKind("generic");
    setStep("confirm");
    setBusy(false);
    dispensingRef.current = false;
  }, [selected, logout, stopPolling]);

  // ── Polling ──────────────────────────────────────────────────

  function startPolling(rxId: string) {
    stopPolling();
    let pollFailures = 0;

    pollRef.current = setInterval(async () => {
      try {
        const gr = create(GetPrescriptionRequestSchema, { id: rxId });
        const fres = await dispensingClient.getPrescription(gr);
        const updated = fres.prescription;
        if (!updated) return;
        setResult(updated);
        pollFailures = 0; // reset on success

        if (
          updated.state === PrescriptionState.DISPENSED ||
          updated.state === PrescriptionState.FAILED
        ) {
          stopPolling();
          setStep("done");
          setBusy(false);
          dispensingRef.current = false;
        }
      } catch {
        pollFailures++;
        // After 10 consecutive failures (~20s), give up.
        if (pollFailures >= 10) {
          stopPolling();
          setError("ไม่สามารถตรวจสอบสถานะการจ่ายยาได้ กรุณาติดต่อเภสัชกร");
          setErrorKind("timeout");
          setStep("done");
          setBusy(false);
          dispensingRef.current = false;
        }
      }
    }, POLL_INTERVAL_MS);
  }

  const handleDone = () => {
    stopPolling();
    if (
      step === "done" &&
      result?.state === PrescriptionState.FAILED &&
      !dismissed
    ) {
      // User must acknowledge failure.
      setDismissed(true);
      return;
    }
    handleBack();
  };

  // ── Retry list load ───────────────────────────────────────────

  const handleRetry = () => {
    setListAttempt((attempt) => attempt + 1);
  };

  // ── List screen ────────────────────────────────────────────────

  if (step === "list") {
    return (
      <div className="kiosk-screen">
        <div className="kiosk-panel">
          <h1 className="kiosk-panel-title">รายการยาที่รอเบิก</h1>
          <p className="kiosk-panel-subtitle">
            {kiosk.displayName} — เลือกรายการเพื่อเริ่มเบิกยา
          </p>

          {error && (
            <div
              className={
                errorKind === "offline"
                  ? "kiosk-error kiosk-error--offline"
                  : "kiosk-error"
              }
              role="alert"
            >
              {error}
              {errorKind === "offline" && (
                <button
                  type="button"
                  className="kiosk-btn kiosk-btn-outline"
                  style={{ marginTop: "var(--space-md)", minHeight: "48px", width: "100%" }}
                  onClick={handleRetry}
                >
                  ลองใหม่
                </button>
              )}
            </div>
          )}

          {busy && (
            <div
              style={{
                display: "flex",
                justifyContent: "center",
                padding: "var(--space-2xl)",
              }}
            >
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
                  onKeyDown={(e) => {
                    if (e.key === "Enter" || e.key === " ")
                      handleSelect(rx);
                  }}
                >
                  <div className="rx-card__info">
                    <div className="rx-card__patient">{rx.patientName}</div>
                    <div className="rx-card__drugs">
                      {rx.items?.map((it) => it.drugName).join(", ")}
                    </div>
                  </div>
                  <div className="rx-card__meta">
                    <span className="rx-card__hn">HN {rx.hn}</span>
                    <span className="status-badge status-badge--info">
                      พร้อมเบิก
                    </span>
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

  // ── Confirm screen ─────────────────────────────────────────────

  if (step === "confirm" && selected) {
    return (
      <div className="kiosk-screen">
        <div className="kiosk-panel">
          <div className="step-indicator">ขั้นตอน 1 จาก 2</div>
          <h1 className="kiosk-panel-title">ยืนยันการเบิกยา</h1>

          {error && (
            <div
              className={
                errorKind === "hardware_busy"
                  ? "kiosk-error kiosk-error--warning"
                  : "kiosk-error"
              }
              role="alert"
            >
              {error}
            </div>
          )}

          <div className="rx-detail">
            <div className="rx-detail__section">
              <div className="rx-detail__heading">ผู้ป่วย</div>
              <div className="rx-detail__value">{selected.patientName}</div>
              <span className="rx-card__hn">HN {selected.hn}</span>
            </div>

            <div className="rx-detail__section">
              <div className="rx-detail__heading">รายการยา</div>
              <ul className="rx-detail__items">
                {selected.items?.map((item, i) => (
                  <li key={i}>
                    <div className="rx-detail__item">
                      <div
                        style={{
                          display: "flex",
                          flexDirection: "column",
                          gap: "var(--space-xs)",
                        }}
                      >
                        <span className="rx-detail__item-name">
                          {item.drugName}
                        </span>
                        {item.dosageText && (
                          <span className="rx-detail__item-dosage">
                            {item.dosageText}
                          </span>
                        )}
                      </div>
                      <span className="rx-detail__item-qty">
                        ×{item.quantity}
                      </span>
                    </div>
                  </li>
                ))}
              </ul>
            </div>

            <div className="rx-detail__section">
              <div className="rx-detail__heading">เลขที่ใบสั่งยา</div>
              <div
                className="rx-detail__value"
                style={{ fontFamily: "var(--font-mono)", fontSize: "1rem" }}
              >
                {selected.prescriptionId}
              </div>
            </div>

            <div className="rx-detail__section">
              <div className="rx-detail__heading">ตู้จ่ายยา</div>
              <div className="rx-detail__value">{kiosk.displayName}</div>
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

  // ── Dispensing / Done screens ──────────────────────────────────

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
            {/* ── Dispensing (in-flight) ──────────────────────── */}
            {isDispensing && (
              <>
                <div className="spinner" />
                <div className="dispense-status__verdict">
                  กำลังจ่ายยา...
                </div>
                <div className="dispense-status__detail">
                  {rx.patientName} — HN {rx.hn}
                </div>
                <div className="dispense-status__detail">
                  {rx.items?.map((it) => `${it.drugName} ×${it.quantity}`).join(", ")}
                </div>
                <div
                  className="dispense-status__detail"
                  style={{
                    fontSize: "1rem",
                    color: "var(--neutral-text-muted)",
                  }}
                >
                  กรุณารอสักครู่ ระบบกำลังทำงาน
                </div>
                {error && (
                  <div className="kiosk-error" role="alert">
                    {error}
                  </div>
                )}
              </>
            )}

            {/* ── Dispensed (success) ─────────────────────────── */}
            {isDispensed && (
              <>
                <div className="dispense-status__icon">✅</div>
                <div className="dispense-status__verdict">
                  จ่ายยาสำเร็จ
                </div>
                <div className="dispense-status__detail">
                  {rx.patientName} — HN {rx.hn}
                </div>
                <div
                  className="rx-detail__section"
                  style={{ textAlign: "left", width: "100%" }}
                >
                  <ul className="rx-detail__items">
                    {rx.items?.map((item, i) => (
                      <li key={i}>
                        <div className="rx-detail__item">
                          <span className="rx-detail__item-name">
                            {item.drugName}
                          </span>
                          <span className="rx-detail__item-qty">
                            ×{item.quantity}
                          </span>
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

            {/* ── Failed ───────────────────────────────────────── */}
            {isFailed && (
              <>
                <div className="dispense-status__icon">⚠️</div>
                <div className="dispense-status__verdict">
                  การจ่ายยาล้มเหลว
                </div>
                {rx.failureReason && (
                  <div className="dispense-status__detail">
                    {rx.failureReason}
                  </div>
                )}
                <div
                  className="dispense-status__detail"
                  style={{
                    fontSize: "1rem",
                    color: "var(--neutral-text-muted)",
                  }}
                >
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

  // ── Fallback: shouldn't reach here ────────────────────────────

  return (
    <div className="kiosk-screen">
      <div className="kiosk-panel">
        <h1 className="kiosk-panel-title">เกิดข้อผิดพลาด</h1>
        <button
          type="button"
          className="kiosk-btn kiosk-btn-outline"
          onClick={handleBack}
        >
          กลับ
        </button>
      </div>
    </div>
  );
}
