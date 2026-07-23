package printing

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/nats-io/nats.go/jetstream"
)

// natsClient submits print jobs to print_ops over NATS JetStream by publishing
// a print-intake envelope. print_ops runs a durable consumer on the subject and
// creates the job through the same path as the HTTP endpoint.
//
// Submission is fire-and-forget from the caller's perspective: JetStream
// acknowledges the publish (durability), but the print_ops job id is assigned
// asynchronously and is not returned here.
type natsClient struct {
	js      jetstream.JetStream
	subject string
}

// printIntakeEnvelope is the wire contract consumed by print_ops. It mirrors the
// TypeScript PrintIntakeEnvelope: code_template + code_profile identify the
// binding; printer_code is an optional override.
type printIntakeEnvelope struct {
	RequestID       string         `json:"request_id"`
	SourceSystem    string         `json:"source_system"`
	SourceReference string         `json:"source_reference,omitempty"`
	CodeTemplate    string         `json:"code_template,omitempty"`
	CodeProfile     string         `json:"code_profile,omitempty"`
	PrinterCode     string         `json:"printer_code,omitempty"`
	Payload         map[string]any `json:"payload"`
	Copies          int            `json:"copies,omitempty"`
	Metadata        map[string]any `json:"metadata,omitempty"`
}

func newNATSClient(js jetstream.JetStream, subject string) *natsClient {
	return &natsClient{js: js, subject: subject}
}

func (c *natsClient) SubmitJob(ctx context.Context, req PrintJobRequest) (*PrintJobResponse, error) {
	if c.js == nil {
		return nil, fmt.Errorf("nats print transport unavailable: no JetStream connection")
	}

	codeTemplate := req.CodeTemplate
	if codeTemplate == "" {
		codeTemplate = req.TemplateCode
	}
	if req.PrinterCode == "" && (codeTemplate == "" || req.CodeProfile == "") {
		return nil, fmt.Errorf(
			"nats print requires code_template and code_profile (or printer_code): template=%q profile=%q",
			codeTemplate, req.CodeProfile,
		)
	}

	env := printIntakeEnvelope{
		RequestID:       req.RequestID,
		SourceSystem:    req.SourceSystem,
		SourceReference: req.SourceReference,
		CodeTemplate:    codeTemplate,
		CodeProfile:     req.CodeProfile,
		PrinterCode:     req.PrinterCode,
		Payload:         req.Payload,
		Copies:          req.Copies,
		Metadata:        req.Metadata,
	}
	data, err := json.Marshal(env)
	if err != nil {
		return nil, fmt.Errorf("marshal print intake envelope: %w", err)
	}

	// MsgId enables JetStream publish-level dedup on the same request_id, in
	// addition to print_ops' own request_id idempotency.
	if _, err := c.js.Publish(ctx, c.subject, data, jetstream.WithMsgID(req.RequestID)); err != nil {
		return nil, fmt.Errorf("publish print intake: %w", err)
	}

	// The job id is assigned asynchronously by print_ops; report acceptance.
	return &PrintJobResponse{ID: "", Status: "ACCEPTED_ASYNC", Duplicate: false}, nil
}

// Ensure natsClient satisfies Client.
var _ Client = (*natsClient)(nil)

// dispatcherClient routes each SubmitJob to the HTTP or NATS transport based on
// the per-request override (PrintJobRequest.Transport) or the configured
// default.
type dispatcherClient struct {
	http             Client
	nats             Client
	defaultTransport string
}

func (d *dispatcherClient) SubmitJob(ctx context.Context, req PrintJobRequest) (*PrintJobResponse, error) {
	transport := req.Transport
	if transport == "" {
		transport = d.defaultTransport
	}
	switch transport {
	case "nats":
		return d.nats.SubmitJob(ctx, req)
	case "http", "":
		return d.http.SubmitJob(ctx, req)
	default:
		return nil, fmt.Errorf("unknown print transport %q", transport)
	}
}

// Ensure dispatcherClient satisfies Client.
var _ Client = (*dispatcherClient)(nil)
