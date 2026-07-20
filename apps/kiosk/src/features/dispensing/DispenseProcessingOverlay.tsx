import type { QueueTransaction } from "./DispensingStatusModal";

export interface DispenseProcessingOverlayProps {
  txn: QueueTransaction;
  onDismiss: () => void;
}

/**
 * Live processing overlay for the active dispense. Driven ONLY by the real
 * prescription state that core writes from hardware-confirmed events
 * (DISPENSING → DISPENSED | FAILED), surfaced through the withdraw queue's 2s
 * reconcile poll. No invented progress %, ETA, or extra steps — the overlay
 * shows exactly what the backend knows, nothing more.
 *
 * Rendered only on the idle scan screen; the moment the operator scans the
 * next sticker it steps aside and the dispense keeps polling in the queue.
 */
export default function DispenseProcessingOverlay({
  txn,
  onDismiss,
}: DispenseProcessingOverlayProps) {
  const rx = txn.prescription;
  const totalQty = rx.items.reduce((sum, item) => sum + item.quantity, 0);
  const drugName = rx.items[0]?.drugName ?? rx.patientName;

  const done = txn.state === "completed";
  const failed = txn.state === "failed";
  const processing = !done && !failed;

  return (
    <div className="dispense-live" role="status" aria-live="polite">
      <div
        className={`dispense-live__card${
          done ? " is-done" : failed ? " is-failed" : " is-processing"
        }`}
      >
        {processing && (
          <>
            <div className="dispense-live__spinner" aria-hidden="true">
              <span className="spinner" />
              <span className="dispense-live__pill">💊</span>
            </div>
            <h2 className="dispense-live__title">ตู้กำลังจ่ายยา…</h2>
            <p className="dispense-live__sub">
              {txn.reconnecting
                ? "กำลังเชื่อมต่อระบบใหม่เพื่อยืนยันผล…"
                : "ระบบกำลังสั่งเครื่องหยิบยาและรอผลยืนยันจากตู้"}
            </p>
          </>
        )}

        {done && (
          <>
            <div className="dispense-live__icon dispense-live__icon--ok" aria-hidden="true">✓</div>
            <h2 className="dispense-live__title">จ่ายยาสำเร็จ</h2>
            <p className="dispense-live__sub">ตู้ยืนยันการจ่ายเรียบร้อย รับยาที่ช่องรับได้</p>
          </>
        )}

        {failed && (
          <>
            <div className="dispense-live__icon dispense-live__icon--fail" aria-hidden="true">⊘</div>
            <h2 className="dispense-live__title">จ่ายยาไม่สำเร็จ</h2>
            <p className="dispense-live__sub">
              {rx.failureReason || txn.message || "เครื่องไม่ยืนยันการจ่าย กรุณาติดต่อเภสัชกร"}
            </p>
          </>
        )}

        <dl className="dispense-live__meta">
          <div>
            <dt>รายการ</dt>
            <dd className="mono">{rx.prescriptionId}</dd>
          </div>
          <div>
            <dt>ยา</dt>
            <dd>{drugName}{rx.items.length > 1 ? ` +${rx.items.length - 1}` : ""}</dd>
          </div>
          <div>
            <dt>จำนวน</dt>
            <dd>{totalQty} หน่วย</dd>
          </div>
          <div>
            <dt>ผู้เบิก</dt>
            <dd>{txn.operator}</dd>
          </div>
        </dl>

        {processing ? (
          <div className="dispense-live__hint">
            <span className="dispense-live__dot" />
            อัปเดตสถานะอัตโนมัติจากระบบ · สามารถสแกนรายการถัดไปได้เลย
          </div>
        ) : (
          <button type="button" className="dispense-live__btn" onClick={onDismiss}>
            {done ? "ทำรายการถัดไป" : "รับทราบ"}
          </button>
        )}
      </div>
    </div>
  );
}
