// Package scanner receives physical QR/barcode/NFC reads from vending agents.
package scanner

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Event is the canonical scanner envelope published to JetStream. Raw bytes
// and text are intentionally retained so a later parser/report can reproduce
// exactly what the cabinet reader sent.
type Event struct {
	EventID   string         `json:"eventId"`
	KioskCode string         `json:"kioskCode"`
	Kind      string         `json:"kind"`
	ScanType  string         `json:"scanType"`
	ScanPurpose string       `json:"scanPurpose"`
	Format    string         `json:"format,omitempty"`
	Value     string         `json:"value"`
	Parsed    map[string]any `json:"parsed,omitempty"`
	Raw       RawPayload     `json:"raw"`
	ScannedAt string         `json:"scannedAt"`
	Source    Source         `json:"source"`
}

type RawPayload struct {
	Text  string `json:"text"`
	Bytes []int  `json:"bytes"`
	Hex   string `json:"hex"`
}

type Source struct {
	Channel  string `json:"channel"`
	PortPath string `json:"portPath,omitempty"`
	BaudRate int    `json:"baudRate,omitempty"`
	Agent    string `json:"agent,omitempty"`
}

func (e Event) Validate() error {
	if strings.TrimSpace(e.EventID) == "" {
		return fmt.Errorf("eventId is required")
	}
	if strings.TrimSpace(e.KioskCode) == "" {
		return fmt.Errorf("kioskCode is required")
	}
	if strings.TrimSpace(e.Kind) == "" {
		return fmt.Errorf("kind is required")
	}
	if e.Kind != "QR" && e.Kind != "BARCODE" && e.Kind != "NFC" {
		return fmt.Errorf("unsupported kind %q", e.Kind)
	}
	if e.ScanType != "QR" && e.ScanType != "BARCODE" && e.ScanType != "NFC" {
		return fmt.Errorf("unsupported scanType %q", e.ScanType)
	}
	if e.ScanType != e.Kind {
		return fmt.Errorf("scanType %q does not match kind %q", e.ScanType, e.Kind)
	}
	if e.ScanPurpose != "STICKER" && e.ScanPurpose != "DRUG_BARCODE" && e.ScanPurpose != "USER_NFC" {
		return fmt.Errorf("unsupported scanPurpose %q", e.ScanPurpose)
	}
	if e.Raw.Text == "" && len(e.Raw.Bytes) == 0 {
		return fmt.Errorf("raw scanner payload is required")
	}
	return nil
}

func Decode(data []byte) (Event, error) {
	var event Event
	if err := json.Unmarshal(data, &event); err != nil {
		return Event{}, fmt.Errorf("decode scanner event: %w", err)
	}
	// Permit one rolling deployment where an older agent only sent `kind`,
	// while normalizing the event before it reaches the browser.
	if event.ScanType == "" {
		event.ScanType = event.Kind
	}
	if event.ScanPurpose == "" {
		switch event.Kind {
		case "NFC":
			event.ScanPurpose = "USER_NFC"
		case "QR":
			event.ScanPurpose = "STICKER"
		case "BARCODE":
			event.ScanPurpose = "DRUG_BARCODE"
		}
	}
	if err := event.Validate(); err != nil {
		return Event{}, fmt.Errorf("invalid scanner event: %w", err)
	}
	return event, nil
}
