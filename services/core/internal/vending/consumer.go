package vending

import (
	"context"
	"encoding/json"
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

const consumeTimeout = 30 * time.Second
const consumerDurable = "core-fulfillment"

// Consumer subscribes to medisync.fulfillment.requested, calls the vending
// agent, and publishes medisync.fulfillment.completed.
type Consumer struct {
	js      jetstream.JetStream
	router  Router
	tracker TransactionTracker
	audit   *audit.Writer
	log     *slog.Logger
}

type TransactionTracker interface {
	TransactionExecutionStatus(ctx context.Context, dispenseID, kioskCode string) (string, error)
	MarkTransactionStarted(ctx context.Context, dispenseID, kioskCode string) error
	LoadHardwareResults(ctx context.Context, dispenseID, kioskCode string) (map[string]*eventsv1.DispenseAllocationResult, error)
	MarkHardwareAttempt(ctx context.Context, dispenseID, kioskCode, allocationID string) error
	RecordHardwareResult(ctx context.Context, dispenseID, kioskCode string, result *eventsv1.DispenseAllocationResult, responseJSON string) error
}

// NewConsumer creates a fulfillment consumer.
func NewConsumer(js jetstream.JetStream, client Client, aw *audit.Writer, log *slog.Logger) *Consumer {
	return &Consumer{
		js: js, router: singleClientRouter{client: client}, audit: aw,
		log: log.With("component", "fulfillment.consumer"),
	}
}

func NewRoutedConsumer(js jetstream.JetStream, router Router, tracker TransactionTracker, aw *audit.Writer, log *slog.Logger) *Consumer {
	return &Consumer{js: js, router: router, tracker: tracker, audit: aw, log: log.With("component", "fulfillment.consumer")}
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
	kioskCode := ev.GetKioskCode()

	if fulfillmentID == "" || prescriptionID == "" || kioskCode == "" || len(ev.Allocations) == 0 {
		c.reject(ctx, msg, "fulfillment_id, prescription_id, kiosk_code, and allocations are required")
		return
	}

	c.log.Info("fulfillment requested",
		"fulfillment_id", fulfillmentID,
		"prescription_id", prescriptionID,
		"kiosk_code", kioskCode,
		"trace_id", traceID,
	)
	recorded := make(map[string]*eventsv1.DispenseAllocationResult)
	if c.tracker != nil {
		status, err := c.tracker.TransactionExecutionStatus(ctx, fulfillmentID, kioskCode)
		if err != nil {
			c.log.Error("load transaction execution status failed", "dispense_id", fulfillmentID, "error", err.Error())
			msg.Nak()
			return
		}
		if status == "DISPENSED" || status == "FAILED" || status == "CANCELLED" || status == "EXPIRED" {
			msg.Ack()
			return
		}
		if err := c.tracker.MarkTransactionStarted(ctx, fulfillmentID, kioskCode); err != nil {
			c.log.Error("mark transaction started failed", "dispense_id", fulfillmentID, "error", err.Error())
			msg.Nak()
			return
		}
		loaded, loadErr := c.tracker.LoadHardwareResults(ctx, fulfillmentID, kioskCode)
		if loadErr != nil {
			c.log.Error("load durable hardware results failed", "dispense_id", fulfillmentID, "error", loadErr.Error())
			msg.Nak()
			return
		}
		recorded = loaded
	}

	needsHardware := false
	for _, allocation := range ev.Allocations {
		if recorded[allocation.AllocationId] == nil {
			needsHardware = true
			break
		}
	}
	var client Client
	if needsHardware {
		var err error
		client, err = c.router.ClientFor(kioskCode)
		if err != nil {
			c.publishFailureAndAck(ctx, msg, ev, "routing_not_configured", err.Error(), nil)
			return
		}
		// Health is checked only when a physical command remains. Redelivery of
		// a fully recorded outcome can be published even if the agent went away.
		if err := client.Health(ctx); err != nil {
			c.publishFailureAndAck(ctx, msg, ev, "agent_unreachable", err.Error(), nil)
			return
		}
	}

	results := make([]*eventsv1.DispenseAllocationResult, 0, len(ev.Allocations))
	hardwareResponses := make([]any, 0, len(ev.Allocations))
	failed := false
	failureReason, failureDetail := "", ""
	for _, allocation := range ev.Allocations {
		if prior := recorded[allocation.AllocationId]; prior != nil {
			results = append(results, prior)
			if !prior.Success {
				failed, failureReason, failureDetail = true, "hardware_failed", prior.Detail
			}
			continue
		}
		if failed {
			results = append(results, &eventsv1.DispenseAllocationResult{AllocationId: allocation.AllocationId, Success: false, Detail: "not attempted after prior hardware failure"})
			continue
		}
		request := DispenseRequest{
			Prescription: fmt.Sprintf("%s:%s", prescriptionID, allocation.AllocationId),
			DoorNo:       int(allocation.DoorNo),
			Items:        []DispenseItem{{Layer: int(allocation.HardwareLayer), ChannelStart: int(allocation.ChannelStart), ChannelEnd: int(allocation.ChannelEnd), Quantity: int(allocation.Quantity)}},
		}
		if c.tracker != nil {
			if err := c.tracker.MarkHardwareAttempt(ctx, fulfillmentID, kioskCode, allocation.AllocationId); err != nil {
				c.log.Error("persist hardware attempt failed before command", "dispense_id", fulfillmentID, "allocation_id", allocation.AllocationId, "error", err.Error())
				msg.Nak()
				return
			}
		}
		response, dispenseErr := client.Dispense(ctx, request)
		if dispenseErr != nil {
			failed, failureReason, failureDetail = true, "agent_error", dispenseErr.Error()
			result := &eventsv1.DispenseAllocationResult{AllocationId: allocation.AllocationId, Success: false, Detail: dispenseErr.Error()}
			results = append(results, result)
			if err := c.recordHardwareResult(ctx, fulfillmentID, kioskCode, result, `{}`); err != nil {
				msg.Nak()
				return
			}
			continue
		}
		hardwareResponses = append(hardwareResponses, response)
		allocationResponse, _ := json.Marshal(response)
		success := response.OK == 1 && response.Data.Status == "success"
		detail := response.Data.Status
		if !success {
			failed, failureReason, failureDetail = true, "hardware_failed", fmt.Sprintf("allocation %s status=%q", allocation.AllocationId, response.Data.Status)
			for _, step := range response.Data.Steps {
				if !step.Success {
					failureReason = "hardware_step_" + step.Phase
					detail = "step " + step.Phase + " failed"
					break
				}
			}
		}
		result := &eventsv1.DispenseAllocationResult{AllocationId: allocation.AllocationId, Success: success, Detail: detail}
		results = append(results, result)
		if err := c.recordHardwareResult(ctx, fulfillmentID, kioskCode, result, string(allocationResponse)); err != nil {
			msg.Nak()
			return
		}
	}
	responseJSON, _ := json.Marshal(hardwareResponses)
	if failed {
		c.publishFailureAndAck(ctx, msg, ev, failureReason, failureDetail, results)
		return
	}

	// Hardware-confirmed success.
	c.log.Info("fulfillment completed",
		"fulfillment_id", fulfillmentID,
	)
	c.writeAudit(fulfillmentID, traceID, "fulfillment.completed", "success")
	if pubErr := c.publishCompleted(ctx, fulfillmentID, prescriptionID, kioskCode, traceID, string(responseJSON), results); pubErr != nil {
		c.log.Error("publish fulfillment.completed failed, will retry",
			"fulfillment_id", fulfillmentID,
			"error", pubErr.Error(),
		)
		msg.Nak()
		return
	}

	msg.Ack()
}

func (c *Consumer) recordHardwareResult(ctx context.Context, dispenseID, kioskCode string, result *eventsv1.DispenseAllocationResult, responseJSON string) error {
	if c.tracker == nil {
		return nil
	}
	if err := c.tracker.RecordHardwareResult(ctx, dispenseID, kioskCode, result, responseJSON); err != nil {
		c.log.Error("persist hardware result failed", "dispense_id", dispenseID, "allocation_id", result.AllocationId, "error", err.Error())
		return err
	}
	return nil
}

func (c *Consumer) publishCompleted(ctx context.Context, fulfillmentID, prescriptionID, kioskCode, traceID, hardwareResponse string, results []*eventsv1.DispenseAllocationResult) error {
	if c.js == nil {
		return fmt.Errorf("no jetstream context")
	}
	completed := &eventsv1.DispenseCompleted{
		DispenseId:       fulfillmentID,
		PrescriptionId:   prescriptionID,
		TraceId:          traceID,
		KioskCode:        kioskCode,
		HardwareResponse: hardwareResponse,
		Results:          results,
		CompletedAt:      timestamppb.Now(),
	}
	payload, err := protojson.Marshal(completed)
	if err != nil {
		return fmt.Errorf("marshal dispense.completed: %w", err)
	}
	_, err = c.js.Publish(ctx, natsx.SubjectDispenseCompleted, payload)
	if err != nil {
		return fmt.Errorf("publish dispense.completed: %w", err)
	}
	c.log.Info("fulfillment completed published",
		"fulfillment_id", fulfillmentID,
		"prescription_id", prescriptionID,
		"success", true,
		"trace_id", traceID,
	)
	return nil
}

func (c *Consumer) publishFailureAndAck(ctx context.Context, msg jetstream.Msg, ev eventsv1.FulfillmentRequested, reason, detail string, results []*eventsv1.DispenseAllocationResult) {
	c.writeAudit(ev.FulfillmentId, ev.TraceId, "fulfillment.failed", fmt.Sprintf("reason=%s detail=%s", reason, detail))
	if c.js == nil {
		c.log.Error("publish dispense.failed failed, will retry", "dispense_id", ev.FulfillmentId, "error", "no jetstream context")
		_ = msg.Nak()
		return
	}
	failed := &eventsv1.DispenseFailed{DispenseId: ev.FulfillmentId, PrescriptionId: ev.PrescriptionId, TraceId: ev.TraceId, KioskCode: ev.KioskCode, Reason: reason, Detail: detail, Results: results}
	payload, err := protojson.Marshal(failed)
	if err == nil {
		_, err = c.js.Publish(ctx, natsx.SubjectDispenseFailed, payload)
	}
	if err != nil {
		c.log.Error("publish dispense.failed failed, will retry", "dispense_id", ev.FulfillmentId, "error", err.Error())
		_ = msg.Nak()
		return
	}
	_ = msg.Ack()
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

	if c.audit != nil {
		if err := c.audit.Write(ctx, audit.Entry{
			Action: "fulfillment.rejected",
			Entity: "fulfillment",
			Detail: map[string]any{"reason": reason, "dlq_subject": dlqSubject},
		}); err != nil {
			c.log.Error("audit write failed for rejection", "error", err.Error())
		}
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
