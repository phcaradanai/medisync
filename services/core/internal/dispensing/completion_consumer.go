package dispensing

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go/jetstream"
	"google.golang.org/protobuf/encoding/protojson"

	eventsv1 "github.com/adm-chura3inter/medisync/services/core/internal/gen/medisync/events/v1"
	"github.com/adm-chura3inter/medisync/services/core/internal/platform/audit"
	"github.com/adm-chura3inter/medisync/services/core/internal/platform/natsx"
)

// CompletionConsumer listens to medisync.dispense.completed and
// medisync.dispense.failed, transitions the prescription state
// accordingly, and publishes stock.changed events.
type CompletionConsumer struct {
	js    jetstream.JetStream
	pool  *pgxpool.Pool
	store *Store
	audit *audit.Writer
	log   *slog.Logger
}

// NewCompletionConsumer creates a consumer that finishes the dispense flow.
func NewCompletionConsumer(js jetstream.JetStream, pool *pgxpool.Pool, store *Store, aw *audit.Writer, log *slog.Logger) *CompletionConsumer {
	return &CompletionConsumer{
		js:    js,
		pool:  pool,
		store: store,
		audit: aw,
		log:   log,
	}
}

// Start creates two durable consumers — one for completed, one for failed.
func (c *CompletionConsumer) Start(ctx context.Context) (stop func(), err error) {
	completedConsumer, err := c.js.CreateOrUpdateConsumer(ctx, natsx.StreamMedisync, jetstream.ConsumerConfig{
		Durable:       "core-dispensing-completed",
		FilterSubject: natsx.SubjectDispenseCompleted,
		AckPolicy:     jetstream.AckExplicitPolicy,
		MaxDeliver:    5,
		BackOff:       []time.Duration{1 * time.Second, 3 * time.Second, 10 * time.Second, 30 * time.Second},
	})
	if err != nil {
		return nil, fmt.Errorf("create dispense.completed consumer: %w", err)
	}

	failedConsumer, err := c.js.CreateOrUpdateConsumer(ctx, natsx.StreamMedisync, jetstream.ConsumerConfig{
		Durable:       "core-dispensing-failed",
		FilterSubject: natsx.SubjectDispenseFailed,
		AckPolicy:     jetstream.AckExplicitPolicy,
		MaxDeliver:    5,
		BackOff:       []time.Duration{1 * time.Second, 3 * time.Second, 10 * time.Second, 30 * time.Second},
	})
	if err != nil {
		return nil, fmt.Errorf("create dispense.failed consumer: %w", err)
	}

	consumeCtx, cancelConsume := context.WithCancel(context.Background())

	doneCh := make(chan struct{}, 1)
	var died bool

	// Consume completed events.
	ccComp, err := completedConsumer.Consume(func(msg jetstream.Msg) {
		if err := c.handleCompleted(consumeCtx, msg); err != nil {
			c.log.Error("dispense.completed consumer error", "error", err.Error())
			_ = msg.Nak()
			return
		}
		_ = msg.Ack()
	})
	if err != nil {
		cancelConsume()
		return nil, fmt.Errorf("subscribe dispense.completed: %w", err)
	}

	// Consume failed events.
	ccFail, err := failedConsumer.Consume(func(msg jetstream.Msg) {
		if err := c.handleFailed(consumeCtx, msg); err != nil {
			c.log.Error("dispense.failed consumer error", "error", err.Error())
			_ = msg.Nak()
			return
		}
		_ = msg.Ack()
	})
	if err != nil {
		cancelConsume()
		ccComp.Stop()
		return nil, fmt.Errorf("subscribe dispense.failed: %w", err)
	}

	go func() {
		<-consumeCtx.Done()
		died = true
		ccComp.Stop()
		ccFail.Stop()
		doneCh <- struct{}{}
	}()

	return func() {
		cancelConsume()
		if !died {
			<-doneCh
		}
	}, nil
}

func (c *CompletionConsumer) handleCompleted(ctx context.Context, msg jetstream.Msg) error {
	var event eventsv1.DispenseCompleted
	if err := protojson.Unmarshal(msg.Data(), &event); err != nil {
		c.log.Warn("dispense.completed: malformed event, rejecting", "error", err.Error())
		c.publishDLQ(ctx, msg)
		_ = msg.Term()
		return nil
	}

	c.log.Info("dispense.completed: finishing prescription",
		"prescription_id", event.PrescriptionId,
		"dispense_id", event.DispenseId,
	)

	// Find the prescription by its external ID, then transition by internal ID.
	pr, err := c.store.GetByPrescriptionID(ctx, event.PrescriptionId, "")
	if err != nil {
		return fmt.Errorf("lookup prescription %s: %w", event.PrescriptionId, err)
	}
	if pr == nil {
		c.log.Warn("dispense.completed: prescription not found",
			"prescription_id", event.PrescriptionId)
		return nil
	}

	// Transition the prescription state in a transaction.
	tx, err := c.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	updated, err := c.store.TransitionState(ctx, tx, pr.ID, StateDispensing, StateDispensed, nil)
	if err != nil {
		return fmt.Errorf("transition DISPENSING→DISPENSED for %s: %w", event.PrescriptionId, err)
	}

	if updated == nil {
		c.log.Warn("dispense.completed: prescription not found or wrong state",
			"prescription_id", event.PrescriptionId)
		return nil
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transition: %w", err)
	}

	// Decrement stock for each item in the prescription.
	if len(pr.Items) > 0 {
		for _, item := range pr.Items {
			if item.DrugCode == "" || item.Quantity <= 0 {
				continue
			}
			tag, err := c.pool.Exec(ctx,
				`UPDATE inventory.slot
				    SET quantity = quantity - $1, updated_at = now()
				  WHERE drug_code = $2 AND quantity >= $1
				  RETURNING id`, item.Quantity, item.DrugCode)
			if err != nil {
				c.log.Warn("stock decrement failed",
					"prescription_id", event.PrescriptionId,
					"drug_code", item.DrugCode,
					"error", err.Error())
			} else if tag.RowsAffected() == 0 {
				c.log.Warn("stock decrement: no matching slot or insufficient stock",
					"prescription_id", event.PrescriptionId,
					"drug_code", item.DrugCode,
					"quantity", item.Quantity)
			} else {
				c.log.Info("stock decremented",
					"prescription_id", event.PrescriptionId,
					"drug_code", item.DrugCode,
					"delta", -item.Quantity)
			}
		}
	}

	c.writeAudit(updated.ID, "dispense.completed", "{}")

	// Publish stock.changed event.
	if c.js != nil {
		stockEv := &eventsv1.StockChanged{
			SlotCode:      event.SlotCode,
			Delta:         -event.Quantity,
			Reason:        eventsv1.StockChangeReason_STOCK_CHANGE_REASON_DISPENSE,
			TraceId:       event.TraceId,
		}
		stockPayload, _ := protojson.Marshal(stockEv)
		if _, err := c.js.Publish(ctx, natsx.SubjectStockChanged, stockPayload); err != nil {
			c.log.Warn("dispense.completed: failed to publish stock.changed",
				"prescription_id", event.PrescriptionId, "error", err.Error())
		}

		// Trigger sticker printing.
		printEv := &eventsv1.PrintRequested{
			PrintId:        event.DispenseId,
			PrescriptionId: event.PrescriptionId,
			TraceId:        event.TraceId,
		}
		printPayload, _ := protojson.Marshal(printEv)
		if _, err := c.js.Publish(ctx, natsx.SubjectPrintRequested, printPayload); err != nil {
			c.log.Warn("dispense.completed: failed to publish print.requested",
				"prescription_id", event.PrescriptionId, "error", err.Error())
		}
	}

	return nil
}

func (c *CompletionConsumer) handleFailed(ctx context.Context, msg jetstream.Msg) error {
	var event eventsv1.DispenseFailed
	if err := protojson.Unmarshal(msg.Data(), &event); err != nil {
		c.log.Warn("dispense.failed: malformed event, rejecting", "error", err.Error())
		c.publishDLQ(ctx, msg)
		_ = msg.Term()
		return nil
	}

	c.log.Warn("dispense.failed: marking prescription failed",
		"prescription_id", event.PrescriptionId,
		"reason", event.Reason,
	)

	// Find the prescription by its external ID, then transition by internal ID.
	pr, err := c.store.GetByPrescriptionID(ctx, event.PrescriptionId, "")
	if err != nil {
		return fmt.Errorf("lookup prescription %s: %w", event.PrescriptionId, err)
	}
	if pr == nil {
		c.log.Warn("dispense.failed: prescription not found",
			"prescription_id", event.PrescriptionId)
		return nil
	}

	// Transition the prescription state in a transaction.
	tx, err := c.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	updated, err := c.store.TransitionState(ctx, tx, pr.ID, StateDispensing, StateFailed, nil)
	if err != nil {
		return fmt.Errorf("transition DISPENSING→FAILED for %s: %w", event.PrescriptionId, err)
	}

	if updated == nil {
		c.log.Warn("dispense.failed: prescription not found or wrong state",
			"prescription_id", event.PrescriptionId)
		return nil
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transition: %w", err)
	}

	c.writeAudit(updated.ID, "dispense.failed",
		fmt.Sprintf(`{"reason":"%s","detail":"%s"}`, event.Reason, event.Detail))

	return nil
}

func (c *CompletionConsumer) writeAudit(entityID, action, detail string) {
	if c.audit == nil {
		return
	}
	_ = c.audit.Write(context.Background(), audit.Entry{
		Entity:   "dispensing",
		Action:   action,
		EntityID: entityID,
		Actor:    "system",
		Detail:   detail,
	})
}

func (c *CompletionConsumer) publishDLQ(ctx context.Context, msg jetstream.Msg) {
	if c.js == nil {
		return
	}
	subject := natsx.SubjectDLQPrefix + msg.Subject()
	_, _ = c.js.Publish(ctx, subject, msg.Data())
}
