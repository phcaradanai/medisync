import { useEffect, useState } from "react";
import type { Prescription } from "@medisync/proto/medisync/dispensing/v1/dispensing_pb";
import { useModalFocus } from "../../hooks/useModalFocus";

export type QueueState = "queued" | "dispensing" | "completed" | "failed" | "unknown";

export interface QueueTransaction {
  id: string;
  requestId: string;
  prescription: Prescription;
  operator: string;
  acceptedAt: number;
  state: QueueState;
  reconnecting?: boolean;
  message?: string;
}

export interface DispensingStatusModalProps {
  queue: readonly QueueTransaction[];
  now?: Date;
  onClose: () => void;
}

/** 4 milestones in dispensing workflow */
const STEPS = ["รับรายการ", "กำลังเบิกยา", "ตรวจสอบ", "พร้อมจ่าย"] as const;

function formatTime(ts: number): string {
  const d = new Date(ts);
  return d.toLocaleTimeString("th-TH", { hour: "2-digit", minute: "2-digit", hour12: false });
}

function thaiDrugName(rx: Prescription): string {
  // Extract first item's drug name as the Thai name convention
  return rx.items[0]?.drugName ?? rx.patientName;
}

function englishDrugName(rx: Prescription): string {
  // Use prescription ID as the English identifier in absence of English name field
  return rx.items[0]?.drugCode ?? rx.prescriptionId;
}

/** Estimate remaining time based on item count and current progress step */
function estimateCompletion(step: number, totalItems: number): { time: string; minutes: number } {
  // Simple heuristic: ~1 min per step remaining, adjusted by items
  const stepsRemaining = STEPS.length - step;
  const minutes = Math.max(1, stepsRemaining * Math.ceil(totalItems / 5));
  const eta = new Date(Date.now() + minutes * 60_000);
  return {
    time: eta.toLocaleTimeString("th-TH", { hour: "2-digit", minute: "2-digit", hour12: false }),
    minutes,
  };
}

export default function DispensingStatusModal({
  queue,
  now,
  onClose,
}: DispensingStatusModalProps) {
  const dialogRef = useModalFocus<HTMLDivElement>(onClose);
  const [liveNow, setLiveNow] = useState(() => now ?? new Date());

  useEffect(() => {
    if (now) {
      setLiveNow(now);
      return;
    }
    const timer = window.setInterval(() => setLiveNow(new Date()), 1_000);
    return () => window.clearInterval(timer);
  }, [now]);

  // Separate dispensing item from waiting items
  const dispensing = queue.find((q) => q.state === "dispensing");
  const waiting = queue.filter((q) => q.state === "queued");

  // Determine current step: 1=รับรายการ, 2=กำลังเบิกยา, 3=ตรวจสอบ, 4=พร้อมจ่าย
  const currentStep = dispensing ? 2 : 1;
  const totalItems = queue.reduce((sum, q) => sum + q.prescription.items.length, 0);
  const eta = estimateCompletion(currentStep, totalItems);

  return (
    <div
      className="dispense-status-modal__backdrop"
      role="dialog"
      aria-modal="true"
      aria-label="สถานะการเบิกยา"
      onClick={(event) => {
        if (event.target === event.currentTarget) onClose();
      }}
    >
      <div ref={dialogRef} className="dispense-status-modal">
        {/* Header */}
        <header className="dispense-status-modal__header">
          <div className="dispense-status-modal__header-icon" aria-hidden="true">
            <svg viewBox="0 0 48 48" fill="none" width="42" height="42">
              <ellipse cx="18" cy="30" rx="10" ry="12" fill="#14B8A6" />
              <ellipse cx="32" cy="32" rx="10" ry="12" fill="#0EA5E9" />
              <rect x="11" y="27" width="14" height="4" rx="2" fill="#fff" opacity="0.7" />
              <rect x="25" y="29" width="14" height="4" rx="2" fill="#fff" opacity="0.7" />
              <circle cx="42" cy="10" r="3" fill="none" stroke="#60A5FA" strokeWidth="2" />
            </svg>
          </div>
          <div className="dispense-status-modal__header-text">
            <h2>สถานะการเบิกยา</h2>
            <p>ติดตามสถานะยาที่กำลังเบิกและคิวรอยา</p>
          </div>
          <button
            type="button"
            className="dispense-status-modal__close"
            aria-label="ปิดหน้าต่างสถานะการเบิกยา"
            onClick={onClose}
          >
            <svg viewBox="0 0 24 24" width="20" height="20" stroke="currentColor" strokeWidth="2" fill="none">
              <path d="M18 6 6 18M6 6l12 12" />
            </svg>
          </button>
        </header>

        {/* Currently dispensing section */}
        <section className="dispense-status-modal__now" aria-label="ยาที่กำลังเบิกอยู่">
          <header className="dispense-section__title">
            <span className="dispense-section__title-icon" aria-hidden="true">
              <svg viewBox="0 0 24 24" width="22" height="22" fill="none" stroke="currentColor" strokeWidth="2">
                <path d="M6 2 3 6v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2V6l-3-4Z" />
                <path d="M3 6h18" />
                <path d="M16 10a4 4 0 0 1-8 0" />
              </svg>
            </span>
            <strong>ยาที่กำลังเบิกอยู่</strong>
            {dispensing && (
              <span className="dispense-status-pill dispense-status-pill--active">
                <span className="dispense-status-pill__dot" />
                กำลังดำเนินการ
              </span>
            )}
          </header>

          {dispensing ? (
            <div className="dispense-now-card">
              {/* Drug illustration */}
              <div className="dispense-now-card__visual" aria-hidden="true">
                <svg viewBox="0 0 80 100" width="80" height="100">
                  <rect x="16" y="32" width="48" height="60" rx="8" fill="white" stroke="white" strokeWidth="2" />
                  <rect x="26" y="18" width="28" height="18" rx="4" fill="white" />
                  <path d="M32 54 l16 8 l-16 8 Z" fill="white" opacity="0.5" />
                  <circle cx="60" cy="30" r="8" fill="none" stroke="white" strokeWidth="2" />
                  <path d="M58 30 l4 3M62 30 l-4 -3" stroke="white" strokeWidth="2" />
                </svg>
              </div>

              {/* Drug details */}
              <div className="dispense-now-card__details">
                <strong className="dispense-now-card__drug-en">
                  {englishDrugName(dispensing.prescription)}
                </strong>
                <span className="dispense-now-card__drug-th">
                  {thaiDrugName(dispensing.prescription)}
                </span>
                <div className="dispense-now-card__chips">
                  <div className="dispense-chip">
                    <span aria-hidden="true">💊</span>
                    <div>
                      <small>จำนวน</small>
                      <strong>
                        {dispensing.prescription.items.reduce((s, i) => s + i.quantity, 0)} หน่วย
                      </strong>
                    </div>
                  </div>
                  <div className="dispense-chip">
                    <span aria-hidden="true">👨‍⚕</span>
                    <div>
                      <small>ผู้เบิก</small>
                      <strong>{dispensing.operator}</strong>
                    </div>
                  </div>
                  <div className="dispense-chip">
                    <span aria-hidden="true">🕐</span>
                    <div>
                      <small>เวลาเริ่มเบิก</small>
                      <strong>{formatTime(dispensing.acceptedAt)} น.</strong>
                    </div>
                  </div>
                </div>
              </div>

              {/* Divider */}
              <div className="dispense-now-card__divider" aria-hidden="true" />

              {/* Progress timeline */}
              <div className="dispense-now-card__timeline">
                <span className="dispense-now-card__timeline-label">สถานะการเบิกยา</span>
                <div className="dispense-timeline">
                  <div className="dispense-timeline__track" />
                  <div className="dispense-timeline__fill" style={{ width: `${((currentStep - 1) / (STEPS.length - 1)) * 100}%` }} />
                  {STEPS.map((label, index) => {
                    const stepNum = index + 1;
                    const isDone = stepNum < currentStep;
                    const isCurrent = stepNum === currentStep;
                    return (
                      <div
                        key={label}
                        className={`dispense-timeline__step${
                          isDone ? " is-done" : isCurrent ? " is-current" : ""
                        }`}
                      >
                        <span className="dispense-timeline__node" />
                        <span className="dispense-timeline__label">{label}</span>
                      </div>
                    );
                  })}
                </div>
              </div>

              {/* Divider */}
              <div className="dispense-now-card__divider" aria-hidden="true" />

              {/* ETA box */}
              <div className="dispense-now-card__eta">
                <span className="dispense-eta__label">คาดว่าจะเสร็จสิ้น</span>
                <strong className="dispense-eta__time">{eta.time} น.</strong>
                <span className="dispense-eta__duration">ประมาณ {eta.minutes} นาที</span>
              </div>
            </div>
          ) : (
            <div className="dispense-now-card dispense-now-card--empty">
              <span className="dispense-now-card__empty-icon">💊</span>
              <p>ไม่มีรายการที่กำลังเบิกยาในขณะนี้</p>
            </div>
          )}
        </section>

        {/* Waiting queue section */}
        <section className="dispense-status-modal__queue" aria-label="ยาที่กำลังรอคิว">
          <header className="dispense-section__title dispense-section__title--queue">
            <span className="dispense-section__title-icon" aria-hidden="true">
              <svg viewBox="0 0 24 24" width="22" height="22" fill="none" stroke="currentColor" strokeWidth="2">
                <circle cx="12" cy="12" r="10" />
                <path d="M12 6v6l4 2" />
              </svg>
            </span>
            <strong>ยาที่กำลังรอคิว</strong>
            <span className="dispense-status-pill dispense-status-pill--queue">
              <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="2">
                <circle cx="12" cy="12" r="10" />
                <path d="M12 6v6l4 2" />
              </svg>
              รอคิว
            </span>
            <span className="dispense-status-pill dispense-status-pill--count">
              <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="2">
                <path d="M17 21v-2a4 4 0 0 0-4-4H5a4 4 0 0 0-4 4v2" />
                <circle cx="9" cy="7" r="4" />
                <path d="M23 21v-2a4 4 0 0 0-3-3.87M16 3.13a4 4 0 0 1 0 7.75" />
              </svg>
              {waiting.length} รายการ
            </span>
          </header>

          {waiting.length > 0 ? (
            <div className="dispense-queue-table">
              {/* Table header */}
              <div className="dispense-queue-table__head">
                <span>ลำดับคิว</span>
                <span>รายการยา</span>
                <span>จำนวน</span>
                <span>ผู้เบิก</span>
                <span>เวลาที่เบิก</span>
                <span>สถานะ</span>
              </div>

              {/* Queue rows */}
              <div className="dispense-queue-table__body">
                {waiting.map((item, index) => {
                  const rx = item.prescription;
                  const totalQty = rx.items.reduce((s, i) => s + i.quantity, 0);
                  const drugName = rx.items[0]?.drugName ?? rx.patientName;
                  const drugCode = rx.items[0]?.drugCode ?? rx.prescriptionId;
                  return (
                    <div key={item.id} className="dispense-queue-row">
                      <span className="dispense-queue-row__number">{index + 1}</span>
                      <div className="dispense-queue-row__drug">
                        <strong>{drugCode}</strong>
                        <span>{drugName}</span>
                      </div>
                      <span className="dispense-queue-row__qty">
                        <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="2" aria-hidden="true">
                          <path d="M17 21v-2a4 4 0 0 0-4-4H5a4 4 0 0 0-4 4v2" />
                          <circle cx="9" cy="7" r="4" />
                        </svg>
                        {totalQty} {totalQty > 1 ? "หน่วย" : "หน่วย"}
                      </span>
                      <span className="dispense-queue-row__operator">{item.operator}</span>
                      <span className="dispense-queue-row__time">{formatTime(item.acceptedAt)} น.</span>
                      <span className="dispense-queue-row__status">
                        <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="2" aria-hidden="true">
                          <circle cx="12" cy="12" r="10" />
                          <path d="M12 6v6l4 2" />
                        </svg>
                        รอคิว
                      </span>
                    </div>
                  );
                })}
              </div>
            </div>
          ) : (
            <div className="dispense-queue-table__empty">
              <span>📋</span>
              <p>ไม่มีรายการที่รอคิวในขณะนี้</p>
            </div>
          )}
        </section>

        {/* Footer */}
        <footer className="dispense-status-modal__footer">
          <div className="dispense-status-modal__updated">
            <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" strokeWidth="2" aria-hidden="true">
              <circle cx="12" cy="12" r="10" />
              <path d="M12 16v-4M12 8h.01" />
            </svg>
            <span>
              ข้อมูลอัปเดตล่าสุด: {liveNow.toLocaleTimeString("th-TH", { hour: "2-digit", minute: "2-digit", second: "2-digit", hour12: false })} น.
            </span>
          </div>
          <button
            type="button"
            className="dispense-status-modal__close-btn"
            onClick={onClose}
          >
            ปิด
          </button>
        </footer>
      </div>
    </div>
  );
}
