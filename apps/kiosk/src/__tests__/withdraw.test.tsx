import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { PrescriptionState } from "@medisync/proto/medisync/dispensing/v1/dispensing_pb";

// ── Mock the transport ─────────────────────────────────────────

vi.mock("../transport.ts", () => ({
  transport: Symbol("mock-transport"),
}));

// ── Mock auth: return a logged-in kiosk ────────────────────────

const mockLogout = vi.fn();

vi.mock("../auth.tsx", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../auth.tsx")>();
  return {
    ...actual,
    useAuth: () => ({
      state: {
        kiosk: {
          id: "k1",
          code: "KIOSK-WARD3A",
          displayName: "ตู้จ่ายยาวอร์ด 3A",
          active: true,
        },
        token: "test-token",
        expiresAt: new Date(Date.now() + 3600_000),
      },
      loading: false,
      login: vi.fn(),
      logout: mockLogout,
    }),
  };
});

// ── Mock createClient for DispensingService ────────────────────

const mockListPrescriptions = vi.fn();
const mockDispense = vi.fn();
const mockGetPrescription = vi.fn();

vi.mock("@connectrpc/connect", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@connectrpc/connect")>();
  return {
    ...actual,
    createClient: () => ({
      listPrescriptions: (...args: unknown[]) => mockListPrescriptions(...args),
      dispense: (...args: unknown[]) => mockDispense(...args),
      getPrescription: (...args: unknown[]) => mockGetPrescription(...args),
    }),
  };
});

// ── Now import the component ───────────────────────────────────

import WithdrawFlow from "../WithdrawFlow.tsx";

// ── Test fixtures ───────────────────────────────────────────────

function makePrescription(overrides: Record<string, unknown> = {}) {
  return {
    id: (overrides.id as string) ?? "p1",
    prescriptionId: (overrides.prescriptionId as string) ?? "RX-001",
    sourceSystem: "HIS",
    hn: (overrides.hn as string) ?? "HN-0001",
    patientName: (overrides.patientName as string) ?? "สมชาย ใจดี",
    wardId: "W01",
    items: [
      {
        drugCode: "PARA500",
        drugName: "Paracetamol 500mg",
        quantity: 10,
        dosageText: "1 tab oral tid pc",
      },
    ],
    state: (overrides.state as number) ?? PrescriptionState.READY,
    failureReason: (overrides.failureReason as string) ?? "",
  };
}

function makeDispenseResponse(state: number = PrescriptionState.DISPENSING) {
  return {
    prescription: makePrescription({ state }),
  };
}

// ── Helpers ──────────────────────────────────────────────────────

/** Navigate from list → confirm screen for a dispense flow. */
async function goToConfirm(prescriptions = [makePrescription()]) {
  mockListPrescriptions.mockResolvedValueOnce({ prescriptions });
  render(<WithdrawFlow />);
  await waitFor(() => {
    expect(screen.getByText("สมชาย ใจดี")).toBeDefined();
  });
  await userEvent.click(screen.getByText("สมชาย ใจดี"));
  await waitFor(() => {
    expect(screen.getByText("ยืนยันการเบิกยา")).toBeDefined();
  });
}

// ── Tests ────────────────────────────────────────────────────────

describe("WithdrawFlow", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    Object.defineProperty(navigator, "onLine", {
      value: true,
      writable: true,
      configurable: true,
    });
  });

  // ── List rendering ──────────────────────────────────────────

  it("renders prescription list from mocked ListPrescriptions", async () => {
    mockListPrescriptions.mockResolvedValueOnce({
      prescriptions: [
        makePrescription({ id: "p1", prescriptionId: "RX-001", patientName: "สมชาย ใจดี", hn: "HN-0001" }),
        makePrescription({ id: "p2", prescriptionId: "RX-002", patientName: "สมหญิง รักดี", hn: "HN-0002" }),
      ],
    });

    render(<WithdrawFlow />);

    await waitFor(() => {
      expect(screen.getByText("สมชาย ใจดี")).toBeDefined();
    });

    expect(screen.getByText("สมหญิง รักดี")).toBeDefined();
    expect(screen.getByText("HN HN-0001")).toBeDefined();
    expect(screen.getByText("HN HN-0002")).toBeDefined();
    expect(screen.getAllByText("Paracetamol 500mg")).toHaveLength(2);
  });

  it("shows empty state when no prescriptions", async () => {
    mockListPrescriptions.mockResolvedValueOnce({ prescriptions: [] });

    render(<WithdrawFlow />);

    await waitFor(() => {
      expect(screen.getByText("ไม่มีใบสั่งยาที่รอเบิกอยู่ในขณะนี้")).toBeDefined();
    });
  });

  it("shows error when ListPrescriptions fails", async () => {
    mockListPrescriptions.mockRejectedValueOnce(new Error("Internal Server Error"));

    render(<WithdrawFlow />);

    await waitFor(() => {
      expect(screen.getByText("เกิดข้อผิดพลาดในการเบิกจ่าย: Internal Server Error")).toBeDefined();
    });
  });

  it("shows offline error and retry button when list fails with offline", async () => {
    Object.defineProperty(navigator, "onLine", {
      value: false,
      writable: true,
      configurable: true,
    });
    mockListPrescriptions.mockRejectedValueOnce(new Error("Failed to fetch"));

    render(<WithdrawFlow />);

    await waitFor(() => {
      expect(screen.getByText(/ไม่สามารถเชื่อมต่อเซิร์ฟเวอร์ได้/)).toBeDefined();
    });
    expect(screen.getByText("ลองใหม่")).toBeDefined();
  });

  // ── Confirm screen ───────────────────────────────────────────

  it("navigates to confirm screen on prescription selection", async () => {
    mockListPrescriptions.mockResolvedValueOnce({
      prescriptions: [makePrescription()],
    });

    render(<WithdrawFlow />);

    await waitFor(() => {
      expect(screen.getByText("สมชาย ใจดี")).toBeDefined();
    });

    await userEvent.click(screen.getByText("สมชาย ใจดี"));

    await waitFor(() => {
      expect(screen.getByText("ยืนยันการเบิกยา")).toBeDefined();
    });
    expect(screen.getByText("RX-001")).toBeDefined();
    expect(screen.getByText("Paracetamol 500mg")).toBeDefined();
    expect(screen.getByText("×10")).toBeDefined();
  });

  it("shows kiosk name on confirm screen", async () => {
    mockListPrescriptions.mockResolvedValueOnce({
      prescriptions: [makePrescription()],
    });

    render(<WithdrawFlow />);

    await waitFor(() => {
      expect(screen.getByText("สมชาย ใจดี")).toBeDefined();
    });
    await userEvent.click(screen.getByText("สมชาย ใจดี"));

    await waitFor(() => {
      expect(screen.getByText("ตู้จ่ายยาวอร์ด 3A")).toBeDefined();
    });
  });

  // ── Confirm → Dispense happy path ────────────────────────────

  it("confirm calls dispense", async () => {
    mockListPrescriptions.mockResolvedValueOnce({
      prescriptions: [makePrescription()],
    });
    // Return DISPENSED immediately — no polling needed.
    mockDispense.mockResolvedValueOnce(makeDispenseResponse(PrescriptionState.DISPENSED));

    render(<WithdrawFlow />);

    await waitFor(() => {
      expect(screen.getByText("สมชาย ใจดี")).toBeDefined();
    });
    await userEvent.click(screen.getByText("สมชาย ใจดี"));

    await waitFor(() => {
      expect(screen.getByText("ยืนยันการเบิกยา")).toBeDefined();
    });

    await userEvent.click(screen.getByRole("button", { name: "เบิกยา" }));

    await waitFor(() => {
      expect(mockDispense).toHaveBeenCalledWith(
        expect.objectContaining({
          prescriptionId: "RX-001",
          traceId: expect.any(String),
        }),
      );
    });

    // With DISPENSED response, we should see success immediately.
    await waitFor(() => {
      expect(screen.getByText("จ่ายยาสำเร็จ")).toBeDefined();
    });
  });

  // ── Dispense failure paths ───────────────────────────────────

  it("shows error on FailedPrecondition dispense error", async () => {
    await goToConfirm();
    const err = new Error("failed precondition") as Error & { code: number };
    err.code = 9; // Code.FailedPrecondition
    mockDispense.mockRejectedValueOnce(err);

    await userEvent.click(screen.getByRole("button", { name: "เบิกยา" }));

    await waitFor(() => {
      expect(
        screen.getByText("ใบสั่งยานี้ไม่พร้อมสำหรับการเบิก กรุณาลองใหม่"),
      ).toBeDefined();
    });

    // Should return to confirm screen.
    expect(screen.getByText("ยืนยันการเบิกยา")).toBeDefined();
  });

  it("shows error on NotFound dispense error", async () => {
    await goToConfirm();
    const err = new Error("not found") as Error & { code: number };
    err.code = 5; // Code.NotFound
    mockDispense.mockRejectedValueOnce(err);

    await userEvent.click(screen.getByRole("button", { name: "เบิกยา" }));

    await waitFor(() => {
      expect(screen.getByText("ไม่พบใบสั่งยานี้")).toBeDefined();
    });
  });

  it("logs out on Unauthenticated dispense error", async () => {
    await goToConfirm();
    const err = new Error("unauthenticated") as Error & { code: number };
    err.code = 16; // Code.Unauthenticated
    mockDispense.mockRejectedValueOnce(err);

    await userEvent.click(screen.getByRole("button", { name: "เบิกยา" }));

    await waitFor(() => {
      expect(mockLogout).toHaveBeenCalled();
    });
  });

  it("shows failure screen when dispense returns FAILED state", async () => {
    await goToConfirm();
    mockDispense.mockResolvedValueOnce({
      prescription: makePrescription({
        state: PrescriptionState.FAILED,
        failureReason: "Slot A01 timeout",
      }),
    });

    await userEvent.click(screen.getByRole("button", { name: "เบิกยา" }));

    await waitFor(() => {
      expect(screen.getByText("การจ่ายยาล้มเหลว")).toBeDefined();
    });
    expect(screen.getByText("Slot A01 timeout")).toBeDefined();
    expect(screen.getByText("กรุณาติดต่อเภสัชกรเพื่อดำเนินการต่อ")).toBeDefined();
  });

  it("shows hardware-busy error on ResourceExhausted", async () => {
    await goToConfirm();
    // Create a mock error with a ConnectError-like code property.
    const err = new Error("resource exhausted") as Error & { code: number };
    err.code = 8; // Code.ResourceExhausted
    mockDispense.mockRejectedValueOnce(err);

    await userEvent.click(screen.getByRole("button", { name: "เบิกยา" }));

    await waitFor(() => {
      expect(screen.getByText(/เครื่องกำลังทำงานอยู่/)).toBeDefined();
    });
  });

  // ── Back button ──────────────────────────────────────────────

  it("back button returns to prescription list from confirm screen", async () => {
    mockListPrescriptions.mockResolvedValueOnce({
      prescriptions: [makePrescription()],
    });

    render(<WithdrawFlow />);

    await waitFor(() => {
      expect(screen.getByText("สมชาย ใจดี")).toBeDefined();
    });
    await userEvent.click(screen.getByText("สมชาย ใจดี"));

    await waitFor(() => {
      expect(screen.getByText("ยืนยันการเบิกยา")).toBeDefined();
    });

    // Going back re-triggers list load.
    mockListPrescriptions.mockResolvedValueOnce({
      prescriptions: [makePrescription()],
    });

    await userEvent.click(screen.getByRole("button", { name: "กลับ" }));

    await waitFor(() => {
      expect(screen.getByText("รายการยาที่รอเบิก")).toBeDefined();
    });
  });

  // ── Failure acknowledgment flow ──────────────────────────────

  it("requires acknowledgment of failure before returning to list", async () => {
    await goToConfirm();
    mockDispense.mockResolvedValueOnce({
      prescription: makePrescription({
        state: PrescriptionState.FAILED,
        failureReason: "Hardware jam",
      }),
    });

    await userEvent.click(screen.getByRole("button", { name: "เบิกยา" }));

    // First click: acknowledge.
    await waitFor(() => {
      expect(screen.getByText("รับทราบ")).toBeDefined();
    });
    await userEvent.click(screen.getByText("รับทราบ"));

    // After acknowledge, show "back to list" button.
    expect(screen.getByText("กลับสู่รายการ")).toBeDefined();
  });

  // ── Duplicate-click guard (ref-based, works with real timers) ─

  it("prevents duplicate dispense calls on rapid clicks", async () => {
    mockListPrescriptions.mockResolvedValueOnce({
      prescriptions: [makePrescription()],
    });
    // Resolve quickly so we can observe the guard.
    let callCount = 0;
    mockDispense.mockImplementation(() => {
      callCount++;
      return Promise.resolve(makeDispenseResponse(PrescriptionState.DISPENSED));
    });

    render(<WithdrawFlow />);

    await waitFor(() => {
      expect(screen.getByText("สมชาย ใจดี")).toBeDefined();
    });
    await userEvent.click(screen.getByText("สมชาย ใจดี"));

    await waitFor(() => {
      expect(screen.getByText("ยืนยันการเบิกยา")).toBeDefined();
    });

    const dispenseBtn = screen.getByRole("button", { name: "เบิกยา" });
    // Rapid double-click.
    await userEvent.click(dispenseBtn);
    await userEvent.click(dispenseBtn);

    // The dispensingRef guard is synchronous — second click is ignored.
    await waitFor(() => {
      expect(callCount).toBe(1);
    });
    expect(mockDispense).toHaveBeenCalledTimes(1);
  });

  // ── Retry behavior ──────────────────────────────────────────

  it("retries on transient network error then succeeds", async () => {
    // Use real timers for rendering + interaction, then verify
    // the internal retry by counting total dispense calls after
    // a transient-first, success-second mock pattern.
    mockListPrescriptions.mockResolvedValueOnce({
      prescriptions: [makePrescription()],
    });
    mockDispense
      .mockRejectedValueOnce(new Error("timeout"))
      .mockResolvedValueOnce(makeDispenseResponse(PrescriptionState.DISPENSED));

    render(<WithdrawFlow />);

    await waitFor(() => {
      expect(screen.getByText("สมชาย ใจดี")).toBeDefined();
    });
    await userEvent.click(screen.getByText("สมชาย ใจดี"));
    await waitFor(() => screen.getByText("ยืนยันการเบิกยา"));
    await userEvent.click(screen.getByRole("button", { name: "เบิกยา" }));

    // The first call fails, second succeeds after retry delay.
    // Wait for the retry to complete and success screen to appear.
    await waitFor(
      () => {
        expect(screen.getByText("จ่ายยาสำเร็จ")).toBeDefined();
      },
      { timeout: 5000 },
    );

    expect(mockDispense).toHaveBeenCalledTimes(2);
  });

  it("gives up after max retries on transient errors", async () => {
    mockListPrescriptions.mockResolvedValueOnce({
      prescriptions: [makePrescription()],
    });
    mockDispense
      .mockRejectedValueOnce(new Error("timeout"))
      .mockRejectedValueOnce(new Error("timeout"))
      .mockRejectedValueOnce(new Error("timeout"));

    render(<WithdrawFlow />);

    await waitFor(() => {
      expect(screen.getByText("สมชาย ใจดี")).toBeDefined();
    });
    await userEvent.click(screen.getByText("สมชาย ใจดี"));
    await waitFor(() => screen.getByText("ยืนยันการเบิกยา"));
    await userEvent.click(screen.getByRole("button", { name: "เบิกยา" }));

    // After 3 attempts, should show timeout error.
    await waitFor(
      () => {
        expect(screen.getByText(/การเชื่อมต่อขัดข้อง/)).toBeDefined();
      },
      { timeout: 10000 },
    );

    expect(mockDispense).toHaveBeenCalledTimes(3);
  });
});
