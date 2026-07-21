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
	if event.DispenseId == "" || event.KioskCode == "" || len(event.Results) == 0 {
		c.log.Warn("dispense.completed: missing routed transaction fields, rejecting")
		c.publishDLQ(ctx, msg)
		_ = msg.Term()
		return nil
	}

	outcomes := make(map[string]bool, len(event.Results))
	for _, result := range event.Results {
		outcomes[result.AllocationId] = result.Success
	}
	tx, err := c.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin completion: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	emergency, err := getEmergencyTransaction(ctx, tx, event.DispenseId, true)
	if err != nil {
		return fmt.Errorf("identify emergency completion: %w", err)
	}
	if emergency != nil {
		record, applied, err := c.store.FinalizeEmergencySuccess(ctx, tx, event.DispenseId, event.KioskCode, event.HardwareResponse, outcomes)
		if err != nil {
			return fmt.Errorf("finalize emergency dispense success: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit emergency completion: %w", err)
		}
		if applied {
			c.writeEmergencyAudit(record, "emergency_dispense.completed", map[string]any{"kiosk_code": event.KioskCode, "hardware_response": event.HardwareResponse})
			c.publishEmergencyStockChanges(ctx, record)
		}
		return nil
	}
	record, applied, err := c.store.FinalizeTransactionSuccess(ctx, tx, event.DispenseId, event.KioskCode, event.HardwareResponse, outcomes)
	if err != nil {
		return fmt.Errorf("finalize dispense success: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit completion: %w", err)
	}
	if !applied {
		return nil
	}
	c.writeTransactionAudit(record, "dispense.completed", map[string]any{"kiosk_code": event.KioskCode, "hardware_response": event.HardwareResponse})
	c.publishStockChanges(ctx, record)
	if c.js != nil {
		printEv := &eventsv1.PrintRequested{PrintId: event.DispenseId, PrescriptionId: event.PrescriptionId, TraceId: event.TraceId, ProjectId: record.ProjectID}
		printPayload, _ := protojson.Marshal(printEv)
		if _, err := c.js.Publish(ctx, natsx.SubjectPrintRequested, printPayload); err != nil {
			c.log.Warn("publish print.requested failed", "error", err.Error())
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
	if event.DispenseId == "" || event.KioskCode == "" {
		c.log.Warn("dispense.failed: missing routed transaction fields, rejecting")
		c.publishDLQ(ctx, msg)
		_ = msg.Term()
		return nil
	}

	outcomes := make(map[string]bool, len(event.Results))
	for _, result := range event.Results {
		outcomes[result.AllocationId] = result.Success
	}
	tx, err := c.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin failure: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	emergency, err := getEmergencyTransaction(ctx, tx, event.DispenseId, true)
	if err != nil {
		return fmt.Errorf("identify emergency failure: %w", err)
	}
	if emergency != nil {
		record, applied, err := c.store.FinalizeEmergencyFailure(ctx, tx, event.DispenseId, event.KioskCode, event.Reason, event.Detail, outcomes)
		if err != nil {
			return fmt.Errorf("finalize emergency dispense failure: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit emergency failure: %w", err)
		}
		if applied {
			c.writeEmergencyAudit(record, "emergency_dispense.failed", map[string]any{"kiosk_code": event.KioskCode, "reason": event.Reason, "detail": event.Detail})
			c.publishEmergencyStockChanges(ctx, record)
		}
		return nil
	}
	c.log.Warn("dispense.failed: marking prescription failed",
		"prescription_id", event.PrescriptionId,
		"reason", event.Reason,
	)
	record, applied, err := c.store.FinalizeTransactionFailure(ctx, tx, event.DispenseId, event.KioskCode, event.Reason, event.Detail, outcomes)
	if err != nil {
		return fmt.Errorf("finalize dispense failure: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit failure: %w", err)
	}
	if !applied {
		return nil
	}
	c.writeTransactionAudit(record, "dispense.failed", map[string]any{"kiosk_code": event.KioskCode, "reason": event.Reason, "detail": event.Detail})
	c.publishStockChanges(ctx, record)
	return nil
}

func (c *CompletionConsumer) writeTransactionAudit(record *TransactionRecord, action string, detail any) {
	if c.audit == nil || record == nil {
		return
	}
	_ = c.audit.Write(context.Background(), audit.Entry{TraceID: record.TraceID, Entity: "dispense_transaction", Action: action, EntityID: record.ID, ProjectID: record.ProjectID, Actor: "system", Detail: detail})
}

func (c *CompletionConsumer) writeEmergencyAudit(record *EmergencyTransactionRecord, action string, detail any) {
	if c.audit == nil || record == nil {
		return
	}
	_ = c.audit.Write(context.Background(), audit.Entry{TraceID: record.TraceID, Entity: "emergency_dispense_transaction", Action: action, EntityID: record.ID, ProjectID: record.ProjectID, Actor: record.EmployeeCode, Detail: detail})
}

func (c *CompletionConsumer) publishStockChanges(ctx context.Context, record *TransactionRecord) {
	if c.js == nil || record == nil {
		return
	}
	for _, item := range record.Items {
		for _, allocation := range item.Allocations {
			if allocation.DispensedQuantity <= 0 {
				continue
			}
			event := &eventsv1.StockChanged{SlotCode: allocation.SlotCode, DrugCode: item.DrugCode, Delta: -allocation.DispensedQuantity, Reason: eventsv1.StockChangeReason_STOCK_CHANGE_REASON_DISPENSE, TraceId: record.TraceID, KioskCode: record.KioskCode, DispenseId: record.ID}
			payload, _ := protojson.Marshal(event)
			if _, err := c.js.Publish(ctx, natsx.SubjectStockChanged, payload); err != nil {
				c.log.Warn("publish stock.changed failed", "error", err.Error())
			}
		}
	}
}

func (c *CompletionConsumer) publishEmergencyStockChanges(ctx context.Context, record *EmergencyTransactionRecord) {
	if c.js == nil || record == nil {
		return
	}
	for _, allocation := range record.Allocations {
		if allocation.DispensedQuantity <= 0 {
			continue
		}
		event := &eventsv1.StockChanged{SlotCode: allocation.SlotCode, DrugCode: record.DrugCode, Delta: -allocation.DispensedQuantity, Reason: eventsv1.StockChangeReason_STOCK_CHANGE_REASON_EMERGENCY_DISPENSE, TraceId: record.TraceID, KioskCode: record.KioskCode, DispenseId: record.ID}
		payload, _ := protojson.Marshal(event)
		if _, err := c.js.Publish(ctx, natsx.SubjectStockChanged, payload); err != nil {
			c.log.Warn("publish emergency stock.changed failed", "error", err.Error())
		}
	}
}

func (c *CompletionConsumer) publishDLQ(ctx context.Context, msg jetstream.Msg) {
	if c.js == nil {
		return
	}
	subject := natsx.SubjectDLQPrefix + msg.Subject()
	_, _ = c.js.Publish(ctx, subject, msg.Data())
}
