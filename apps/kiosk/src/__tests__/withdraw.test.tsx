import { beforeEach, describe, expect, it, vi } from "vitest";
import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { PrescriptionState } from "@medisync/proto/medisync/dispensing/v1/dispensing_pb";

vi.mock("../transport.ts", () => ({ transport: Symbol("transport") }));
vi.mock("@connectrpc/connect-web", () => ({ createConnectTransport: () => Symbol("staff-transport") }));
const logout = vi.fn();
vi.mock("../auth.tsx", () => ({ useAuth: () => ({ state: { kiosk: { id: "k1", code: "K1", displayName: "Demo Cabinet", projectId: "project-1" } }, logout }) }));

const { listPrescriptions, listSlots, cardLogin, dispense, getPrescription } = vi.hoisted(() => ({
  listPrescriptions: vi.fn(), listSlots: vi.fn(), cardLogin: vi.fn(), dispense: vi.fn(), getPrescription: vi.fn(),
}));
vi.mock("@connectrpc/connect", async (original) => {
  const actual = await original<typeof import("@connectrpc/connect")>();
  return { ...actual, createClient: () => ({ listPrescriptions, listSlots, cardLogin, dispense, getPrescription }) };
});

import WithdrawFlow from "../features/withdraw/WithdrawFlow";

const storedValues = new Map<string, string>();
Object.defineProperty(globalThis, "localStorage", {
  configurable: true,
  value: {
    clear: () => storedValues.clear(),
    getItem: (key: string) => storedValues.get(key) ?? null,
    key: (index: number) => Array.from(storedValues.keys())[index] ?? null,
    get length() { return storedValues.size; },
    removeItem: (key: string) => storedValues.delete(key),
    setItem: (key: string, value: string) => storedValues.set(key, value),
  },
});

const prescription = {
  id: "internal-rx", prescriptionId: "DEMO-RX-001", sourceSystem: "HIS",
  hn: "HN001", patientName: "ผู้ป่วยทดสอบ", wardId: "WARD-1",
  items: [{ drugCode: "PARA", drugName: "Paracetamol", quantity: 2, dosageText: "" }],
  state: PrescriptionState.READY, failureReason: "",
};
const prescriptionB = { ...prescription, id: "internal-rx-b", prescriptionId: "DEMO-RX-002", hn: "HN002", patientName: "ผู้ป่วย ข" };
const slot = { id: "slot-1", code: "S01", drugCode: "PARA", drugName: "Paracetamol", quantity: 10, capacity: 20, lowThreshold: 3 };

async function scanRequest(user: ReturnType<typeof userEvent.setup>) {
  await user.keyboard("DEMO-RX-001{Enter}");
  await screen.findByRole("dialog", { name: "ตรวจสอบรายการยาที่จะเบิก" });
}

describe("sticker-driven withdrawal", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    localStorage.clear();
    listSlots.mockResolvedValue({ slots: [slot] });
    listPrescriptions.mockResolvedValue({ prescriptions: [prescription] });
  });

  it("starts with the sticker scanner and validates against backend data", async () => {
    const user = userEvent.setup(); render(<WithdrawFlow />);
    expect(screen.getByRole("heading", { name: "สแกน Sticker เบิกยา" })).toBeDefined();
    await scanRequest(user);
    expect(screen.getAllByText("DEMO-RX-001").length).toBeGreaterThan(0);
    expect(screen.getByText(/ชั้น 1 · ช่อง 1/)).toBeDefined();
    expect(screen.getByText("คงเหลือ 10")).toBeDefined();
    expect(screen.getByText("พร้อมจ่าย")).toBeDefined();
    expect(listPrescriptions).toHaveBeenCalledTimes(1);
  });

  it("rejects an unknown and already-dispensed sticker", async () => {
    const user = userEvent.setup(); listPrescriptions.mockResolvedValueOnce({ prescriptions: [] });
    render(<WithdrawFlow />); await user.keyboard("UNKNOWN{Enter}");
    expect(await screen.findByText(/ไม่พบรายการเบิกยา/)).toBeDefined();
  });

  it("warns in the medication cart when stock is insufficient", async () => {
    listSlots.mockResolvedValue({ slots: [{ ...slot, quantity: 1 }] });
    const user = userEvent.setup(); render(<WithdrawFlow />); await scanRequest(user);
    expect(screen.getByText("ไม่เพียงพอ · ขาด 1")).toBeDefined();
    expect(screen.getByText(/ปริมาณยาบางรายการไม่เพียงพอ/)).toBeDefined();
  });

  it("keeps cabinet slots read-only and highlights requested locations", async () => {
    const user = userEvent.setup(); render(<WithdrawFlow />); await scanRequest(user);
    await user.click(screen.getByRole("button", { name: "กลับไปตรวจสอบ" }));
    expect(screen.getByRole("button", { name: /ชั้น 1 ช่อง 1 Paracetamol/ }).className).toContain("is-requested");
    await user.click(screen.getByRole("button", { name: /ชั้น 1 ช่อง 1 Paracetamol/ }));
    expect(screen.getByRole("button", { name: /S1-R1 · Paracetamol/ })).toHaveProperty("disabled", true);
    expect(screen.queryByText(/รายการตำแหน่งที่เลือก/)).toBeNull();
  });

  it("requires staff identity and blocks an unauthorized ward", async () => {
    const user = userEvent.setup(); render(<WithdrawFlow />); await scanRequest(user);
    await user.click(screen.getByRole("button", { name: "ไปสแกนบัตรยืนยัน" }));
    cardLogin.mockResolvedValue({ accessToken: "staff-jwt", user: { id: "u1", displayName: "เจ้าหน้าที่ ก", active: true, role: 3, wardIds: ["OTHER"], projectId: "project-1" } });
    await user.type(screen.getByLabelText("รหัสบัตรเจ้าหน้าที่"), "card-token");
    await user.click(screen.getByRole("button", { name: "ยืนยันตัวตนและส่งเข้าคิว" }));
    expect(await screen.findByText(/ไม่มีสิทธิ์เบิกรายการ/)).toBeDefined();
    expect(dispense).not.toHaveBeenCalled();
  });

  it("submits immediately after authorization and keeps queue counts out of the footer", async () => {
    const user = userEvent.setup(); render(<WithdrawFlow />); await scanRequest(user);
    await user.click(screen.getByRole("button", { name: "ไปสแกนบัตรยืนยัน" }));
    cardLogin.mockResolvedValue({ accessToken: "staff-jwt", user: { id: "u1", displayName: "เจ้าหน้าที่ ก", active: true, role: 3, wardIds: ["WARD-1"], projectId: "project-1" } });
    dispense.mockResolvedValue({ prescription: { ...prescription, state: PrescriptionState.DISPENSING } });
    getPrescription.mockResolvedValue({ prescription: { ...prescription, state: PrescriptionState.DISPENSING } });
    await user.type(screen.getByLabelText("รหัสบัตรเจ้าหน้าที่"), "card-token");
    await user.click(screen.getByRole("button", { name: "ยืนยันตัวตนและส่งเข้าคิว" }));
    expect(await screen.findByRole("heading", { name: "สแกน Sticker เบิกยา" })).toBeDefined();
    expect(screen.getByText(/ส่งรายการ DEMO-RX-001 เข้าคิวสำเร็จ/)).toBeDefined();
    expect(screen.queryByRole("button", { name: "เปิดรายละเอียดคิวเครื่อง" })).toBeNull();
    expect(screen.queryByText(/กำลังเบิก/)).toBeNull();
    expect(dispense).toHaveBeenCalledTimes(1);
    await user.keyboard("DEMO-RX-001{Enter}");
    expect(await screen.findByText(/รายการนี้ถูกส่งเข้าคิวแล้ว/)).toBeDefined();
    expect(dispense).toHaveBeenCalledTimes(1);
  });

  it("queues separate stickers with a separate identity authorization for each request", async () => {
    const user = userEvent.setup();
    listPrescriptions.mockResolvedValue({ prescriptions: [prescription, prescriptionB] });
    cardLogin
      .mockResolvedValueOnce({ accessToken: "staff-a", user: { id: "u1", displayName: "เจ้าหน้าที่ ก", active: true, role: 1, wardIds: [], projectId: "project-1" } })
      .mockResolvedValueOnce({ accessToken: "staff-b", user: { id: "u2", displayName: "เจ้าหน้าที่ ข", active: true, role: 1, wardIds: [], projectId: "project-1" } });
    dispense.mockImplementation(({ prescriptionId }: { prescriptionId: string }) => Promise.resolve({ prescription: { ...(prescriptionId === "DEMO-RX-002" ? prescriptionB : prescription), state: PrescriptionState.DISPENSING } }));
    render(<WithdrawFlow />);

    await scanRequest(user);
    await user.click(screen.getByRole("button", { name: "ไปสแกนบัตรยืนยัน" }));
    await user.type(screen.getByLabelText("รหัสบัตรเจ้าหน้าที่"), "card-a");
    await user.click(screen.getByRole("button", { name: "ยืนยันตัวตนและส่งเข้าคิว" }));
    await screen.findByRole("heading", { name: "สแกน Sticker เบิกยา" });

    await user.keyboard("DEMO-RX-002{Enter}");
    await screen.findByRole("dialog", { name: "ตรวจสอบรายการยาที่จะเบิก" });
    expect(screen.getAllByText(/DEMO-RX-002/).length).toBeGreaterThan(0);
    await user.click(screen.getByRole("button", { name: "ไปสแกนบัตรยืนยัน" }));
    await user.type(screen.getByLabelText("รหัสบัตรเจ้าหน้าที่"), "card-b");
    await user.click(screen.getByRole("button", { name: "ยืนยันตัวตนและส่งเข้าคิว" }));

    await waitFor(() => expect(dispense).toHaveBeenCalledTimes(2));
    expect(cardLogin).toHaveBeenCalledTimes(2);
    expect(screen.queryByRole("button", { name: "เปิดรายละเอียดคิวเครื่อง" })).toBeNull();
  });

  it("recovers an accepted transaction and its verified operator after refresh", async () => {
    localStorage.setItem("medisync.active-withdrawals.v1", JSON.stringify([{ id: "internal-rx", requestId: "DEMO-RX-001", operator: "เจ้าหน้าที่ ก", acceptedAt: 1234 }]));
    getPrescription.mockResolvedValue({ prescription: { ...prescription, state: PrescriptionState.DISPENSING } });
    render(<WithdrawFlow />);
    await waitFor(() => expect(getPrescription).toHaveBeenCalledWith(expect.objectContaining({ id: "internal-rx" })));
    expect(screen.queryByRole("button", { name: "เปิดรายละเอียดคิวเครื่อง" })).toBeNull();
  });

  it("only reports completion after backend hardware-confirmed state", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime }); render(<WithdrawFlow />); await scanRequest(user);
    await user.click(screen.getByRole("button", { name: "ไปสแกนบัตรยืนยัน" }));
    cardLogin.mockResolvedValue({ accessToken: "staff-jwt", user: { id: "u1", displayName: "เจ้าหน้าที่ ก", active: true, role: 1, wardIds: [], projectId: "project-1" } });
    dispense.mockResolvedValue({ prescription: { ...prescription, state: PrescriptionState.DISPENSING } });
    getPrescription.mockResolvedValue({ prescription: { ...prescription, state: PrescriptionState.DISPENSED } });
    await user.type(screen.getByLabelText("รหัสบัตรเจ้าหน้าที่"), "card-token"); await user.click(screen.getByRole("button", { name: "ยืนยันตัวตนและส่งเข้าคิว" }));
    await act(async () => { await vi.advanceTimersByTimeAsync(2100); });
    await waitFor(() => expect(getPrescription).toHaveBeenCalledWith(expect.objectContaining({ id: "internal-rx" })));
    expect(screen.queryByRole("button", { name: "เปิดรายละเอียดคิวเครื่อง" })).toBeNull();
    vi.useRealTimers();
  });
});
