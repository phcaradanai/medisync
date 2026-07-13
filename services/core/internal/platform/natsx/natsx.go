// Package natsx owns the NATS connection and JetStream stream topology.
package natsx

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

const (
	// StreamRX buffers inbound prescriptions from the hospital feeder.
	// Work-queue retention: exactly one consumer group drains it.
	StreamRX = "RX"
	// StreamMedisync carries all internal domain events, DLQ included.
	StreamMedisync = "MEDISYNC"

	SubjectPrescriptionCreated = "rx.prescription.created"
	SubjectDLQPrefix           = "medisync.dlq."
)

// Connect dials NATS, retrying until ctx expires.
func Connect(ctx context.Context, url string, log *slog.Logger) (*nats.Conn, error) {
	for {
		nc, err := nats.Connect(url,
			nats.Name("medisync-core"),
			nats.MaxReconnects(-1),
			nats.ReconnectWait(2*time.Second),
		)
		if err == nil {
			return nc, nil
		}

		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("nats not reachable before deadline: %w", err)
		case <-time.After(2 * time.Second):
			log.Info("waiting for nats", "error", err.Error())
		}
	}
}

// EnsureStreams declares the stream topology. Idempotent; safe on every boot.
func EnsureStreams(ctx context.Context, js jetstream.JetStream) error {
	_, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:      StreamRX,
		Subjects:  []string{"rx.>"},
		Retention: jetstream.WorkQueuePolicy,
		Storage:   jetstream.FileStorage,
	})
	if err != nil {
		return fmt.Errorf("ensure stream %s: %w", StreamRX, err)
	}

	_, err = js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:      StreamMedisync,
		Subjects:  []string{"medisync.>"},
		Retention: jetstream.LimitsPolicy,
		MaxAge:    7 * 24 * time.Hour,
		Storage:   jetstream.FileStorage,
	})
	if err != nil {
		return fmt.Errorf("ensure stream %s: %w", StreamMedisync, err)
	}

	return nil
}
