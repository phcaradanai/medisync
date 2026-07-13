#!/usr/bin/env node

/**
 * MediSync environment-file validator.
 *
 * Usage:
 *   node scripts/env/validate.mjs [--mode dev|prod] [file]
 *
 * - Parses an env file without printing secret values.
 * - Detects missing required variables (prod only).
 * - Rejects placeholder production secrets (<...> patterns).
 * - Rejects duplicate keys and malformed lines.
 * - Validates URLs, ports, log levels, and positive timeout values.
 * - Returns exit code 0 on success, non-zero on failure.
 *
 * Default mode: dev (no required-variable checks, no placeholder rejection).
 * Default file: .env
 */

import { readFileSync, existsSync } from "node:fs";
import { resolve, dirname } from "node:path";
import { fileURLToPath } from "node:url";

const __dirname = dirname(fileURLToPath(import.meta.url));
const PROJECT_ROOT = resolve(__dirname, "..", "..");

// ── Variable definitions ─────────────────────────────────────────────

const VARIABLES = {
  DATABASE_URL: {
    purpose: "pgx-compatible PostgreSQL connection string",
    service: "core",
    required: true,
    secret: true,
  },
  NATS_URL: {
    purpose: "NATS server URL (JetStream required)",
    service: "core",
    required: true,
    secret: false,
  },
  HTTP_ADDR: {
    purpose: "Connect-RPC API listen address (host:port or :port)",
    service: "core",
    required: false,
    secret: false,
  },
  LOG_LEVEL: {
    purpose: "Structured log level (debug|info|warn|error)",
    service: "core",
    required: false,
    secret: false,
  },
  STARTUP_TIMEOUT_SECONDS: {
    purpose: "Seconds to wait for postgres + NATS at startup",
    service: "core",
    required: false,
    secret: false,
  },
  JWT_SECRET: {
    purpose: "HMAC key for signing JWT access tokens (minimum 32 bytes)",
    service: "core",
    required: true,
    secret: true,
    minBytes: 32,
  },
  JWT_EXPIRY_SECONDS: {
    purpose: "Access-token lifetime in seconds (positive integer)",
    service: "core",
    required: false,
    secret: false,
  },
  ADMIN_BOOTSTRAP_PASSWORD: {
    purpose: "Bootstrap admin password, bcrypt-hashed before storage",
    service: "core",
    required: true,
    secret: true,
    minBytes: 12,
  },
  CARD_TOKEN_HMAC_KEY: {
    purpose: "HMAC key for deterministic card-token hashing (minimum 32 bytes)",
    service: "core",
    required: true,
    secret: true,
    minBytes: 32,
  },
  // Production Compose secrets (used by docker-compose.prod.yml, not the Go core).
  POSTGRES_USER: {
    purpose: "PostgreSQL superuser name (Compose only)",
    service: "postgres",
    required: true,
    secret: false,
  },
  POSTGRES_PASSWORD: {
    purpose: "PostgreSQL superuser password (Compose only)",
    service: "postgres",
    required: true,
    secret: true,
  },
  POSTGRES_DB: {
    purpose: "PostgreSQL database name (Compose only)",
    service: "postgres",
    required: true,
    secret: false,
  },
};

const VALID_LOG_LEVELS = new Set(["debug", "info", "warn", "error"]);
const KNOWN_PLACEHOLDER_VALUES = new Set([
  "medisync-dev-secret-change-in-production",
  "medisync-dev-card-hmac-change-in-prod",
]);

// ── Helpers ───────────────────────────────────────────────────────────

function parseEnvFile(filePath) {
  const raw = readFileSync(filePath, "utf-8");
  const lines = raw.split(/\r?\n/);
  const entries = []; // { key, value, line }
  const seenKeys = new Map(); // key -> first line number

  for (let i = 0; i < lines.length; i++) {
    const lineNum = i + 1;
    const rawLine = lines[i];

    // Skip empty lines and comments
    const trimmed = rawLine.trim();
    if (trimmed === "" || trimmed.startsWith("#")) continue;

    // Must have KEY=VALUE form
    const eqIdx = rawLine.indexOf("=");
    if (eqIdx < 0) {
      return { error: `line ${lineNum}: malformed line (no '=') — "${rawLine.slice(0, 80)}"` };
    }

    const key = rawLine.slice(0, eqIdx).trim();
    const value = rawLine.slice(eqIdx + 1);

    if (key === "") {
      return { error: `line ${lineNum}: empty key` };
    }

    // Detect duplicate keys
    if (seenKeys.has(key)) {
      return {
        error: `line ${lineNum}: duplicate key "${key}" (first seen at line ${seenKeys.get(key)})`,
      };
    }
    seenKeys.set(key, lineNum);

    entries.push({ key, value, line: lineNum });
  }

  return { entries };
}

function maskValue(value) {
  if (value.length <= 4) return "***";
  return value.slice(0, 2) + "***" + value.slice(-1);
}

// ── Validators ────────────────────────────────────────────────────────

function validateUrl(value, key, line) {
  try {
    const url = new URL(value);
    if (!url.protocol || !url.hostname) {
      return `line ${line}: ${key} is not a valid URL — "${maskValue(value)}"`;
    }
    return null;
  } catch {
    return `line ${line}: ${key} is not a valid URL — "${maskValue(value)}"`;
  }
}

function validatePort(value, key, line) {
  const num = Number(value);
  if (!Number.isInteger(num) || num < 1 || num > 65535) {
    return `line ${line}: ${key} must be a valid port (1-65535), got "${value}"`;
  }
  return null;
}

function validateLogLevel(value, key, line) {
  if (!VALID_LOG_LEVELS.has(value)) {
    return `line ${line}: ${key} must be one of [${[...VALID_LOG_LEVELS].join(", ")}], got "${value}"`;
  }
  return null;
}

function validatePositiveInt(value, key, line) {
  const num = Number(value);
  if (!Number.isInteger(num) || num <= 0) {
    return `line ${line}: ${key} must be a positive integer, got "${value}"`;
  }
  return null;
}

function isPlaceholder(value) {
  const trimmed = value.trim();
  const normalized = trimmed.toLowerCase();
  return (
    /^<.+>$/.test(trimmed) ||
    normalized === "changeme" ||
    normalized === "change-me" ||
    KNOWN_PLACEHOLDER_VALUES.has(normalized)
  );
}

// ── Main validation logic ─────────────────────────────────────────────

function validate(filePath, mode) {
  const absPath = resolve(PROJECT_ROOT, filePath);
  if (!existsSync(absPath)) {
    console.error(`File not found: ${absPath}`);
    process.exit(1);
  }

  const result = parseEnvFile(absPath);
  if (result.error) {
    console.error(`ERROR: ${result.error}`);
    process.exit(1);
  }

  const { entries } = result;
  const errors = [];
  const foundKeys = new Set(entries.map((e) => e.key));

  for (const { key, value, line } of entries) {
    const def = VARIABLES[key];

    if (!def) {
      // Unknown variable — warn but don't fail (allows future expansion).
      console.warn(`WARN: line ${line}: unknown variable "${key}"`);
      continue;
    }

    // Production: reject placeholders for required and secret vars
    if (mode === "prod") {
      if ((def.required || def.secret) && isPlaceholder(value)) {
        const detail = def.secret ? "" : ` "${value.trim()}"`;
        errors.push(`line ${line}: ${key} contains a placeholder${detail} — real value required`);
        continue;
      }
    }

    if (def.minBytes && Buffer.byteLength(value.trim(), "utf8") < def.minBytes) {
      errors.push(`${key} must be at least ${def.minBytes} bytes`);
      continue;
    }

    // URL validation — skip if value looks like a placeholder (dev mode)
    if ((key === "DATABASE_URL" || key === "NATS_URL") && !isPlaceholder(value)) {
      const err = validateUrl(value, key, line);
      if (err) errors.push(err);
    }

    // Port validation for HTTP_ADDR
    if (key === "HTTP_ADDR") {
      // Extract port from :8080 or 0.0.0.0:8080
      const portMatch = value.match(/:(\d+)$/);
      if (portMatch) {
        const err = validatePort(portMatch[1], key, line);
        if (err) errors.push(err);
      }
    }

    // Log level validation
    if (key === "LOG_LEVEL") {
      const err = validateLogLevel(value, key, line);
      if (err) errors.push(err);
    }

    // Positive integer validation
    if (key === "STARTUP_TIMEOUT_SECONDS" || key === "JWT_EXPIRY_SECONDS") {
      const err = validatePositiveInt(value, key, line);
      if (err) errors.push(err);
    }
  }

  // Production: check for missing required variables
  if (mode === "prod") {
    for (const [key, def] of Object.entries(VARIABLES)) {
      if (def.required && !foundKeys.has(key)) {
        errors.push(`MISSING: required variable "${key}" (${def.purpose}) is not set`);
      }
    }
  }

  if (errors.length > 0) {
    console.error(`\nValidation FAILED (${errors.length} issue${errors.length === 1 ? "" : "s"}):\n`);
    for (const e of errors) {
      console.error(`  - ${e}`);
    }
    console.error();
    process.exit(1);
  }

  const modeLabel = mode === "prod" ? "production" : "development";
  console.log(`\nOK: ${filePath} is valid for ${modeLabel} (${entries.length} variables parsed).\n`);
}

// ── Entry point ───────────────────────────────────────────────────────

const args = process.argv.slice(2);
let mode = "dev";
let file = ".env";

for (let i = 0; i < args.length; i++) {
  if (args[i] === "--mode" && i + 1 < args.length) {
    mode = args[++i];
  } else if (!args[i].startsWith("--")) {
    file = args[i];
  }
}

if (mode !== "dev" && mode !== "prod") {
  console.error(`Invalid mode "${mode}". Use "dev" or "prod".`);
  process.exit(2);
}

validate(file, mode);
