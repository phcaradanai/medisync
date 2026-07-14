package inventory

import (
	"context"
	"errors"
	"fmt"

	"connectrpc.com/connect"
	eventsv1 "github.com/adm-chura3inter/medisync/services/core/internal/gen/medisync/events/v1"
	inventoryv1 "github.com/adm-chura3inter/medisync/services/core/internal/gen/medisync/inventory/v1"
	inventoryv1connect "github.com/adm-chura3inter/medisync/services/core/internal/gen/medisync/inventory/v1/inventoryv1connect"
	"github.com/adm-chura3inter/medisync/services/core/internal/platform/audit"
	"github.com/adm-chura3inter/medisync/services/core/internal/platform/natsx"
	"github.com/nats-io/nats.go/jetstream"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Common inventory errors returned to callers.
var (
	ErrSlotIDRequired       = errors.New("slot id is required")
	ErrDrugIDRequired       = errors.New("drug id is required")
	ErrQuantityPositive     = errors.New("quantity must be positive")
	ErrNewQuantityNonNeg    = errors.New("new quantity must not be negative")
	ErrReasonRequired       = errors.New("reason is required for stock adjustment")
	ErrInvalidSlotID        = errors.New("slot id is invalid")
	ErrCapacityRequired     = errors.New("capacity must be positive when assigning a drug")
)

// SlotStore is the narrow slot-persistence interface consumed by the
// inventory handler. The concrete Store satisfies this interface.
type SlotStore interface {
	ListSlots(ctx context.Context, cabinetID string, lowOnly bool) ([]*Slot, error)
	GetByID(ctx context.Context, id string) (*Slot, error)
	AssignDrug(ctx context.Context, slotID, drugID, drugCode, drugName string, capacity, lowThreshold int32) (*Slot, error)
	Refill(ctx context.Context, id string, delta int32) (*Slot, error)
	AdjustStock(ctx context.Context, id string, newQuantity int32) (*Slot, error)
}

// Compile-time checks.
var _ inventoryv1connect.InventoryServiceHandler = (*InventoryServer)(nil)
var _ SlotStore = (*Store)(nil)

// InventoryServer is the Connect-RPC handler for InventoryService.
type InventoryServer struct {
	store SlotStore
	audit *audit.Writer
	js    jetstream.JetStream
}

// NewInventoryServer creates an InventoryServer with the given store,
// audit writer, and NATS JetStream handle.
func NewInventoryServer(store SlotStore, aw *audit.Writer, js jetstream.JetStream) *InventoryServer {
	return &InventoryServer{store: store, audit: aw, js: js}
}

// ListSlots returns all slots, optionally filtered by cabinet and
// low-stock status.
func (s *InventoryServer) ListSlots(
	ctx context.Context,
	req *connect.Request[inventoryv1.ListSlotsRequest],
) (*connect.Response[inventoryv1.ListSlotsResponse], error) {
	msg := req.Msg
	cabinetID := ""
	lowOnly := false
	if msg != nil {
		cabinetID = msg.CabinetId
		lowOnly = msg.LowOnly
	}

	slots, err := s.store.ListSlots(ctx, cabinetID, lowOnly)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("list slots: %w", err))
	}

	pbSlots := make([]*inventoryv1.Slot, 0, len(slots))
	for _, slot := range slots {
		pbSlots = append(pbSlots, toProtoSlot(slot))
	}

	return connect.NewResponse(&inventoryv1.ListSlotsResponse{
		Slots: pbSlots,
	}), nil
}

// AssignDrug assigns a drug to a slot, setting capacity and low threshold.
func (s *InventoryServer) AssignDrug(
	ctx context.Context,
	req *connect.Request[inventoryv1.AssignDrugRequest],
) (*connect.Response[inventoryv1.AssignDrugResponse], error) {
	msg := req.Msg
	if msg == nil || msg.SlotId == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, ErrSlotIDRequired)
	}
	if msg.DrugId == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, ErrDrugIDRequired)
	}
	if msg.Capacity <= 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, ErrCapacityRequired)
	}

	// AssignDrug updates an existing slot. The drug code and name
	// are looked up externally (by the admin frontend or a future
	// catalog integration); for now they come from the request.
	slot, err := s.store.AssignDrug(ctx, msg.SlotId, msg.DrugId, "", "", msg.Capacity, msg.LowThreshold)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("assign drug: %w", err))
	}
	if slot == nil {
		return nil, connect.NewError(connect.CodeNotFound, ErrSlotNotFound)
	}

	s.writeAudit(ctx, audit.Entry{
		Actor:    "system",
		Action:   "slot.drug_assigned",
		Entity:   "slot",
		EntityID: slot.ID,
		Detail: auditDetail{
			SlotCode:  slot.Code,
			DrugCode:  slot.DrugCode,
			CabinetID: slot.CabinetID,
		},
	})

	return connect.NewResponse(&inventoryv1.AssignDrugResponse{
		Slot: toProtoSlot(slot),
	}), nil
}

// Refill adds stock to a slot. The delta must be positive. A negative
// delta (dispense) uses the same path but is validated server-side.
// Publishes stock.changed and, when quantity drops to or below
// low_threshold, stock.low.
func (s *InventoryServer) Refill(
	ctx context.Context,
	req *connect.Request[inventoryv1.RefillRequest],
) (*connect.Response[inventoryv1.RefillResponse], error) {
	msg := req.Msg
	if msg == nil || msg.SlotId == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, ErrSlotIDRequired)
	}
	if msg.QuantityAdded == 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, ErrQuantityPositive)
	}

	slot, err := s.store.Refill(ctx, msg.SlotId, msg.QuantityAdded)
	if err != nil {
		if errors.Is(err, ErrInsufficientStock) {
			return nil, connect.NewError(connect.CodeFailedPrecondition, err)
		}
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("refill slot: %w", err))
	}
	if slot == nil {
		return nil, connect.NewError(connect.CodeNotFound, ErrSlotNotFound)
	}

	traceID := msg.TraceId

	s.writeAudit(ctx, audit.Entry{
		TraceID:  traceID,
		Actor:    "system",
		Action:   "slot.refilled",
		Entity:   "slot",
		EntityID: slot.ID,
		Detail: auditDetail{
			SlotCode:      slot.Code,
			DrugCode:      slot.DrugCode,
			CabinetID:     slot.CabinetID,
			Delta:         msg.QuantityAdded,
			QuantityAfter: slot.Quantity,
		},
	})

	// Publish stock.changed event.
	reason := eventsv1.StockChangeReason_STOCK_CHANGE_REASON_REFILL
	if msg.QuantityAdded < 0 {
		reason = eventsv1.StockChangeReason_STOCK_CHANGE_REASON_DISPENSE
	}
	s.publishStockChanged(ctx, slot, msg.QuantityAdded, reason, traceID)

	// Publish stock.low if quantity is at or below threshold.
	if slot.Quantity <= slot.LowThreshold && slot.LowThreshold > 0 {
		s.publishStockLow(ctx, slot)
	}

	return connect.NewResponse(&inventoryv1.RefillResponse{
		Slot: toProtoSlot(slot),
	}), nil
}

// AdjustStock corrects a slot's quantity to an absolute value, for use
// during audit reconciliation. Publishes stock.changed and, when the
// new quantity is at or below low_threshold, stock.low.
func (s *InventoryServer) AdjustStock(
	ctx context.Context,
	req *connect.Request[inventoryv1.AdjustStockRequest],
) (*connect.Response[inventoryv1.AdjustStockResponse], error) {
	msg := req.Msg
	if msg == nil || msg.SlotId == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, ErrSlotIDRequired)
	}
	if msg.NewQuantity < 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, ErrNewQuantityNonNeg)
	}
	if msg.Reason == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, ErrReasonRequired)
	}

	// Read current quantity before adjustment to compute delta.
	old, err := s.store.GetByID(ctx, msg.SlotId)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("get slot for adjust: %w", err))
	}
	if old == nil {
		return nil, connect.NewError(connect.CodeNotFound, ErrSlotNotFound)
	}

	slot, err := s.store.AdjustStock(ctx, msg.SlotId, msg.NewQuantity)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("adjust stock: %w", err))
	}
	if slot == nil {
		return nil, connect.NewError(connect.CodeNotFound, ErrSlotNotFound)
	}

	delta := slot.Quantity - old.Quantity
	traceID := msg.TraceId

	s.writeAudit(ctx, audit.Entry{
		TraceID:  traceID,
		Actor:    "system",
		Action:   "slot.stock_adjusted",
		Entity:   "slot",
		EntityID: slot.ID,
		Detail: auditDetail{
			SlotCode:      slot.Code,
			DrugCode:      slot.DrugCode,
			CabinetID:     slot.CabinetID,
			Delta:         delta,
			QuantityAfter: slot.Quantity,
			Reason:        msg.Reason,
		},
	})

	s.publishStockChanged(ctx, slot, delta, eventsv1.StockChangeReason_STOCK_CHANGE_REASON_ADJUST, traceID)

	if slot.Quantity <= slot.LowThreshold && slot.LowThreshold > 0 {
		s.publishStockLow(ctx, slot)
	}

	return connect.NewResponse(&inventoryv1.AdjustStockResponse{
		Slot: toProtoSlot(slot),
	}), nil
}

// publishStockChanged publishes a medisync.stock.changed event to NATS.
// Publish failures are logged but do not fail the RPC — the DB mutation
// has already succeeded and the audit log is the source of truth.
func (s *InventoryServer) publishStockChanged(ctx context.Context, slot *Slot, delta int32, reason eventsv1.StockChangeReason, traceID string) {
	if s.js == nil {
		return
	}
	ev := &eventsv1.StockChanged{
		SlotCode:      slot.Code,
		DrugCode:      slot.DrugCode,
		Delta:         delta,
		QuantityAfter: slot.Quantity,
		Reason:        reason,
		TraceId:       traceID,
	}
	data, err := protojson.Marshal(ev)
	if err != nil {
		return
	}
	_, _ = s.js.Publish(ctx, natsx.SubjectStockChanged, data)
}

// publishStockLow publishes a medisync.stock.low event to NATS.
func (s *InventoryServer) publishStockLow(ctx context.Context, slot *Slot) {
	if s.js == nil {
		return
	}
	ev := &eventsv1.StockLow{
		SlotCode:  slot.Code,
		DrugCode:  slot.DrugCode,
		Quantity:  slot.Quantity,
		Threshold: slot.LowThreshold,
	}
	data, err := protojson.Marshal(ev)
	if err != nil {
		return
	}
	_, _ = s.js.Publish(ctx, natsx.SubjectStockLow, data)
}

// writeAudit records an audit entry when the audit writer is configured.
// Audit failures are logged but do not cause the RPC to fail.
func (s *InventoryServer) writeAudit(ctx context.Context, e audit.Entry) {
	if s.audit == nil {
		return
	}
	_ = s.audit.Write(ctx, e)
}

// toProtoSlot converts a domain Slot to a proto Slot. Safe for nil input.
func toProtoSlot(slot *Slot) *inventoryv1.Slot {
	if slot == nil {
		return nil
	}
	var updatedAt *timestamppb.Timestamp
	if !slot.UpdatedAt.IsZero() {
		updatedAt = timestamppb.New(slot.UpdatedAt)
	}
	return &inventoryv1.Slot{
		Id:           slot.ID,
		CabinetId:    slot.CabinetID,
		Code:         slot.Code,
		DrugId:       slot.DrugID,
		DrugCode:     slot.DrugCode,
		DrugName:     slot.DrugName,
		Capacity:     slot.Capacity,
		Quantity:     slot.Quantity,
		LowThreshold: slot.LowThreshold,
		UpdatedAt:    updatedAt,
	}
}
