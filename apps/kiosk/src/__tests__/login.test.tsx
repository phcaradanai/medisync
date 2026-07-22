import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";

// ── Mock the transport ─────────────────────────────────────────

vi.mock("../transport.ts", () => ({
  transport: Symbol("mock-transport"),
}));

// ── Mock createClient to return spies ──────────────────────────

const mockKioskLogin = vi.fn();
const mockKioskValidate = vi.fn();

vi.mock("@connectrpc/connect", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@connectrpc/connect")>();
  return {
    ...actual,
    createClient: () => ({
      kioskLogin: (...args: unknown[]) => mockKioskLogin(...args),
      kioskValidate: (...args: unknown[]) => mockKioskValidate(...args),
    }),
  };
});

// ── Now import components ──────────────────────────────────────

import { AuthProvider, clearKioskSessionStorage } from "../auth.tsx";
import LoginScreen from "../LoginScreen.tsx";

function Wrapper({ children }: { children: ReactNode }) {
  return <AuthProvider>{children}</AuthProvider>;
}

// ── Helpers ──────────────────────────────────────────────────────

function makeKioskLoginResponse(overrides: Record<string, unknown> = {}) {
  return {
    accessToken: (overrides.accessToken as string) ?? "test-kiosk-token",
    kiosk: {
      id: (overrides.kioskId as string) ?? "k1",
      code: (overrides.code as string) ?? "KIOSK-WARD3A",
      displayName: (overrides.displayName as string) ?? "ตู้จ่ายยาวอร์ด 3A",
      active: true,
    },
    expiresAt: {
      seconds: BigInt(Math.floor(Date.now() / 1000) + 3600),
    },
  };
}

// ── Tests ────────────────────────────────────────────────────────

describe("kiosk login flow (code + PIN)", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    localStorage.clear();
    // kioskValidate is called on mount for session restore — reject to simulate fresh session
    mockKioskValidate.mockRejectedValue(new Error("no session"));

    mockKioskLogin.mockResolvedValue(makeKioskLoginResponse());
  });

  it("renders the login screen with kiosk code + PIN fields", async () => {
    render(<LoginScreen />, { wrapper: Wrapper });

    await waitFor(() => {
      expect(screen.getByText("ระบบเบิกจ่ายยา")).toBeDefined();
    });
    expect(screen.getByLabelText("รหัสเครื่อง (Kiosk Code)")).toBeDefined();
    expect(screen.getByLabelText("PIN")).toBeDefined();
    expect(screen.getByRole("button", { name: "เข้าสู่ระบบ" })).toBeDefined();
  });

  it("shows error when code and PIN are empty", async () => {
    render(<LoginScreen />, { wrapper: Wrapper });

    await waitFor(() => {
      expect(screen.getByText("ระบบเบิกจ่ายยา")).toBeDefined();
    });

    await userEvent.click(screen.getByRole("button", { name: "เข้าสู่ระบบ" }));

    await waitFor(() => {
      expect(
        screen.getByText("กรุณากรอกรหัสเครื่องและ PIN"),
      ).toBeDefined();
    });
  });

  it("calls kioskLogin with code and PIN on submit", async () => {
    render(<LoginScreen />, { wrapper: Wrapper });

    await waitFor(() => {
      expect(screen.getByText("ระบบเบิกจ่ายยา")).toBeDefined();
    });

    const user = userEvent.setup();
    await user.type(screen.getByLabelText("รหัสเครื่อง (Kiosk Code)"), "KIOSK-WARD3A");
    await user.type(screen.getByLabelText("PIN"), "123456");
    await user.click(screen.getByRole("button", { name: "เข้าสู่ระบบ" }));

    await waitFor(() => {
      expect(mockKioskLogin).toHaveBeenCalledWith(
        expect.objectContaining({ code: "KIOSK-WARD3A", pin: "123456" }),
      );
    });
  });

  it("shows error on failed kiosk login", async () => {
    mockKioskLogin.mockRejectedValueOnce(new Error("Unauthenticated"));

    render(<LoginScreen />, { wrapper: Wrapper });

    await waitFor(() => {
      expect(screen.getByText("ระบบเบิกจ่ายยา")).toBeDefined();
    });

    const user = userEvent.setup();
    await user.type(screen.getByLabelText("รหัสเครื่อง (Kiosk Code)"), "BAD-KIOSK");
    await user.type(screen.getByLabelText("PIN"), "wrong");
    await user.click(screen.getByRole("button", { name: "เข้าสู่ระบบ" }));

    await waitFor(() => {
      expect(
        screen.getByText("รหัสเครื่องหรือ PIN ไม่ถูกต้อง"),
      ).toBeDefined();
    });
  });

  it("persists session to localStorage on successful login", async () => {
    mockKioskLogin.mockResolvedValueOnce(makeKioskLoginResponse());

    render(<LoginScreen />, { wrapper: Wrapper });

    await waitFor(() => {
      expect(screen.getByText("ระบบเบิกจ่ายยา")).toBeDefined();
    });

    const user = userEvent.setup();
    await user.type(screen.getByLabelText("รหัสเครื่อง (Kiosk Code)"), "KIOSK-WARD3A");
    await user.type(screen.getByLabelText("PIN"), "123456");
    await user.click(screen.getByRole("button", { name: "เข้าสู่ระบบ" }));

    await waitFor(() => {
      expect(localStorage.getItem("medisync_kiosk_token")).toBe("test-kiosk-token");
      expect(localStorage.getItem("medisync_kiosk_expires")).toBeTruthy();
      expect(localStorage.getItem("medisync_kiosk_kiosk")).toBeTruthy();
    });
  });

  it("restores session from localStorage and validates on mount", async () => {
    const expiresAt = new Date(Date.now() + 3600_000).toISOString();
    localStorage.setItem("medisync_kiosk_token", "stored-token");
    localStorage.setItem("medisync_kiosk_expires", expiresAt);
    localStorage.setItem(
      "medisync_kiosk_kiosk",
      JSON.stringify({
        id: "k1",
        code: "KIOSK-WARD3A",
        displayName: "ตู้จ่ายยาวอร์ด 3A",
        active: true,
      }),
    );

    mockKioskValidate.mockResolvedValueOnce({
      kiosk: {
        id: "k1",
        code: "KIOSK-WARD3A",
        displayName: "ตู้จ่ายยาวอร์ด 3A",
        active: true,
      },
    });

    render(<LoginScreen />, { wrapper: Wrapper });

    await waitFor(() => {
      expect(mockKioskValidate).toHaveBeenCalled();
    });
  });

  it("clears session from localStorage on logout", async () => {
    mockKioskLogin.mockResolvedValueOnce(makeKioskLoginResponse());

    render(<LoginScreen />, { wrapper: Wrapper });

    await waitFor(() => {
      expect(screen.getByText("ระบบเบิกจ่ายยา")).toBeDefined();
    });

    const user = userEvent.setup();
    await user.type(screen.getByLabelText("รหัสเครื่อง (Kiosk Code)"), "KIOSK-WARD3A");
    await user.type(screen.getByLabelText("PIN"), "123456");
    await user.click(screen.getByRole("button", { name: "เข้าสู่ระบบ" }));

    await waitFor(() => {
      expect(localStorage.getItem("medisync_kiosk_token")).toBe("test-kiosk-token");
    });
  });

  it("clears expired session on mount", async () => {
    const expiredAt = new Date(Date.now() - 1000).toISOString();
    localStorage.setItem("medisync_kiosk_token", "expired-token");
    localStorage.setItem("medisync_kiosk_expires", expiredAt);
    localStorage.setItem(
      "medisync_kiosk_kiosk",
      JSON.stringify({ id: "k1", code: "OLD", displayName: "Old", active: true }),
    );

    render(<LoginScreen />, { wrapper: Wrapper });

    await waitFor(() => {
      expect(screen.getByLabelText("รหัสเครื่อง (Kiosk Code)")).toBeDefined();
      // Expired session should be cleared
      expect(localStorage.getItem("medisync_kiosk_token")).toBeNull();
    });
  });

  it("clears only kiosk session keys and preserves accepted queue recovery", () => {
    localStorage.setItem("medisync_kiosk_token", "secret");
    localStorage.setItem("medisync.kiosk.current-sticker.v1", "sticker");
    localStorage.setItem("medisync.active-withdrawals.v1", "accepted-queue");
    localStorage.setItem("unrelated-device-setting", "keep");

    clearKioskSessionStorage();

    expect(localStorage.getItem("medisync_kiosk_token")).toBeNull();
    expect(localStorage.getItem("medisync.kiosk.current-sticker.v1")).toBeNull();
    expect(localStorage.getItem("medisync.active-withdrawals.v1")).toBe("accepted-queue");
    expect(localStorage.getItem("unrelated-device-setting")).toBe("keep");
  });
});
