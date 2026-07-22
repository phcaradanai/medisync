package vending

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"

	eventsv1 "github.com/adm-chura3inter/medisync/services/core/internal/gen/medisync/events/v1"
	"github.com/adm-chura3inter/medisync/services/core/internal/platform/audit"
	"github.com/adm-chura3inter/medisync/services/core/internal/platform/natsx"
)

// The hardware call remains open while the pickup compartment waits for the
// user. Keep the consumer context longer than the default vending HTTP client
// timeout so a confirmed pickup can complete normally.
const consumeTimeout = 4 * time.Minute
const consumerDurable = "core-fulfillment"

// Consumer subscribes to medisync.fulfillment.requested, calls the vending
// agent, and publishes medisync.fulfillment.completed.
type Consumer struct {
	js      jetstream.JetStream
	router  Router
	tracker TransactionTracker
	audit   *audit.Writer
	log     *slog.Logger
	locker  *kioskLocker
}

type TransactionTracker interface {
	TransactionExecutionStatus(ctx context.Context, dispenseID, kioskCode string) (string, error)
	MarkTransactionStarted(ctx context.Context, dispenseID, kioskCode string) error
	LoadHardwareResults(ctx context.Context, dispenseID, kioskCode string) (map[string]*eventsv1.DispenseAllocationResult, error)
	MarkHardwareAttempt(ctx context.Context, dispenseID, kioskCode, allocationID string) error
	RecordHardwareResult(ctx context.Context, dispenseID, kioskCode string, result *eventsv1.DispenseAllocationResult, responseJSON string) error
}

// orderAllocationsForDispense protects the physical sequence from database
// insertion order. The lift must pick higher shelves first, then move down;
// the vending agent performs the single delivery/pickup pass after this list.
func orderAllocationsForDispense(allocations []*eventsv1.DispenseAllocation) []*eventsv1.DispenseAllocation {
	ordered := append([]*eventsv1.DispenseAllocation(nil), allocations...)
	sort.SliceStable(ordered, func(i, j int) bool {
		return ordered[i].GetHardwareLayer() > ordered[j].GetHardwareLayer()
	})
	return ordered
}

// allocationResults maps one aggregate vending response back to the
// allocations in the request. The agent echoes allocationId on item steps, so
// a failure in the middle can still report completed lower-level work and
// leave later allocations as not attempted.
func allocationResults(allocations []*eventsv1.DispenseAllocation, response *DispenseResponse) (map[string]*eventsv1.DispenseAllocationResult, bool, string, string) {
	results := make(map[string]*eventsv1.DispenseAllocationResult, len(allocations))
	if response == nil {
		for _, allocation := range allocations {
			results[allocation.AllocationId] = &eventsv1.DispenseAllocationResult{AllocationId: allocation.AllocationId, Success: false, Detail: "empty hardware response"}
		}
		return results, true, "empty_hardware_response", "vending agent returned an empty response"
	}

	overallSuccess := response.OK == 1 && response.Data.Status == "success"
	if overallSuccess {
		for _, allocation := range allocations {
			results[allocation.AllocationId] = &eventsv1.DispenseAllocationResult{AllocationId: allocation.AllocationId, Success: true, Detail: "success"}
		}
		return results, false, "", ""
	}

	firstFailedPhase := ""
	firstFailedIndex := -1
	taggedDispenseSteps := make(map[string]DispenseStep)
	for index, step := range response.Data.Steps {
		if step.Phase == "dispense" && step.AllocationID != "" {
			taggedDispenseSteps[step.AllocationID] = step
		}
		if !step.Success && firstFailedIndex < 0 {
			firstFailedIndex = index
			firstFailedPhase = step.Phase
		}
	}

	// A delivery/pickup failure happens after the tray has been assembled and
	// therefore fails the aggregate transaction safely for every allocation.
	deliveryFailed := firstFailedIndex >= 0 && firstFailedPhase != "lift" && firstFailedPhase != "dispense"
	failureDetail := fmt.Sprintf("hardware status=%q", response.Data.Status)
	if firstFailedPhase != "" {
		failureDetail = "step " + firstFailedPhase + " failed"
	}
	for _, allocation := range allocations {
		step, attempted := taggedDispenseSteps[allocation.AllocationId]
		success := attempted && step.Success && !deliveryFailed
		detail := failureDetail
		if success {
			detail = "success"
		} else if deliveryFailed {
			detail = failureDetail
		} else if !attempted && firstFailedIndex >= 0 {
			detail = "not attempted after prior hardware failure"
		}
		results[allocation.AllocationId] = &eventsv1.DispenseAllocationResult{AllocationId: allocation.AllocationId, Success: success, Detail: detail}
	}
	return results, true, "hardware_failed", failureDetail
}

// NewConsumer creates a fulfillment consumer.
func NewConsumer(js jetstream.JetStream, client Client, aw *audit.Writer, log *slog.Logger) *Consumer {
	return &Consumer{
		js: js, router: singleClientRouter{client: client}, audit: aw,
		locker: newKioskLocker(),
		log:    log.With("component", "fulfillment.consumer"),
	}
}

func NewRoutedConsumer(js jetstream.JetStream, router Router, tracker TransactionTracker, aw *audit.Writer, log *slog.Logger) *Consumer {
	return &Consumer{js: js, router: router, tracker: tracker, audit: aw, locker: newKioskLocker(), log: log.With("component", "fulfillment.consumer")}
}

// Start creates a durable JetStream consumer and begins processing.
func (c *Consumer) Start(ctx context.Context) (func(), error) {
	cons, err := c.js.CreateOrUpdateConsumer(ctx, natsx.StreamMedisync, jetstream.ConsumerConfig{
		Durable:       consumerDurable,
		FilterSubject: natsx.SubjectFulfillmentRequested,
		AckPolicy:     jetstream.AckExplicitPolicy,
		// A message stays in-flight while the cabinet waits for the user to
		// remove the item. Keep JetStream from redelivering the same Sticker
		// before the physical pickup confirmation has completed.
		AckWait:    consumeTimeout + time.Minute,
		MaxDeliver: 5,
		BackOff:    []time.Duration{2 * time.Second, 5 * time.Second, 15 * time.Second, 30 * time.Second},
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
	var ev eventsv1.FulfillmentRequested
	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(msg.Data(), &ev); err != nil {
		c.reject(context.Background(), msg, fmt.Sprintf("malformed payload: %v", err))
		return
	}

	fulfillmentID := ev.GetFulfillmentId()
	prescriptionID := ev.GetPrescriptionId()
	traceID := ev.GetTraceId()
	kioskCode := ev.GetKioskCode()

	if fulfillmentID == "" || prescriptionID == "" || kioskCode == "" || len(ev.Allocations) == 0 {
		c.reject(context.Background(), msg, "fulfillment_id, prescription_id, kiosk_code, and allocations are required")
		return
	}

	c.log.Info("fulfillment requested",
		"fulfillment_id", fulfillmentID,
		"prescription_id", prescriptionID,
		"kiosk_code", kioskCode,
		"trace_id", traceID,
	)
	if c.locker != nil {
		unlock := c.locker.lock(kioskCode)
		defer unlock()
	}
	// Start the per-request deadline after the kiosk lock is acquired. A
	// Sticker waiting behind a previous pickup confirmation must still receive
	// the full hardware timeout once its cabinet is available.
	ctx, cancel := context.WithTimeout(context.Background(), consumeTimeout)
	defer cancel()
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

	orderedAllocations := orderAllocationsForDispense(ev.Allocations)
	pending := make([]*eventsv1.DispenseAllocation, 0, len(orderedAllocations))
	for _, allocation := range orderedAllocations {
		if recorded[allocation.AllocationId] == nil {
			pending = append(pending, allocation)
		}
	}
	var client Client
	if len(pending) > 0 {
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

	resultsByAllocation := make(map[string]*eventsv1.DispenseAllocationResult, len(orderedAllocations))
	hardwareResponses := make([]any, 0, 1)
	failed := false
	failureReason, failureDetail := "", ""
	if len(pending) > 0 {
		// Mark every allocation before the single physical command. If the
		// process dies after the command, redelivery fails closed rather than
		// dispensing an unrecorded item a second time.
		if c.tracker != nil {
			for _, allocation := range pending {
				if err := c.tracker.MarkHardwareAttempt(ctx, fulfillmentID, kioskCode, allocation.AllocationId); err != nil {
					c.log.Error("persist hardware attempt failed before command", "dispense_id", fulfillmentID, "allocation_id", allocation.AllocationId, "error", err.Error())
					msg.Nak()
					return
				}
			}
		}

		doorNo := int(pending[0].DoorNo)
		for _, allocation := range pending[1:] {
			if int(allocation.DoorNo) != doorNo {
				c.publishFailureAndAck(ctx, msg, ev, "mixed_pickup_doors", "all allocations in one sticker must use the same pickup door", nil)
				return
			}
		}
		request := DispenseRequest{
			Prescription: prescriptionID,
			DoorNo:       doorNo,
			Items:        make([]DispenseItem, 0, len(pending)),
		}
		for _, allocation := range pending {
			request.Items = append(request.Items, DispenseItem{
				AllocationID: allocation.AllocationId,
				Layer:        int(allocation.HardwareLayer), ChannelStart: int(allocation.ChannelStart),
				ChannelEnd: int(allocation.ChannelEnd), Quantity: int(allocation.Quantity),
			})
		}

		response, dispenseErr := client.Dispense(ctx, request)
		responseJSON := `{}`
		if response != nil {
			hardwareResponses = append(hardwareResponses, response)
			payload, _ := json.Marshal(response)
			responseJSON = string(payload)
		}
		if dispenseErr != nil && response != nil {
			// A 502/504 response can still carry the failed step trail. Keep
			// that detail and map any shelves completed before the failure.
			resultsByAllocation, failed, failureReason, failureDetail = allocationResults(pending, response)
			if failureDetail == "" {
				failureDetail = dispenseErr.Error()
			}
		} else if dispenseErr != nil {
			failed, failureReason, failureDetail = true, "agent_error", dispenseErr.Error()
			for _, allocation := range pending {
				resultsByAllocation[allocation.AllocationId] = &eventsv1.DispenseAllocationResult{AllocationId: allocation.AllocationId, Success: false, Detail: dispenseErr.Error()}
			}
		} else {
			resultsByAllocation, failed, failureReason, failureDetail = allocationResults(pending, response)
			if failed && failureReason == "" {
				failureReason = "hardware_failed"
			}
		}
		if c.tracker != nil {
			for _, allocation := range pending {
				if err := c.recordHardwareResult(ctx, fulfillmentID, kioskCode, resultsByAllocation[allocation.AllocationId], responseJSON); err != nil {
					msg.Nak()
					return
				}
			}
		}
	}

	results := make([]*eventsv1.DispenseAllocationResult, 0, len(orderedAllocations))
	for _, allocation := range orderedAllocations {
		result := recorded[allocation.AllocationId]
		if result == nil {
			result = resultsByAllocation[allocation.AllocationId]
		}
		if result == nil {
			result = &eventsv1.DispenseAllocationResult{AllocationId: allocation.AllocationId, Success: false, Detail: "allocation outcome unavailable"}
		}
		results = append(results, result)
		if !result.Success {
			failed = true
			if failureReason == "" {
				failureReason = "hardware_failed"
			}
			if failureDetail == "" {
				failureDetail = result.Detail
			}
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
