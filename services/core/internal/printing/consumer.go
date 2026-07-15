package printing

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"google.golang.org/protobuf/encoding/protojson"

	eventsv1 "github.com/adm-chura3inter/medisync/services/core/internal/gen/medisync/events/v1"
	"github.com/adm-chura3inter/medisync/services/core/internal/platform/audit"
	"github.com/adm-chura3inter/medisync/services/core/internal/platform/natsx"
)

const consumeTimeout = 15 * time.Second
const consumerDurable = "core-printing"

// Consumer drains medisync.print.requested from JetStream, builds a sticker,
// submits it to print_ops, writes an audit log, and publishes
// medisync.print.completed. Poison messages go to the DLQ; transient failures
// (DB down, print_ops unreachable) are NAKed for redelivery.
type Consumer struct {
	js     jetstream.JetStream
	client Client
	audit  *audit.Writer
	log    *slog.Logger
}

func NewConsumer(js jetstream.JetStream, client Client, auditw *audit.Writer, log *slog.Logger) *Consumer {
	return &Consumer{
		js:     js,
		client: client,
		audit:  auditw,
		log:    log.With("component", "printing.consumer"),
	}
}

// Start registers the durable consumer and begins consuming. The returned
// stop function drains in-flight handlers. Graceful shutdown callers: call
// stop() before draining NATS.
func (c *Consumer) Start(ctx context.Context) (func(), error) {
	cons, err := c.js.CreateOrUpdateConsumer(ctx, natsx.StreamMedisync, jetstream.ConsumerConfig{
		Durable:       consumerDurable,
		FilterSubject: natsx.SubjectPrintRequested,
		AckPolicy:     jetstream.AckExplicitPolicy,
		MaxDeliver:    5,
		BackOff:       []time.Duration{2 * time.Second, 5 * time.Second, 15 * time.Second, 30 * time.Second},
	})
	if err != nil {
		return nil, fmt.Errorf("create consumer %s: %w", consumerDurable, err)
	}

	cctx, err := cons.Consume(c.handle)
	if err != nil {
		return nil, fmt.Errorf("start consuming: %w", err)
	}

	c.log.Info("consuming", "stream", natsx.StreamMedisync, "subject", natsx.SubjectPrintRequested)
	return cctx.Drain, nil
}

func (c *Consumer) handle(msg jetstream.Msg) {
	ctx, cancel := context.WithTimeout(context.Background(), consumeTimeout)
	defer cancel()

	var ev eventsv1.PrintRequested
	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(msg.Data(), &ev); err != nil {
		c.reject(ctx, msg, fmt.Sprintf("malformed payload: %v", err))
		return
	}

	printID := ev.GetPrintId()
	prescriptionID := ev.GetPrescriptionId()
	traceID := ev.GetTraceId()

	if printID == "" || prescriptionID == "" {
		c.reject(ctx, msg, "print_id and prescription_id are required")
		return
	}

	// Build sticker payload from the event data.
	// The PrintRequested event carries the prescription info needed for sticker rendering.
	sticker := StickerPayload{
		PrescriptionID: prescriptionID,
		GeneratedAt:    time.Now().Format(time.RFC3339),
	}

	// Submit the print job to print_ops.
	// request_id is derived from print_id for idempotency (matches print_ops contract).
	jobReq := PrintJobRequest{
		RequestID:   printID,
		SourceSystem: "medisync",
		SourceReference: prescriptionID,
		PrinterCode: "sticker-printer",
		TemplateCode: "prescription-sticker",
		Payload: map[string]any{
			"prescription_id": sticker.PrescriptionID,
			"patient_name":    sticker.PatientName,
			"hn":              sticker.HN,
			"ward_id":         sticker.WardID,
			"items":           sticker.Items,
			"issued_at":       sticker.IssuedAt,
			"generated_at":    sticker.GeneratedAt,
		},
		Copies: 1,
	}

	jobResp, err := c.client.SubmitJob(ctx, jobReq)
	if err != nil {
		// Transient (print_ops unreachable): redeliver with backoff.
		c.log.Error("print_ops submit failed, will retry",
			"print_id", printID,
			"prescription_id", prescriptionID,
			"error", err.Error(),
		)
		msg.Nak()
		return
	}

	c.log.Info("print job submitted",
		"print_id", printID,
		"prescription_id", prescriptionID,
		"job_id", jobResp.ID,
		"status", jobResp.Status,
		"trace_id", traceID,
	)

	// Write audit entry for the print job submission.
	if err := c.audit.Write(ctx, audit.Entry{
		TraceID:  traceID,
		Action:   "print.requested",
		Entity:   "print_job",
		EntityID: printID,
		Detail: map[string]any{
			"prescription_id": prescriptionID,
			"print_ops_job_id": jobResp.ID,
			"status":           jobResp.Status,
		},
	}); err != nil {
		c.log.Error("audit write failed, will retry",
			"print_id", printID,
			"error", err.Error(),
		)
		msg.Nak()
		return
	}

	// Publish medisync.print.completed event.
	completed := &eventsv1.PrintCompleted{
		PrintId:        printID,
		PrescriptionId: prescriptionID,
		Success:        true,
		Detail:         jobResp.Status,
		TraceId:        traceID,
	}
	completedPayload, err := protojson.Marshal(completed)
	if err != nil {
		c.log.Error("marshal print completed event failed, will retry",
			"print_id", printID,
			"error", err.Error(),
		)
		msg.Nak()
		return
	}

	if _, err := c.js.Publish(ctx, natsx.SubjectPrintCompleted, completedPayload); err != nil {
		c.log.Error("publish print completed failed, will retry",
			"print_id", printID,
			"error", err.Error(),
		)
		msg.Nak()
		return
	}

	c.log.Info("print completed published",
		"print_id", printID,
		"prescription_id", prescriptionID,
		"trace_id", traceID,
	)
	msg.Ack()
}

// reject routes a poison message to the DLQ and terminates it.
func (c *Consumer) reject(ctx context.Context, msg jetstream.Msg, reason string) {
	c.log.Warn("rejecting print event", "reason", reason)

	dlqSubject := natsx.SubjectDLQPrefix + msg.Subject()
	if _, err := c.js.Publish(ctx, dlqSubject, msg.Data()); err != nil {
		c.log.Error("dlq publish failed, will retry", "error", err.Error())
		msg.Nak()
		return
	}

	if err := c.audit.Write(ctx, audit.Entry{
		Action: "print.rejected",
		Entity: "print_job",
		Detail: map[string]any{"reason": reason, "dlq_subject": dlqSubject},
	}); err != nil {
		c.log.Error("audit write failed for rejection", "error", err.Error())
	}
	msg.Term()
}
