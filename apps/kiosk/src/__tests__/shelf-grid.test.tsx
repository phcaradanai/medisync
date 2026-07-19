import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import ShelfGrid, { getSlotPosition } from "../features/catalog/ShelfGrid";

const slots = [
  { id: "slot-1", code: "S01", drugCode: "PARA", drugName: "Paracetamol 500 mg", quantity: 20, capacity: 30, lowThreshold: 5, category: "ยาแก้ปวด", manufacturer: "Demo Pharma", safetyClassification: "LASA" },
  { id: "slot-2", code: "S02", drugCode: " para ", drugName: "Paracetamol 500 mg", quantity: 12, capacity: 30, lowThreshold: 5 },
  { id: "slot-31", code: "S31", drugCode: "AMOX", drugName: "Amoxicillin 500 mg", quantity: 2, capacity: 30, lowThreshold: 5 },
];

describe("hybrid cabinet navigation", () => {
  it("keeps sequential physical addresses stable", () => {
    expect(getSlotPosition(slots[2])).toEqual({ shelf: 2, row: 9 });
  });

  it("shows all shelves, opens the correct shelf and keeps slots read-only", async () => {
    const user = userEvent.setup();
    render(<ShelfGrid slots={slots} variant="overview" requestedSlotIds={["slot-31"]} />);

    expect(screen.getByLabelText("ผังช่องยาในตู้").className).toContain("cabinet-browser--overview");
    expect(screen.getByLabelText("ภาพรวมตู้ยาครบ 5 ชั้น 110 ช่อง")).toBeDefined();
    expect(screen.getAllByRole("button", { name: /เปิดรายละเอียดชั้น/ })).toHaveLength(5);

    await user.click(screen.getByRole("button", { name: /ชั้น 2 ช่อง 31 Amoxicillin/ }));
    expect(screen.getByLabelText("ผังช่องยาในตู้").className).toContain("cabinet-browser--shelf");
    expect(screen.getByLabelText("รายละเอียดชั้น 2")).toBeDefined();

    const detailSlot = screen.getByRole("button", { name: /S2-R31 · Amoxicillin/ });
    expect(detailSlot).toHaveProperty("disabled", false);
    expect(detailSlot.getAttribute("aria-pressed")).toBeNull();
    const emptySlot = screen.getByRole("button", { name: /S2-R23 · ช่องว่าง/ });
    expect(detailSlot.className).toContain("slot-cell");
    expect(screen.getByRole("dialog", { name: /รายละเอียดช่องยา S31/ })).toBeDefined();
    expect(screen.getByRole("region", { name: "ช่องทั้งหมดของยาชนิดเดียวกัน" })).toBeDefined();
    expect(screen.getByRole("button", { name: /S31 คงเหลือ 2 ชิ้น ช่องที่กำลังแสดง/ })).toBeDefined();
    expect(emptySlot.className).toContain("slot-cell--empty");
    expect(detailSlot.querySelector(".slot-cell__position")?.textContent).toBe("S31");
    expect(emptySlot.querySelector(".slot-cell__position")?.textContent).toBe("S2-R23");

    await user.click(screen.getByRole("button", { name: /ภาพรวมตู้/ }));
    expect(screen.getByRole("button", { name: /ชั้น 2 ช่อง 31 Amoxicillin/ }).className).toContain("is-requested");
  });

  it("keeps the current channel in the carousel and switches between same-drug channels", async () => {
    const user = userEvent.setup();
    render(<ShelfGrid slots={slots} variant="overview" />);

    await user.click(screen.getByRole("button", { name: /ชั้น 1 ช่อง 1 Paracetamol/ }));
    expect(screen.getByText("LASA")).toBeDefined();
    expect(screen.getByText("Demo Pharma")).toBeDefined();
    expect(screen.getByText("2 ช่อง · เลือกเพื่อดูรายละเอียดแต่ละช่อง")).toBeDefined();
    expect(screen.getByRole("button", { name: /S01 คงเหลือ 20 ชิ้น ช่องที่กำลังแสดง/ })).toBeDefined();

    await user.click(screen.getByRole("button", { name: /^S02 คงเหลือ 12 ชิ้น$/ }));
    expect(screen.getByRole("dialog", { name: /รายละเอียดช่องยา S02/ })).toBeDefined();
  });
});
