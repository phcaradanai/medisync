export type KioskTesterCommand =
  | { id?: number; kioskCode: string; type: "scan_sticker"; code: string }
  | { id?: number; kioskCode: string; type: "scan_card"; cardToken: string };

type LocationHost = Pick<Location, "protocol" | "hostname">;

export function kioskTesterEventsURL(kioskCode: string, location: LocationHost = window.location): string {
  return `${location.protocol}//${location.hostname}:8899/api/kiosk-events?kioskCode=${encodeURIComponent(kioskCode)}`;
}

export function parseKioskTesterCommand(value: unknown, expectedKioskCode?: string): KioskTesterCommand | null {
	if (!value || typeof value !== "object") return null;
	const data = value as Record<string, unknown>;
	if (typeof data.kioskCode !== "string" || !/^(?!0000)\d{4}(?!0000)\d{4}$/.test(data.kioskCode)) return null;
	if (expectedKioskCode && data.kioskCode !== expectedKioskCode) return null;
	const kioskCode = data.kioskCode;
	const id = typeof data.id === "number" ? data.id : undefined;
	if (data.type === "scan_sticker" && typeof data.code === "string" && data.code.trim()) {
		return { id, kioskCode, type: "scan_sticker", code: data.code.trim() };
	}
	if (data.type === "scan_card" && typeof data.cardToken === "string" && data.cardToken.trim()) {
		return { id, kioskCode, type: "scan_card", cardToken: data.cardToken.trim() };
  }
  return null;
}

export function subscribeToKioskTester(kioskCode: string, onCommand: (command: KioskTesterCommand) => void): () => void {
  if (typeof EventSource === "undefined") return () => undefined;

	const source = new EventSource(kioskTesterEventsURL(kioskCode));
  source.onmessage = (event) => {
    try {
			const command = parseKioskTesterCommand(JSON.parse(event.data) as unknown, kioskCode);
      if (command) onCommand(command);
    } catch {
      // Ignore malformed test events. EventSource reconnects automatically when
      // the tester container or network becomes available again.
    }
  };
  return () => source.close();
}
