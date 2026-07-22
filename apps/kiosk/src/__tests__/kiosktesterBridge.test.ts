import { afterEach, describe, expect, it, vi } from "vitest";
import { kioskTesterEventsURL, parseKioskTesterCommand, subscribeToKioskTester } from "../kiosktesterBridge.ts";

class FakeEventSource {
  static instances: FakeEventSource[] = [];
  readonly url: string;
  onmessage: ((event: MessageEvent<string>) => void) | null = null;
  closed = false;

  constructor(url: string | URL) {
    this.url = String(url);
    FakeEventSource.instances.push(this);
  }

  emit(value: unknown) {
    this.onmessage?.({ data: JSON.stringify(value) } as MessageEvent<string>);
  }

  close() {
    this.closed = true;
  }
}

afterEach(() => {
  vi.unstubAllGlobals();
  FakeEventSource.instances = [];
});

describe("kiosktester bridge", () => {
  it("derives the tester stream from the kiosk hostname, not its port", () => {
    expect(kioskTesterEventsURL("00010001", { protocol: "http:", hostname: "192.168.1.20" }))
      .toBe("http://192.168.1.20:8899/api/kiosk-events?kioskCode=00010001");
  });

  it("parses only supported non-empty scan commands", () => {
    expect(parseKioskTesterCommand({ id: 4, kioskCode: "00010001", type: "scan_sticker", code: " RX-1 " }))
      .toEqual({ id: 4, kioskCode: "00010001", type: "scan_sticker", code: "RX-1" });
    expect(parseKioskTesterCommand({ kioskCode: "00010001", type: "scan_card", cardToken: " card-nurse " }))
      .toEqual({ id: undefined, kioskCode: "00010001", type: "scan_card", cardToken: "card-nurse" });
    expect(parseKioskTesterCommand({ kioskCode: "00010001", type: "scan_sticker", code: "" })).toBeNull();
    expect(parseKioskTesterCommand({ type: "unknown" })).toBeNull();
    expect(parseKioskTesterCommand({ kioskCode: "00010002", type: "scan_sticker", code: "RX-2" }, "00010001")).toBeNull();
  });

  it("receives commands in an independently opened kiosk and closes cleanly", () => {
    vi.stubGlobal("EventSource", FakeEventSource);
    const received = vi.fn();
    const disconnect = subscribeToKioskTester("00010001", received);
    const source = FakeEventSource.instances[0];

    expect(source.url).toBe("http://localhost:8899/api/kiosk-events?kioskCode=00010001");
    source.emit({ id: 9, kioskCode: "00010001", type: "scan_sticker", code: "RX-9" });
    expect(received).toHaveBeenCalledWith({ id: 9, kioskCode: "00010001", type: "scan_sticker", code: "RX-9" });

    disconnect();
    expect(source.closed).toBe(true);
  });
});
