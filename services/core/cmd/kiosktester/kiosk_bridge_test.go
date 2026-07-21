package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestKioskCommandBrokerPublishesOnlyToSelectedKiosk(t *testing.T) {
	broker := newKioskCommandBroker()
	first, unsubscribeFirst := broker.subscribe("00010001")
	defer unsubscribeFirst()
	second, unsubscribeSecond := broker.subscribe("00010002")
	defer unsubscribeSecond()
	if got := broker.clientCount("00010001"); got != 1 {
		t.Fatalf("client count = %d, want 1", got)
	}

	published, recipients := broker.publish(kioskCommand{KioskCode: "00010001", Type: "scan_sticker", Code: "RX-1"})
	if recipients != 1 {
		t.Fatalf("recipients = %d, want 1", recipients)
	}
	if published.ID == 0 || published.SentAt == "" {
		t.Fatalf("published command lacks broker metadata: %+v", published)
	}

	select {
	case got := <-first:
		if got.KioskCode != "00010001" || got.Type != "scan_sticker" || got.Code != "RX-1" {
			t.Fatalf("selected kiosk received %+v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("selected kiosk did not receive command")
	}
	select {
	case got := <-second:
		t.Fatalf("other kiosk must not receive command: %+v", got)
	case <-time.After(20 * time.Millisecond):
	}
}

func TestKioskCommandHandlerReportsRecipients(t *testing.T) {
	broker := newKioskCommandBroker()
	events, unsubscribe := broker.subscribe("00010001")
	defer unsubscribe()

	req := httptest.NewRequest(http.MethodPost, "/api/kiosk-command",
		bytes.NewBufferString(`{"kioskCode":"00010001","type":"scan_card","cardToken":"card-pharmacist"}`))
	res := httptest.NewRecorder()
	broker.handleCommand(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
	var body struct {
		OK         bool `json:"ok"`
		Recipients int  `json:"recipients"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !body.OK || body.Recipients != 1 {
		t.Fatalf("response = %+v", body)
	}

	select {
	case got := <-events:
		if got.CardToken != "card-pharmacist" {
			t.Fatalf("card token = %q", got.CardToken)
		}
	case <-time.After(time.Second):
		t.Fatal("subscriber did not receive card command")
	}
}

func TestKioskCommandHandlerRejectsInvalidPayload(t *testing.T) {
	broker := newKioskCommandBroker()
	req := httptest.NewRequest(http.MethodPost, "/api/kiosk-command",
		bytes.NewBufferString(`{"kioskCode":"00010001","type":"scan_sticker"}`))
	res := httptest.NewRecorder()
	broker.handleCommand(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusBadRequest)
	}
}
