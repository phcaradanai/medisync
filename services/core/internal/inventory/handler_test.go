package inventory

import (
	"context"
	"errors"
	"testing"
	"time"

	"connectrpc.com/connect"
	inventoryv1 "github.com/adm-chura3inter/medisync/services/core/internal/gen/medisync/inventory/v1"

	"github.com/adm-chura3inter/medisync/services/core/internal/platform/audit"
	"github.com/adm-chura3inter/medisync/services/core/internal/testutil"
)

type fakeTokenParser struct {
	claims Claims
	err    error
}

func (p *fakeTokenParser) Parse(tokenString string) (TokenClaimser, error) { return p.claims, p.err }

func adminClaims() Claims { return Claims{Subject: "user-1", Role: "ADMIN", ProjectID: "proj-1"} }

func newAuthedRequest[T any](msg *T) *connect.Request[T] {
	req := connect.NewRequest(msg)
	req.Header().Set("Authorization", "Bearer test-token")
	return req
}

// ── Fake SlotStore ─────────────────────────────────────────

type fakeSlotStore struct {
	listResult    []*Slot
	listErr       error
	createResult  *Slot
	createErr     error
	getByIDResult *Slot
	getByIDErr    error
	assignResult  *Slot
	assignErr     error
	refillResult  *Slot
	refillErr     error
	adjustResult  *Slot
	adjustErr     error
	// call recording
	refillCalls  []refillCall
	adjustCalls  []adjustCall
	assignCalls  []assignCall
}

type refillCall struct {
	id    string
	delta int32
}

type adjustCall struct {
	id          string
	newQuantity int32
}

type assignCall struct {
	slotID       string
	drugID       string
	drugCode     string
	drugName     string
	capacity     int32
	lowThreshold int32
}

func (s *fakeSlotStore) ListSlots(_ context.Context, cabinetID, projectID string, lowOnly bool) ([]*Slot, error) {
	return s.listResult, s.listErr
}

func (s *fakeSlotStore) CreateSlot(_ context.Context, cabinetID, code, displayName, projectID string, capacity, lowThreshold int32) (*Slot, error) {
	return s.createResult, s.createErr
}

func (s *fakeSlotStore) GetByID(_ context.Context, id string) (*Slot, error) {
	return s.getByIDResult, s.getByIDErr
}

func (s *fakeSlotStore) AssignDrug(_ context.Context, slotID, drugID, drugCode, drugName string, capacity, lowThreshold int32) (*Slot, error) {
	s.assignCalls = append(s.assignCalls, assignCall{slotID, drugID, drugCode, drugName, capacity, lowThreshold})
	return s.assignResult, s.assignErr
}

func (s *fakeSlotStore) Refill(_ context.Context, id string, delta int32) (*Slot, error) {
	s.refillCalls = append(s.refillCalls, refillCall{id, delta})
	return s.refillResult, s.refillErr
}

func (s *fakeSlotStore) AdjustStock(_ context.Context, id string, newQuantity int32) (*Slot, error) {
	s.adjustCalls = append(s.adjustCalls, adjustCall{id, newQuantity})
	return s.adjustResult, s.adjustErr
}

// ── Fake audit.Writer DB ────────────────────────────────────────────

type fakeAuditDB struct {
	*testutil.FakeExecer
}

// ── Handler tests ───────────────────────────────────────────────────

func sampleSlot() *Slot {
	now := time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC)
	return &Slot{
		ID: "slot-1", CabinetID: "cab-1", Code: "A01",
		DrugID: "drug-1", DrugCode: "PARA-500", DrugName: "Paracetamol",
		Capacity: 100, Quantity: 50, LowThreshold: 10,
		CreatedAt: now, UpdatedAt: now,
	}
}

func TestHandlerListSlotsSuccess(t *testing.T) {
	fakeStore := &fakeSlotStore{
		listResult: []*Slot{sampleSlot()},
	}
	server := NewInventoryServerWithAuth(fakeStore, nil, nil, &fakeTokenParser{claims: adminClaims()})

	resp, err := server.ListSlots(context.Background(), newAuthedRequest(&inventoryv1.ListSlotsRequest{}))
	if err != nil {
		t.Fatalf("ListSlots: %v", err)
	}
	if len(resp.Msg.Slots) != 1 {
		t.Errorf("expected 1 slot, got %d", len(resp.Msg.Slots))
	}
	if resp.Msg.Slots[0].Id != "slot-1" {
		t.Errorf("Id = %q, want slot-1", resp.Msg.Slots[0].Id)
	}
}

func TestHandlerListSlotsWithFilter(t *testing.T) {
	fakeStore := &fakeSlotStore{
		listResult: []*Slot{},
	}
	server := NewInventoryServerWithAuth(fakeStore, nil, nil, &fakeTokenParser{claims: adminClaims()})

	_, err := server.ListSlots(context.Background(), newAuthedRequest(&inventoryv1.ListSlotsRequest{
		CabinetId: "cab-1",
		LowOnly:   true,
	}))
	if err != nil {
		t.Fatalf("ListSlots: %v", err)
	}
}

func TestHandlerListSlotsStoreError(t *testing.T) {
	fakeStore := &fakeSlotStore{listErr: errors.New("db down")}
	server := NewInventoryServerWithAuth(fakeStore, nil, nil, &fakeTokenParser{claims: adminClaims()})

	_, err := server.ListSlots(context.Background(), newAuthedRequest(&inventoryv1.ListSlotsRequest{}))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if connect.CodeOf(err) != connect.CodeInternal {
		t.Errorf("code = %v, want CodeInternal", connect.CodeOf(err))
	}
}

func TestHandlerAssignDrugSuccess(t *testing.T) {
	fakeStore := &fakeSlotStore{
		assignResult: sampleSlot(),
	}
	auditDB := &fakeAuditDB{FakeExecer: &testutil.FakeExecer{}}
	auditWriter := audit.NewWriterWithDB(auditDB)
	server := NewInventoryServerWithAuth(fakeStore, auditWriter, nil, &fakeTokenParser{claims: adminClaims()})

	resp, err := server.AssignDrug(context.Background(), newAuthedRequest(&inventoryv1.AssignDrugRequest{
		SlotId:       "slot-1",
		DrugId:       "drug-1",
		Capacity:     100,
		LowThreshold: 10,
	}))
	if err != nil {
		t.Fatalf("AssignDrug: %v", err)
	}
	if resp.Msg.Slot == nil {
		t.Fatal("expected slot in response")
	}
	if resp.Msg.Slot.DrugId != "drug-1" {
		t.Errorf("DrugId = %q, want drug-1", resp.Msg.Slot.DrugId)
	}
	if len(auditDB.Calls) != 1 {
		t.Fatalf("expected 1 audit call, got %d", len(auditDB.Calls))
	}
}

func TestHandlerAssignDrugMissingSlotID(t *testing.T) {
	server := NewInventoryServerWithAuth(&fakeSlotStore{}, nil, nil, &fakeTokenParser{claims: adminClaims()})
	_, err := server.AssignDrug(context.Background(), newAuthedRequest(&inventoryv1.AssignDrugRequest{}))
	if err == nil {
		t.Fatal("expected error for missing slot id")
	}
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("code = %v, want CodeInvalidArgument", connect.CodeOf(err))
	}
}

func TestHandlerAssignDrugMissingDrugID(t *testing.T) {
	server := NewInventoryServerWithAuth(&fakeSlotStore{}, nil, nil, &fakeTokenParser{claims: adminClaims()})
	_, err := server.AssignDrug(context.Background(), newAuthedRequest(&inventoryv1.AssignDrugRequest{
		SlotId: "slot-1",
	}))
	if err == nil {
		t.Fatal("expected error for missing drug id")
	}
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("code = %v, want CodeInvalidArgument", connect.CodeOf(err))
	}
}

func TestHandlerAssignDrugNoCapacity(t *testing.T) {
	server := NewInventoryServerWithAuth(&fakeSlotStore{}, nil, nil, &fakeTokenParser{claims: adminClaims()})
	_, err := server.AssignDrug(context.Background(), newAuthedRequest(&inventoryv1.AssignDrugRequest{
		SlotId:   "slot-1",
		DrugId:   "drug-1",
		Capacity: 0,
	}))
	if err == nil {
		t.Fatal("expected error for zero capacity")
	}
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("code = %v, want CodeInvalidArgument", connect.CodeOf(err))
	}
}

func TestHandlerAssignDrugNotFound(t *testing.T) {
	fakeStore := &fakeSlotStore{assignResult: nil}
	server := NewInventoryServerWithAuth(fakeStore, nil, nil, &fakeTokenParser{claims: adminClaims()})

	_, err := server.AssignDrug(context.Background(), newAuthedRequest(&inventoryv1.AssignDrugRequest{
		SlotId:   "ghost",
		DrugId:   "drug-1",
		Capacity: 100,
	}))
	if err == nil {
		t.Fatal("expected error")
	}
	if connect.CodeOf(err) != connect.CodeNotFound {
		t.Errorf("code = %v, want CodeNotFound", connect.CodeOf(err))
	}
}

func TestHandlerRefillSuccess(t *testing.T) {
	fakeStore := &fakeSlotStore{
		getByIDResult: sampleSlot(),
		refillResult:  sampleSlot(),
	}
	auditDB := &fakeAuditDB{FakeExecer: &testutil.FakeExecer{}}
	auditWriter := audit.NewWriterWithDB(auditDB)
	server := NewInventoryServerWithAuth(fakeStore, auditWriter, nil, &fakeTokenParser{claims: adminClaims()})

	resp, err := server.Refill(context.Background(), newAuthedRequest(&inventoryv1.RefillRequest{
		SlotId:        "slot-1",
		QuantityAdded: 10,
	}))
	if err != nil {
		t.Fatalf("Refill: %v", err)
	}
	if resp.Msg.Slot == nil {
		t.Fatal("expected slot in response")
	}
	if len(auditDB.Calls) != 1 {
		t.Fatalf("expected 1 audit call, got %d", len(auditDB.Calls))
	}
}

func TestHandlerRefillMissingSlotID(t *testing.T) {
	server := NewInventoryServerWithAuth(&fakeSlotStore{}, nil, nil, &fakeTokenParser{claims: adminClaims()})
	_, err := server.Refill(context.Background(), newAuthedRequest(&inventoryv1.RefillRequest{}))
	if err == nil {
		t.Fatal("expected error for missing slot id")
	}
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("code = %v, want CodeInvalidArgument", connect.CodeOf(err))
	}
}

func TestHandlerRefillZeroDelta(t *testing.T) {
	server := NewInventoryServerWithAuth(&fakeSlotStore{}, nil, nil, &fakeTokenParser{claims: adminClaims()})
	_, err := server.Refill(context.Background(), newAuthedRequest(&inventoryv1.RefillRequest{
		SlotId:        "slot-1",
		QuantityAdded: 0,
	}))
	if err == nil {
		t.Fatal("expected error for zero delta")
	}
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("code = %v, want CodeInvalidArgument", connect.CodeOf(err))
	}
}

func TestHandlerRefillInsufficientStock(t *testing.T) {
	fakeStore := &fakeSlotStore{
		getByIDResult: sampleSlot(),
		refillErr:     ErrInsufficientStock,
	}
	server := NewInventoryServerWithAuth(fakeStore, nil, nil, &fakeTokenParser{claims: adminClaims()})

	_, err := server.Refill(context.Background(), newAuthedRequest(&inventoryv1.RefillRequest{
		SlotId:        "slot-1",
		QuantityAdded: -100,
	}))
	if err == nil {
		t.Fatal("expected error for insufficient stock")
	}
	if connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Errorf("code = %v, want CodeFailedPrecondition", connect.CodeOf(err))
	}
}

func TestHandlerRefillNotFound(t *testing.T) {
	fakeStore := &fakeSlotStore{
		getByIDResult: sampleSlot(),
		refillResult:  nil,
	}
	server := NewInventoryServerWithAuth(fakeStore, nil, nil, &fakeTokenParser{claims: adminClaims()})

	_, err := server.Refill(context.Background(), newAuthedRequest(&inventoryv1.RefillRequest{
		SlotId:        "ghost",
		QuantityAdded: 10,
	}))
	if err == nil {
		t.Fatal("expected error")
	}
	if connect.CodeOf(err) != connect.CodeNotFound {
		t.Errorf("code = %v, want CodeNotFound", connect.CodeOf(err))
	}
}

func TestHandlerAdjustStockSuccess(t *testing.T) {
	now := time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC)
	oldSlot := &Slot{ID: "slot-1", Code: "A01", Quantity: 50, DrugCode: "PARA-500",
		CreatedAt: now, UpdatedAt: now}
	newSlot := &Slot{ID: "slot-1", Code: "A01", Quantity: 10, DrugCode: "PARA-500",
		CreatedAt: now, UpdatedAt: now}

	fakeStore := &fakeSlotStore{
		getByIDResult: oldSlot,
		adjustResult:  newSlot,
	}
	auditDB := &fakeAuditDB{FakeExecer: &testutil.FakeExecer{}}
	auditWriter := audit.NewWriterWithDB(auditDB)
	server := NewInventoryServerWithAuth(fakeStore, auditWriter, nil, &fakeTokenParser{claims: adminClaims()})

	resp, err := server.AdjustStock(context.Background(), newAuthedRequest(&inventoryv1.AdjustStockRequest{
		SlotId:      "slot-1",
		NewQuantity: 10,
		Reason:      "audit correction",
	}))
	if err != nil {
		t.Fatalf("AdjustStock: %v", err)
	}
	if resp.Msg.Slot == nil {
		t.Fatal("expected slot in response")
	}
	if len(auditDB.Calls) != 1 {
		t.Fatalf("expected 1 audit call, got %d", len(auditDB.Calls))
	}
}

func TestHandlerAdjustStockMissingSlotID(t *testing.T) {
	server := NewInventoryServerWithAuth(&fakeSlotStore{}, nil, nil, &fakeTokenParser{claims: adminClaims()})
	_, err := server.AdjustStock(context.Background(), newAuthedRequest(&inventoryv1.AdjustStockRequest{}))
	if err == nil {
		t.Fatal("expected error for missing slot id")
	}
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("code = %v, want CodeInvalidArgument", connect.CodeOf(err))
	}
}

func TestHandlerAdjustStockNegative(t *testing.T) {
	server := NewInventoryServerWithAuth(&fakeSlotStore{}, nil, nil, &fakeTokenParser{claims: adminClaims()})
	_, err := server.AdjustStock(context.Background(), newAuthedRequest(&inventoryv1.AdjustStockRequest{
		SlotId:      "slot-1",
		NewQuantity: -1,
		Reason:      "test",
	}))
	if err == nil {
		t.Fatal("expected error for negative quantity")
	}
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("code = %v, want CodeInvalidArgument", connect.CodeOf(err))
	}
}

func TestHandlerAdjustStockMissingReason(t *testing.T) {
	server := NewInventoryServerWithAuth(&fakeSlotStore{}, nil, nil, &fakeTokenParser{claims: adminClaims()})
	_, err := server.AdjustStock(context.Background(), newAuthedRequest(&inventoryv1.AdjustStockRequest{
		SlotId:      "slot-1",
		NewQuantity: 10,
	}))
	if err == nil {
		t.Fatal("expected error for missing reason")
	}
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("code = %v, want CodeInvalidArgument", connect.CodeOf(err))
	}
}

func TestHandlerAdjustStockSlotNotFound(t *testing.T) {
	fakeStore := &fakeSlotStore{
		getByIDResult: nil,
	}
	server := NewInventoryServerWithAuth(fakeStore, nil, nil, &fakeTokenParser{claims: adminClaims()})

	_, err := server.AdjustStock(context.Background(), newAuthedRequest(&inventoryv1.AdjustStockRequest{
		SlotId:      "ghost",
		NewQuantity: 10,
		Reason:      "audit correction",
	}))
	if err == nil {
		t.Fatal("expected error")
	}
	if connect.CodeOf(err) != connect.CodeNotFound {
		t.Errorf("code = %v, want CodeNotFound", connect.CodeOf(err))
	}
}

// ── toProtoSlot tests ───────────────────────────────────────────────

func TestToProtoSlotNil(t *testing.T) {
	pb := toProtoSlot(nil)
	if pb != nil {
		t.Errorf("expected nil, got %+v", pb)
	}
}

func TestToProtoSlotFull(t *testing.T) {
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	slot := &Slot{
		ID: "slot-1", CabinetID: "cab-1", Code: "A01",
		DrugID: "drug-1", DrugCode: "PARA-500", DrugName: "Paracetamol",
		Capacity: 100, Quantity: 50, LowThreshold: 10,
		CreatedAt: now, UpdatedAt: now,
	}
	pb := toProtoSlot(slot)
	if pb.Id != "slot-1" {
		t.Errorf("Id = %q", pb.Id)
	}
	if pb.CabinetId != "cab-1" {
		t.Errorf("CabinetId = %q", pb.CabinetId)
	}
	if pb.Code != "A01" {
		t.Errorf("Code = %q", pb.Code)
	}
	if pb.DrugId != "drug-1" {
		t.Errorf("DrugId = %q", pb.DrugId)
	}
	if pb.DrugCode != "PARA-500" {
		t.Errorf("DrugCode = %q", pb.DrugCode)
	}
	if pb.DrugName != "Paracetamol" {
		t.Errorf("DrugName = %q", pb.DrugName)
	}
	if pb.Capacity != 100 {
		t.Errorf("Capacity = %d", pb.Capacity)
	}
	if pb.Quantity != 50 {
		t.Errorf("Quantity = %d", pb.Quantity)
	}
	if pb.LowThreshold != 10 {
		t.Errorf("LowThreshold = %d", pb.LowThreshold)
	}
	if pb.UpdatedAt == nil {
		t.Error("UpdatedAt should not be nil")
	}
}

func TestToProtoSlotZeroTime(t *testing.T) {
	slot := &Slot{ID: "slot-1", Code: "A01"}
	pb := toProtoSlot(slot)
	if pb.UpdatedAt != nil {
		t.Error("UpdatedAt should be nil for zero time")
	}
}

// ── Interface compliance ────────────────────────────────────────────

func TestHandlerImplementsInterface(t *testing.T) {
	// Compile-time check is at the top of handler.go.
	server := NewInventoryServerWithAuth(&fakeSlotStore{}, nil, nil, &fakeTokenParser{claims: adminClaims()})
	_, err := server.ListSlots(context.Background(), newAuthedRequest(&inventoryv1.ListSlotsRequest{}))
	_ = err
}
