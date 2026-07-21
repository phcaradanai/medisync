import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Code, ConnectError } from "@connectrpc/connect";
import { DispenseTransactionStatus, EmergencyDispenseStatus, EmergencyOperatorAuthMethod, PrescriptionState } from "@medisync/proto/medisync/dispensing/v1/dispensing_pb";

vi.mock("../transport.ts", () => ({ transport: Symbol("transport") }));
vi.mock("@connectrpc/connect-web", () => ({ createConnectTransport: () => Symbol("staff-transport") }));
const logout = vi.fn();
vi.mock("../auth.tsx", () => ({ useAuth: () => ({ state: { kiosk: { id: "k1", code: "00010001", displayName: "Demo Cabinet", projectId: "project-1" }, token: "kiosk-jwt" }, logout }) }));

const { listSlots, cardLogin, prepareDispense, confirmDispense, cancelDispense, getDispenseTransaction, listDispenseTransactions, listEmergencyDrugs, emergencyDispense, getEmergencyDispenseTransaction } = vi.hoisted(() => ({
  listSlots: vi.fn(), cardLogin: vi.fn(), prepareDispense: vi.fn(), confirmDispense: vi.fn(), cancelDispense: vi.fn(), getDispenseTransaction: vi.fn(), listDispenseTransactions: vi.fn(), listEmergencyDrugs: vi.fn(), emergencyDispense: vi.fn(), getEmergencyDispenseTransaction: vi.fn(),
}));
vi.mock("@connectrpc/connect", async (original) => {
  const actual = await original<typeof import("@connectrpc/connect")>();
  return { ...actual, createClient: () => ({ listSlots, cardLogin, prepareDispense, confirmDispense, cancelDispense, getDispenseTransaction, listDispenseTransactions, listEmergencyDrugs, emergencyDispense, getEmergencyDispenseTransaction }) };
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
const transaction = {
  dispenseId: "dispense-1", prescriptionId: "DEMO-RX-001", kioskCode: "00010001",
  operatorUserId: "", operatorDisplayName: "", status: DispenseTransactionStatus.AWAITING_IDENTITY,
  traceId: "trace-1", failureCode: "", failureDetail: "",
  items: [{ id: "item-1", sequenceNo: 1, drugCode: "PARA", drugName: "Paracetamol", requestedQuantity: 2, allocatedQuantity: 2, dispensedQuantity: 0, status: "RESERVED", allocations: [] }],
};
const transactionB = { ...transaction, dispenseId: "dispense-2", prescriptionId: "DEMO-RX-002", traceId: "trace-2" };
const emergencyTransaction = {
  dispenseId: "emergency-1", kioskCode: "00010001", projectId: "project-1",
  hn: "HN0099", employeeCode: "EMP009", operatorUserId: "u9", operatorDisplayName: "Nurse Emergency",
  slotCode: "S01", drugCode: "PARA", drugName: "Paracetamol", requestedQuantity: 1,
  dispensedQuantity: 0, status: EmergencyDispenseStatus.QUEUED, reason: "", failureCode: "",
  failureDetail: "", traceId: "emergency-trace-1",
  operatorAuthMethod: EmergencyOperatorAuthMethod.EMPLOYEE_CODE,
};

class FakeEventSource {
  static instances: FakeEventSource[] = [];
  readonly url: string;
  onmessage: ((event: MessageEvent<string>) => void) | null = null;

  constructor(url: string | URL) {
    this.url = String(url);
    FakeEventSource.instances.push(this);
  }

  emit(value: unknown) {
    this.onmessage?.({ data: JSON.stringify(value) } as MessageEvent<string>);
  }

  close() {}
}

async function scanRequest(user: ReturnType<typeof userEvent.setup>) {
  await user.keyboard("DEMO-RX-001{Enter}");
  await screen.findByRole("dialog", { name: "ตรวจสอบรายการยาที่จะเบิก" });
}

describe("sticker-driven withdrawal", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    localStorage.clear();
    listSlots.mockResolvedValue({ slots: [slot] });
    listDispenseTransactions.mockResolvedValue({ transactions: [], totalCount: 0n });
    listEmergencyDrugs.mockResolvedValue({ drugs: [{ slotCode: "S01", drugCode: "PARA", drugName: "Paracetamol", drugType: "tablet", quantity: 10, maxDispense: 2 }], totalCount: 1n });
    emergencyDispense.mockResolvedValue({ transaction: emergencyTransaction });
    prepareDispense.mockResolvedValue({ prescription, transaction });
    cancelDispense.mockResolvedValue({ transaction: { ...transaction, status: DispenseTransactionStatus.CANCELLED } });
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    FakeEventSource.instances = [];
  });

  it("accepts a tester scan in a kiosk opened independently from the tester", async () => {
    vi.stubGlobal("EventSource", FakeEventSource);
    render(<WithdrawFlow />);

    expect(FakeEventSource.instances).toHaveLength(1);
    expect(FakeEventSource.instances[0].url).toBe("http://localhost:8899/api/kiosk-events?kioskCode=00010001");
    act(() => FakeEventSource.instances[0].emit({ id: 7, kioskCode: "00010001", type: "scan_sticker", code: "DEMO-RX-001" }));

    expect(await screen.findByRole("dialog", { name: "ตรวจสอบรายการยาที่จะเบิก" })).toBeDefined();
    expect(prepareDispense).toHaveBeenCalledTimes(1);
  });

  it("starts with the sticker scanner and validates against backend data", async () => {
    const user = userEvent.setup(); render(<WithdrawFlow />);
    expect(screen.getByRole("heading", { name: "สแกน Sticker เบิกยา" })).toBeDefined();
    await scanRequest(user);
    expect(screen.getAllByText("DEMO-RX-001").length).toBeGreaterThan(0);
    expect(screen.getByText(/ชั้น 1 · ช่อง 1/)).toBeDefined();
    expect(screen.getByText("คงเหลือ 10")).toBeDefined();
    expect(screen.getByText("พร้อมจ่าย")).toBeDefined();
    expect(prepareDispense).toHaveBeenCalledWith(expect.objectContaining({ stickerCode: "DEMO-RX-001" }));
  });

  it("rejects an unknown and already-dispensed sticker", async () => {
    const user = userEvent.setup(); prepareDispense.mockRejectedValueOnce(new ConnectError("not found", Code.NotFound));
    render(<WithdrawFlow />); await user.keyboard("UNKNOWN{Enter}");
    expect(await screen.findByText(/ไม่พบรายการ READY/)).toBeDefined();
  });

  it("allows manual employee code for an emergency without a prescription sticker", async () => {
    const user = userEvent.setup();
    render(<WithdrawFlow />);

    await user.click(screen.getByRole("button", { name: "เปิดเบิกยาฉุกเฉิน" }));
    expect(await screen.findByRole("dialog", { name: "เบิกยาฉุกเฉิน" })).toBeDefined();
    expect(screen.getByText("ใช้เฉพาะการเบิกฉุกเฉินที่ไม่มี Prescription")).toBeDefined();
    await user.click(screen.getByRole("tab", { name: "กรอกรหัสพนักงาน" }));
    expect(listEmergencyDrugs).toHaveBeenCalledWith(expect.objectContaining({ kioskCode: "00010001" }));
    const submit = screen.getByRole("button", { name: "ยืนยันและเริ่มจ่ายยาฉุกเฉิน" });
    expect(submit).toHaveProperty("disabled", true);

    await user.type(screen.getByLabelText(/HN ผู้ป่วย/), "HN0099");
    await user.type(screen.getByLabelText(/รหัสพนักงานผู้เบิก/), "emp009");
    expect(submit).toHaveProperty("disabled", false);
    await user.click(submit);

    await waitFor(() => expect(emergencyDispense).toHaveBeenCalledWith(expect.objectContaining({
      kioskCode: "00010001", hn: "HN0099", employeeCode: "EMP009",
      slotCode: "S01", drugCode: "PARA", quantity: 1,
    })));
    expect(screen.getByText("รายการอยู่ในคิวตู้ยา")).toBeDefined();
    expect(cardLogin).not.toHaveBeenCalled();
  });

  it("verifies a staff card normally and records CARD authentication", async () => {
    cardLogin.mockResolvedValueOnce({
      accessToken: "emergency-staff-jwt",
      employeeCode: "EMP009",
      user: { id: "u9", displayName: "Nurse Emergency", active: true, role: 3, wardIds: ["WARD-1"], projectId: "project-1" },
    });
    emergencyDispense.mockResolvedValueOnce({ transaction: { ...emergencyTransaction, operatorAuthMethod: EmergencyOperatorAuthMethod.CARD } });
    const user = userEvent.setup();
    render(<WithdrawFlow />);

    await user.click(screen.getByRole("button", { name: "เปิดเบิกยาฉุกเฉิน" }));
    await user.type(screen.getByLabelText(/HN ผู้ป่วย/), "HN0099");
    const cardInput = screen.getByLabelText("สแกนบัตรเจ้าหน้าที่");
    await user.click(cardInput);
    await user.type(cardInput, "CARD-009");
    await user.keyboard("{Enter}");

    expect(await screen.findByText("ยืนยันบัตรแล้ว")).toBeDefined();
    expect(screen.getByText("Nurse Emergency · EMP009")).toBeDefined();
    expect(cardLogin).toHaveBeenCalledWith(expect.objectContaining({ cardToken: "CARD-009", projectId: "project-1" }));
    const submit = screen.getByRole("button", { name: "ยืนยันและเริ่มจ่ายยาฉุกเฉิน" });
    expect(submit).toHaveProperty("disabled", false);
    await user.click(submit);

    await waitFor(() => expect(emergencyDispense).toHaveBeenCalledWith(expect.objectContaining({
      kioskCode: "00010001", hn: "HN0099", employeeCode: "EMP009",
    })));
    expect(screen.getByText("สแกนบัตรเจ้าหน้าที่")).toBeDefined();
  });

  it("routes a kiosktester card scan into the open emergency flow", async () => {
    vi.stubGlobal("EventSource", FakeEventSource);
    cardLogin.mockResolvedValueOnce({
      accessToken: "emergency-staff-jwt",
      employeeCode: "EMP009",
      user: { id: "u9", displayName: "Nurse Emergency", active: true, role: 3, wardIds: ["WARD-1"], projectId: "project-1" },
    });
    const user = userEvent.setup();
    render(<WithdrawFlow />);
    await user.click(screen.getByRole("button", { name: "เปิดเบิกยาฉุกเฉิน" }));
    await user.type(screen.getByLabelText(/HN ผู้ป่วย/), "HN0099");

    const source = FakeEventSource.instances[FakeEventSource.instances.length - 1];
    act(() => source.emit({ id: 9, kioskCode: "00010001", type: "scan_card", cardToken: "CARD-009" }));

    expect(await screen.findByText("ยืนยันบัตรแล้ว")).toBeDefined();
    expect(cardLogin).toHaveBeenCalledWith(expect.objectContaining({ cardToken: "CARD-009", projectId: "project-1" }));
    expect(prepareDispense).not.toHaveBeenCalledWith(expect.objectContaining({ stickerCode: "CARD-009" }));
  });

  it("does not open a cart or request identity when this kiosk has insufficient stock", async () => {
    prepareDispense.mockRejectedValueOnce(new ConnectError("insufficient stock in kiosk 00010001", Code.FailedPrecondition));
    const user = userEvent.setup(); render(<WithdrawFlow />); await user.keyboard("DEMO-RX-001{Enter}");
    expect(await screen.findByText(/ยาในตู้นี้ไม่พอ/)).toBeDefined();
    expect(screen.queryByRole("dialog", { name: "ตรวจสอบรายการยาที่จะเบิก" })).toBeNull();
    expect(cardLogin).not.toHaveBeenCalled();
  });

  it("keeps cabinet slots read-only and highlights requested locations", async () => {
    const user = userEvent.setup(); render(<WithdrawFlow />); await scanRequest(user);
    await user.click(screen.getByRole("button", { name: "กลับไปตรวจสอบ" }));
    expect(screen.getByRole("button", { name: /ชั้น 1 ช่อง 1 Paracetamol/ }).className).toContain("is-requested");
    await user.click(screen.getByRole("button", { name: /ชั้น 1 ช่อง 1 Paracetamol/ }));
    const detailSlot = screen.getByRole("button", { name: /S1-R1 · Paracetamol/ });
    expect(detailSlot).toHaveProperty("disabled", false);
    expect(detailSlot.getAttribute("aria-pressed")).toBeNull();
    expect(screen.getByRole("dialog", { name: /รายละเอียดช่องยา S01/ })).toBeDefined();
    expect(screen.queryByText(/รายการตำแหน่งที่เลือก/)).toBeNull();
  });

  it("requires staff identity and blocks an unauthorized ward", async () => {
    const user = userEvent.setup(); render(<WithdrawFlow />); await scanRequest(user);
    await user.click(screen.getByRole("button", { name: "ไปสแกนบัตรยืนยัน" }));
    cardLogin.mockResolvedValue({ accessToken: "staff-jwt", user: { id: "u1", displayName: "เจ้าหน้าที่ ก", active: true, role: 3, wardIds: ["OTHER"], projectId: "project-1" } });
    await user.type(screen.getByLabelText("รหัสบัตรเจ้าหน้าที่"), "card-token");
    await user.click(screen.getByRole("button", { name: "ยืนยันตัวตนและส่งเข้าคิว" }));
    expect(await screen.findByText(/ไม่มีสิทธิ์เบิกรายการ/)).toBeDefined();
    expect(confirmDispense).not.toHaveBeenCalled();
  });

  it("submits immediately after authorization and keeps queue counts out of the footer", async () => {
    const user = userEvent.setup(); render(<WithdrawFlow />); await scanRequest(user);
    await user.click(screen.getByRole("button", { name: "ไปสแกนบัตรยืนยัน" }));
    cardLogin.mockResolvedValue({ accessToken: "staff-jwt", user: { id: "u1", displayName: "เจ้าหน้าที่ ก", active: true, role: 3, wardIds: ["WARD-1"], projectId: "project-1" } });
    confirmDispense.mockResolvedValue({ transaction: { ...transaction, operatorDisplayName: "เจ้าหน้าที่ ก", status: DispenseTransactionStatus.QUEUED } });
    getDispenseTransaction.mockResolvedValue({ transaction: { ...transaction, operatorDisplayName: "เจ้าหน้าที่ ก", status: DispenseTransactionStatus.DISPENSING } });
    await user.type(screen.getByLabelText("รหัสบัตรเจ้าหน้าที่"), "card-token");
    await user.click(screen.getByRole("button", { name: "ยืนยันตัวตนและส่งเข้าคิว" }));
    expect(await screen.findByRole("heading", { name: "สแกน Sticker เบิกยา" })).toBeDefined();
    expect(screen.getByText(/ส่งรายการ DEMO-RX-001 เข้าคิวตู้ 00010001 สำเร็จ/)).toBeDefined();
    expect(screen.queryByRole("button", { name: "เปิดรายละเอียดคิวเครื่อง" })).toBeNull();
    expect(screen.queryByText(/กำลังเบิก/)).toBeNull();
    expect(confirmDispense).toHaveBeenCalledWith(expect.objectContaining({ dispenseId: "dispense-1" }));
    await user.keyboard("DEMO-RX-001{Enter}");
    expect(await screen.findByText(/รายการนี้ถูกส่งเข้าคิวแล้ว/)).toBeDefined();
    expect(confirmDispense).toHaveBeenCalledTimes(1);
  });

  it("queues separate stickers with a separate identity authorization for each request", async () => {
    const user = userEvent.setup();
    prepareDispense.mockImplementation(({ stickerCode }: { stickerCode: string }) => Promise.resolve(stickerCode === "DEMO-RX-002" ? { prescription: prescriptionB, transaction: transactionB } : { prescription, transaction }));
    cardLogin
      .mockResolvedValueOnce({ accessToken: "staff-a", user: { id: "u1", displayName: "เจ้าหน้าที่ ก", active: true, role: 1, wardIds: [], projectId: "project-1" } })
      .mockResolvedValueOnce({ accessToken: "staff-b", user: { id: "u2", displayName: "เจ้าหน้าที่ ข", active: true, role: 1, wardIds: [], projectId: "project-1" } });
    confirmDispense.mockImplementation(({ dispenseId }: { dispenseId: string }) => Promise.resolve({ transaction: { ...(dispenseId === "dispense-2" ? transactionB : transaction), status: DispenseTransactionStatus.QUEUED } }));
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

    await waitFor(() => expect(confirmDispense).toHaveBeenCalledTimes(2));
    expect(cardLogin).toHaveBeenCalledTimes(2);
    expect(screen.queryByRole("button", { name: "เปิดรายละเอียดคิวเครื่อง" })).toBeNull();
  });

  it("recovers an accepted transaction and its verified operator after refresh", async () => {
    localStorage.setItem("medisync.active-withdrawals.v1", JSON.stringify([{ id: "dispense-1", requestId: "DEMO-RX-001", operator: "เจ้าหน้าที่ ก", acceptedAt: 1234 }]));
    getDispenseTransaction.mockResolvedValue({ transaction: { ...transaction, operatorDisplayName: "เจ้าหน้าที่ ก", status: DispenseTransactionStatus.DISPENSING } });
    render(<WithdrawFlow />);
    await waitFor(() => expect(getDispenseTransaction).toHaveBeenCalledWith(expect.objectContaining({ dispenseId: "dispense-1" })));
    expect(screen.queryByRole("button", { name: "เปิดรายละเอียดคิวเครื่อง" })).toBeNull();
  });

  it("does not keep the queue button at one after a recovered transaction is already complete", async () => {
    localStorage.setItem("medisync.active-withdrawals.v1", JSON.stringify([{ id: "dispense-1", requestId: "DEMO-RX-001", operator: "เจ้าหน้าที่ ก", acceptedAt: 1234 }]));
    getDispenseTransaction.mockResolvedValue({ transaction: { ...transaction, operatorDisplayName: "เจ้าหน้าที่ ก", status: DispenseTransactionStatus.DISPENSED } });

    render(<WithdrawFlow />);

    await waitFor(() => expect(localStorage.getItem("medisync.active-withdrawals.v1")).toBe("[]"));
    expect(screen.queryByRole("button", { name: /สถานะคิว/ })).toBeNull();
  });

  it("only reports completion after backend hardware-confirmed state", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime }); render(<WithdrawFlow />); await scanRequest(user);
    await user.click(screen.getByRole("button", { name: "ไปสแกนบัตรยืนยัน" }));
    cardLogin.mockResolvedValue({ accessToken: "staff-jwt", user: { id: "u1", displayName: "เจ้าหน้าที่ ก", active: true, role: 1, wardIds: [], projectId: "project-1" } });
    confirmDispense.mockResolvedValue({ transaction: { ...transaction, status: DispenseTransactionStatus.QUEUED } });
    getDispenseTransaction.mockResolvedValue({ transaction: { ...transaction, status: DispenseTransactionStatus.DISPENSED } });
    await user.type(screen.getByLabelText("รหัสบัตรเจ้าหน้าที่"), "card-token"); await user.click(screen.getByRole("button", { name: "ยืนยันตัวตนและส่งเข้าคิว" }));
    await act(async () => { await vi.advanceTimersByTimeAsync(2100); });
    await waitFor(() => expect(getDispenseTransaction).toHaveBeenCalledWith(expect.objectContaining({ dispenseId: "dispense-1" })));
    expect(screen.queryByRole("button", { name: "เปิดรายละเอียดคิวเครื่อง" })).toBeNull();
    vi.useRealTimers();
  });
});
