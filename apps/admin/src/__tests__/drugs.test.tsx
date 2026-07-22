import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";

const { listDrugs, createDrug, updateDrug, deactivateDrug } = vi.hoisted(() => ({
  listDrugs: vi.fn(),
  createDrug: vi.fn(),
  updateDrug: vi.fn(),
  deactivateDrug: vi.fn(),
}));

vi.mock("../api/client", () => ({
  catalogClient: { listDrugs, createDrug, updateDrug, deactivateDrug },
  projectClient: { listProjects: vi.fn().mockResolvedValue({ projects: [] }) },
}));

vi.mock("../auth/AuthContext", () => ({
  useAuth: () => ({ user: { displayName: "Admin Demo", username: "admin" } }),
}));

import { DrugsPage } from "../features/drugs/DrugsPage";

function renderPage() {
  return render(<MemoryRouter><DrugsPage /></MemoryRouter>);
}

const drug = {
  id: "d1",
  code: "PARA500",
  name: "Paracetamol 500 mg",
  displayName: "พาราเซตามอล 500 มก.",
  genericName: "Acetaminophen",
  form: "Tablet",
  strength: "500 mg",
  unit: "เม็ด",
  barcode: "8850000123456",
  stickerNote: "",
  projectId: "project-1",
  active: true,
  defaultSlotCapacity: 100,
  category: "ยาแก้ปวด",
  manufacturer: "Demo Pharma",
  safetyClassification: "NORMAL",
};

function openCreateDrawer() {
  return userEvent.click(screen.getAllByRole("button", { name: /เพิ่มยา/ })[0]);
}

describe("DrugsPage default slot capacity", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    listDrugs.mockResolvedValue({ drugs: [drug] });
    createDrug.mockResolvedValue({ drug });
    updateDrug.mockResolvedValue({ drug });
    deactivateDrug.mockResolvedValue({ drug: { ...drug, active: false } });
  });

  it("shows the persisted per-slot capacity in the drug table", async () => {
    renderPage();
    expect(await screen.findByText("PARA500")).toBeDefined();
    expect(screen.getByRole("columnheader", { name: "ความจุ/ช่อง" })).toBeDefined();
    expect(screen.getByText("100")).toBeDefined();
  });

  it("uses 100 as the initial capacity for a new drug", async () => {
    renderPage();
    await screen.findByText("PARA500");
    await openCreateDrawer();
    expect(screen.getByRole("spinbutton")).toHaveProperty("value", "100");
    expect(screen.getByText(/ใช้เป็นค่าเริ่มต้นเมื่อผูกยานี้กับช่องใหม่/)).toBeDefined();
  });

  it("rejects a non-positive capacity", async () => {
    renderPage();
    await screen.findByText("PARA500");
    await userEvent.click(screen.getByTitle("แก้ไข"));
    fireEvent.change(screen.getByRole("spinbutton"), { target: { value: "0" } });
    fireEvent.submit(screen.getByRole("button", { name: "บันทึกข้อมูล" }).closest("form")!);
    expect(await screen.findByText("ความจุมาตรฐานต่อช่องต้องเป็นจำนวนเต็มมากกว่า 0")).toBeDefined();
    expect(updateDrug).not.toHaveBeenCalled();
  });

  it("sends the capacity when creating a drug", async () => {
    renderPage();
    await screen.findByText("PARA500");
    await openCreateDrawer();

    await userEvent.type(screen.getByPlaceholderText("PARA500"), "AMOX500");
    await userEvent.type(screen.getByPlaceholderText("Paracetamol 500 mg"), "Amoxicillin 500 mg");
    await userEvent.type(screen.getByPlaceholderText("พาราเซตามอล 500 มก."), "อะม็อกซีซิลลิน 500 มก.");
    fireEvent.change(screen.getByRole("spinbutton"), { target: { value: "240" } });
    await userEvent.click(screen.getByRole("button", { name: "บันทึกข้อมูล" }));

    await waitFor(() => expect(createDrug).toHaveBeenCalled());
    expect(createDrug.mock.calls[0][0].defaultSlotCapacity).toBe(240);
  });

  it("preserves and updates capacity when editing", async () => {
    renderPage();
    await screen.findByText("PARA500");
    await userEvent.click(screen.getByTitle("แก้ไข"));
    const capacity = screen.getByRole("spinbutton");
    expect(capacity).toHaveProperty("value", "100");
    fireEvent.change(capacity, { target: { value: "150" } });
    await userEvent.click(screen.getByRole("button", { name: "บันทึกข้อมูล" }));

    await waitFor(() => expect(updateDrug).toHaveBeenCalled());
    expect(updateDrug.mock.calls[0][0].drug.defaultSlotCapacity).toBe(150);
    expect((await screen.findByRole("status")).textContent).toContain("บันทึกข้อมูลยาเรียบร้อยแล้ว");
  });

  it("persists category, manufacturer, and LASA classification", async () => {
    renderPage();
    await screen.findByText("PARA500");
    await userEvent.click(screen.getByTitle("แก้ไข"));

    const category = screen.getByPlaceholderText("เช่น ยาแก้ปวด, ยาปฏิชีวนะ");
    await userEvent.clear(category);
    await userEvent.type(category, "ยาฉีด");
    const manufacturer = screen.getByPlaceholderText("ชื่อบริษัทผู้ผลิต");
    await userEvent.clear(manufacturer);
    await userEvent.type(manufacturer, "MediSync Pharma");
    await userEvent.selectOptions(screen.getByRole("combobox", { name: "ประเภทความเสี่ยง" }), "LASA");
    await userEvent.click(screen.getByRole("button", { name: "บันทึกข้อมูล" }));

    await waitFor(() => expect(updateDrug).toHaveBeenCalled());
    const saved = updateDrug.mock.calls[0][0].drug;
    expect(saved.category).toBe("ยาฉีด");
    expect(saved.manufacturer).toBe("MediSync Pharma");
    expect(saved.safetyClassification).toBe("LASA");
  });
});
