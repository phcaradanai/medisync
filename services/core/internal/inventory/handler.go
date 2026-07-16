package inventory

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"connectrpc.com/connect"
	"github.com/nats-io/nats.go/jetstream"
	eventsv1 "github.com/adm-chura3inter/medisync/services/core/internal/gen/medisync/events/v1"
	inventoryv1 "github.com/adm-chura3inter/medisync/services/core/internal/gen/medisync/inventory/v1"
	inventoryv1connect "github.com/adm-chura3inter/medisync/services/core/internal/gen/medisync/inventory/v1/inventoryv1connect"
	"github.com/adm-chura3inter/medisync/services/core/internal/platform/audit"
	"github.com/adm-chura3inter/medisync/services/core/internal/platform/natsx"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var (
	ErrSlotIDRequired    = errors.New("slot id is required")
	ErrDrugIDRequired    = errors.New("drug id is required")
	ErrQuantityPositive  = errors.New("quantity must be positive")
	ErrNewQuantityNonNeg = errors.New("new quantity must not be negative")
	ErrReasonRequired    = errors.New("reason is required for stock adjustment")
	ErrInvalidSlotID     = errors.New("slot id is invalid")
	ErrCapacityRequired  = errors.New("capacity must be positive when assigning a drug")
	ErrNotAuthenticated  = errors.New("authentication required")
	ErrNotAdmin          = errors.New("admin role required")
)

type TokenClaimser interface {
	GetSubject() string
	GetRole() string
	GetProjectID() string
}

type Claims struct {
	Subject   string
	Role      string
	ProjectID string
}

func (c Claims) GetSubject() string   { return c.Subject }
func (c Claims) GetRole() string      { return c.Role }
func (c Claims) GetProjectID() string { return c.ProjectID }

type TokenParser interface{ Parse(tokenString string) (TokenClaimser, error) }

type SlotStore interface {
	ListSlots(ctx context.Context, cabinetID, projectID string, lowOnly bool) ([]*Slot, error)
	GetByID(ctx context.Context, id string) (*Slot, error)
	AssignDrug(ctx context.Context, slotID, drugID, drugCode, drugName string, capacity, lowThreshold int32) (*Slot, error)
	Refill(ctx context.Context, id string, delta int32) (*Slot, error)
	AdjustStock(ctx context.Context, id string, newQuantity int32) (*Slot, error)
}

var _ inventoryv1connect.InventoryServiceHandler = (*InventoryServer)(nil)
var _ SlotStore = (*Store)(nil)

type InventoryServer struct {
	store  SlotStore
	audit  *audit.Writer
	js     jetstream.JetStream
	parser TokenParser
}

func NewInventoryServer(store SlotStore, aw *audit.Writer, js jetstream.JetStream) *InventoryServer {
	return &InventoryServer{store: store, audit: aw, js: js}
}

func NewInventoryServerWithAuth(store SlotStore, aw *audit.Writer, js jetstream.JetStream, parser TokenParser) *InventoryServer {
	return &InventoryServer{store: store, audit: aw, js: js, parser: parser}
}

func (s *InventoryServer) authenticate(header http.Header) (TokenClaimser, *connect.Error) {
	if s.parser == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, ErrNotAuthenticated)
	}
	auth := header.Get("Authorization")
	if auth == "" {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("authorization header is required"))
	}
	parts := strings.SplitN(auth, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("bearer scheme required"))
	}
	claims, err := s.parser.Parse(strings.TrimSpace(parts[1]))
	if err != nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("invalid credentials"))
	}
	if claims.GetRole() != "ADMIN" {
		return nil, connect.NewError(connect.CodePermissionDenied, ErrNotAdmin)
	}
	return claims, nil
}

func (s *InventoryServer) ListSlots(ctx context.Context, req *connect.Request[inventoryv1.ListSlotsRequest]) (*connect.Response[inventoryv1.ListSlotsResponse], error) {
	claims, cerr := s.authenticate(req.Header())
	if cerr != nil {
		return nil, cerr
	}
	msg := req.Msg
	cabinetID, lowOnly := "", false
	if msg != nil {
		cabinetID, lowOnly = msg.CabinetId, msg.LowOnly
	}
	slots, err := s.store.ListSlots(ctx, cabinetID, claims.GetProjectID(), lowOnly)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("list slots: %w", err))
	}
	pbSlots := make([]*inventoryv1.Slot, 0, len(slots))
	for _, slot := range slots {
		pbSlots = append(pbSlots, toProtoSlot(slot))
	}
	return connect.NewResponse(&inventoryv1.ListSlotsResponse{Slots: pbSlots}), nil
}

func (s *InventoryServer) AssignDrug(ctx context.Context, req *connect.Request[inventoryv1.AssignDrugRequest]) (*connect.Response[inventoryv1.AssignDrugResponse], error) {
	claims, cerr := s.authenticate(req.Header())
	if cerr != nil {
		return nil, cerr
	}
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
	slot, err := s.store.AssignDrug(ctx, msg.SlotId, msg.DrugId, "", "", msg.Capacity, msg.LowThreshold)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("assign drug: %w", err))
	}
	if slot == nil {
		return nil, connect.NewError(connect.CodeNotFound, ErrSlotNotFound)
	}
	s.writeAudit(ctx, audit.Entry{
		Actor:     claims.GetSubject(),
		Action:    "slot.drug_assigned",
		Entity:    "slot",
		EntityID:  slot.ID,
		ProjectID: claims.GetProjectID(),
		Detail:    auditDetail{SlotCode: slot.Code, DrugCode: slot.DrugCode, CabinetID: slot.CabinetID},
	})
	return connect.NewResponse(&inventoryv1.AssignDrugResponse{Slot: toProtoSlot(slot)}), nil
}

func (s *InventoryServer) Refill(ctx context.Context, req *connect.Request[inventoryv1.RefillRequest]) (*connect.Response[inventoryv1.RefillResponse], error) {
	claims, cerr := s.authenticate(req.Header())
	if cerr != nil {
		return nil, cerr
	}
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
		Actor:    claims.GetSubject(),
		Action:   "slot.refilled",
		Entity:   "slot",
		EntityID: slot.ID,
		ProjectID: claims.GetProjectID(),
		Detail:   auditDetail{SlotCode: slot.Code, DrugCode: slot.DrugCode, CabinetID: slot.CabinetID, Delta: msg.QuantityAdded, QuantityAfter: slot.Quantity},
	})
	reason := eventsv1.StockChangeReason_STOCK_CHANGE_REASON_REFILL
	if msg.QuantityAdded < 0 {
		reason = eventsv1.StockChangeReason_STOCK_CHANGE_REASON_DISPENSE
	}
	s.publishStockChanged(ctx, slot, msg.QuantityAdded, reason, traceID)
	if slot.Quantity <= slot.LowThreshold && slot.LowThreshold > 0 {
		s.publishStockLow(ctx, slot)
	}
	return connect.NewResponse(&inventoryv1.RefillResponse{Slot: toProtoSlot(slot)}), nil
}

func (s *InventoryServer) AdjustStock(ctx context.Context, req *connect.Request[inventoryv1.AdjustStockRequest]) (*connect.Response[inventoryv1.AdjustStockResponse], error) {
	claims, cerr := s.authenticate(req.Header())
	if cerr != nil {
		return nil, cerr
	}
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
	old, err := s.store.GetByID(ctx, msg.SlotId)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("get slot: %w", err))
	}
	if old == nil {
		return nil, connect.NewError(connect.CodeNotFound, ErrSlotNotFound)
	}
	slot, err := s.store.AdjustStock(ctx, msg.SlotId, msg.NewQuantity)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("adjust: %w", err))
	}
	if slot == nil {
		return nil, connect.NewError(connect.CodeNotFound, ErrSlotNotFound)
	}
	delta := slot.Quantity - old.Quantity
	traceID := msg.TraceId
	s.writeAudit(ctx, audit.Entry{
		TraceID:  traceID,
		Actor:    claims.GetSubject(),
		Action:   "slot.stock_adjusted",
		Entity:   "slot",
		EntityID: slot.ID,
		ProjectID: claims.GetProjectID(),
		Detail:   auditDetail{SlotCode: slot.Code, DrugCode: slot.DrugCode, CabinetID: slot.CabinetID, Delta: delta, QuantityAfter: slot.Quantity, Reason: msg.Reason},
	})
	s.publishStockChanged(ctx, slot, delta, eventsv1.StockChangeReason_STOCK_CHANGE_REASON_ADJUST, traceID)
	if slot.Quantity <= slot.LowThreshold && slot.LowThreshold > 0 {
		s.publishStockLow(ctx, slot)
	}
	return connect.NewResponse(&inventoryv1.AdjustStockResponse{Slot: toProtoSlot(slot)}), nil
}

func (s *InventoryServer) publishStockChanged(ctx context.Context, slot *Slot, delta int32, reason eventsv1.StockChangeReason, traceID string) {
	if s.js == nil { return }
	ev := &eventsv1.StockChanged{SlotCode: slot.Code, DrugCode: slot.DrugCode, Delta: delta, QuantityAfter: slot.Quantity, Reason: reason, TraceId: traceID}
	data, err := protojson.Marshal(ev)
	if err != nil { return }
	_, _ = s.js.Publish(ctx, natsx.SubjectStockChanged, data)
}

func (s *InventoryServer) publishStockLow(ctx context.Context, slot *Slot) {
	if s.js == nil { return }
	ev := &eventsv1.StockLow{SlotCode: slot.Code, DrugCode: slot.DrugCode, Quantity: slot.Quantity, Threshold: slot.LowThreshold}
	data, _ := protojson.Marshal(ev)
	_, _ = s.js.Publish(ctx, natsx.SubjectStockLow, data)
}

func (s *InventoryServer) writeAudit(ctx context.Context, e audit.Entry) {
	if s.audit == nil { return }
	_ = s.audit.Write(ctx, e)
}

func toProtoSlot(slot *Slot) *inventoryv1.Slot {
	if slot == nil { return nil }
	var ua *timestamppb.Timestamp
	if !slot.UpdatedAt.IsZero() { ua = timestamppb.New(slot.UpdatedAt) }
	return &inventoryv1.Slot{Id: slot.ID, CabinetId: slot.CabinetID, Code: slot.Code, DisplayName: slot.DisplayName, DrugId: slot.DrugID, DrugCode: slot.DrugCode, DrugName: slot.DrugName, Capacity: slot.Capacity, Quantity: slot.Quantity, LowThreshold: slot.LowThreshold, ProjectId: slot.ProjectID, UpdatedAt: ua}
}

// ErrSlotNotFound is returned when a slot does not exist.
var ErrSlotNotFound = errors.New("slot not found")

// auditDetail is the JSON payload for inventory audit entries.
type auditDetail struct {
	SlotCode      string `json:"slot_code,omitempty"`
	DrugCode      string `json:"drug_code,omitempty"`
	CabinetID     string `json:"cabinet_id,omitempty"`
	Delta         int32  `json:"delta,omitempty"`
	QuantityAfter int32  `json:"quantity_after,omitempty"`
	Reason        string `json:"reason,omitempty"`
}
