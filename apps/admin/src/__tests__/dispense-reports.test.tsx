import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { EmergencyOperatorAuthMethod } from "@medisync/proto/medisync/dispensing/v1/dispensing_pb";

const { listDispenseTransactions, listEmergencyDispenseTransactions } = vi.hoisted(() => ({
  listDispenseTransactions: vi.fn(),
  listEmergencyDispenseTransactions: vi.fn(),
}));

vi.mock("../api/client", () => ({
  dispensingClient: { listDispenseTransactions, listEmergencyDispenseTransactions },
  kioskClient: { listKiosks: vi.fn().mockResolvedValue({ kiosks: [{ id: "k1", code: "00010001", displayName: "Cabinet 1" }] }) },
}));

import { DispenseTransactionsPage } from "../features/reports/DispenseTransactionsPage";

describe("Dispense transaction reports", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    listDispenseTransactions.mockResolvedValue({ transactions: [], totalCount: 0n, nextPageToken: "" });
    listEmergencyDispenseTransactions.mockResolvedValue({ transactions: [], totalCount: 0n, nextPageToken: "" });
  });

  it("sends traceable emergency filters to the report API", async () => {
    const user = userEvent.setup();
    render(<DispenseTransactionsPage />);
    await waitFor(() => expect(listDispenseTransactions).toHaveBeenCalled());

    await user.selectOptions(screen.getAllByRole("combobox")[0], "emergency");
    await user.type(screen.getByPlaceholderText("ค้นหา HN"), "HN0099");
    await user.type(screen.getByPlaceholderText("ค้นหารหัสยา"), "PARA500");
    await user.type(screen.getByPlaceholderText("ค้นหารหัสพนักงาน"), "EMP009");
    await user.selectOptions(screen.getAllByRole("combobox")[3], String(EmergencyOperatorAuthMethod.CARD));
    fireEvent.change(screen.getByLabelText("ตั้งแต่"), { target: { value: "2026-07-01" } });
    fireEvent.change(screen.getByLabelText("ถึง"), { target: { value: "2026-07-21" } });

    await waitFor(() => expect(listEmergencyDispenseTransactions).toHaveBeenCalledWith(expect.objectContaining({
      hn: "HN0099",
      employeeCode: "EMP009",
      drugCode: "PARA500",
      operatorAuthMethods: [EmergencyOperatorAuthMethod.CARD],
      createdFrom: expect.any(Object),
      createdTo: expect.any(Object),
    })));
  });
});
