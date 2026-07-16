package vending

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

const consumeTimeout = 30 * time.Second
const consumerDurable = "core-fulfillment"

// Consumer subscribes to medisync.fulfillment.requested, calls the vending
// agent, and publishes medisync.fulfillment.completed.
type Consumer struct {
	js     jetstream.JetStream
	client Client
	audit  *audit.Writer
	log    *slog.Logger
}

// NewConsumer creates a fulfillment consumer.
func NewConsumer(js jetstream.JetStream, client Client, aw *audit.Writer, log *slog.Logger) *Consumer {
	return &Consumer{
		js:     js,
		client: client,
		audit:  aw,
		log:    log.With("component", "fulfillment.consumer"),
	}
}

// Start creates a durable JetStream consumer and begins processing.
func (c *Consumer) Start(ctx context.Context) (func(), error) {
	cons, err := c.js.CreateOrUpdateConsumer(ctx, natsx.StreamMedisync, jetstream.ConsumerConfig{
		Durable:       consumerDurable,
		FilterSubject: natsx.SubjectFulfillmentRequested,
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

	c.log.Info("consuming",
		"stream", natsx.StreamMedisync,
		"subject", natsx.SubjectFulfillmentRequested,
	)
	return cctx.Drain, nil
}

func (c *Consumer) handle(msg jetstream.Msg) {
	ctx, cancel := context.WithTimeout(context.Background(), consumeTimeout)
	defer cancel()

	var ev eventsv1.FulfillmentRequested
	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(msg.Data(), &ev); err != nil {
		c.reject(ctx, msg, fmt.Sprintf("malformed payload: %v", err))
		return
	}

	fulfillmentID := ev.GetFulfillmentId()
	prescriptionID := ev.GetPrescriptionId()
	traceID := ev.GetTraceId()

	if fulfillmentID == "" || prescriptionID == "" {
		c.reject(ctx, msg, "fulfillment_id and prescription_id are required")
		return
	}

	c.log.Info("fulfillment requested",
		"fulfillment_id", fulfillmentID,
		"prescription_id", prescriptionID,
		"trace_id", traceID,
	)

	// Health check before attempting dispense.
	if err := c.client.Health(ctx); err != nil {
		c.log.Warn("vending agent unhealthy, will retry",
			"fulfillment_id", fulfillmentID,
			"error", err.Error(),
		)
		msg.Nak()
		return
	}

	// Build a minimal dispense request. In M3 the consumer does not
	// resolve slots—it uses fake defaults. Production will resolve
	// slots from the inventory module.
	vendingReq := DispenseRequest{
		Prescription: prescriptionID,
		DoorNo:       1,
		Items: []DispenseItem{
			{Layer: 1, ChannelStart: 1, ChannelEnd: 1, Quantity: 1},
		},
	}

	resp, err := c.client.Dispense(ctx, vendingReq)
	if err != nil {
		c.log.Error("vending agent call failed",
			"fulfillment_id", fulfillmentID,
			"error", err.Error(),
		)
		// Publish fulfillment.completed with success=false on permanent failure.
		c.writeAudit(fulfillmentID, traceID, "fulfillment.failed",
			fmt.Sprintf("agent_unreachable: %s", err.Error()))
		if pubErr := c.publishCompleted(ctx, fulfillmentID, prescriptionID, traceID, false,
			fmt.Sprintf("agent_unreachable: %s", err.Error())); pubErr != nil {
			c.log.Error("publish fulfillment.completed failed, will retry",
				"fulfillment_id", fulfillmentID,
				"error", pubErr.Error(),
			)
			msg.Nak()
			return
		}
		msg.Ack()
		return
	}

	if resp.OK != 1 || resp.Data.Status != "success" {
		reason := "hw_failed"
		detail := fmt.Sprintf("status=%q", resp.Data.Status)
		for _, s := range resp.Data.Steps {
			if !s.Success {
				reason = fmt.Sprintf("hw_step_%s", s.Phase)
				detail = fmt.Sprintf("step %q failed", s.Phase)
				break
			}
		}
		c.log.Warn("fulfillment dispense failed",
			"fulfillment_id", fulfillmentID,
			"reason", reason,
		)
		c.writeAudit(fulfillmentID, traceID, "fulfillment.failed",
			fmt.Sprintf("reason=%s detail=%s", reason, detail))
		if pubErr := c.publishCompleted(ctx, fulfillmentID, prescriptionID, traceID, false, detail); pubErr != nil {
			c.log.Error("publish fulfillment.completed failed, will retry",
				"fulfillment_id", fulfillmentID,
				"error", pubErr.Error(),
			)
			msg.Nak()
			return
		}
		msg.Ack()
		return
	}

	// Hardware-confirmed success.
	c.log.Info("fulfillment completed",
		"fulfillment_id", fulfillmentID,
	)
	c.writeAudit(fulfillmentID, traceID, "fulfillment.completed", "success")
	if pubErr := c.publishCompleted(ctx, fulfillmentID, prescriptionID, traceID, true, "dispensed"); pubErr != nil {
		c.log.Error("publish fulfillment.completed failed, will retry",
			"fulfillment_id", fulfillmentID,
			"error", pubErr.Error(),
		)
		msg.Nak()
		return
	}

	msg.Ack()
}

func (c *Consumer) publishCompleted(ctx context.Context, fulfillmentID, prescriptionID, traceID string, success bool, detail string) error {
	if c.js == nil {
		return fmt.Errorf("no jetstream context")
	}
	completed := &eventsv1.FulfillmentCompleted{
		FulfillmentId:  fulfillmentID,
		PrescriptionId: prescriptionID,
		Success:        success,
		Detail:         detail,
		TraceId:        traceID,
	}
	payload, err := protojson.Marshal(completed)
	if err != nil {
		return fmt.Errorf("marshal fulfillment.completed: %w", err)
	}
	_, err = c.js.Publish(ctx, natsx.SubjectFulfillmentCompleted, payload)
	if err != nil {
		return fmt.Errorf("publish fulfillment.completed: %w", err)
	}
	c.log.Info("fulfillment completed published",
		"fulfillment_id", fulfillmentID,
		"prescription_id", prescriptionID,
		"success", success,
		"trace_id", traceID,
	)
	return nil
}

// reject routes a poison message to the DLQ and terminates it.
func (c *Consumer) reject(ctx context.Context, msg jetstream.Msg, reason string) {
	c.log.Warn("rejecting fulfillment event", "reason", reason)

	if c.js == nil {
		c.log.Error("cannot DLQ: no jetstream context")
		msg.Term()
		return
	}

	dlqSubject := natsx.SubjectDLQPrefix + msg.Subject()
	if _, err := c.js.Publish(ctx, dlqSubject, msg.Data()); err != nil {
		c.log.Error("dlq publish failed, will retry", "error", err.Error())
		msg.Nak()
		return
	}

	if err := c.audit.Write(ctx, audit.Entry{
		Action: "fulfillment.rejected",
		Entity: "fulfillment",
		Detail: map[string]any{"reason": reason, "dlq_subject": dlqSubject},
	}); err != nil {
		c.log.Error("audit write failed for rejection", "error", err.Error())
	}
	msg.Term()
}

func (c *Consumer) writeAudit(fulfillmentID, traceID, action, detail string) {
	if c.audit == nil {
		return
	}
	_ = c.audit.Write(context.Background(), audit.Entry{
		TraceID:  traceID,
		Entity:   "fulfillment",
		Action:   action,
		EntityID: fulfillmentID,
		Actor:    "system",
		Detail:   detail,
	})
}
