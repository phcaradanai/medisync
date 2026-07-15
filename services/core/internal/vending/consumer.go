package vending

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"

	eventsv1 "github.com/adm-chura3inter/medisync/services/core/internal/gen/medisync/events/v1"
	"github.com/adm-chura3inter/medisync/services/core/internal/platform/audit"
	"github.com/adm-chura3inter/medisync/services/core/internal/platform/natsx"
)

// Consumer subscribes to medisync.dispense.requested, calls the vending
// agent, and publishes medisync.dispense.completed or .failed.
type Consumer struct {
	js     jetstream.JetStream
	client Client
	audit  *audit.Writer
	log    *slog.Logger
}

// NewConsumer creates a vending consumer.
func NewConsumer(js jetstream.JetStream, client Client, aw *audit.Writer, log *slog.Logger) *Consumer {
	return &Consumer{
		js:     js,
		client: client,
		audit:  aw,
		log:    log,
	}
}

// Start creates a durable JetStream consumer and begins processing.
func (c *Consumer) Start(ctx context.Context) (stop func(), err error) {
	consumer, err := c.js.CreateOrUpdateConsumer(ctx, natsx.StreamMedisync, jetstream.ConsumerConfig{
		Durable:       "core-vending",
		FilterSubject: natsx.SubjectDispenseRequested,
		AckPolicy:     jetstream.AckExplicitPolicy,
		MaxDeliver:    3,
		BackOff:       []time.Duration{2 * time.Second, 5 * time.Second, 15 * time.Second},
	})
	if err != nil {
		return nil, fmt.Errorf("create vending consumer: %w", err)
	}

	consumeCtx, cancelConsume := context.WithCancel(context.Background())
	cc, err := consumer.Consume(func(msg jetstream.Msg) {
		if err := c.handle(consumeCtx, msg); err != nil {
			c.log.Error("vending consumer: error", "error", err.Error())
			_ = msg.Nak()
			return
		}
		_ = msg.Ack()
	})
	if err != nil {
		cancelConsume()
		return nil, fmt.Errorf("vending consumer subscribe: %w", err)
	}

	return func() {
		cancelConsume()
		cc.Stop()
	}, nil
}

func (c *Consumer) handle(ctx context.Context, msg jetstream.Msg) error {
	var event eventsv1.DispenseRequested
	if err := protojson.Unmarshal(msg.Data(), &event); err != nil {
		c.log.Warn("vending consumer: malformed event", "error", err.Error())
		c.publishDLQ(ctx, msg)
		_ = msg.Term()
		return nil
	}

	c.log.Info("vending consumer: dispense requested",
		"dispense_id", event.DispenseId,
		"prescription_id", event.PrescriptionId,
		"trace_id", event.TraceId,
	)

	// Health check before attempting dispense.
	if err := c.client.Health(ctx); err != nil {
		c.log.Warn("vending consumer: agent unhealthy, retrying", "error", err.Error())
		return fmt.Errorf("vending agent unhealthy: %w", err)
	}

	// Build a minimal dispense request. In M3 the consumer does not
	// resolve slots—it uses fake defaults. Production will resolve
	// slots from the inventory module (M6).
	vendingReq := DispenseRequest{
		Prescription: event.PrescriptionId,
		DoorNo:       1,
		Items: []DispenseItem{
			{Layer: 1, ChannelStart: 1, ChannelEnd: 1, Quantity: 1},
		},
	}

	resp, err := c.client.Dispense(ctx, vendingReq)
	if err != nil {
		c.log.Error("vending consumer: agent call failed",
			"dispense_id", event.DispenseId,
			"error", err.Error(),
		)
		c.writeAudit(event.DispenseId, "dispense.failed",
			fmt.Sprintf(`{"reason":"agent_unreachable","error":"%s"}`, err.Error()))
		return c.publishFailed(ctx, &event, "agent_unreachable", err.Error())
	}

	if !resp.OK || resp.Data.Status != "success" {
		reason := "hw_failed"
		detail := fmt.Sprintf("status=%q", resp.Data.Status)
		for _, s := range resp.Data.Steps {
			if !s.Success {
				reason = fmt.Sprintf("hw_step_%s", s.Phase)
				detail = fmt.Sprintf("step %q failed", s.Phase)
				break
			}
		}
		c.log.Warn("vending consumer: dispense failed",
			"dispense_id", event.DispenseId, "reason", reason,
		)
		c.writeAudit(event.DispenseId, "dispense.failed",
			fmt.Sprintf(`{"reason":"%s","detail":"%s"}`, reason, detail))
		return c.publishFailed(ctx, &event, reason, detail)
	}

	c.log.Info("vending consumer: dispense completed",
		"dispense_id", event.DispenseId,
	)
	c.writeAudit(event.DispenseId, "dispense.completed", "{}")
	return c.publishCompleted(ctx, &event)
}

func (c *Consumer) publishCompleted(ctx context.Context, ev *eventsv1.DispenseRequested) error {
	if c.js == nil {
		return fmt.Errorf("publish dispense.completed: no jetstream context")
	}
	completed := &eventsv1.DispenseCompleted{
		DispenseId:     ev.DispenseId,
		PrescriptionId: ev.PrescriptionId,
		SlotCode:       ev.SlotCode,
		Quantity:       ev.Quantity,
		CompletedAt:    timestamppb.Now(),
		TraceId:        ev.TraceId,
	}
	payload, err := protojson.Marshal(completed)
	if err != nil {
		return fmt.Errorf("marshal dispense.completed: %w", err)
	}
	_, err = c.js.Publish(ctx, natsx.SubjectDispenseCompleted, payload)
	if err != nil {
		return fmt.Errorf("publish dispense.completed: %w", err)
	}
	return nil
}

func (c *Consumer) publishFailed(ctx context.Context, ev *eventsv1.DispenseRequested, reason, detail string) error {
	if c.js == nil {
		return fmt.Errorf("publish dispense.failed: no jetstream context")
	}
	failed := &eventsv1.DispenseFailed{
		DispenseId:     ev.DispenseId,
		PrescriptionId: ev.PrescriptionId,
		SlotCode:       ev.SlotCode,
		Reason:         reason,
		Detail:         detail,
		TraceId:        ev.TraceId,
	}
	payload, err := protojson.Marshal(failed)
	if err != nil {
		return fmt.Errorf("marshal dispense.failed: %w", err)
	}
	_, err = c.js.Publish(ctx, natsx.SubjectDispenseFailed, payload)
	if err != nil {
		return fmt.Errorf("publish dispense.failed: %w", err)
	}
	return nil
}

func (c *Consumer) publishDLQ(ctx context.Context, msg jetstream.Msg) {
	if c.js == nil {
		c.log.Warn("cannot publish DLQ: no jetstream context")
		return
	}
	subject := natsx.SubjectDLQPrefix + msg.Subject()
	if _, err := c.js.Publish(ctx, subject, msg.Data()); err != nil {
		c.log.Error("vending consumer: dlq publish failed", "error", err.Error())
	}
}

func (c *Consumer) writeAudit(dispenseID, action, detail string) {
	if c.audit == nil {
		return
	}
	_ = c.audit.Write(context.Background(), audit.Entry{
		Entity:   "vending",
		Action:   action,
		EntityID: dispenseID,
		Actor:    "system",
		Detail:   detail,
	})
}
