import { useCallback, useEffect, useMemo, useState } from "react";
import { create } from "@bufbuild/protobuf";
import {
  DispenseTransactionStatus,
  ListDispenseTransactionsRequestSchema,
  type DispenseTransaction,
} from "@medisync/proto/medisync/dispensing/v1/dispensing_pb";
import { ListKiosksRequestSchema } from "@medisync/proto/medisync/kiosk/v1/kiosk_pb";
import type { Kiosk } from "@medisync/proto/medisync/kiosk/v1/kiosk_pb";
import { dispensingClient, kioskClient } from "../../api/client";
import { Icon } from "../masterdata/icons";
import { MasterHeader, SearchInput, Select } from "../masterdata/kit";

const STATUS_OPTIONS = [
  DispenseTransactionStatus.AWAITING_IDENTITY,
  DispenseTransactionStatus.QUEUED,
  DispenseTransactionStatus.DISPENSING,
  DispenseTransactionStatus.DISPENSED,
  DispenseTransactionStatus.FAILED,
  DispenseTransactionStatus.CANCELLED,
  DispenseTransactionStatus.EXPIRED,
] as const;

const STATUS_LABEL: Record<number, string> = {
  [DispenseTransactionStatus.AWAITING_IDENTITY]: "รอยืนยันตัวตน",
  [DispenseTransactionStatus.QUEUED]: "รอคิว",
  [DispenseTransactionStatus.DISPENSING]: "กำลังจ่าย",
  [DispenseTransactionStatus.DISPENSED]: "จ่ายสำเร็จ",
  [DispenseTransactionStatus.FAILED]: "จ่ายไม่สำเร็จ",
  [DispenseTransactionStatus.CANCELLED]: "ยกเลิก",
  [DispenseTransactionStatus.EXPIRED]: "หมดเวลา",
};

function dateTime(value: DispenseTransaction["createdAt"]): string {
  if (!value) return "—";
  return new Date(Number(value.seconds) * 1000).toLocaleString("th-TH", { dateStyle: "short", timeStyle: "medium" });
}

function statusClass(status: DispenseTransactionStatus): string {
  if (status === DispenseTransactionStatus.DISPENSED) return "md-badge-on";
  if (status === DispenseTransactionStatus.FAILED || status === DispenseTransactionStatus.EXPIRED) return "md-badge-error";
  if (status === DispenseTransactionStatus.DISPENSING || status === DispenseTransactionStatus.QUEUED) return "md-badge-warn";
  return "md-badge-off";
}

function csvCell(value: unknown): string {
  return `"${String(value ?? "").replaceAll('"', '""')}"`;
}

export function DispenseTransactionsPage() {
  const [transactions, setTransactions] = useState<DispenseTransaction[]>([]);
  const [kiosks, setKiosks] = useState<Kiosk[]>([]);
  const [kioskCode, setKioskCode] = useState("");
  const [status, setStatus] = useState("");
  const [prescription, setPrescription] = useState("");
  const [loading, setLoading] = useState(true);
  const [exporting, setExporting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [total, setTotal] = useState(0);

  const request = useCallback((pageToken = "") => create(ListDispenseTransactionsRequestSchema, {
    kioskCode,
    prescriptionId: prescription.trim(),
    statuses: status ? [Number(status) as DispenseTransactionStatus] : [],
    pageSize: 200,
    pageToken,
  }), [kioskCode, prescription, status]);

  const load = useCallback(async () => {
    setLoading(true); setError(null);
    try {
      const response = await dispensingClient.listDispenseTransactions(request());
      setTransactions(response.transactions);
      setTotal(Number(response.totalCount));
    } catch (caught: unknown) {
      setError(caught instanceof Error ? caught.message : "โหลด dispense transactions ไม่สำเร็จ");
    } finally {
      setLoading(false);
    }
  }, [request]);

  useEffect(() => {
    kioskClient.listKiosks(create(ListKiosksRequestSchema, {})).then((response) => setKiosks(response.kiosks)).catch(() => undefined);
  }, []);
  useEffect(() => { void load(); }, [load]);

  const totals = useMemo(() => ({
    dispensed: transactions.filter((tx) => tx.status === DispenseTransactionStatus.DISPENSED).length,
    failed: transactions.filter((tx) => tx.status === DispenseTransactionStatus.FAILED).length,
    units: transactions.reduce((sum, tx) => sum + tx.items.reduce((itemSum, item) => itemSum + item.dispensedQuantity, 0), 0),
  }), [transactions]);

  const exportCSV = async () => {
    setExporting(true); setError(null);
    try {
      const all: DispenseTransaction[] = [];
      let token = "";
      do {
        const response = await dispensingClient.listDispenseTransactions(request(token));
        all.push(...response.transactions);
        token = response.nextPageToken;
      } while (token);
      const header = ["dispense_id", "prescription_id", "kiosk_code", "operator_user_id", "operator", "transaction_status", "drug_code", "drug_name", "requested_qty", "dispensed_qty", "item_status", "slot_code", "lot_number", "expiry_date", "allocation_qty", "allocation_dispensed_qty", "allocation_status", "door_no", "hardware_layer", "channel_start", "channel_end", "hardware_attempted_at", "hardware_success", "hardware_detail", "hardware_response", "failure_code", "failure_detail", "sticker_scanned_at", "identity_confirmed_at", "queued_at", "started_at", "completed_at", "failed_at"].map(csvCell).join(",");
      const rows = all.flatMap((tx) => tx.items.flatMap((item) => {
        const allocations = item.allocations.length ? item.allocations : [undefined];
        return allocations.map((allocation) => [
          tx.dispenseId, tx.prescriptionId, tx.kioskCode, tx.operatorUserId, tx.operatorDisplayName,
          STATUS_LABEL[tx.status] ?? tx.status, item.drugCode, item.drugName, item.requestedQuantity,
          item.dispensedQuantity, item.status, allocation?.slotCode, allocation?.lotNumber,
          dateTime(allocation?.expiryDate), allocation?.quantity, allocation?.dispensedQuantity,
          allocation?.status, allocation?.doorNo, allocation?.hardwareLayer, allocation?.channelStart,
          allocation?.channelEnd, dateTime(allocation?.hardwareAttemptedAt), allocation?.hardwareSuccess,
          allocation?.hardwareDetail, allocation?.hardwareResponse, tx.failureCode, tx.failureDetail, dateTime(tx.stickerScannedAt),
          dateTime(tx.identityConfirmedAt), dateTime(tx.queuedAt), dateTime(tx.startedAt),
          dateTime(tx.completedAt), dateTime(tx.failedAt),
        ].map(csvCell).join(","));
      }));
      const blob = new Blob(["\uFEFF", header, "\r\n", rows.join("\r\n")], { type: "text/csv;charset=utf-8" });
      const url = URL.createObjectURL(blob);
      const anchor = document.createElement("a");
      anchor.href = url; anchor.download = `dispense-transactions-${new Date().toISOString().slice(0, 10)}.csv`; anchor.click();
      URL.revokeObjectURL(url);
    } catch (caught: unknown) {
      setError(caught instanceof Error ? caught.message : "ส่งออก CSV ไม่สำเร็จ");
    } finally {
      setExporting(false);
    }
  };

  return <>
    <MasterHeader icon={Icon.clock} title="Dispense Transactions" subtitle={`ประวัติการจ่ายยาที่ตรวจสอบย้อนหลังได้ ${total} รายการ`}>
      <button className="md-btn md-btn-outline" type="button" onClick={() => void exportCSV()} disabled={exporting}><Icon.download size={18}/>{exporting ? "กำลังส่งออก…" : "ส่งออก CSV ทั้งหมด"}</button>
      <button className="md-btn md-btn-ghost" type="button" onClick={() => void load()}><Icon.undo size={18}/>รีเฟรช</button>
    </MasterHeader>
    {error && <div className="md-err">{error}</div>}
    <div className="md-kpis">
      <div className="md-kpi"><div className="md-kpi-label">รายการที่แสดง</div><div className="md-kpi-value">{transactions.length}</div></div>
      <div className="md-kpi"><div className="md-kpi-label">จ่ายสำเร็จ</div><div className="md-kpi-value">{totals.dispensed}</div></div>
      <div className="md-kpi"><div className="md-kpi-label">ไม่สำเร็จ</div><div className="md-kpi-value">{totals.failed}</div></div>
      <div className="md-kpi"><div className="md-kpi-label">หน่วยยาที่จ่ายจริง</div><div className="md-kpi-value">{totals.units}</div></div>
    </div>
    <div className="md-panel">
      <div className="md-toolbar">
        <SearchInput value={prescription} onChange={setPrescription} placeholder="ค้นหา Prescription ID"/>
        <Select value={kioskCode} onChange={setKioskCode}><option value="">ทุกตู้</option>{kiosks.map((kiosk) => <option key={kiosk.id} value={kiosk.code}>{kiosk.code} — {kiosk.displayName}</option>)}</Select>
        <Select value={status} onChange={setStatus}><option value="">ทุกสถานะ</option>{STATUS_OPTIONS.map((value) => <option key={value} value={value}>{STATUS_LABEL[value]}</option>)}</Select>
      </div>
      <div className="md-table-wrap"><table className="md-table"><thead><tr><th>เวลา</th><th>Dispense ID</th><th>Prescription</th><th>ตู้</th><th>ผู้เบิก</th><th>ยา</th><th>จ่ายจริง</th><th>สถานะ</th><th>รายละเอียด</th></tr></thead><tbody>
        {loading ? <tr><td colSpan={9}><div className="md-empty">กำลังโหลด…</div></td></tr> : transactions.length === 0 ? <tr><td colSpan={9}><div className="md-empty">ไม่พบ transaction ตามตัวกรอง</div></td></tr> : transactions.map((tx) => <tr key={tx.dispenseId}>
          <td className="md-cell-muted">{dateTime(tx.createdAt)}</td><td><span className="md-code" title={tx.dispenseId}>{tx.dispenseId.slice(0, 8)}</span></td><td>{tx.prescriptionId}</td><td><span className="md-code">{tx.kioskCode}</span></td><td>{tx.operatorDisplayName || "—"}</td>
          <td>{tx.items.map((item) => `${item.drugCode} ×${item.requestedQuantity}`).join(", ")}</td><td><strong>{tx.items.reduce((sum, item) => sum + item.dispensedQuantity, 0)}</strong></td><td><span className={`md-badge ${statusClass(tx.status)}`}>{STATUS_LABEL[tx.status] ?? "ไม่ทราบ"}</span></td><td className="md-cell-muted" title={tx.failureDetail}>{tx.failureDetail || tx.items.flatMap((item) => item.allocations.map((a) => `${a.slotCode}/${a.lotNumber}`)).join(", ") || "—"}</td>
        </tr>)}</tbody></table></div>
    </div>
  </>;
}
