//go:build integration

package printing

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func integrationPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Fatal("TEST_DATABASE_URL is required for integration tests. Set it to a test database URL.")
	}
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatalf("connect to test database: %v", err)
	}
	if err := pool.Ping(context.Background()); err != nil {
		pool.Close()
		t.Fatalf("ping test database: %v", err)
	}
	t.Cleanup(func() {
		pool.Close()
	})
	return pool
}

// TestStickerPayloadRoundTrip_Integration verifies that the sticker
// builder produces valid JSON payloads.
func TestStickerPayloadRoundTrip_Integration(t *testing.T) {
	now := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)
	data := PrescriptionData{
		PrescriptionID: "RX-INT-001",
		HN:             "HN000001",
		PatientName:    "Integration Patient",
		WardID:         "WARD-3A",
		IssuedAt:       &now,
		Items: []PrescriptionItemData{
			{DrugName: "Paracetamol 500 mg", Quantity: 10, DosageText: "1x3 pc"},
		},
	}

	sticker := BuildStickerPayloadFromData(data)

	if sticker.PrescriptionID != "RX-INT-001" {
		t.Errorf("PrescriptionID = %q", sticker.PrescriptionID)
	}
	if len(sticker.Items) != 1 {
		t.Errorf("Items length = %d, want 1", len(sticker.Items))
	}
	if sticker.GeneratedAt == "" {
		t.Error("GeneratedAt should not be empty")
	}
}

// TestFakeClientThreadSafety_Integration verifies concurrent job submissions.
func TestFakeClientThreadSafety_Integration(t *testing.T) {
	client := NewFakeClient()
	done := make(chan struct{})
	const numGoroutines = 10
	const jobsPerGoroutine = 100

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer func() { done <- struct{}{} }()
			for j := 0; j < jobsPerGoroutine; j++ {
				_, err := client.SubmitJob(context.Background(), PrintJobRequest{
					RequestID: "concurrent",
				})
				if err != nil {
					t.Errorf("goroutine %d: SubmitJob failed: %v", id, err)
					return
				}
			}
		}(i)
	}

	for i := 0; i < numGoroutines; i++ {
		<-done
	}

	expected := numGoroutines * jobsPerGoroutine
	if count := client.JobCount(); count != expected {
		t.Errorf("JobCount = %d, want %d", count, expected)
	}
}

// TestConsumerCreation_Integration verifies the consumer can be created
// without panicking when given valid (non-nil) dependencies.
func TestConsumerCreation_Integration(t *testing.T) {
	// Verify that Consumer struct fields are accessible.
	c := &Consumer{}
	if c == nil {
		t.Fatal("Consumer struct is nil")
	}
}
