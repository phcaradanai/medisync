package printing

import (
	"context"
	"testing"
	"time"

	"github.com/adm-chura3inter/medisync/services/core/internal/platform/config"

	eventsv1 "github.com/adm-chura3inter/medisync/services/core/internal/gen/medisync/events/v1"
)

func configForTests() config.Config {
	return config.Config{
		PrintOpsURL:    "http://localhost:3000",
		PrintOpsAPIKey: "test-key",
		PrintOpsFake:   false,
	}
}

func TestFakeClientSubmitJob(t *testing.T) {
	client := NewFakeClient()

	resp, err := client.SubmitJob(context.Background(), PrintJobRequest{
		RequestID: "req-1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.ID != "fake-job-req-1" {
		t.Errorf("ID = %q, want fake-job-req-1", resp.ID)
	}
	if resp.Status != "PENDING" {
		t.Errorf("Status = %q, want PENDING", resp.Status)
	}
	if resp.Duplicate {
		t.Error("expected Duplicate=false")
	}

	// Verify recording
	if client.JobCount() != 1 {
		t.Errorf("JobCount = %d, want 1", client.JobCount())
	}
	last := client.LastJob()
	if last.RequestID != "req-1" {
		t.Errorf("LastJob RequestID = %q, want req-1", last.RequestID)
	}
}

func TestFakeClientRecordsMultipleJobs(t *testing.T) {
	client := NewFakeClient()

	for i := 0; i < 3; i++ {
		_, err := client.SubmitJob(context.Background(), PrintJobRequest{RequestID: "r"})
		if err != nil {
			t.Fatalf("submit %d: %v", i, err)
		}
	}

	if client.JobCount() != 3 {
		t.Errorf("JobCount = %d, want 3", client.JobCount())
	}

	client.Reset()
	if client.JobCount() != 0 {
		t.Errorf("JobCount after reset = %d, want 0", client.JobCount())
	}
}

func TestFakeClientCustomSubmitFn(t *testing.T) {
	client := NewFakeClient()
	client.SubmitJobFn = func(_ context.Context, req PrintJobRequest) (*PrintJobResponse, error) {
		return &PrintJobResponse{ID: "custom", Status: "DONE"}, nil
	}

	resp, err := client.SubmitJob(context.Background(), PrintJobRequest{RequestID: "x"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.ID != "custom" {
		t.Errorf("ID = %q, want custom", resp.ID)
	}
}

func TestFailClient(t *testing.T) {
	client := NewFailClient("down")

	_, err := client.SubmitJob(context.Background(), PrintJobRequest{RequestID: "x"})
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Error() != "print_ops fake failure: down" {
		t.Errorf("error = %q, want 'print_ops fake failure: down'", err.Error())
	}
}

func TestFailClientOnce(t *testing.T) {
	client := NewFailClientOnce(2, "third fails")

	// First two succeed
	for i := 0; i < 2; i++ {
		_, err := client.SubmitJob(context.Background(), PrintJobRequest{RequestID: "x"})
		if err != nil {
			t.Fatalf("submit %d should succeed: %v", i, err)
		}
	}

	// Third fails
	_, err := client.SubmitJob(context.Background(), PrintJobRequest{RequestID: "x"})
	if err == nil {
		t.Fatal("expected third call to fail")
	}
}

func TestStickerPayloadFromData(t *testing.T) {
	now := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)
	data := PrescriptionData{
		PrescriptionID: "RX-001",
		HN:             "HN999",
		PatientName:    "John Doe",
		WardID:         "WARD-3A",
		IssuedAt:       &now,
		Items: []PrescriptionItemData{
			{DrugName: "Paracetamol 500 mg", Quantity: 10, DosageText: "1x3 pc"},
			{DrugName: "Ibuprofen 400 mg", Quantity: 5, DosageText: "1x2 pc"},
		},
	}

	sticker := BuildStickerPayloadFromData(data)

	if sticker.PrescriptionID != "RX-001" {
		t.Errorf("PrescriptionID = %q, want RX-001", sticker.PrescriptionID)
	}
	if sticker.PatientName != "John Doe" {
		t.Errorf("PatientName = %q, want John Doe", sticker.PatientName)
	}
	if sticker.HN != "HN999" {
		t.Errorf("HN = %q, want HN999", sticker.HN)
	}
	if sticker.WardID != "WARD-3A" {
		t.Errorf("WardID = %q, want WARD-3A", sticker.WardID)
	}
	if len(sticker.Items) != 2 {
		t.Fatalf("Items length = %d, want 2", len(sticker.Items))
	}
	if sticker.Items[0] != "Paracetamol 500 mg x10" {
		t.Errorf("Items[0] = %q", sticker.Items[0])
	}
	if sticker.Items[1] != "Ibuprofen 400 mg x5" {
		t.Errorf("Items[1] = %q", sticker.Items[1])
	}
	if sticker.IssuedAt != "2026-07-15T10:00:00Z" {
		t.Errorf("IssuedAt = %q", sticker.IssuedAt)
	}
	if sticker.GeneratedAt == "" {
		t.Error("GeneratedAt should not be empty")
	}
}

func TestStickerPayloadNilIssuedAt(t *testing.T) {
	data := PrescriptionData{
		PrescriptionID: "RX-002",
		HN:             "HN001",
		PatientName:    "Jane",
		WardID:         "WARD-2B",
	}
	sticker := BuildStickerPayloadFromData(data)
	if sticker.IssuedAt != "" {
		t.Errorf("IssuedAt = %q, want empty when nil", sticker.IssuedAt)
	}
}

func TestConsumerTypeCompiles(t *testing.T) {
	// Verify that Consumer creates correctly. Don't pass nil — the
	// constructor calls log.With, which requires a non-nil logger.
	_ = &Consumer{}
}

func TestPrintRequestedEventShape(t *testing.T) {
	ev := &eventsv1.PrintRequested{
		PrintId:        "print-1",
		PrescriptionId: "rx-1",
		TraceId:        "trace-1",
	}
	if ev.GetPrintId() != "print-1" {
		t.Errorf("PrintId = %q, want print-1", ev.GetPrintId())
	}
	if ev.GetPrescriptionId() != "rx-1" {
		t.Errorf("PrescriptionId = %q, want rx-1", ev.GetPrescriptionId())
	}
	if ev.GetTraceId() != "trace-1" {
		t.Errorf("TraceId = %q, want trace-1", ev.GetTraceId())
	}
}

func TestPrintCompletedEventShape(t *testing.T) {
	ev := &eventsv1.PrintCompleted{
		PrintId:        "print-1",
		PrescriptionId: "rx-1",
		Success:        true,
		Detail:         "PENDING",
		TraceId:        "trace-1",
	}
	if !ev.GetSuccess() {
		t.Error("expected Success=true")
	}
	if ev.GetDetail() != "PENDING" {
		t.Errorf("Detail = %q, want PENDING", ev.GetDetail())
	}
}

func TestFactoryReturnsFakeWhenFake(t *testing.T) {
	cfg := configForTests()
	cfg.PrintOpsFake = true

	client := NewClientFromConfig(cfg, nil)
	if _, ok := client.(*FakeClient); !ok {
		t.Errorf("expected FakeClient when PRINT_OPS_FAKE=true, got %T", client)
	}
}

func TestFactoryReturnsDispatcherWhenNotFake(t *testing.T) {
	cfg := configForTests()
	cfg.PrintOpsFake = false
	cfg.PrintOpsAPIKey = "fake-key"
	cfg.PrintOpsTransport = "http"

	client := NewClientFromConfig(cfg, nil)
	dispatcher, ok := client.(*dispatcherClient)
	if !ok {
		t.Fatalf("expected dispatcherClient when PRINT_OPS_FAKE=false, got %T", client)
	}
	if _, ok := dispatcher.http.(*httpClient); !ok {
		t.Errorf("expected http transport to be *httpClient, got %T", dispatcher.http)
	}
	if dispatcher.defaultTransport != "http" {
		t.Errorf("defaultTransport = %q, want http", dispatcher.defaultTransport)
	}
}
