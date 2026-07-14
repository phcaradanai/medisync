import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { AuthProvider } from "../auth/AuthContext";
import { LoginPage } from "../components/LoginPage";
import type { ReactNode } from "react";

// ── Mock the Connect-RPC client module ───────────────────────────

const mockLogin = vi.fn();

vi.mock("../api/client", () => ({
  identityClient: {
    login: (...args: unknown[]) => mockLogin(...args),
    whoAmI: vi.fn().mockRejectedValue(new Error("unauthenticated")),
    cardLogin: vi.fn(),
  },
  catalogClient: {
    listDrugs: vi.fn().mockResolvedValue({ drugs: [] }),
    createDrug: vi.fn(),
    updateDrug: vi.fn(),
    deactivateDrug: vi.fn(),
  },
  setTokenProvider: vi.fn(),
  resetTransport: vi.fn(),
}));

// ── Helpers ──────────────────────────────────────────────────────

function Wrapper({ children }: { children: ReactNode }) {
  return <AuthProvider>{children}</AuthProvider>;
}

function renderLogin() {
  return render(<LoginPage />, { wrapper: Wrapper });
}

// ── Tests ────────────────────────────────────────────────────────

describe("admin login flow", () => {
  beforeEach(() => {
    mockLogin.mockReset();
    sessionStorage.clear();
  });

  it("renders the login form", () => {
    renderLogin();
    expect(screen.getByText("MediSync Admin")).toBeDefined();
    expect(screen.getByLabelText("Username")).toBeDefined();
    expect(screen.getByLabelText("Password")).toBeDefined();
    expect(screen.getByRole("button", { name: "Sign In" })).toBeDefined();
  });

  it("shows error for empty username", async () => {
    renderLogin();
    const btn = screen.getByRole("button", { name: "Sign In" });
    await userEvent.click(btn);

    await waitFor(() => {
      expect(screen.getByText("Username is required")).toBeDefined();
    });
  });

  it("shows error for empty password", async () => {
    renderLogin();
    const user = userEvent.setup();
    await user.type(screen.getByLabelText("Username"), "admin");
    await user.click(screen.getByRole("button", { name: "Sign In" }));

    await waitFor(() => {
      expect(screen.getByText("Password is required")).toBeDefined();
    });
  });

  it("calls login with credentials on submit", async () => {
    mockLogin.mockResolvedValueOnce({
      accessToken: "test-token",
      user: { id: "u1", username: "admin", displayName: "Admin" },
      expiresAt: { seconds: BigInt(Date.now() + 3600_000) },
    });

    renderLogin();
    const user = userEvent.setup();
    await user.type(screen.getByLabelText("Username"), "admin");
    await user.type(screen.getByLabelText("Password"), "secret");
    await user.click(screen.getByRole("button", { name: "Sign In" }));

    await waitFor(() => {
      expect(mockLogin).toHaveBeenCalledWith({ username: "admin", password: "secret" });
    });
  });

  it("shows error on failed login (invalid credentials)", async () => {
    mockLogin.mockRejectedValueOnce(new Error("Unauthenticated"));

    renderLogin();
    const user = userEvent.setup();
    await user.type(screen.getByLabelText("Username"), "bad");
    await user.type(screen.getByLabelText("Password"), "wrong");
    await user.click(screen.getByRole("button", { name: "Sign In" }));

    await waitFor(() => {
      // AuthContext catches the error and sets it in state.
      expect(screen.getByText("Unauthenticated")).toBeDefined();
    });
  });
});
