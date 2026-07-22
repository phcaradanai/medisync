import { useCallback, useEffect, useMemo, useState } from "react";
import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import {
  DispenseTransactionStatus,
  EmergencyDispenseStatus,
  EmergencyOperatorAuthMethod,
  ListEmergencyDispenseTransactionsRequestSchema,
  ListDispenseTransactionsRequestSchema,
  type DispenseTransaction,
  type EmergencyDispenseTransaction,
} from "@medisync/proto/medisync/dispensing/v1/dispensing_pb";
import { ListKiosksRequestSchema } from "@medisync/proto/medisync/kiosk/v1/kiosk_pb";
import type { Kiosk } from "@medisync/proto/medisync/kiosk/v1/kiosk_pb";
import { dispensingClient, kioskClient } from "../../api/client";
import { Icon } from "../masterdata/icons";
import {
  MasterHeader, ListHeading, SearchInput, Select, MasterTable, type Column,
} from "../masterdata/kit";

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

const EMERGENCY_STATUS_LABEL: Record<number, string> = {
  [EmergencyDispenseStatus.QUEUED]: "รอคิว",
  [EmergencyDispenseStatus.DISPENSING]: "กำลังจ่าย",
  [EmergencyDispenseStatus.DISPENSED]: "จ่ายสำเร็จ",
  [EmergencyDispenseStatus.FAILED]: "จ่ายไม่สำเร็จ",
};

function dateTime(value: { seconds: bigint | number } | null | undefined): string {
  if (!value) return "—";
  return new Date(Number(value.seconds) * 1000).toLocaleString("th-TH", {
    dateStyle: "short",
    timeStyle: "medium",
  });
}

function statusClass(status: DispenseTransactionStatus): string {
  if (status === DispenseTransactionStatus.DISPENSED) return "md-badge-on";
  if (
    status === DispenseTransactionStatus.FAILED ||
    status === DispenseTransactionStatus.EXPIRED
  )
    return "md-badge-error";
  if (
    status === DispenseTransactionStatus.DISPENSING ||
    status === DispenseTransactionStatus.QUEUED
  )
    return "md-badge-warn";
  return "md-badge-off";
}

function csvCell(value: unknown): string {
  return `"${String(value ?? "").replaceAll('"', '""')}"`;
}

function dateBoundary(value: string, nextDay: boolean) {
  if (!value) return undefined;
  const date = new Date(`${value}T00:00:00`);
  if (nextDay) date.setDate(date.getDate() + 1);
  return timestampFromDate(date);
}

export function DispenseTransactionsPage() {
  const [transactions, setTransactions] = useState<DispenseTransaction[]>([]);
  const [emergencyTransactions, setEmergencyTransactions] = useState<
    EmergencyDispenseTransaction[]
  >([]);
  const [transactionType, setTransactionType] = useState<
    "prescription" | "emergency"
  >("prescription");
  const [kiosks, setKiosks] = useState<Kiosk[]>([]);
  const [kioskCode, setKioskCode] = useState("");
  const [status, setStatus] = useState("");
  const [prescription, setPrescription] = useState("");
  const [drugCode, setDrugCode] = useState("");
  const [employeeCode, setEmployeeCode] = useState("");
  const [authMethod, setAuthMethod] = useState("");
  const [createdFrom, setCreatedFrom] = useState("");
  const [createdTo, setCreatedTo] = useState("");
  const [loading, setLoading] = useState(true);
  const [exporting, setExporting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [total, setTotal] = useState(0);

  const request = useCallback(
    (pageToken = "") =>
      create(ListDispenseTransactionsRequestSchema, {
        kioskCode,
        prescriptionId: prescription.trim(),
        drugCode: drugCode.trim(),
        statuses: status ? [Number(status) as DispenseTransactionStatus] : [],
        createdFrom: dateBoundary(createdFrom, false),
        createdTo: dateBoundary(createdTo, true),
        pageSize: 200,
        pageToken,
      }),
    [createdFrom, createdTo, drugCode, kioskCode, prescription, status],
  );

  const emergencyRequest = useCallback(
    (pageToken = "") =>
      create(ListEmergencyDispenseTransactionsRequestSchema, {
        kioskCode,
        hn: prescription.trim(),
        employeeCode: employeeCode.trim(),
        drugCode: drugCode.trim(),
        statuses: status ? [Number(status) as EmergencyDispenseStatus] : [],
        operatorAuthMethods: authMethod
          ? [Number(authMethod) as EmergencyOperatorAuthMethod]
          : [],
        createdFrom: dateBoundary(createdFrom, false),
        createdTo: dateBoundary(createdTo, true),
        pageSize: 200,
        pageToken,
      }),
    [
      authMethod,
      createdFrom,
      createdTo,
      drugCode,
      employeeCode,
      kioskCode,
      prescription,
      status,
    ],
  );

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      if (transactionType === "emergency") {
        const response =
          await dispensingClient.listEmergencyDispenseTransactions(
            emergencyRequest(),
          );
        setEmergencyTransactions(response.transactions);
        setTotal(Number(response.totalCount));
      } else {
        const response =
          await dispensingClient.listDispenseTransactions(request());
        setTransactions(response.transactions);
        setTotal(Number(response.totalCount));
      }
    } catch (caught: unknown) {
      setError(
        caught instanceof Error
          ? caught.message
          : "โหลด dispense transactions ไม่สำเร็จ",
      );
    } finally {
      setLoading(false);
    }
  }, [emergencyRequest, request, transactionType]);

  useEffect(() => {
    setStatus("");
    setPrescription("");
    setDrugCode("");
    setEmployeeCode("");
    setAuthMethod("");
  }, [transactionType]);

  useEffect(() => {
    kioskClient
      .listKiosks(create(ListKiosksRequestSchema, {}))
      .then((response) => setKiosks(response.kiosks))
      .catch(() => undefined);
  }, []);
  useEffect(() => {
    void load();
  }, [load]);

  const totals = useMemo(
    () =>
      transactionType === "emergency"
        ? {
            dispensed: emergencyTransactions.filter(
              (tx) => tx.status === EmergencyDispenseStatus.DISPENSED,
            ).length,
            failed: emergencyTransactions.filter(
              (tx) => tx.status === EmergencyDispenseStatus.FAILED,
            ).length,
            units: emergencyTransactions.reduce(
              (sum, tx) => sum + tx.dispensedQuantity,
              0,
            ),
          }
        : {
            dispensed: transactions.filter(
              (tx) => tx.status === DispenseTransactionStatus.DISPENSED,
            ).length,
            failed: transactions.filter(
              (tx) => tx.status === DispenseTransactionStatus.FAILED,
            ).length,
            units: transactions.reduce(
              (sum, tx) =>
                sum +
                tx.items.reduce(
                  (itemSum, item) => itemSum + item.dispensedQuantity,
                  0,
                ),
              0,
            ),
          },
    [emergencyTransactions, transactionType, transactions],
  );

  // ── MasterTable columns ────────────────────────────────────────────

  const emergencyColumns: Column<EmergencyDispenseTransaction>[] = [
    {
      key: "created_at",
      header: "เวลา",
      render: (tx) => <span className="md-cell-muted">{dateTime(tx.createdAt)}</span>,
    },
    {
      key: "dispense_id",
      header: "Dispense ID",
      render: (tx) => (
        <span className="md-code" title={tx.dispenseId}>
          {tx.dispenseId.slice(0, 8)}
        </span>
      ),
    },
    { key: "hn", header: "HN", render: (tx) => tx.hn },
    {
      key: "kiosk_slot",
      header: "ตู้ / ช่อง",
      render: (tx) => (
        <span className="md-code">
          {tx.kioskCode} / {tx.slotCode}
        </span>
      ),
    },
    {
      key: "operator",
      header: "ผู้เบิก",
      render: (tx) => (
        <>
          {tx.operatorDisplayName || tx.employeeCode}
          <br />
          <small>
            {tx.operatorAuthMethod === EmergencyOperatorAuthMethod.CARD
              ? "บัตร"
              : "รหัสพนักงาน"}
          </small>
        </>
      ),
    },
    {
      key: "drug",
      header: "ยา",
      render: (tx) => `${tx.drugCode} — ${tx.drugName}`,
    },
    {
      key: "quantity",
      header: "จ่ายจริง",
      render: (tx) => (
        <strong>
          {tx.dispensedQuantity} / {tx.requestedQuantity}
        </strong>
      ),
    },
    {
      key: "status",
      header: "สถานะ",
      render: (tx) => {
        const cls =
          tx.status === EmergencyDispenseStatus.DISPENSED
            ? "md-badge-on"
            : tx.status === EmergencyDispenseStatus.FAILED
              ? "md-badge-error"
              : "md-badge-warn";
        return (
          <span className={`md-badge ${cls}`}>
            {EMERGENCY_STATUS_LABEL[tx.status] ?? "ไม่ทราบ"}
          </span>
        );
      },
    },
    {
      key: "detail",
      header: "รายละเอียด",
      render: (tx) => (
        <span className="md-cell-muted" title={tx.failureDetail}>
          {tx.failureDetail || tx.reason || "—"}
        </span>
      ),
    },
  ];

  const prescriptionColumns: Column<DispenseTransaction>[] = [
    {
      key: "created_at",
      header: "เวลา",
      render: (tx) => <span className="md-cell-muted">{dateTime(tx.createdAt)}</span>,
    },
    {
      key: "dispense_id",
      header: "Dispense ID",
      render: (tx) => (
        <span className="md-code" title={tx.dispenseId}>
          {tx.dispenseId.slice(0, 8)}
        </span>
      ),
    },
    { key: "prescription_id", header: "Prescription", render: (tx) => tx.prescriptionId },
    {
      key: "kiosk",
      header: "ตู้",
      render: (tx) => <span className="md-code">{tx.kioskCode}</span>,
    },
    {
      key: "operator",
      header: "ผู้เบิก",
      render: (tx) => tx.operatorDisplayName || "—",
    },
    {
      key: "drug",
      header: "ยา",
      render: (tx) =>
        tx.items
          .map((item) => `${item.drugCode} ×${item.requestedQuantity}`)
          .join(", "),
    },
    {
      key: "quantity",
      header: "จ่ายจริง",
      render: (tx) => (
        <strong>
          {tx.items.reduce((sum, item) => sum + item.dispensedQuantity, 0)}
        </strong>
      ),
    },
    {
      key: "status",
      header: "สถานะ",
      render: (tx) => (
        <span className={`md-badge ${statusClass(tx.status)}`}>
          {STATUS_LABEL[tx.status] ?? "ไม่ทราบ"}
        </span>
      ),
    },
    {
      key: "detail",
      header: "รายละเอียด",
      render: (tx) => (
        <span className="md-cell-muted" title={tx.failureDetail}>
          {tx.failureDetail ||
            tx.items
              .flatMap((item) =>
                item.allocations.map((a) => `${a.slotCode}/${a.lotNumber}`),
              )
              .join(", ") ||
            "—"}
        </span>
      ),
    },
  ];

  // ── CSV export (unchanged) ─────────────────────────────────────────

  const exportCSV = async () => {
    setExporting(true);
    setError(null);
    try {
      if (transactionType === "emergency") {
        const all: EmergencyDispenseTransaction[] = [];
        let token = "";
        do {
          const response =
            await dispensingClient.listEmergencyDispenseTransactions(
              emergencyRequest(token),
            );
          all.push(...response.transactions);
          token = response.nextPageToken;
        } while (token);
        const header = [
          "transaction_type",
          "dispense_id",
          "kiosk_code",
          "hn",
          "employee_code",
          "operator",
          "auth_method",
          "drug_code",
          "drug_name",
          "slot_code",
          "requested_qty",
          "dispensed_qty",
          "status",
          "reason",
          "failure_code",
          "failure_detail",
          "trace_id",
          "queued_at",
          "started_at",
          "completed_at",
          "failed_at",
        ]
          .map(csvCell)
          .join(",");
        const rows = all.map((tx) =>
          [
            "EMERGENCY",
            tx.dispenseId,
            tx.kioskCode,
            tx.hn,
            tx.employeeCode,
            tx.operatorDisplayName,
            tx.operatorAuthMethod === EmergencyOperatorAuthMethod.CARD
              ? "CARD"
              : "EMPLOYEE_CODE",
            tx.drugCode,
            tx.drugName,
            tx.slotCode,
            tx.requestedQuantity,
            tx.dispensedQuantity,
            EMERGENCY_STATUS_LABEL[tx.status] ?? tx.status,
            tx.reason,
            tx.failureCode,
            tx.failureDetail,
            tx.traceId,
            dateTime(tx.queuedAt),
            dateTime(tx.startedAt),
            dateTime(tx.completedAt),
            dateTime(tx.failedAt),
          ]
            .map(csvCell)
            .join(","),
        );
        const blob = new Blob(["\uFEFF", header, "\r\n", rows.join("\r\n")], {
          type: "text/csv;charset=utf-8",
        });
        const url = URL.createObjectURL(blob);
        const anchor = document.createElement("a");
        anchor.href = url;
        anchor.download = `emergency-dispense-transactions-${new Date().toISOString().slice(0, 10)}.csv`;
        anchor.click();
        URL.revokeObjectURL(url);
        return;
      }
      const all: DispenseTransaction[] = [];
      let token = "";
      do {
        const response = await dispensingClient.listDispenseTransactions(
          request(token),
        );
        all.push(...response.transactions);
        token = response.nextPageToken;
      } while (token);
      const header = [
        "dispense_id",
        "prescription_id",
        "kiosk_code",
        "operator_user_id",
        "operator",
        "transaction_status",
        "drug_code",
        "drug_name",
        "requested_qty",
        "dispensed_qty",
        "item_status",
        "slot_code",
        "lot_number",
        "expiry_date",
        "allocation_qty",
        "allocation_dispensed_qty",
        "allocation_status",
        "door_no",
        "hardware_layer",
        "channel_start",
        "channel_end",
        "hardware_attempted_at",
        "hardware_success",
        "hardware_detail",
        "hardware_response",
        "failure_code",
        "failure_detail",
        "sticker_scanned_at",
        "identity_confirmed_at",
        "queued_at",
        "started_at",
        "completed_at",
        "failed_at",
      ]
        .map(csvCell)
        .join(",");
      const rows = all.flatMap((tx) =>
        tx.items.flatMap((item) => {
          const allocations = item.allocations.length
            ? item.allocations
            : [undefined];
          return allocations.map((allocation) =>
            [
              tx.dispenseId,
              tx.prescriptionId,
              tx.kioskCode,
              tx.operatorUserId,
              tx.operatorDisplayName,
              STATUS_LABEL[tx.status] ?? tx.status,
              item.drugCode,
              item.drugName,
              item.requestedQuantity,
              item.dispensedQuantity,
              item.status,
              allocation?.slotCode,
              allocation?.lotNumber,
              dateTime(allocation?.expiryDate),
              allocation?.quantity,
              allocation?.dispensedQuantity,
              allocation?.status,
              allocation?.doorNo,
              allocation?.hardwareLayer,
              allocation?.channelStart,
              allocation?.channelEnd,
              dateTime(allocation?.hardwareAttemptedAt),
              allocation?.hardwareSuccess,
              allocation?.hardwareDetail,
              allocation?.hardwareResponse,
              tx.failureCode,
              tx.failureDetail,
              dateTime(tx.stickerScannedAt),
              dateTime(tx.identityConfirmedAt),
              dateTime(tx.queuedAt),
              dateTime(tx.startedAt),
              dateTime(tx.completedAt),
              dateTime(tx.failedAt),
            ]
              .map(csvCell)
              .join(","),
          );
        }),
      );
      const blob = new Blob(["\uFEFF", header, "\r\n", rows.join("\r\n")], {
        type: "text/csv;charset=utf-8",
      });
      const url = URL.createObjectURL(blob);
      const anchor = document.createElement("a");
      anchor.href = url;
      anchor.download = `dispense-transactions-${new Date().toISOString().slice(0, 10)}.csv`;
      anchor.click();
      URL.revokeObjectURL(url);
    } catch (caught: unknown) {
      setError(
        caught instanceof Error ? caught.message : "ส่งออก CSV ไม่สำเร็จ",
      );
    } finally {
      setExporting(false);
    }
  };

  // ── Render ─────────────────────────────────────────────────────────

  const currentList = transactionType === "emergency" ? emergencyTransactions : transactions;
  const listTitle =
    transactionType === "emergency" ? "รายการเบิกจ่ายฉุกเฉิน" : "รายการจ่ายยาตาม Prescription";

  return (
    <>
      <MasterHeader
        icon={Icon.clock}
        title='Dispense Transactions'
        subtitle={`${transactionType === "emergency" ? "Emergency" : "Prescription"} transactions ที่ตรวจสอบย้อนหลังได้ ${total} รายการ`}
      >
        <button
          className='md-btn md-btn-outline'
          type='button'
          onClick={() => void exportCSV()}
          disabled={exporting}
        >
          <Icon.download size={18} />
          {exporting ? "กำลังส่งออก…" : "ส่งออก CSV ทั้งหมด"}
        </button>
        <button
          className='md-btn md-btn-ghost'
          type='button'
          onClick={() => void load()}
        >
          <Icon.undo size={18} />
          รีเฟรช
        </button>
      </MasterHeader>
      {error && <div className='md-err'>{error}</div>}

      <div className='md-kpis'>
        <div className='md-kpi'>
          <div className='md-kpi-label'>รายการที่แสดง</div>
          <div className='md-kpi-value'>
            {currentList.length}
          </div>
        </div>
        <div className='md-kpi'>
          <div className='md-kpi-label'>จ่ายสำเร็จ</div>
          <div className='md-kpi-value'>{totals.dispensed}</div>
        </div>
        <div className='md-kpi'>
          <div className='md-kpi-label'>ไม่สำเร็จ</div>
          <div className='md-kpi-value'>{totals.failed}</div>
        </div>
        <div className='md-kpi'>
          <div className='md-kpi-label'>หน่วยยาที่จ่ายจริง</div>
          <div className='md-kpi-value'>{totals.units}</div>
        </div>
      </div>

      <div className='md-panel'>
        <ListHeading icon={Icon.clock} title={listTitle} count={currentList.length} />

        <div className='md-toolbar'>
          <Select
            value={transactionType}
            onChange={(value) =>
              setTransactionType(value as "prescription" | "emergency")
            }
          >
            <option value='prescription'>ตาม Prescription</option>
            <option value='emergency'>เบิกฉุกเฉิน</option>
          </Select>
          <SearchInput
            value={prescription}
            onChange={setPrescription}
            placeholder={
              transactionType === "emergency"
                ? "ค้นหา HN"
                : "ค้นหา Prescription ID"
            }
          />
          <SearchInput
            value={drugCode}
            onChange={setDrugCode}
            placeholder='ค้นหารหัสยา'
          />
          {transactionType === "emergency" && (
            <SearchInput
              value={employeeCode}
              onChange={setEmployeeCode}
              placeholder='ค้นหารหัสพนักงาน'
            />
          )}
          <Select value={kioskCode} onChange={setKioskCode}>
            <option value=''>ทุกตู้</option>
            {kiosks.map((kiosk) => (
              <option key={kiosk.id} value={kiosk.code}>
                {kiosk.code} — {kiosk.displayName}
              </option>
            ))}
          </Select>
          <Select value={status} onChange={setStatus}>
            <option value=''>ทุกสถานะ</option>
            {transactionType === "emergency"
              ? [
                  EmergencyDispenseStatus.QUEUED,
                  EmergencyDispenseStatus.DISPENSING,
                  EmergencyDispenseStatus.DISPENSED,
                  EmergencyDispenseStatus.FAILED,
                ].map((value) => (
                  <option key={value} value={value}>
                    {EMERGENCY_STATUS_LABEL[value]}
                  </option>
                ))
              : STATUS_OPTIONS.map((value) => (
                  <option key={value} value={value}>
                    {STATUS_LABEL[value]}
                  </option>
                ))}
          </Select>
          {transactionType === "emergency" && (
            <Select value={authMethod} onChange={setAuthMethod}>
              <option value=''>ทุกวิธียืนยัน</option>
              <option value={EmergencyOperatorAuthMethod.CARD}>สแกนบัตร</option>
              <option value={EmergencyOperatorAuthMethod.EMPLOYEE_CODE}>
                รหัสพนักงาน
              </option>
            </Select>
          )}
          <label className='md-date-filter'>
            <span>ตั้งแต่</span>
            <input
              type='date'
              value={createdFrom}
              max={createdTo || undefined}
              onChange={(event) => setCreatedFrom(event.target.value)}
            />
          </label>
          <label className='md-date-filter'>
            <span>ถึง</span>
            <input
              type='date'
              value={createdTo}
              min={createdFrom || undefined}
              onChange={(event) => setCreatedTo(event.target.value)}
            />
          </label>
        </div>

        {transactionType === "emergency" ? (
          <MasterTable
            rows={emergencyTransactions}
            columns={emergencyColumns}
            getId={(tx) => tx.dispenseId}
            loading={loading}
            emptyText='ไม่พบ Emergency transaction ตามตัวกรอง'
          />
        ) : (
          <MasterTable
            rows={transactions}
            columns={prescriptionColumns}
            getId={(tx) => tx.dispenseId}
            loading={loading}
            emptyText='ไม่พบ transaction ตามตัวกรอง'
          />
        )}
      </div>
    </>
  );
}