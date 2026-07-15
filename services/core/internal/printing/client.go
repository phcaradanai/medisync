// Package printing is the anti-corruption layer over print_ops. It owns:
//   - The HTTP client for POST /api/v1/print-jobs (X-Api-Key, idempotent request_id)
//   - A fake client for testing
//   - Sticker payload building from prescription data
//   - The JetStream durable consumer on medisync.print.requested
package printing

import "context"

// PrintJobRequest is the payload sent to POST /api/v1/print-jobs.
type PrintJobRequest struct {
	RequestID       string            `json:"request_id"`
	SourceSystem    string            `json:"source_system"`
	SourceReference string            `json:"source_reference"`
	PrinterCode     string            `json:"printer_code"`
	TemplateCode    string            `json:"template_code"`
	Payload         map[string]any    `json:"payload"`
	Copies          int               `json:"copies"`
	Metadata        map[string]any    `json:"metadata"`
}

// PrintJobResponse is the response from print_ops after job acceptance.
type PrintJobResponse struct {
	ID        string `json:"id"`
	Status    string `json:"status"`
	Duplicate bool   `json:"duplicate"`
}

// Client is the interface for submitting print jobs to print_ops.
// Real: HTTP POST /api/v1/print-jobs. Fake: always-succeed stub for tests.
type Client interface {
	SubmitJob(ctx context.Context, req PrintJobRequest) (*PrintJobResponse, error)
}
