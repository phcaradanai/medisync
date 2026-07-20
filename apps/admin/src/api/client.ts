// Connect-RPC v2 client factory.
// Uses createClient against exported service descriptors from _pb.ts files.
import { createClient, type Transport } from "@connectrpc/connect";
import { createConnectTransport } from "@connectrpc/connect-web";
import {
  IdentityService,
  ProjectService,
} from "@medisync/proto/medisync/identity/v1/identity_pb";
import {
  CatalogService,
} from "@medisync/proto/medisync/catalog/v1/catalog_pb";
import {
  InventoryService,
} from "@medisync/proto/medisync/inventory/v1/inventory_pb";
import {
  KioskService,
} from "@medisync/proto/medisync/kiosk/v1/kiosk_pb";

// ── Transport ──────────────────────────────────────────────────────

let tokenStore: (() => string | null) = () => null;

/** Register a function that returns the current Bearer token. */
export function setTokenProvider(fn: () => string | null) {
  tokenStore = fn;
}

function makeTransport(): Transport {
  return createConnectTransport({
    // Empty baseUrl — Vite dev server proxies /medisync.* to core.
    baseUrl: "",
    useBinaryFormat: false,
    fetch: (input, init) => {
      const token = tokenStore();
      const headers: Record<string, string> = {
        ...(init?.headers as Record<string, string>),
      };
      if (token) {
        headers["Authorization"] = `Bearer ${token}`;
      }
      // Force application/json: server doesn't support application/connect+json.
      headers["Content-Type"] = "application/json";
      return fetch(input, { ...init, headers });
    },
  });
}

// ── Singleton clients ──────────────────────────────────────────────

let transport: Transport;

function getTransport(): Transport {
  if (!transport) transport = makeTransport();
  return transport;
}

/** Clears the cached transport so a new one picks up the latest token. */
export function resetTransport() {
  transport = makeTransport();
}

export const identityClient = createClient(IdentityService, getTransport());

export const projectClient = createClient(ProjectService, getTransport());

export const catalogClient = createClient(CatalogService, getTransport());

export const inventoryClient = createClient(InventoryService, getTransport());

export const kioskClient = createClient(KioskService, getTransport());

// Re-export types that pages use.
export type {
  LoginRequest,
  WhoAmIRequest,
} from "@medisync/proto/medisync/identity/v1/identity_pb";

export type {
  CreateDrugRequest,
  GetDrugRequest,
  ListDrugsRequest,
  UpdateDrugRequest,
  DeactivateDrugRequest,
} from "@medisync/proto/medisync/catalog/v1/catalog_pb";
