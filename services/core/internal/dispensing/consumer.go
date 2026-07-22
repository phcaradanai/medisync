package dispensing

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/nats-io/nats.go/jetstream"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/adm-chura3inter/medisync/services/core/internal/gen/medisync/events/v1"
	"github.com/adm-chura3inter/medisync/services/core/internal/platform/audit"
	"github.com/adm-chura3inter/medisync/services/core/internal/platform/natsx"
)

const consumerDurable = "core-dispensing"

// Consumer drains rx.prescription.created from JetStream into the
// prescription store. Poison messages go to the DLQ subject and are
// terminated; transient failures (DB down) are NAKed for redelivery.
type Consumer struct {
	js    jetstream.JetStream
	store *Store
	audit *audit.Writer
	log   *slog.Logger
}

func NewConsumer(js jetstream.JetStream, store *Store, auditw *audit.Writer, log *slog.Logger) *Consumer {
	return &Consumer{js: js, store: store, audit: auditw, log: log.With("component", "dispensing.consumer")}
}

// Start registers the durable consumer and begins consuming. The returned
// stop function drains in-flight handlers.
func (c *Consumer) Start(ctx context.Context) (func(), error) {
	cons, err := c.js.CreateOrUpdateConsumer(ctx, natsx.StreamRX, jetstream.ConsumerConfig{
		Durable:       consumerDurable,
		FilterSubject: natsx.SubjectPrescriptionCreated,
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

	c.log.Info("consuming", "stream", natsx.StreamRX, "subject", natsx.SubjectPrescriptionCreated)
	return cctx.Drain, nil
}

func (c *Consumer) handle(msg jetstream.Msg) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var ev eventsv1.PrescriptionCreated
	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(msg.Data(), &ev); err != nil {
		c.reject(ctx, msg, fmt.Sprintf("malformed payload: %v", err))
		return
	}
	if reason := validate(&ev); reason != "" {
		c.reject(ctx, msg, reason)
		return
	}
	projectID, err := c.store.ProjectIDByCode(ctx, ev.GetProjectCode())
	if errors.Is(err, pgx.ErrNoRows) {
		c.reject(ctx, msg, fmt.Sprintf("unknown or inactive project_code %q", ev.GetProjectCode()))
		return
	}
	if err != nil {
		c.log.Error("resolve project failed, will retry", "project_code", ev.GetProjectCode(), "error", err.Error())
		msg.Nak()
		return
	}

	p := Prescription{
		PrescriptionID: ev.GetPrescriptionId(),
		SourceSystem:   ev.GetSourceSystem(),
		ProjectID:      projectID,
		HN:             ev.GetHn(),
		PatientName:    ev.GetPatientName(),
		WardID:         ev.GetWardId(),
	}
	for _, it := range ev.GetItems() {
		p.Items = append(p.Items, Item{
			DrugCode:   it.GetDrugCode(),
			DrugName:   it.GetDrugName(),
			Quantity:   it.GetQuantity(),
			DosageText: it.GetDosageText(),
		})
	}
	if ev.GetIssuedAt() != nil {
		t := ev.GetIssuedAt().AsTime()
		p.IssuedAt = &t
	}

	inserted, err := c.store.Insert(ctx, p)
	if err != nil {
		// Transient (likely DB): let JetStream redeliver with backoff.
		c.log.Error("insert failed, will retry", "prescription_id", p.PrescriptionID, "error", err.Error())
		msg.Nak()
		return
	}

	if !inserted {
		c.log.Debug("duplicate prescription event ignored",
			"prescription_id", p.PrescriptionID, "source_system", p.SourceSystem)
		msg.Ack()
		return
	}

	if err := c.audit.Write(ctx, audit.Entry{
		TraceID:   ev.GetTraceId(),
		ProjectID: projectID,
		Action:    "prescription.received",
		Entity:    "prescription",
		EntityID:  p.PrescriptionID,
		Detail:    map[string]any{"ward_id": p.WardID, "items": len(p.Items), "source_system": p.SourceSystem},
	}); err != nil {
		// Audit is mandatory: without it the event is not fully processed.
		c.log.Error("audit write failed, will retry", "prescription_id", p.PrescriptionID, "error", err.Error())
		msg.Nak()
		return
	}

	c.log.Info("prescription received",
		"prescription_id", p.PrescriptionID,
		"project_code", ev.GetProjectCode(),
		"ward_id", p.WardID,
		"items", len(p.Items),
		"trace_id", ev.GetTraceId())
	msg.Ack()
}

// reject routes a poison message to the DLQ and terminates it (no redelivery).
func (c *Consumer) reject(ctx context.Context, msg jetstream.Msg, reason string) {
	c.log.Warn("rejecting prescription event", "reason", reason)

	dlqSubject := natsx.SubjectDLQPrefix + msg.Subject()
	if _, err := c.js.Publish(ctx, dlqSubject, msg.Data()); err != nil {
		// DLQ unreachable is transient; keep the message for redelivery.
		c.log.Error("dlq publish failed, will retry", "error", err.Error())
		msg.Nak()
		return
	}

	if err := c.audit.Write(ctx, audit.Entry{
		Action: "prescription.rejected",
		Entity: "prescription",
		Detail: map[string]any{"reason": reason, "dlq_subject": dlqSubject},
	}); err != nil {
		c.log.Error("audit write failed for rejection", "error", err.Error())
	}
	msg.Term()
}

func validate(ev *eventsv1.PrescriptionCreated) string {
	switch {
	case ev.GetPrescriptionId() == "":
		return "missing prescription_id"
	case ev.GetSourceSystem() == "":
		return "missing source_system"
	case !validProjectCode(ev.GetProjectCode()):
		return "project_code must be exactly 4 non-zero digits"
	case len(ev.GetItems()) == 0:
		return "prescription has no items"
	}
	for i, it := range ev.GetItems() {
		if it.GetDrugCode() == "" {
			return fmt.Sprintf("item %d missing drug_code", i)
		}
		if it.GetQuantity() <= 0 {
			return fmt.Sprintf("item %d quantity must be positive", i)
		}
	}
	return ""
}

func validProjectCode(code string) bool {
	if len(code) != 4 || code == "0000" {
		return false
	}
	for _, ch := range code {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
}
