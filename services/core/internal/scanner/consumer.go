package scanner

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/adm-chura3inter/medisync/services/core/internal/platform/natsx"
)

const durableName = "core-kiosk-scanner-read"

type Consumer struct {
	js     jetstream.JetStream
	broker *Broker
	log    *slog.Logger
}

func NewConsumer(js jetstream.JetStream, broker *Broker, log *slog.Logger) *Consumer {
	return &Consumer{
		js:     js,
		broker: broker,
		log:    log.With("component", "scanner.consumer"),
	}
}

func (c *Consumer) Start(ctx context.Context) (stop func(), err error) {
	consumer, err := c.js.CreateOrUpdateConsumer(ctx, natsx.StreamMedisync, jetstream.ConsumerConfig{
		Durable:       durableName,
		FilterSubject: natsx.SubjectScannerRead,
		// Scanner reads are user actions, not a work queue to replay into a
		// later kiosk session. JetStream still retains the raw event for audit,
		// while the live consumer accepts only reads after it is available.
		DeliverPolicy: jetstream.DeliverNewPolicy,
		AckPolicy:     jetstream.AckExplicitPolicy,
		MaxDeliver:    5,
		BackOff:       []time.Duration{time.Second, 3 * time.Second, 10 * time.Second, 30 * time.Second},
	})
	if err != nil {
		return nil, fmt.Errorf("create scanner consumer: %w", err)
	}
	c.log.Info("consuming", "stream", natsx.StreamMedisync, "subject", natsx.SubjectScannerRead, "durable", durableName)

	consumeCtx, cancel := context.WithCancel(context.Background())
	cc, err := consumer.Consume(func(msg jetstream.Msg) {
		event, decodeErr := Decode(msg.Data())
		if decodeErr != nil {
			// Bad device data cannot become valid by retrying; acknowledge it and
			// leave the original bytes available in the JetStream audit trail.
			c.log.Warn("scanner event rejected", "error", decodeErr.Error())
			_ = msg.Ack()
			return
		}
		c.broker.Publish(event)
		c.log.Info("scanner event routed", "event_id", event.EventID, "kiosk_code", event.KioskCode, "kind", event.Kind)
		_ = msg.Ack()
	})
	if err != nil {
		cancel()
		return nil, fmt.Errorf("consume scanner events: %w", err)
	}

	done := make(chan struct{})
	go func() {
		<-consumeCtx.Done()
		cc.Stop()
		close(done)
	}()
	return func() {
		cancel()
		<-done
	}, nil
}
