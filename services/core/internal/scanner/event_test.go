package scanner

import (
	"context"
	"testing"
	"time"
)

func TestDecodePreservesRawAndParsedValues(t *testing.T) {
	event, err := Decode([]byte(`{
      "eventId":"evt-1",
      "kioskCode":"00010001",
      "kind":"QR",
      "scanType":"QR",
      "scanPurpose":"STICKER",
      "format":"qrcode_wny",
      "value":"prescription_00010001_1000174_1_OUT_20260722120000",
      "parsed":{"prescription_id":"prescription"},
      "raw":{"text":"prescription_00010001_1000174_1_OUT_20260722120000","bytes":[112],"hex":"70"},
      "scannedAt":"2026-07-22T12:00:00Z",
      "source":{"channel":"qr-nfc","portPath":"/dev/ttyS1","baudRate":9600}
    }`))
	if err != nil {
		t.Fatal(err)
	}
	if event.KioskCode != "00010001" || event.Value == "" || event.Raw.Hex != "70" {
		t.Fatalf("decoded envelope lost fields: %+v", event)
	}
	if event.ScanType != "QR" || event.ScanPurpose != "STICKER" {
		t.Fatalf("scan classification missing: %+v", event)
	}
	if event.Parsed["prescription_id"] != "prescription" {
		t.Fatalf("parsed value missing: %+v", event.Parsed)
	}
}

func TestBrokerRoutesOnlyMatchingKiosk(t *testing.T) {
	broker := NewBroker()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	matching, stopMatching := broker.Subscribe(ctx, "00010001")
	defer stopMatching()
	other, stopOther := broker.Subscribe(ctx, "00010002")
	defer stopOther()

	broker.Publish(Event{EventID: "evt-1", KioskCode: "00010001", Kind: "QR"})
	select {
	case got := <-matching:
		if got.KioskCode != "00010001" {
			t.Fatalf("unexpected kiosk: %s", got.KioskCode)
		}
	case <-time.After(time.Second):
		t.Fatal("matching subscriber did not receive event")
	}
	select {
	case got := <-other:
		t.Fatalf("event leaked to another kiosk: %+v", got)
	case <-time.After(20 * time.Millisecond):
	}
}
