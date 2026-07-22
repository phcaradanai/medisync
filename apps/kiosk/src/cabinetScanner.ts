export type CabinetScannerEvent = {
  eventId?: string;
  kioskCode?: string;
  kind?: "QR" | "BARCODE" | "NFC" | string;
  scanType?: "QR" | "BARCODE" | "NFC" | string;
  scanPurpose?: "STICKER" | "DRUG_BARCODE" | "USER_NFC" | string;
  format?: string;
  value?: string;
  parsed?: {
    prescription_id?: string;
    [key: string]: unknown;
  };
  act?: string;
  code?: string;
  ts?: string;
  info?: {
    payloadText?: string;
    type?: string;
    scanType?: string;
    scanPurpose?: string;
  };
};

type SseHandler = (event: CabinetScannerEvent) => void;

/**
 * The WNY reader format carries the prescription UUID as its first segment.
 * MediSync stores that UUID as prescription_id, while the vending agent keeps
 * the complete barcode in its MQTT audit payload. Accept both formats.
 */
export function normalizeStickerScan(raw: string): string {
  const value = raw.trim();
  const match = value.match(
    /^([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})_\d{8}_\d+_\d+_OUT_\d{14}$/i,
  );
  return match?.[1] || value;
}

function dispatchSseBlock(block: string, onEvent: SseHandler): void {
  let eventName = "message";
  const data: string[] = [];
  for (const line of block.split(/\r?\n/)) {
    if (line.startsWith("event:")) eventName = line.slice(6).trim();
    else if (line.startsWith("data:")) data.push(line.slice(5).trimStart());
  }
  if (eventName !== "scan" || data.length === 0) return;
  try {
    const parsed = JSON.parse(data.join("\n")) as CabinetScannerEvent;
    const kind = String(parsed.scanType || parsed.kind || parsed.info?.scanType || parsed.info?.type || "").toUpperCase();
    // The same serial channel may be used by an NFC reader. NFC is staff
    // identity input, not a prescription sticker, so do not consume it here.
    if (
      kind === "NFC" ||
      kind === "NFC-MIFARE" ||
      String(parsed.scanPurpose || parsed.info?.scanPurpose || "").toUpperCase() === "USER_NFC" ||
      parsed.act === "mifare"
    ) return;
    onEvent(parsed);
  } catch {
    // Ignore malformed frames; the agent keeps the serial stream alive.
  }
}

/**
 * Subscribe to the scanner belonging to one kiosk. Fetch is used instead of
 * EventSource so the kiosk JWT can be sent in an Authorization header.
 */
export function subscribeToCabinetScanner(
  kioskCode: string,
  kioskToken: string,
  onScan: (code: string, event: CabinetScannerEvent) => void,
): () => void {
  if (typeof window === "undefined" || typeof window.fetch !== "function") return () => undefined;

  const controller = new AbortController();
  let stopped = false;

  const consume = async () => {
    while (!stopped) {
      try {
        const response = await window.fetch(
          `/api/v1/kiosks/${encodeURIComponent(kioskCode)}/scanner/events`,
          {
            headers: { Accept: "text/event-stream", Authorization: `Bearer ${kioskToken}` },
            signal: controller.signal,
          },
        );
        if (!response.ok || !response.body) throw new Error(`scanner stream ${response.status}`);
        const reader = response.body.getReader();
        const decoder = new TextDecoder();
        let buffer = "";
        while (!stopped) {
          const next = await reader.read();
          if (next.done) break;
          buffer += decoder.decode(next.value, { stream: true });
          const blocks = buffer.split(/\r?\n\r?\n/);
          buffer = blocks.pop() || "";
          for (const block of blocks) {
            dispatchSseBlock(block, (event) => {
              const rawValue =
                event.parsed?.prescription_id ||
                event.value ||
                event.code ||
                event.info?.payloadText ||
                "";
              const code = normalizeStickerScan(rawValue);
              if (code) onScan(code, event);
            });
          }
        }
        reader.releaseLock();
      } catch {
        if (stopped) return;
        await new Promise((resolve) => window.setTimeout(resolve, 1500));
      }
    }
  };

  void consume();
  return () => {
    stopped = true;
    controller.abort();
  };
}
