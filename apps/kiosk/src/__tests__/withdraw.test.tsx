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

// ── Tests ────────────────────────────────────────────────────────

describe("WithdrawFlow", () => {
  beforeEach(() => {
    vi.clearAllMocks();
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
    mockListPrescriptions.mockRejectedValueOnce(new Error("Network error"));

    render(<WithdrawFlow />);

    await waitFor(() => {
      expect(screen.getByText("ไม่สามารถโหลดรายการยาได้: Network error")).toBeDefined();
    });
  });

  // ── Confirm → Dispense happy path ────────────────────────────

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

  it("confirm calls dispense", async () => {
    mockListPrescriptions.mockResolvedValueOnce({
      prescriptions: [makePrescription()],
    });
    // Return DISPENSED immediately so the polling loop resolves on first tick
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

    // With DISPENSED response, we should see success immediately
    await waitFor(() => {
      expect(screen.getByText("จ่ายยาสำเร็จ")).toBeDefined();
    });
  });

  // ── Dispense failure paths ───────────────────────────────────

  it("shows error on FailedPrecondition dispense error", async () => {
    mockListPrescriptions.mockResolvedValueOnce({
      prescriptions: [makePrescription()],
    });
    mockDispense.mockRejectedValueOnce(new Error("FailedPrecondition"));

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
      expect(
        screen.getByText("ใบสั่งยานี้ไม่พร้อมสำหรับการเบิก กรุณาลองใหม่"),
      ).toBeDefined();
    });

    // Should return to confirm screen
    expect(screen.getByText("ยืนยันการเบิกยา")).toBeDefined();
  });

  it("shows error on NotFound dispense error", async () => {
    mockListPrescriptions.mockResolvedValueOnce({
      prescriptions: [makePrescription()],
    });
    mockDispense.mockRejectedValueOnce(new Error("NotFound"));

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
      expect(screen.getByText("ไม่พบใบสั่งยานี้")).toBeDefined();
    });
  });

  it("logs out on Unauthenticated dispense error", async () => {
    mockListPrescriptions.mockResolvedValueOnce({
      prescriptions: [makePrescription()],
    });
    mockDispense.mockRejectedValueOnce(new Error("Unauthenticated"));

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
      expect(mockLogout).toHaveBeenCalled();
    });
  });

  it("shows failure screen when dispense returns FAILED state", async () => {
    mockListPrescriptions.mockResolvedValueOnce({
      prescriptions: [makePrescription()],
    });
    mockDispense.mockResolvedValueOnce({
      prescription: makePrescription({
        state: PrescriptionState.FAILED,
        failureReason: "Slot A01 timeout",
      }),
    });

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
      expect(screen.getByText("การจ่ายยาล้มเหลว")).toBeDefined();
    });
    expect(screen.getByText("Slot A01 timeout")).toBeDefined();
    expect(screen.getByText("กรุณาติดต่อเภสัชกรเพื่อดำเนินการต่อ")).toBeDefined();
  });

  // ── Back button ──────────────────────────────────────────────

  it("back button returns to prescription list from confirm screen", async () => {
    mockListPrescriptions.mockResolvedValue({
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

    await userEvent.click(screen.getByRole("button", { name: "กลับ" }));

    await waitFor(() => {
      expect(screen.getByText("รายการยาที่รอเบิก")).toBeDefined();
    });
  });
});
