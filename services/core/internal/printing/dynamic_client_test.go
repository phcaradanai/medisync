package printing

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/adm-chura3inter/medisync/services/core/internal/platform/config"
)

// TestHTTPClientDynamicPathSubstitution verifies that a path template with
// {{code_template}}/{{code_profile}} placeholders is substituted into the URL
// and that the reduced dynamic body is sent (no template_code field).
func TestHTTPClientDynamicPathSubstitution(t *testing.T) {
	var gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(PrintJobResponse{ID: "job-dyn", Status: "QUEUED"})
	}))
	defer srv.Close()

	client := NewClient(config.Config{
		PrintOpsURL:          srv.URL,
		PrintOpsAPIKey:       "k",
		PrintOpsPathTemplate: "/api/v1/printer/{{code_template}}/{{code_profile}}",
	})

	resp, err := client.SubmitJob(context.Background(), PrintJobRequest{
		RequestID:    "REQ-1",
		SourceSystem: "medisync",
		CodeTemplate: "prescription-sticker",
		CodeProfile:  "sticker-profile",
		Payload:      map[string]any{"a": "b"},
		Copies:       1,
	})
	if err != nil {
		t.Fatalf("SubmitJob: %v", err)
	}
	if resp.ID != "job-dyn" || resp.Status != "QUEUED" {
		t.Errorf("resp = %+v", resp)
	}
	if gotPath != "/api/v1/printer/prescription-sticker/sticker-profile" {
		t.Errorf("path = %q", gotPath)
	}
	if _, hasTemplate := gotBody["template_code"]; hasTemplate {
		t.Errorf("dynamic body must not carry template_code: %v", gotBody)
	}
	if gotBody["request_id"] != "REQ-1" {
		t.Errorf("body.request_id = %v", gotBody["request_id"])
	}
}

// TestHTTPClientLegacyPathUnchanged verifies the default path is preserved and
// the full request body is posted (backward compatible).
func TestHTTPClientLegacyPathUnchanged(t *testing.T) {
	var gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(PrintJobResponse{ID: "job-legacy", Status: "QUEUED"})
	}))
	defer srv.Close()

	client := NewClient(config.Config{PrintOpsURL: srv.URL, PrintOpsAPIKey: "k"})
	if _, err := client.SubmitJob(context.Background(), PrintJobRequest{
		RequestID:    "REQ-2",
		SourceSystem: "medisync",
		PrinterCode:  "sticker-printer",
		TemplateCode: "prescription-sticker",
		Payload:      map[string]any{},
	}); err != nil {
		t.Fatalf("SubmitJob: %v", err)
	}
	if gotPath != "/api/v1/print-jobs" {
		t.Errorf("path = %q, want /api/v1/print-jobs", gotPath)
	}
	if gotBody["printer_code"] != "sticker-printer" || gotBody["template_code"] != "prescription-sticker" {
		t.Errorf("legacy body missing fields: %v", gotBody)
	}
}

// TestHTTPClientDynamicRequiresProfile ensures a dynamic path without a profile
// is rejected before any network call.
func TestHTTPClientDynamicRequiresProfile(t *testing.T) {
	client := NewClient(config.Config{
		PrintOpsURL:          "http://127.0.0.1:0",
		PrintOpsPathTemplate: "/api/v1/printer/{{code_template}}/{{code_profile}}",
	})
	_, err := client.SubmitJob(context.Background(), PrintJobRequest{
		RequestID:    "REQ-3",
		SourceSystem: "medisync",
		CodeTemplate: "t",
		// CodeProfile intentionally empty
		Payload: map[string]any{},
	})
	if err == nil {
		t.Fatal("expected error when code_profile is missing")
	}
}

// TestDispatcherRoutesPerRequestTransport verifies the per-request Transport
// override wins over the configured default.
func TestDispatcherRoutesPerRequestTransport(t *testing.T) {
	httpFake := NewFakeClient()
	natsFake := NewFakeClient()
	d := &dispatcherClient{http: httpFake, nats: natsFake, defaultTransport: "http"}

	// Default -> http.
	if _, err := d.SubmitJob(context.Background(), PrintJobRequest{RequestID: "a"}); err != nil {
		t.Fatalf("default submit: %v", err)
	}
	// Per-request override -> nats.
	if _, err := d.SubmitJob(context.Background(), PrintJobRequest{RequestID: "b", Transport: "nats"}); err != nil {
		t.Fatalf("nats submit: %v", err)
	}

	if httpFake.JobCount() != 1 || httpFake.LastJob().RequestID != "a" {
		t.Errorf("http fake got %d jobs, last=%q", httpFake.JobCount(), httpFake.LastJob().RequestID)
	}
	if natsFake.JobCount() != 1 || natsFake.LastJob().RequestID != "b" {
		t.Errorf("nats fake got %d jobs, last=%q", natsFake.JobCount(), natsFake.LastJob().RequestID)
	}
}

// TestNATSClientNilConnectionFailsFast documents that a NATS submission without
// a JetStream connection returns an error instead of panicking.
func TestNATSClientNilConnectionFailsFast(t *testing.T) {
	c := newNATSClient(nil, "medisync.print.intake")
	if _, err := c.SubmitJob(context.Background(), PrintJobRequest{
		RequestID: "x", SourceSystem: "medisync", CodeTemplate: "t", CodeProfile: "p",
	}); err == nil {
		t.Fatal("expected error with nil JetStream connection")
	}
}
