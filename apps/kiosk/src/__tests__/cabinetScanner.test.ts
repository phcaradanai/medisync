import { describe, expect, it } from "vitest";
import { normalizeStickerScan } from "../cabinetScanner";

describe("cabinet scanner payloads", () => {
  it("extracts the prescription UUID from an OUT sticker payload", () => {
    expect(
      normalizeStickerScan(
        "fa97dbc9-3e28-4978-9588-9008bd86209f_00010001_1000174_1_OUT_20260507235753\r\n",
      ),
    ).toBe("fa97dbc9-3e28-4978-9588-9008bd86209f");
  });

  it("keeps MediSync sticker ids unchanged", () => {
    expect(normalizeStickerScan("DEMO-RX-001\n")).toBe("DEMO-RX-001");
  });

  it("does not turn an IN inventory label into a withdrawal id", () => {
    const value = "fa97dbc9-3e28-4978-9588-9008bd86209f_00010001_1000174_1_IN_20260507235753";
    expect(normalizeStickerScan(value)).toBe(value);
  });
});
