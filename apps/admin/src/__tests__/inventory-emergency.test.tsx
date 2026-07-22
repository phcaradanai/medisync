import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";

const { listSlots, updateSlotEmergencyConfig } = vi.hoisted(() => ({
  listSlots: vi.fn(),
  updateSlotEmergencyConfig: vi.fn(),
}));

vi.mock("../api/client", () => ({
  inventoryClient: {
    listSlots,
    updateSlotEmergencyConfig,
    createSlot: vi.fn(), assignDrug: vi.fn(), refill: vi.fn(), adjustStock: vi.fn(),
  },
  catalogClient: { listDrugs: vi.fn().mockResolvedValue({ drugs: [] }) },
  kioskClient: { listKiosks: vi.fn().mockResolvedValue({ kiosks: [{ id: "k1", code: "00010001", displayName: "Cabinet 1", active: true }] }) },
  projectClient: { listProjects: vi.fn().mockResolvedValue({ projects: [{ id: "project-1", name: "Project 1", active: true }] }) },
}));

import { InventoryPage } from "../features/inventory/InventoryPage";

const slot = {
  id: "row-id-may-change", cabinetId: "00010001", code: "S01", displayName: "Slot 1",
  drugId: "drug-1", drugCode: "PARA500", drugName: "Paracetamol", capacity: 100,
  quantity: 80, lowThreshold: 10, shelf: 1, rowNum: 1, projectId: "project-1",
  emergencyDrug: false, emergencyMaxQuantity: 1,
};

describe("Inventory emergency configuration", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    listSlots.mockResolvedValue({ slots: [slot] });
    updateSlotEmergencyConfig.mockResolvedValue({ slot: { ...slot, emergencyDrug: true, emergencyMaxQuantity: 3 } });
  });

  it("persists configuration by kiosk code and slot code", async () => {
    render(<MemoryRouter><InventoryPage /></MemoryRouter>);
    await screen.findByText("PARA500");
    await userEvent.click(screen.getByRole("button", { name: "ปิด" }));
    await userEvent.click(screen.getByRole("checkbox", { name: /อนุญาตให้เบิกยานี้/ }));
    const quantity = screen.getByRole("spinbutton", { name: "จำนวนสูงสุดต่อ transaction" });
    await userEvent.clear(quantity);
    await userEvent.type(quantity, "3");
    await userEvent.click(screen.getByRole("button", { name: "Save Emergency Config" }));

    await waitFor(() => expect(updateSlotEmergencyConfig).toHaveBeenCalledWith(expect.objectContaining({
      kioskCode: "00010001", slotCode: "S01", enabled: true, maxQuantity: 3,
    })));
  });
});
