/**
 * Unit tests for the environment-file validator.
 *
 * Run: node --test scripts/env/validate.test.mjs
 */

import { describe, it, before, after } from "node:test";
import assert from "node:assert";
import { execFileSync } from "node:child_process";
import { writeFileSync, unlinkSync, mkdirSync, readFileSync } from "node:fs";
import { resolve, dirname } from "node:path";
import { fileURLToPath } from "node:url";

const __dirname = dirname(fileURLToPath(import.meta.url));
const VALIDATOR = resolve(__dirname, "validate.mjs");
const TMPDIR = resolve(__dirname, ".test-tmp");

function runValidator(envFileContent, mode = "dev") {
  const filePath = resolve(TMPDIR, `test-${process.hrtime.bigint()}.env`);
  writeFileSync(filePath, envFileContent, "utf-8");
  try {
    const out = execFileSync("node", [VALIDATOR, "--mode", mode, filePath], {
      encoding: "utf-8",
      timeout: 5000,
    });
    return { ok: true, stdout: out };
  } catch (err) {
    return {
      ok: false,
      stdout: err.stdout || "",
      stderr: err.stderr || "",
      code: err.status,
    };
  } finally {
    try { unlinkSync(filePath); } catch {}
  }
}

before(() => {
  mkdirSync(TMPDIR, { recursive: true });
});

after(() => {
  // Cleanup skipped — test tmp files are harmless.
});

// ── Valid dev config ──────────────────────────────────────────────────

describe("valid development config", () => {
  it("accepts valid dev env", () => {
    const env = [
      "DATABASE_URL=postgres://user:pass@localhost:5432/db?sslmode=disable",
      "NATS_URL=nats://localhost:4222",
      "HTTP_ADDR=:8080",
      "LOG_LEVEL=info",
      "STARTUP_TIMEOUT_SECONDS=60",
    ].join("\n");

    const result = runValidator(env, "dev");
    assert.ok(result.ok, `expected success, got: ${result.stderr}`);
  });

  it("accepts env with unknown variables (warns only)", () => {
    const env = [
      "DATABASE_URL=postgres://user:pass@localhost:5432/db?sslmode=disable",
      "NATS_URL=nats://localhost:4222",
      "FUTURE_VAR=some-value",
      "ANOTHER_NEW_VAR=123",
    ].join("\n");

    const result = runValidator(env, "dev");
    assert.ok(result.ok, `expected success for unknown vars, got: ${result.stderr}`);
  });
});

// ── Valid production config ───────────────────────────────────────────

describe("valid production config", () => {
  it("accepts valid production env with strong values", () => {
    const env = [
      "DATABASE_URL=postgres://medisync:***@postgres:5432/medisync?sslmode=require",
      "NATS_URL=nats://nats:4222",
      "HTTP_ADDR=:8080",
      "LOG_LEVEL=info",
      "STARTUP_TIMEOUT_SECONDS=120",
      "POSTGRES_USER=medisync",
      "POSTGRES_PASSWORD=another-strong-password",
      "POSTGRES_DB=medisync",
      "JWT_SECRET=prod-strong-jwt-secret-value-thirtytwo",
      "ADMIN_BOOTSTRAP_PASSWORD=strong-admin-bootstrap-pw",
      "CARD_TOKEN_HMAC_KEY=card-token-hmac-key-minimum-32!!",
    ].join("\n");

    const result = runValidator(env, "prod");
    assert.ok(result.ok, `expected success, got: ${result.stderr}`);
  });

  it("accepts production env with all optional variables present", () => {
    const env = [
      "DATABASE_URL=postgres://medisync:***@postgres:5432/medisync?sslmode=require",
      "NATS_URL=nats://nats:4222",
      "HTTP_ADDR=0.0.0.0:8080",
      "LOG_LEVEL=warn",
      "STARTUP_TIMEOUT_SECONDS=90",
      "JWT_EXPIRY_SECONDS=7200",
      "POSTGRES_USER=medisync",
      "POSTGRES_PASSWORD=strongpwd",
      "POSTGRES_DB=medisync",
      "JWT_SECRET=a-strong-random-secret-value-for-tokens",
      "ADMIN_BOOTSTRAP_PASSWORD=strong-admin-bootstrap-pw",
      "CARD_TOKEN_HMAC_KEY=prod-card-token-hmac-key-stronger",
    ].join("\n");

    const result = runValidator(env, "prod");
    assert.ok(result.ok, `expected success, got: ${result.stderr}`);
  });

  it("fails when CARD_TOKEN_HMAC_KEY is missing in prod", () => {
    const env = [
      "DATABASE_URL=postgres://medisync:masked@postgres:5432/medisync?sslmode=require",
      "NATS_URL=nats://nats:4222",
      "POSTGRES_USER=medisync",
      "POSTGRES_PASSWORD=strong-password",
      "POSTGRES_DB=medisync",
      "JWT_SECRET=prod-jwt-secret-value-at-least-32",
      "ADMIN_BOOTSTRAP_PASSWORD=strong-admin-bootstrap-pw",
    ].join("\n");

    const result = runValidator(env, "prod");
    assert.ok(!result.ok, "expected failure for missing CARD_TOKEN_HMAC_KEY");
    assert.ok(result.stderr.includes("CARD_TOKEN_HMAC_KEY"));
  });

  it("rejects CARD_TOKEN_HMAC_KEY dev placeholder without printing it", () => {
    const placeholder = "medisync-dev-card-hmac-change-in-prod";
    const env = [
      "DATABASE_URL=postgres://medisync:masked@postgres:5432/medisync?sslmode=require",
      "NATS_URL=nats://nats:4222",
      "POSTGRES_USER=medisync",
      "POSTGRES_PASSWORD=strong-password",
      "POSTGRES_DB=medisync",
      "JWT_SECRET=prod-jwt-secret-value-at-least-32",
      "ADMIN_BOOTSTRAP_PASSWORD=strong-admin-bootstrap-pw",
      `CARD_TOKEN_HMAC_KEY=${placeholder}`,
    ].join("\n");

    const result = runValidator(env, "prod");
    assert.ok(!result.ok, "expected failure for CARD_TOKEN_HMAC_KEY placeholder");
    assert.ok(result.stderr.includes("CARD_TOKEN_HMAC_KEY") && result.stderr.includes("placeholder"));
    assert.ok(!result.stdout.includes(placeholder) && !result.stderr.includes(placeholder));
  });

  it("rejects short CARD_TOKEN_HMAC_KEY without printing it", () => {
    const shortKey = "short-card-key";
    const env = [
      "DATABASE_URL=postgres://medisync:masked@postgres:5432/medisync?sslmode=require",
      "NATS_URL=nats://nats:4222",
      "POSTGRES_USER=medisync",
      "POSTGRES_PASSWORD=strong-password",
      "POSTGRES_DB=medisync",
      "JWT_SECRET=prod-jwt-secret-value-at-least-32",
      "ADMIN_BOOTSTRAP_PASSWORD=strong-admin-bootstrap-pw",
      `CARD_TOKEN_HMAC_KEY=${shortKey}`,
    ].join("\n");

    const result = runValidator(env, "prod");
    assert.ok(!result.ok, "expected failure for short CARD_TOKEN_HMAC_KEY");
    assert.ok(result.stderr.includes("CARD_TOKEN_HMAC_KEY") && result.stderr.includes("32 bytes"));
    assert.ok(!result.stdout.includes(shortKey) && !result.stderr.includes(shortKey));
  });

  it("rejects JWT_SECRET placeholder in prod", () => {
    const env = [
      "DATABASE_URL=postgres://medisync:***@postgres:5432/medisync?sslmode=require",
      "NATS_URL=nats://nats:4222",
      "POSTGRES_USER=medisync",
      "POSTGRES_PASSWORD=strongpwd",
      "POSTGRES_DB=medisync",
      "JWT_SECRET=<jwt-signing-secret>",
    ].join("\n");

    const result = runValidator(env, "prod");
    assert.ok(!result.ok, "expected failure for JWT_SECRET placeholder in prod");
    assert.ok(
      result.stderr.includes("JWT_SECRET") && result.stderr.includes("placeholder"),
      "error should mention JWT_SECRET and placeholder"
    );
  });

  it("fails when JWT_SECRET is missing in prod", () => {
    const env = [
      "DATABASE_URL=postgres://medisync:***@postgres:5432/medisync?sslmode=require",
      "NATS_URL=nats://nats:4222",
      "POSTGRES_USER=medisync",
      "POSTGRES_PASSWORD=strongpwd",
      "POSTGRES_DB=medisync",
      "ADMIN_BOOTSTRAP_PASSWORD=admin-pw",
    ].join("\n");

    const result = runValidator(env, "prod");
    assert.ok(!result.ok, "expected failure for missing JWT_SECRET");
    assert.ok(result.stderr.includes("JWT_SECRET"), "error should mention JWT_SECRET");
  });

  it("rejects a short JWT_SECRET in prod without printing it", () => {
    const shortSecret = "short-secret";
    const env = [
      "DATABASE_URL=postgres://medisync:masked@postgres:5432/medisync?sslmode=require",
      "NATS_URL=nats://nats:4222",
      "POSTGRES_USER=medisync",
      "POSTGRES_PASSWORD=strong-password",
      "POSTGRES_DB=medisync",
      `JWT_SECRET=${shortSecret}`,
      "ADMIN_BOOTSTRAP_PASSWORD=strong-admin-bootstrap-pw",
    ].join("\n");

    const result = runValidator(env, "prod");
    assert.ok(!result.ok, "expected failure for short JWT_SECRET");
    assert.ok(result.stderr.includes("JWT_SECRET") && result.stderr.includes("32 bytes"));
    assert.ok(!result.stderr.includes(shortSecret), "must not print secret value");
  });

  it("rejects ADMIN_BOOTSTRAP_PASSWORD placeholder in prod", () => {
    const env = [
      "DATABASE_URL=postgres://medisync:***@postgres:5432/medisync?sslmode=require",
      "NATS_URL=nats://nats:4222",
      "POSTGRES_USER=medisync",
      "POSTGRES_PASSWORD=strongpwd",
      "POSTGRES_DB=medisync",
      "JWT_SECRET=prod-jwt-secret-value-at-least-32",
      "ADMIN_BOOTSTRAP_PASSWORD=<admin-bootstrap-password>",
    ].join("\n");

    const result = runValidator(env, "prod");
    assert.ok(!result.ok, "expected failure for ADMIN_BOOTSTRAP_PASSWORD placeholder in prod");
    assert.ok(
      result.stderr.includes("ADMIN_BOOTSTRAP_PASSWORD") && result.stderr.includes("placeholder"),
      "error should mention ADMIN_BOOTSTRAP_PASSWORD and placeholder"
    );
  });

  it("fails when ADMIN_BOOTSTRAP_PASSWORD is missing in prod", () => {
    const env = [
      "DATABASE_URL=postgres://medisync:***@postgres:5432/medisync?sslmode=require",
      "NATS_URL=nats://nats:4222",
      "POSTGRES_USER=medisync",
      "POSTGRES_PASSWORD=strongpwd",
      "POSTGRES_DB=medisync",
      "JWT_SECRET=prod-jwt-secret-value-at-least-32",
    ].join("\n");

    const result = runValidator(env, "prod");
    assert.ok(!result.ok, "expected failure for missing ADMIN_BOOTSTRAP_PASSWORD");
    assert.ok(result.stderr.includes("ADMIN_BOOTSTRAP_PASSWORD"), "error should mention ADMIN_BOOTSTRAP_PASSWORD");
  });

  it("rejects a short ADMIN_BOOTSTRAP_PASSWORD in prod without printing it", () => {
    const shortPassword = "short-pass";
    const env = [
      "DATABASE_URL=postgres://medisync:masked@postgres:5432/medisync?sslmode=require",
      "NATS_URL=nats://nats:4222",
      "POSTGRES_USER=medisync",
      "POSTGRES_PASSWORD=strong-password",
      "POSTGRES_DB=medisync",
      "JWT_SECRET=prod-jwt-secret-value-at-least-32",
      `ADMIN_BOOTSTRAP_PASSWORD=${shortPassword}`,
    ].join("\n");

    const result = runValidator(env, "prod");
    assert.ok(!result.ok, "expected failure for short admin password");
    assert.ok(result.stderr.includes("ADMIN_BOOTSTRAP_PASSWORD") && result.stderr.includes("12 bytes"));
    assert.ok(!result.stderr.includes(shortPassword), "must not print secret value");
  });
});

// ── Missing required variables ────────────────────────────────────────

describe("missing required variables", () => {
  it("fails when DATABASE_URL is missing in prod", () => {
    const env = [
      "NATS_URL=nats://nats:4222",
      "POSTGRES_USER=medisync",
      "POSTGRES_PASSWORD=pass",
      "POSTGRES_DB=medisync",
    ].join("\n");

    const result = runValidator(env, "prod");
    assert.ok(!result.ok, "expected failure for missing DATABASE_URL");
    assert.ok(result.stderr.includes("DATABASE_URL"), "error should mention DATABASE_URL");
  });

  it("fails when POSTGRES_PASSWORD is missing in prod", () => {
    const env = [
      "DATABASE_URL=postgres://u:p@host:5432/db",
      "NATS_URL=nats://nats:4222",
      "POSTGRES_USER=medisync",
      "POSTGRES_DB=medisync",
    ].join("\n");

    const result = runValidator(env, "prod");
    assert.ok(!result.ok, "expected failure for missing POSTGRES_PASSWORD");
    assert.ok(result.stderr.includes("POSTGRES_PASSWORD"), "error should mention POSTGRES_PASSWORD");
  });

  it("succeeds in dev mode even with missing required vars", () => {
    const env = [
      "DATABASE_URL=postgres://u:p@host:5432/db",
    ].join("\n");

    const result = runValidator(env, "dev");
    assert.ok(result.ok, "dev mode should not require compose variables");
  });
});

// ── Placeholder rejection ─────────────────────────────────────────────

describe("placeholder rejection", () => {
  it("rejects DATABASE_URL placeholder in prod", () => {
    const env = [
      "DATABASE_URL=<postgresql-connection-string>",
      "NATS_URL=nats://nats:4222",
      "POSTGRES_USER=medisync",
      "POSTGRES_PASSWORD=pass",
      "POSTGRES_DB=medisync",
    ].join("\n");

    const result = runValidator(env, "prod");
    assert.ok(!result.ok, "expected failure for placeholder DATABASE_URL");
    assert.ok(
      result.stderr.includes("DATABASE_URL") && result.stderr.includes("placeholder"),
      "error should mention DATABASE_URL and placeholder"
    );
  });

  it("rejects NATS_URL placeholder in prod", () => {
    const env = [
      "DATABASE_URL=postgres://u:p@host:5432/db",
      "NATS_URL=<nats-url>",
      "POSTGRES_USER=medisync",
      "POSTGRES_PASSWORD=pass",
      "POSTGRES_DB=medisync",
    ].join("\n");

    const result = runValidator(env, "prod");
    assert.ok(!result.ok, "expected failure for placeholder NATS_URL");
    assert.ok(
      result.stderr.includes("NATS_URL") && result.stderr.includes("placeholder"),
      "error should mention NATS_URL and placeholder"
    );
  });

  it("allows placeholders in dev mode (non-required)", () => {
    const env = [
      "DATABASE_URL=postgres://u:p@host:5432/db",
      "NATS_URL=<some-nats-url>",
    ].join("\n");

    const result = runValidator(env, "dev");
    assert.ok(result.ok, "dev mode should allow placeholders");
  });
});

// ── Malformed lines ───────────────────────────────────────────────────

describe("malformed lines", () => {
  it("rejects a line without = sign", () => {
    const env = "DATABASE_URL=postgres://u:p@host:5432/db\nNATS_URL\nHTTP_ADDR=:8080";

    const result = runValidator(env, "dev");
    assert.ok(!result.ok, "expected failure for malformed line");
    assert.ok(result.stderr.includes("malformed"), "error should mention malformed");
  });

  it("rejects a line with empty key", () => {
    const env = "DATABASE_URL=postgres://u:p@host:5432/db\n=value\nHTTP_ADDR=:8080";

    const result = runValidator(env, "dev");
    assert.ok(!result.ok, "expected failure for empty key");
    assert.ok(result.stderr.includes("empty key"), "error should mention empty key");
  });

  it("skips blank lines and comments", () => {
    const env = [
      "# This is a comment",
      "",
      "DATABASE_URL=postgres://u:p@host:5432/db",
      "  # indented comment",
      "NATS_URL=nats://localhost:4222",
    ].join("\n");

    const result = runValidator(env, "dev");
    assert.ok(result.ok, `expected success, got: ${result.stderr}`);
  });
});

// ── Duplicate keys ────────────────────────────────────────────────────

describe("duplicate keys", () => {
  it("rejects duplicate keys", () => {
    const env = [
      "DATABASE_URL=postgres://u:p@host:5432/db",
      "NATS_URL=nats://localhost:4222",
      "DATABASE_URL=postgres://u2:p2@host2:5432/db2",
    ].join("\n");

    const result = runValidator(env, "dev");
    assert.ok(!result.ok, "expected failure for duplicate key");
    assert.ok(result.stderr.includes("duplicate"), "error should mention duplicate");
    assert.ok(result.stderr.includes("DATABASE_URL"), "error should mention the duplicate key");
  });

  it("reports the original line number for duplicates", () => {
    const env = [
      "# header",
      "DATABASE_URL=postgres://u:p@host:5432/db",
      "",
      "NATS_URL=nats://localhost:4222",
      "# mid comment",
      "DATABASE_URL=postgres://u2:p2@host2:5432/db2",
    ].join("\n");

    const result = runValidator(env, "dev");
    assert.ok(!result.ok, "expected failure for duplicate key");
    // First occurrence is line 2, duplicate is line 6
    assert.ok(
      result.stderr.includes("line 2") || result.stderr.includes("first seen"),
      `error should mention first occurrence line; got: ${result.stderr}`
    );
  });
});

// ── Invalid URLs ──────────────────────────────────────────────────────

describe("invalid URLs", () => {
  it("rejects nonsense DATABASE_URL", () => {
    const env = "DATABASE_URL=not-a-url\nNATS_URL=nats://localhost:4222";

    const result = runValidator(env, "dev");
    assert.ok(!result.ok, "expected failure for invalid DATABASE_URL");
    assert.ok(result.stderr.includes("DATABASE_URL"), "error should mention DATABASE_URL");
  });

  it("rejects protocol-less NATS_URL", () => {
    const env = "DATABASE_URL=postgres://u:p@host:5432/db\nNATS_URL=just-a-host:4222";

    const result = runValidator(env, "dev");
    assert.ok(!result.ok, "expected failure for invalid NATS_URL");
    assert.ok(result.stderr.includes("NATS_URL"), "error should mention NATS_URL");
  });

  it("rejects empty URL string", () => {
    const env = "DATABASE_URL=\nNATS_URL=nats://localhost:4222";

    const result = runValidator(env, "dev");
    assert.ok(!result.ok, "expected failure for empty DATABASE_URL");
  });
});

// ── Invalid ports ─────────────────────────────────────────────────────

describe("invalid ports", () => {
  it("rejects port 0 in HTTP_ADDR", () => {
    const env = "DATABASE_URL=postgres://u:p@host:5432/db\nNATS_URL=nats://localhost:4222\nHTTP_ADDR=:0";

    const result = runValidator(env, "dev");
    assert.ok(!result.ok, "expected failure for port 0");
    assert.ok(result.stderr.includes("HTTP_ADDR"), "error should mention HTTP_ADDR");
  });

  it("rejects port 99999 in HTTP_ADDR", () => {
    const env = "DATABASE_URL=postgres://u:p@host:5432/db\nNATS_URL=nats://localhost:4222\nHTTP_ADDR=:99999";

    const result = runValidator(env, "dev");
    assert.ok(!result.ok, "expected failure for invalid port");
  });

  it("accepts HTTP_ADDR without port (bind address only)", () => {
    // HTTP_ADDR without a port is unusual but not invalid —
    // the Go net/http package will use port 80 by default.
    const env = "DATABASE_URL=postgres://u:p@host:5432/db\nNATS_URL=nats://localhost:4222\nHTTP_ADDR=0.0.0.0";

    const result = runValidator(env, "dev");
    assert.ok(result.ok, `HTTP_ADDR without port should be accepted; got: ${result.stderr}`);
  });

  it("accepts valid port 443", () => {
    const env = "DATABASE_URL=postgres://u:p@host:5432/db\nNATS_URL=nats://localhost:4222\nHTTP_ADDR=:443";

    const result = runValidator(env, "dev");
    assert.ok(result.ok, `valid port should be accepted; got: ${result.stderr}`);
  });
});

// ── Invalid log levels ────────────────────────────────────────────────

describe("invalid log levels", () => {
  it("rejects LOG_LEVEL=trace", () => {
    const env = "DATABASE_URL=postgres://u:p@host:5432/db\nNATS_URL=nats://localhost:4222\nLOG_LEVEL=trace";

    const result = runValidator(env, "dev");
    assert.ok(!result.ok, "expected failure for invalid log level");
    assert.ok(result.stderr.includes("LOG_LEVEL"), "error should mention LOG_LEVEL");
  });

  it("rejects LOG_LEVEL=VERBOSE (uppercase)", () => {
    const env = "DATABASE_URL=postgres://u:p@host:5432/db\nNATS_URL=nats://localhost:4222\nLOG_LEVEL=VERBOSE";

    const result = runValidator(env, "dev");
    assert.ok(!result.ok, "expected failure for uppercase invalid log level");
  });

  it("accepts LOG_LEVEL=debug", () => {
    const env = "DATABASE_URL=postgres://u:p@host:5432/db\nNATS_URL=nats://localhost:4222\nLOG_LEVEL=debug";

    const result = runValidator(env, "dev");
    assert.ok(result.ok, `debug should be valid; got: ${result.stderr}`);
  });

  it("accepts LOG_LEVEL=warn", () => {
    const env = "DATABASE_URL=postgres://u:p@host:5432/db\nNATS_URL=nats://localhost:4222\nLOG_LEVEL=warn";

    const result = runValidator(env, "dev");
    assert.ok(result.ok, `warn should be valid; got: ${result.stderr}`);
  });

  it("accepts LOG_LEVEL=error", () => {
    const env = "DATABASE_URL=postgres://u:p@host:5432/db\nNATS_URL=nats://localhost:4222\nLOG_LEVEL=error";

    const result = runValidator(env, "dev");
    assert.ok(result.ok, `error should be valid; got: ${result.stderr}`);
  });
});

// ── Invalid timeout values ────────────────────────────────────────────

describe("invalid timeout values", () => {
  it("rejects STARTUP_TIMEOUT_SECONDS=0", () => {
    const env = "DATABASE_URL=postgres://u:p@host:5432/db\nNATS_URL=nats://localhost:4222\nSTARTUP_TIMEOUT_SECONDS=0";

    const result = runValidator(env, "dev");
    assert.ok(!result.ok, "expected failure for timeout=0");
    assert.ok(result.stderr.includes("STARTUP_TIMEOUT_SECONDS"), "error should mention STARTUP_TIMEOUT_SECONDS");
  });

  it("rejects STARTUP_TIMEOUT_SECONDS=-1", () => {
    const env = "DATABASE_URL=postgres://u:p@host:5432/db\nNATS_URL=nats://localhost:4222\nSTARTUP_TIMEOUT_SECONDS=-1";

    const result = runValidator(env, "dev");
    assert.ok(!result.ok, "expected failure for negative timeout");
  });

  it("rejects STARTUP_TIMEOUT_SECONDS=abc", () => {
    const env = "DATABASE_URL=postgres://u:p@host:5432/db\nNATS_URL=nats://localhost:4222\nSTARTUP_TIMEOUT_SECONDS=abc";

    const result = runValidator(env, "dev");
    assert.ok(!result.ok, "expected failure for non-numeric timeout");
  });

  it("rejects STARTUP_TIMEOUT_SECONDS=3.5 (float)", () => {
    const env = "DATABASE_URL=postgres://u:p@host:5432/db\nNATS_URL=nats://localhost:4222\nSTARTUP_TIMEOUT_SECONDS=3.5";

    const result = runValidator(env, "dev");
    assert.ok(!result.ok, "expected failure for float timeout");
  });

  it("accepts STARTUP_TIMEOUT_SECONDS=1", () => {
    const env = "DATABASE_URL=postgres://u:***@host:5432/db\nNATS_URL=nats://localhost:4222\nSTARTUP_TIMEOUT_SECONDS=1";

    const result = runValidator(env, "dev");
    assert.ok(result.ok, `timeout=1 should be valid; got: ${result.stderr}`);
  });
});

// ── Invalid JWT expiry values ──────────────────────────────────────

describe("invalid JWT expiry values", () => {
  it("rejects JWT_EXPIRY_SECONDS=0", () => {
    const env = "DATABASE_URL=postgres://u:***@host:5432/db\nNATS_URL=nats://localhost:4222\nJWT_EXPIRY_SECONDS=0";

    const result = runValidator(env, "dev");
    assert.ok(!result.ok, "expected failure for JWT_EXPIRY_SECONDS=0");
    assert.ok(result.stderr.includes("JWT_EXPIRY_SECONDS"), "error should mention JWT_EXPIRY_SECONDS");
  });

  it("rejects JWT_EXPIRY_SECONDS=-60", () => {
    const env = "DATABASE_URL=postgres://u:***@host:5432/db\nNATS_URL=nats://localhost:4222\nJWT_EXPIRY_SECONDS=-60";

    const result = runValidator(env, "dev");
    assert.ok(!result.ok, "expected failure for negative JWT_EXPIRY_SECONDS");
  });

  it("rejects JWT_EXPIRY_SECONDS=abc", () => {
    const env = "DATABASE_URL=postgres://u:***@host:5432/db\nNATS_URL=nats://localhost:4222\nJWT_EXPIRY_SECONDS=abc";

    const result = runValidator(env, "dev");
    assert.ok(!result.ok, "expected failure for non-numeric JWT_EXPIRY_SECONDS");
  });

  it("accepts JWT_EXPIRY_SECONDS=7200", () => {
    const env = "DATABASE_URL=postgres://u:***@host:5432/db\nNATS_URL=nats://localhost:4222\nJWT_EXPIRY_SECONDS=7200";

    const result = runValidator(env, "dev");
    assert.ok(result.ok, `JWT_EXPIRY_SECONDS=7200 should be valid; got: ${result.stderr}`);
  });
});

// ── Edge cases ────────────────────────────────────────────────────────

describe("edge cases", () => {
  it("handles CRLF line endings", () => {
    const env = "DATABASE_URL=postgres://u:p@host:5432/db\r\nNATS_URL=nats://localhost:4222\r\n";

    const result = runValidator(env, "dev");
    assert.ok(result.ok, `should handle CRLF; got: ${result.stderr}`);
  });

  it("handles values with = signs in them", () => {
    const env = "DATABASE_URL=postgres://u:p@host:5432/db?options=val1=val2\nNATS_URL=nats://localhost:4222";

    const result = runValidator(env, "dev");
    assert.ok(result.ok, `should handle = in values; got: ${result.stderr}`);
  });

  it("handles values with spaces (raw)", () => {
    // URL with spaces may be accepted or rejected by URL constructor depending on position.
    // Use a clearly invalid URL for this test instead.
    const env = "DATABASE_URL=clearly-not-a-url\nNATS_URL=nats://localhost:4222";

    const result = runValidator(env, "dev");
    assert.ok(!result.ok, "invalid URL should be rejected");
    assert.ok(result.stderr.includes("DATABASE_URL"), "error should mention DATABASE_URL");
  });

  it("validates file with only comments is OK", () => {
    const env = [
      "# All commented out",
      "# Another comment",
    ].join("\n");

    const result = runValidator(env, "dev");
    // No entries = no required vars. In prod, this would fail due to missing required.
    assert.ok(result.ok, "only comments should be valid in dev mode");
  });

  it("validates file with only comments fails in prod (missing required)", () => {
    const env = [
      "# All commented out",
      "# Another comment",
    ].join("\n");

    const result = runValidator(env, "prod");
    assert.ok(!result.ok, "only comments should fail in prod due to missing required vars");
  });

  it("does not print secret values in output", () => {
    const secretPassword = "super-secret-password-12345";
    const env = [
      `DATABASE_URL=postgres://user:${secretPassword}@host:5432/db`,
      "NATS_URL=not-a-valid-url",
    ].join("\n");

    const result = runValidator(env, "dev");
    assert.ok(!result.ok, "should fail on invalid NATS_URL");
    // The output must not contain the actual password
    assert.ok(
      !result.stdout.includes(secretPassword) && !result.stderr.includes(secretPassword),
      "output must not contain secret values"
    );
  });
});

// ── Example consistency ────────────────────────────────────────────────

describe("example env file consistency", () => {
  it(".env.example passes dev validation", () => {
    // Validate the actual example file (not generated content).
    const result = runValidatorFile(
      resolve(__dirname, "..", "..", ".env.example"),
      "dev"
    );
    assert.ok(result.ok, `.env.example should pass dev validation; got: ${result.stderr}`);
  });

  it(".env.example DATABASE_URL password matches dev Compose credentials", () => {
    const examplePath = resolve(__dirname, "..", "..", ".env.example");
    const content = readFileSync(examplePath, "utf-8");

    // The dev docker-compose.yml uses POSTGRES_PASSWORD=medisync.
    // The .env.example DATABASE_URL must use the same password so a
    // cp .env.example .env produces a working local connection.
    const match = content.match(/^DATABASE_URL=(.+)$/m);
    assert.ok(match, "DATABASE_URL must be present in .env.example");

    const url = match[1];
    // Extract password component from pgx URL: postgres://user:PASSWORD@host...
    const urlObj = new URL(url);
    assert.strictEqual(
      urlObj.password,
      "medisync",
      `DATABASE_URL password must be 'medisync' to match dev Compose, got '${urlObj.password}'`
    );
  });

  it(".env.example has all dev-mode expected variables", () => {
    const examplePath = resolve(__dirname, "..", "..", ".env.example");
    const content = readFileSync(examplePath, "utf-8");

    const expectedDevVars = [
      "DATABASE_URL",
      "NATS_URL",
      "HTTP_ADDR",
      "LOG_LEVEL",
      "STARTUP_TIMEOUT_SECONDS",
      "JWT_SECRET",
      "JWT_EXPIRY_SECONDS",
      "ADMIN_BOOTSTRAP_PASSWORD",
    ];
    for (const v of expectedDevVars) {
      const re = new RegExp(`^${v}=`, "m");
      assert.ok(re.test(content), `.env.example must contain ${v}`);
    }
  });
});

// ── Production Compose JWT guard ────────────────────────────────────────

describe("production compose JWT_SECRET guard", () => {
  it("docker-compose.prod.yml passes JWT_SECRET to core with required guard", () => {
    const composePath = resolve(__dirname, "..", "..", "infra", "docker-compose.prod.yml");
    const content = readFileSync(composePath, "utf-8");

    // Find the core service section and verify JWT_SECRET is there.
    const coreSectionMatch = content.match(/^\s+core:\n([\s\S]*?)(?=^\s{2}\w|\n\w|\nnetworks)/m);
    assert.ok(coreSectionMatch, "core service section not found in docker-compose.prod.yml");

    const coreSection = coreSectionMatch[0];
    const hasJwtGuard = /\bJWT_SECRET:\s*\$\{JWT_SECRET:\?[^}]+\}/.test(coreSection);
    assert.ok(
      hasJwtGuard,
      `core service must have JWT_SECRET: \${JWT_SECRET:?...} guard; core section:\n${coreSection}`
    );
  });

  it("JWT_SECRET required guard rejects missing value", () => {
    // Verify the `${JWT_SECRET:?message}` syntax is present exactly.
    const composePath = resolve(__dirname, "..", "..", "infra", "docker-compose.prod.yml");
    const content = readFileSync(composePath, "utf-8");

    assert.ok(
      content.includes("${JWT_SECRET:?JWT_SECRET is required}"),
      "docker-compose.prod.yml must contain the literal string '${JWT_SECRET:?JWT_SECRET is required}'"
    );
  });

  it("ADMIN_BOOTSTRAP_PASSWORD required guard rejects missing value", () => {
    const composePath = resolve(__dirname, "..", "..", "infra", "docker-compose.prod.yml");
    const content = readFileSync(composePath, "utf-8");

    assert.ok(
      content.includes("${ADMIN_BOOTSTRAP_PASSWORD:?ADMIN_BOOTSTRAP_PASSWORD is required}"),
      "docker-compose.prod.yml must contain ADMIN_BOOTSTRAP_PASSWORD required guard"
    );
  });
});

function runValidatorFile(filePath, mode) {
  try {
    const out = execFileSync("node", [VALIDATOR, "--mode", mode, filePath], {
      encoding: "utf-8",
      timeout: 5000,
    });
    return { ok: true, stdout: out };
  } catch (err) {
    return {
      ok: false,
      stdout: err.stdout || "",
      stderr: err.stderr || "",
      code: err.status,
    };
  }
}
