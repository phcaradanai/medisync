import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";

// ── Mock the transport ─────────────────────────────────────────

vi.mock("../transport.ts", () => ({
  transport: Symbol("mock-transport"),
}));

// ── Mock createClient to return spies ──────────────────────────

const mockLogin = vi.fn();
const mockCardLogin = vi.fn();
const mockWhoAmI = vi.fn();

vi.mock("@connectrpc/connect", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@connectrpc/connect")>();
  return {
    ...actual,
    createClient: () => ({
      login: (...args: unknown[]) => mockLogin(...args),
      cardLogin: (...args: unknown[]) => mockCardLogin(...args),
      whoAmI: (...args: unknown[]) => mockWhoAmI(...args),
    }),
  };
});

// ── Now import components ──────────────────────────────────────

import { AuthProvider } from "../auth.tsx";
import LoginScreen from "../LoginScreen.tsx";

function Wrapper({ children }: { children: ReactNode }) {
  return <AuthProvider>{children}</AuthProvider>;
}

// ── Helpers ──────────────────────────────────────────────────────

function makeLoginResponse(overrides: Record<string, unknown> = {}) {
  return {
    accessToken: (overrides.accessToken as string) ?? "test-token",
    user: {
      id: (overrides.userId as string) ?? "u1",
      username: (overrides.username as string) ?? "nurse1",
      displayName: (overrides.displayName as string) ?? "Nurse Joy",
      role: 3,
      wardIds: ["W01"],
    },
    expiresAt: {
      seconds: BigInt(Math.floor(Date.now() / 1000) + 3600),
    },
  };
}

// ── Tests ────────────────────────────────────────────────────────

describe("kiosk login flow (password path)", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    sessionStorage.clear();
    // whoAmI is called on mount for session restore — reject to simulate fresh session
    mockWhoAmI.mockRejectedValue(new Error("no session"));

    mockLogin.mockResolvedValue(makeLoginResponse());
  });

  it("renders the login screen", async () => {
    render(<LoginScreen />, { wrapper: Wrapper });

    await waitFor(() => {
      expect(screen.getByText("ระบบเบิกจ่ายยา")).toBeDefined();
    });
    expect(screen.getByLabelText("ชื่อผู้ใช้")).toBeDefined();
    expect(screen.getByLabelText("รหัสผ่าน")).toBeDefined();
    expect(screen.getByRole("button", { name: "เข้าสู่ระบบ" })).toBeDefined();
  });

  it("shows error when username and password are empty", async () => {
    render(<LoginScreen />, { wrapper: Wrapper });

    await waitFor(() => {
      expect(screen.getByText("ระบบเบิกจ่ายยา")).toBeDefined();
    });

    await userEvent.click(screen.getByRole("button", { name: "เข้าสู่ระบบ" }));

    await waitFor(() => {
      expect(
        screen.getByText("กรุณากรอกชื่อผู้ใช้และรหัสผ่าน"),
      ).toBeDefined();
    });
  });

  it("calls login with credentials on submit", async () => {
    render(<LoginScreen />, { wrapper: Wrapper });

    await waitFor(() => {
      expect(screen.getByText("ระบบเบิกจ่ายยา")).toBeDefined();
    });

    const user = userEvent.setup();
    await user.type(screen.getByLabelText("ชื่อผู้ใช้"), "nurse1");
    await user.type(screen.getByLabelText("รหัสผ่าน"), "secret");
    await user.click(screen.getByRole("button", { name: "เข้าสู่ระบบ" }));

    await waitFor(() => {
      expect(mockLogin).toHaveBeenCalledWith(
        expect.objectContaining({ username: "nurse1", password: "secret" }),
      );
    });
  });

  it("shows error on failed login", async () => {
    mockLogin.mockRejectedValueOnce(new Error("Unauthenticated"));

    render(<LoginScreen />, { wrapper: Wrapper });

    await waitFor(() => {
      expect(screen.getByText("ระบบเบิกจ่ายยา")).toBeDefined();
    });

    const user = userEvent.setup();
    await user.type(screen.getByLabelText("ชื่อผู้ใช้"), "bad");
    await user.type(screen.getByLabelText("รหัสผ่าน"), "wrong");
    await user.click(screen.getByRole("button", { name: "เข้าสู่ระบบ" }));

    await waitFor(() => {
      // The kiosk login returns Thai error messages for Unauthenticated
      expect(
        screen.getByText("ชื่อผู้ใช้หรือรหัสผ่านไม่ถูกต้อง"),
      ).toBeDefined();
    });
  });

  it("switches to card mode on card button click", async () => {
    render(<LoginScreen />, { wrapper: Wrapper });

    await waitFor(() => {
      expect(screen.getByText("ระบบเบิกจ่ายยา")).toBeDefined();
    });

    const cardBtn = screen.getByRole("button", { name: "🪪 บัตร" });
    await userEvent.click(cardBtn);

    await waitFor(() => {
      expect(screen.getByText("แตะเพื่อสแกนบัตร")).toBeDefined();
    });
  });

  it("can switch back to password mode", async () => {
    render(<LoginScreen />, { wrapper: Wrapper });

    await waitFor(() => {
      expect(screen.getByText("ระบบเบิกจ่ายยา")).toBeDefined();
    });

    // Switch to card mode
    await userEvent.click(screen.getByRole("button", { name: "🪪 บัตร" }));

    await waitFor(() => {
      expect(screen.getByText("แตะเพื่อสแกนบัตร")).toBeDefined();
    });

    // Switch back to password mode
    await userEvent.click(screen.getByRole("button", { name: "🔑 รหัสผ่าน" }));

    await waitFor(() => {
      expect(screen.getByRole("button", { name: "เข้าสู่ระบบ" })).toBeDefined();
    });
  });
});
