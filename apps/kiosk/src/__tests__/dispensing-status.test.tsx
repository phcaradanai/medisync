import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { PrescriptionState } from "@medisync/proto/medisync/dispensing/v1/dispensing_pb";
import type { Prescription } from "@medisync/proto/medisync/dispensing/v1/dispensing_pb";
import DispensingStatusModal, {
  type QueueTransaction,
} from "../features/dispensing/DispensingStatusModal";

const baseRx = {
  $typeName: "medisync.dispensing.v1.Prescription",
  id: "rx-1",
  prescriptionId: "RX-001",
  sourceSystem: "HIS",
  hn: "HN001",
  patientName: "ผู้ป่วยทดสอบ",
  wardId: "WARD-1",
  items: [
    { $typeName: "medisync.dispensing.v1.PrescriptionItem", drugCode: "PARA500", drugName: "พาราเซตามอล 500 มก.", quantity: 30, dosageText: "" },
  ],
  state: PrescriptionState.READY,
  failureReason: "",
} satisfies Prescription;

const dispensingItem: QueueTransaction = {
  id: "q-1",
  requestId: "RX-001",
  prescription: baseRx,
  operator: "นพ. ธนกฤต",
  acceptedAt: new Date("2026-07-20T10:24:00").getTime(),
  state: "dispensing",
};

const waitingItems: QueueTransaction[] = [
  {
    id: "q-2",
    requestId: "RX-002",
    prescription: {
      ...baseRx,
      id: "rx-2",
      prescriptionId: "RX-002",
      hn: "HN002",
      patientName: "ผู้ป่วย ข",
      items: [
        { $typeName: "medisync.dispensing.v1.PrescriptionItem", drugCode: "AMOX500", drugName: "อะม็อกซีซิลลิน 500 มก.", quantity: 20, dosageText: "" },
      ],
    },
    operator: "พญ. ศิริลักษณ์",
    acceptedAt: new Date("2026-07-20T10:25:00").getTime(),
    state: "queued",
  },
  {
    id: "q-3",
    requestId: "RX-003",
    prescription: {
      ...baseRx,
      id: "rx-3",
      prescriptionId: "RX-003",
      hn: "HN003",
      patientName: "ผู้ป่วย ค",
      items: [
        { $typeName: "medisync.dispensing.v1.PrescriptionItem", drugCode: "IBU400", drugName: "ไอบูโพรเฟน 400 มก.", quantity: 10, dosageText: "" },
      ],
    },
    operator: "นพ. วรเชษฐ์",
    acceptedAt: new Date("2026-07-20T10:26:00").getTime(),
    state: "queued",
  },
  {
    id: "q-4",
    requestId: "RX-004",
    prescription: {
      ...baseRx,
      id: "rx-4",
      prescriptionId: "RX-004",
      hn: "HN004",
      patientName: "ผู้ป่วย ง",
      items: [
        { $typeName: "medisync.dispensing.v1.PrescriptionItem", drugCode: "OME20", drugName: "โอเมพราโซล 20 มก.", quantity: 14, dosageText: "" },
      ],
    },
    operator: "พญ. กนกวรรณ",
    acceptedAt: new Date("2026-07-20T10:27:00").getTime(),
    state: "queued",
  },
];

const now = new Date("2026-07-20T10:28:00");

describe("DispensingStatusModal", () => {
  it("renders header with title and close button", () => {
    render(<DispensingStatusModal queue={[]} now={now} onClose={vi.fn()} />);
    expect(screen.getByRole("dialog", { name: "สถานะการเบิกยา" })).toBeDefined();
    expect(screen.getByRole("heading", { name: "สถานะการเบิกยา" })).toBeDefined();
    expect(screen.getByText("ติดตามสถานะยาที่กำลังเบิกและคิวรอยา")).toBeDefined();
    expect(screen.getByRole("button", { name: "ปิดหน้าต่างสถานะการเบิกยา" })).toBeDefined();
  });

  it("shows empty states when queue is empty", () => {
    render(<DispensingStatusModal queue={[]} now={now} onClose={vi.fn()} />);
    expect(screen.getByText("ไม่มีรายการที่กำลังเบิกยาในขณะนี้")).toBeDefined();
    expect(screen.getByText("ไม่มีรายการที่รอคิวในขณะนี้")).toBeDefined();
    // "กำลังดำเนินการ" pill should not appear
    expect(screen.queryByText("กำลังดำเนินการ")).toBeNull();
    // "รอคิว" and count pills should show 0
    expect(screen.getByText("0 รายการ")).toBeDefined();
  });

  it("renders dispensing item with drug details, chips, timeline, and ETA", () => {
    render(<DispensingStatusModal queue={[dispensingItem]} now={now} onClose={vi.fn()} />);

    // Drug name
    expect(screen.getByText("PARA500")).toBeDefined();
    expect(screen.getByText("พาราเซตามอล 500 มก.")).toBeDefined();

    // Chips
    expect(screen.getByText("30 หน่วย")).toBeDefined();
    expect(screen.getByText("นพ. ธนกฤต")).toBeDefined();

    // Active pill
    expect(screen.getByText("กำลังดำเนินการ")).toBeDefined();

    // Timeline steps
    expect(screen.getByText("รับรายการ")).toBeDefined();
    expect(screen.getByText("กำลังเบิกยา")).toBeDefined();
    expect(screen.getByText("ตรวจสอบ")).toBeDefined();
    expect(screen.getByText("พร้อมจ่าย")).toBeDefined();

    // ETA box
    expect(screen.getByText("คาดว่าจะเสร็จสิ้น")).toBeDefined();
    expect(screen.getByText(/ประมาณ/)).toBeDefined();
  });

  it("highlights current step (กำลังเบิกยา) with is-current class when dispensing", () => {
    render(<DispensingStatusModal queue={[dispensingItem]} now={now} onClose={vi.fn()} />);

    // Step 1 (รับรายการ) should be done
    const step1 = screen.getByText("รับรายการ").closest(".dispense-timeline__step");
    expect(step1?.className).toContain("is-done");

    // Step 2 (กำลังเบิกยา) should be current
    const step2 = screen.getByText("กำลังเบิกยา").closest(".dispense-timeline__step");
    expect(step2?.className).toContain("is-current");

    // Step 3,4 should have no special class
    const step3 = screen.getByText("ตรวจสอบ").closest(".dispense-timeline__step");
    expect(step3?.className).not.toContain("is-done");
    expect(step3?.className).not.toContain("is-current");
  });

  it("renders waiting queue rows with correct ordering", () => {
    render(
      <DispensingStatusModal
        queue={waitingItems}
        now={now}
        onClose={vi.fn()}
      />,
    );

    // Table headers
    expect(screen.getByText("ลำดับคิว")).toBeDefined();
    expect(screen.getByText("รายการยา")).toBeDefined();
    expect(screen.getByText("จำนวน")).toBeDefined();
    expect(screen.getByText("ผู้เบิก")).toBeDefined();
    expect(screen.getByText("เวลาที่เบิก")).toBeDefined();
    expect(screen.getByText("สถานะ")).toBeDefined();

    // Queue rows
    expect(screen.getByText("AMOX500")).toBeDefined();
    expect(screen.getByText("อะม็อกซีซิลลิน 500 มก.")).toBeDefined();
    expect(screen.getByText("IBU400")).toBeDefined();
    expect(screen.getByText("OME20")).toBeDefined();

    // Operators
    expect(screen.getByText("พญ. ศิริลักษณ์")).toBeDefined();
    expect(screen.getByText("นพ. วรเชษฐ์")).toBeDefined();
    expect(screen.getByText("พญ. กนกวรรณ")).toBeDefined();

    // Queue numbers (1, 2, 3)
    expect(screen.getByText("1")).toBeDefined();
    expect(screen.getByText("2")).toBeDefined();
    expect(screen.getByText("3")).toBeDefined();

    // Status pills
    const statusPills = screen.getAllByText("รอคิว");
    expect(statusPills.length).toBeGreaterThanOrEqual(3);

    // Count pill
    expect(screen.getByText("3 รายการ")).toBeDefined();
  });

  it("shows both dispensing section and queue section together", () => {
    render(
      <DispensingStatusModal
        queue={[dispensingItem, ...waitingItems]}
        now={now}
        onClose={vi.fn()}
      />,
    );

    // Both sections present
    expect(screen.getByText("ยาที่กำลังเบิกอยู่")).toBeDefined();
    expect(screen.getByText("ยาที่กำลังรอคิว")).toBeDefined();

    // Dispensing drug
    expect(screen.getByText("พาราเซตามอล 500 มก.")).toBeDefined();

    // Queue drug
    expect(screen.getByText("อะม็อกซีซิลลิน 500 มก.")).toBeDefined();
  });

  it("calls onClose when Escape key is pressed", async () => {
    const onClose = vi.fn();
    const user = userEvent.setup();
    render(<DispensingStatusModal queue={[]} now={now} onClose={onClose} />);

    await user.keyboard("{Escape}");
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it("calls onClose when close button is clicked", async () => {
    const onClose = vi.fn();
    const user = userEvent.setup();
    render(<DispensingStatusModal queue={[]} now={now} onClose={onClose} />);

    await user.click(screen.getByRole("button", { name: "ปิดหน้าต่างสถานะการเบิกยา" }));
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it("calls onClose when footer close button is clicked", async () => {
    const onClose = vi.fn();
    const user = userEvent.setup();
    render(<DispensingStatusModal queue={[]} now={now} onClose={onClose} />);

    await user.click(screen.getByText("ปิด"));
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it("calls onClose when backdrop is clicked", async () => {
    const onClose = vi.fn();
    const user = userEvent.setup();
    render(<DispensingStatusModal queue={[]} now={now} onClose={onClose} />);

    await user.click(screen.getByRole("dialog"));
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it("shows footer with last updated timestamp and gradient close button", () => {
    render(<DispensingStatusModal queue={[]} now={now} onClose={vi.fn()} />);

    // Footer timestamp
    expect(screen.getByText(/ข้อมูลอัปเดตล่าสุด:/)).toBeDefined();

    // Close button
    const closeBtn = screen.getByText("ปิด");
    expect(closeBtn.className).toContain("dispense-status-modal__close-btn");
  });

  it("filters completed and failed items — only queued items appear in waiting list", () => {
    const mixed: QueueTransaction[] = [
      { ...dispensingItem, state: "dispensing" },
      waitingItems[0], // queued
      {
        ...waitingItems[1],
        id: "q-c",
        state: "completed" as const,
      },
      {
        ...waitingItems[2],
        id: "q-f",
        state: "failed" as const,
      },
    ];

    render(<DispensingStatusModal queue={mixed} now={now} onClose={vi.fn()} />);

    // Only 1 queued item should show in the table
    expect(screen.getByText("1 รายการ")).toBeDefined();
    expect(screen.getByText("AMOX500")).toBeDefined();
    // completed/failed items should NOT appear
    expect(screen.queryByText("IBU400")).toBeNull();
    expect(screen.queryByText("OME20")).toBeNull();
  });

  it("falls back to patientName when items array is empty", () => {
    const noItems: QueueTransaction = {
      id: "q-empty",
      requestId: "RX-EMPTY",
      prescription: { ...baseRx, items: [] },
      operator: "นพ. ทดสอบ",
      acceptedAt: now.getTime(),
      state: "dispensing",
    };

    render(<DispensingStatusModal queue={[noItems]} now={now} onClose={vi.fn()} />);

    // Should show patient name as fallback for Thai drug name
    expect(screen.getByText("ผู้ป่วยทดสอบ")).toBeDefined();
  });
});
