package dispensing

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"google.golang.org/protobuf/encoding/protojson"

	eventsv1 "github.com/adm-chura3inter/medisync/services/core/internal/gen/medisync/events/v1"
	"github.com/adm-chura3inter/medisync/services/core/internal/platform/natsx"
)

// DispenseRequestedConsumer bridges the gap between medisync.dispense.requested
// (published by the outbox after Dispense API) and medisync.fulfillment.requested
// (consumed by the vending consumer). It is a simple coordinator that fans out
// the dispense request to the fulfillment pipeline.
type DispenseRequestedConsumer struct {
	js  jetstream.JetStream
	log *slog.Logger
}

// NewDispenseRequestedConsumer creates a coordinator consumer.
func NewDispenseRequestedConsumer(js jetstream.JetStream, log *slog.Logger) *DispenseRequestedConsumer {
	return &DispenseRequestedConsumer{
		js:  js,
		log: log.With("component", "dispensing.dispense-requested-consumer"),
	}
}

// Start creates a durable consumer on medisync.dispense.requested and
// bridges each message to medisync.fulfillment.requested.
func (c *DispenseRequestedConsumer) Start(ctx context.Context) (stop func(), err error) {
	consumer, err := c.js.CreateOrUpdateConsumer(ctx, natsx.StreamMedisync, jetstream.ConsumerConfig{
		Durable:       "core-dispensing-dispense-requested",
		FilterSubject: natsx.SubjectDispenseRequested,
		AckPolicy:     jetstream.AckExplicitPolicy,
		MaxDeliver:    5,
		BackOff:       []time.Duration{1 * time.Second, 3 * time.Second, 10 * time.Second, 30 * time.Second},
	})
	if err != nil {
		return nil, fmt.Errorf("create dispense.requested consumer: %w", err)
	}

	consumeCtx, cancelConsume := context.WithCancel(context.Background())

	cc, err := consumer.Consume(func(msg jetstream.Msg) {
		if err := c.handle(consumeCtx, msg); err != nil {
			c.log.Error("dispense.requested consumer error", "error", err.Error())
			_ = msg.Nak()
			return
		}
		_ = msg.Ack()
	})
	if err != nil {
		cancelConsume()
		return nil, fmt.Errorf("subscribe dispense.requested: %w", err)
	}

	doneCh := make(chan struct{})
	var died bool

	go func() {
		<-consumeCtx.Done()
		died = true
		cc.Stop()
		doneCh <- struct{}{}
	}()

	return func() {
		cancelConsume()
		if !died {
			<-doneCh
		}
	}, nil
}

func (c *DispenseRequestedConsumer) handle(ctx context.Context, msg jetstream.Msg) error {
	var ev eventsv1.DispenseRequested
	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(msg.Data(), &ev); err != nil {
		c.log.Warn("dispense.requested: malformed event, rejecting", "error", err.Error())
		return nil // ack to drop
	}

	c.log.Info("dispense.requested: forwarding to fulfillment",
		"prescription_id", ev.PrescriptionId,
		"dispense_id", ev.DispenseId,
		"slot_code", ev.SlotCode,
	)

	// Publish fulfillment.requested for the vending consumer.
	fulfillEv := &eventsv1.FulfillmentRequested{
		FulfillmentId:  ev.DispenseId,
		PrescriptionId: ev.PrescriptionId,
		TraceId:        ev.TraceId,
	}
	payload, err := protojson.Marshal(fulfillEv)
	if err != nil {
		return fmt.Errorf("marshal fulfillment.requested: %w", err)
	}

	if _, err := c.js.Publish(ctx, natsx.SubjectFulfillmentRequested, payload); err != nil {
		return fmt.Errorf("publish fulfillment.requested: %w", err)
	}

	c.log.Info("dispense.requested: fulfillment.requested published",
		"fulfillment_id", ev.DispenseId,
		"prescription_id", ev.PrescriptionId,
	)

	return nil
}
