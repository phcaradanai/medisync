package vending

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPClientDispensePreservesStructuredHardwareFailure(t *testing.T) {
	want := DispenseResponse{
		OK: 0,
		Data: DispenseData{
			Status: "failed",
			Steps:  []DispenseStep{{Phase: "dispense", AllocationID: "alloc-5", Layer: 5, Success: false}},
		},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(want)
	}))
	defer server.Close()

	response, err := NewClient(server.URL, "token").Dispense(context.Background(), DispenseRequest{
		Prescription: "RX-1",
		DoorNo:       1,
		Items:        []DispenseItem{{AllocationID: "alloc-5", Layer: 5, ChannelStart: 1, ChannelEnd: 1, Quantity: 1}},
	})
	if err == nil {
		t.Fatal("expected structured hardware failure to return an error")
	}
	if response == nil || len(response.Data.Steps) != 1 || response.Data.Steps[0].AllocationID != "alloc-5" {
		t.Fatalf("structured failure response was lost: %+v", response)
	}
}
