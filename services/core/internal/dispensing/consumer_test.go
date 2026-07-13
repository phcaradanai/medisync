package dispensing

import (
	"testing"

	"google.golang.org/protobuf/encoding/protojson"

	"github.com/adm-chura3inter/medisync/services/core/internal/gen/medisync/events/v1"
)

func validEvent() *eventsv1.PrescriptionCreated {
	return &eventsv1.PrescriptionCreated{
		PrescriptionId: "RX-1",
		SourceSystem:   "mock-his",
		Hn:             "HN000001",
		WardId:         "WARD-3A",
		Items: []*eventsv1.PrescriptionItem{
			{DrugCode: "PARA500", DrugName: "Paracetamol 500 mg", Quantity: 10},
		},
	}
}

func TestValidateAcceptsCompleteEvent(t *testing.T) {
	if reason := validate(validEvent()); reason != "" {
		t.Fatalf("expected valid, got rejection: %s", reason)
	}
}

func TestValidateRejects(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*eventsv1.PrescriptionCreated)
	}{
		{"missing prescription_id", func(ev *eventsv1.PrescriptionCreated) { ev.PrescriptionId = "" }},
		{"missing source_system", func(ev *eventsv1.PrescriptionCreated) { ev.SourceSystem = "" }},
		{"no items", func(ev *eventsv1.PrescriptionCreated) { ev.Items = nil }},
		{"item missing drug_code", func(ev *eventsv1.PrescriptionCreated) { ev.Items[0].DrugCode = "" }},
		{"zero quantity", func(ev *eventsv1.PrescriptionCreated) { ev.Items[0].Quantity = 0 }},
		{"negative quantity", func(ev *eventsv1.PrescriptionCreated) { ev.Items[0].Quantity = -1 }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ev := validEvent()
			tc.mutate(ev)
			if reason := validate(ev); reason == "" {
				t.Fatal("expected rejection, got valid")
			}
		})
	}
}

// The consumer terminates malformed payloads instead of retrying them; this
// pins the parse behavior the poison-message path relies on.
func TestMalformedPayloadFailsUnmarshal(t *testing.T) {
	var ev eventsv1.PrescriptionCreated
	err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal([]byte(`{"items": "not-an-array"`), &ev)
	if err == nil {
		t.Fatal("expected unmarshal error for malformed payload")
	}
}
