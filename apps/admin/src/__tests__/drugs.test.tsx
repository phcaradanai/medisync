import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { fireEvent } from "@testing-library/react";

// ── Mock the Connect-RPC client module ───────────────────────────

const mockListDrugs = vi.fn();
const mockCreateDrug = vi.fn();
const mockUpdateDrug = vi.fn();
const mockDeactivateDrug = vi.fn();

vi.mock("../api/client", () => ({
  identityClient: {
    login: vi.fn(),
    whoAmI: vi.fn(),
    cardLogin: vi.fn(),
  },
  catalogClient: {
    listDrugs: (...args: unknown[]) => mockListDrugs(...args),
    createDrug: (...args: unknown[]) => mockCreateDrug(...args),
    updateDrug: (...args: unknown[]) => mockUpdateDrug(...args),
    deactivateDrug: (...args: unknown[]) => mockDeactivateDrug(...args),
  },
  setTokenProvider: vi.fn(),
  resetTransport: vi.fn(),
}));

// ── Import the component after mocks are set up ─────────────────

import { DrugsPage } from "../pages/DrugsPage";

// ── Test fixtures ───────────────────────────────────────────────

function makeDrug(overrides: Record<string, unknown> = {}) {
  return {
    id: overrides.id as string ?? "d1",
    code: overrides.code as string ?? "PARA500",
    name: overrides.name as string ?? "Paracetamol 500mg",
    genericName: (overrides.genericName as string) ?? "Acetaminophen",
    form: (overrides.form as string) ?? "tablet",
    strength: (overrides.strength as string) ?? "500 mg",
    unit: (overrides.unit as string) ?? "tab",
    stickerNote: (overrides.stickerNote as string) ?? "",
    active: (overrides.active as boolean) ?? true,
    createdAt: undefined,
    updatedAt: undefined,
  };
}

const activeDrug = makeDrug({ id: "d1", code: "PARA500", name: "Paracetamol", active: true });
const inactiveDrug = makeDrug({ id: "d2", code: "IBU400", name: "Ibuprofen", active: false });

// ── Helpers ──────────────────────────────────────────────────────

function renderDrugsPage() {
  return render(<DrugsPage />);
}

// ── Tests ────────────────────────────────────────────────────────

describe("DrugsPage", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockListDrugs.mockResolvedValue({ drugs: [activeDrug, inactiveDrug] });
    mockCreateDrug.mockResolvedValue({ drug: activeDrug });
    mockUpdateDrug.mockResolvedValue({ drug: activeDrug });
    mockDeactivateDrug.mockResolvedValue({ drug: inactiveDrug });
  });

  // ── List rendering ──────────────────────────────────────────

  it("renders the drug list from mocked ListDrugs response", async () => {
    renderDrugsPage();

    await waitFor(() => {
      expect(screen.getByText("PARA500")).toBeDefined();
    });

    expect(screen.getByText("Paracetamol")).toBeDefined();
    expect(screen.getByText("Ibuprofen")).toBeDefined();
    expect(screen.getByText("IBU400")).toBeDefined();

    // Active badge
    expect(screen.getByText("Active")).toBeDefined();
    // Inactive badge
    expect(screen.getByText("Inactive")).toBeDefined();

    expect(mockListDrugs).toHaveBeenCalledWith(
      expect.objectContaining({ query: "", includeInactive: true, pageSize: 100 }),
    );
  });

  it("shows empty state when no drugs returned", async () => {
    mockListDrugs.mockResolvedValueOnce({ drugs: [] });
    renderDrugsPage();

    await waitFor(() => {
      expect(
        screen.getByText("No drugs in the catalog yet. Click + Add Drug to create one."),
      ).toBeDefined();
    });
  });

  it("shows loading indicator initially", () => {
    mockListDrugs.mockReturnValueOnce(new Promise(() => {})); // never resolves
    renderDrugsPage();
    expect(screen.getByText("Loading…")).toBeDefined();
  });

  // ── Form validation ─────────────────────────────────────────

  it("opens create-drug modal on + Add Drug click", async () => {
    renderDrugsPage();
    await waitFor(() => {
      expect(screen.getByText("PARA500")).toBeDefined();
    });

    await userEvent.click(screen.getByRole("button", { name: "+ Add Drug" }));

    await waitFor(() => {
      expect(screen.getByText("Add Drug")).toBeDefined();
    });
    expect(screen.getByPlaceholderText("e.g. PARA500")).toBeDefined();
    expect(screen.getByPlaceholderText("e.g. Paracetamol 500mg")).toBeDefined();
  });

  it("rejects form submission with missing code", async () => {
    renderDrugsPage();
    await waitFor(() => {
      expect(screen.getByText("PARA500")).toBeDefined();
    });

    await userEvent.click(screen.getByRole("button", { name: "+ Add Drug" }));

    await waitFor(() => {
      expect(screen.getByText("Add Drug")).toBeDefined();
    });

    // Fill name but leave code empty, then submit form via fireEvent
    await userEvent.type(screen.getByPlaceholderText("e.g. Paracetamol 500mg"), "Test Drug");

    const forms = document.querySelectorAll("form");
    const modalForm = forms[1];  // second form is the modal
    fireEvent.submit(modalForm);

    await waitFor(() => {
      expect(mockCreateDrug).not.toHaveBeenCalled();
    });

    // Error should be shown — use queryByText which is synchronous after waitFor
    expect(screen.getByText("Drug code is required")).toBeDefined();
  });

  it("rejects form submission with missing name", async () => {
    renderDrugsPage();
    await waitFor(() => {
      expect(screen.getByText("PARA500")).toBeDefined();
    });

    await userEvent.click(screen.getByRole("button", { name: "+ Add Drug" }));

    await waitFor(() => {
      expect(screen.getByText("Add Drug")).toBeDefined();
    });

    // Fill code but leave name empty, then submit form via fireEvent
    await userEvent.type(screen.getByPlaceholderText("e.g. PARA500"), "TST001");

    const forms = document.querySelectorAll("form");
    const modalForm = forms[1];  // second form is the modal
    fireEvent.submit(modalForm);

    await waitFor(() => {
      expect(mockCreateDrug).not.toHaveBeenCalled();
    });

    // Error should be shown
    expect(screen.getByText("Drug name is required")).toBeDefined();
  });

  it("creates a drug when required fields are filled", async () => {
    renderDrugsPage();
    await waitFor(() => {
      expect(screen.getByText("PARA500")).toBeDefined();
    });

    await userEvent.click(screen.getByRole("button", { name: "+ Add Drug" }));

    await waitFor(() => {
      expect(screen.getByText("Add Drug")).toBeDefined();
    });

    await userEvent.type(screen.getByPlaceholderText("e.g. PARA500"), "TST001");
    await userEvent.type(screen.getByPlaceholderText("e.g. Paracetamol 500mg"), "Test Drug");
    await userEvent.click(screen.getByRole("button", { name: "Create Drug" }));

    await waitFor(() => {
      expect(mockCreateDrug).toHaveBeenCalledWith(
        expect.objectContaining({
          code: "TST001",
          name: "Test Drug",
        }),
      );
    });
  });

  // ── Deactivate confirmation ─────────────────────────────────

  it("deactivates a drug after confirmation", async () => {
    // Override window.confirm to return true
    const origConfirm = window.confirm;
    window.confirm = vi.fn().mockReturnValue(true);

    renderDrugsPage();
    await waitFor(() => {
      expect(screen.getByText("PARA500")).toBeDefined();
    });

    // Click Deactivate on the active drug (PARA500)
    const deactivateBtns = screen.getAllByTitle("Deactivate");
    expect(deactivateBtns.length).toBe(1); // only active drug shows deactivate

    await userEvent.click(deactivateBtns[0]);

    expect(window.confirm).toHaveBeenCalledWith(
      "Deactivate this drug? It will no longer appear in dispensing lists.",
    );
    expect(mockDeactivateDrug).toHaveBeenCalledWith(
      expect.objectContaining({ id: "d1" }),
    );

    window.confirm = origConfirm;
  });

  it("does not deactivate if confirmation is cancelled", async () => {
    const origConfirm = window.confirm;
    window.confirm = vi.fn().mockReturnValue(false);

    renderDrugsPage();
    await waitFor(() => {
      expect(screen.getByText("PARA500")).toBeDefined();
    });

    const deactivateBtns = screen.getAllByTitle("Deactivate");
    await userEvent.click(deactivateBtns[0]);

    expect(window.confirm).toHaveBeenCalled();
    expect(mockDeactivateDrug).not.toHaveBeenCalled();

    window.confirm = origConfirm;
  });

  it("does not show deactivate button for inactive drugs", async () => {
    renderDrugsPage();
    await waitFor(() => {
      expect(screen.getByText("IBU400")).toBeDefined();
    });

    // Only the active drug (PARA500) should have a Deactivate button
    expect(screen.queryAllByTitle("Deactivate").length).toBe(1);
  });

  // ── CQ4 regression: editing inactive drug sends active:false ─

  it("CQ4 regression: editing an inactive drug sends active:false in UpdateDrugRequest", async () => {
    renderDrugsPage();
    await waitFor(() => {
      expect(screen.getByText("IBU400")).toBeDefined();
    });

    // Click Edit on the inactive drug (IBU400)
    const editBtns = screen.getAllByTitle("Edit");
    // IBU400 is second in the list
    await userEvent.click(editBtns[1]); // second drug = IBU400 (inactive)

    await waitFor(() => {
      expect(screen.getByText("Edit Drug")).toBeDefined();
    });

    // Change the name
    const nameInput = screen.getByPlaceholderText("e.g. Paracetamol 500mg");
    await userEvent.clear(nameInput);
    await userEvent.type(nameInput, "Ibuprofen Updated");

    // Save
    await userEvent.click(screen.getByRole("button", { name: "Save Changes" }));

    await waitFor(() => {
      expect(mockUpdateDrug).toHaveBeenCalled();
    });

    // This is the critical assertion: active must be false, not true
    const updateCall = mockUpdateDrug.mock.calls[0][0];
    expect(updateCall.drug.active).toBe(false);
    expect(updateCall.drug.id).toBe("d2");
    expect(updateCall.drug.name).toBe("Ibuprofen Updated");
  });

  it("CQ4 regression: editing an active drug sends active:true in UpdateDrugRequest", async () => {
    renderDrugsPage();
    await waitFor(() => {
      expect(screen.getByText("PARA500")).toBeDefined();
    });

    // Click Edit on the active drug (PARA500)
    const editBtns = screen.getAllByTitle("Edit");
    await userEvent.click(editBtns[0]); // first drug = PARA500 (active)

    await waitFor(() => {
      expect(screen.getByText("Edit Drug")).toBeDefined();
    });

    await userEvent.click(screen.getByRole("button", { name: "Save Changes" }));

    await waitFor(() => {
      expect(mockUpdateDrug).toHaveBeenCalled();
    });

    const updateCall = mockUpdateDrug.mock.calls[0][0];
    // Active drug should stay active
    expect(updateCall.drug.active).toBe(true);
    expect(updateCall.drug.id).toBe("d1");
  });
});
