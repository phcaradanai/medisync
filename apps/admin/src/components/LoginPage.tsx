import { useState, type FormEvent } from "react";
import { useAuth } from "../auth/AuthContext";

export function LoginPage() {
  const { login, loading, error: authError } = useAuth();
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [localError, setLocalError] = useState<string | null>(null);

  const error = localError || authError;

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    setLocalError(null);
    if (!username.trim()) {
      setLocalError("Username is required");
      return;
    }
    if (!password) {
      setLocalError("Password is required");
      return;
    }
    try {
      await login(username.trim(), password);
    } catch {
      // error is already set via auth context
    }
  }

  return (
    <div className="login-page">
      <form className="login-panel" onSubmit={handleSubmit}>
        <h1>MediSync Admin</h1>

        {error && <div className="login-error">{error}</div>}

        <div className="login-field">
          <label htmlFor="login-username">Username</label>
          <input
            id="login-username"
            type="text"
            autoComplete="username"
            value={username}
            onChange={(e) => setUsername(e.target.value)}
            placeholder="Enter username"
            autoFocus
          />
        </div>

        <div className="login-field">
          <label htmlFor="login-password">Password</label>
          <input
            id="login-password"
            type="password"
            autoComplete="current-password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            placeholder="Enter password"
          />
        </div>

        <button
          type="submit"
          className="btn-primary"
          disabled={loading}
          style={{ width: "100%", justifyContent: "center" }}
        >
          {loading ? "Signing in…" : "Sign In"}
        </button>
      </form>
    </div>
  );
}
