package main

import (
	"testing"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"

	eventsv1 "github.com/adm-chura3inter/medisync/services/core/internal/gen/medisync/events/v1"
)

func TestSampleHasRequiredFields(t *testing.T) {
	ev := sample("WARD-3A", "mock-his", 0)

	if ev.GetPrescriptionId() == "" {
		t.Error("prescription_id must not be empty")
	}
	if ev.GetSourceSystem() == "" {
		t.Error("source_system must not be empty")
	}
	if ev.GetProjectCode() != "0001" {
		t.Errorf("project_code = %q, want 0001", ev.GetProjectCode())
	}
	if len(ev.GetItems()) == 0 {
		t.Error("items must not be empty")
	}
	for _, it := range ev.GetItems() {
		if it.GetDrugCode() == "" {
			t.Error("item drug_code must not be empty")
		}
		if it.GetQuantity() <= 0 {
			t.Errorf("item %s quantity must be positive, got %d", it.GetDrugCode(), it.GetQuantity())
		}
	}
	if ev.GetIssuedAt() == nil {
		t.Error("issued_at must not be nil")
	}
	if ev.GetTraceId() == "" {
		t.Error("trace_id must not be empty")
	}
}

func TestSampleProducesUniqueIDs(t *testing.T) {
	ev1 := sample("WARD-3A", "mock-his", 0)
	ev2 := sample("WARD-3A", "mock-his", 1)

	if ev1.GetPrescriptionId() == ev2.GetPrescriptionId() {
		t.Error("consecutive sample() calls must produce different prescription_ids")
	}
}

func TestSampleMarshalRoundtrip(t *testing.T) {
	ev := sample("WARD-3A", "mock-his", 0)

	data, err := protojson.Marshal(ev)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var roundtripped eventsv1.PrescriptionCreated
	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(data, &roundtripped); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if roundtripped.GetPrescriptionId() != ev.GetPrescriptionId() {
		t.Error("prescription_id mismatch after roundtrip")
	}
	if roundtripped.GetSourceSystem() != ev.GetSourceSystem() {
		t.Error("source_system mismatch after roundtrip")
	}
	if len(roundtripped.GetItems()) != len(ev.GetItems()) {
		t.Errorf("items count mismatch: got %d, want %d", len(roundtripped.GetItems()), len(ev.GetItems()))
	}
	if roundtripped.GetHn() != ev.GetHn() {
		t.Error("hn mismatch after roundtrip")
	}
}

func TestSampleUsesSpecifiedWard(t *testing.T) {
	ev := sample("WARD-7B", "mock-his", 0)
	if ev.GetWardId() != "WARD-7B" {
		t.Errorf("ward_id = %q, want WARD-7B", ev.GetWardId())
	}
}

func TestSampleUsesSpecifiedSource(t *testing.T) {
	ev := sample("WARD-3A", "real-his", 0)
	if ev.GetSourceSystem() != "real-his" {
		t.Errorf("source_system = %q, want real-his", ev.GetSourceSystem())
	}
}

func TestSampleItemsHaveDosageText(t *testing.T) {
	ev := sample("WARD-3A", "mock-his", 0)
	for i, it := range ev.GetItems() {
		if it.GetDosageText() == "" {
			t.Errorf("item %d (%s) dosage_text must not be empty in test samples", i, it.GetDrugCode())
		}
	}
}

func TestSampleIssuedAtIsRecent(t *testing.T) {
	ev := sample("WARD-3A", "mock-his", 0)
	now := timestamppb.Now()
	diff := now.AsTime().Sub(ev.GetIssuedAt().AsTime())
	if diff < 0 {
		diff = -diff
	}
	if diff.Seconds() > 5 {
		t.Errorf("issued_at too far from now: %v", diff)
	}
}
