package catalog

import (
	"context"
	"errors"
	"testing"
	"time"

	"connectrpc.com/connect"
	catalogv1 "github.com/adm-chura3inter/medisync/services/core/internal/gen/medisync/catalog/v1"
	"github.com/adm-chura3inter/medisync/services/core/internal/platform/audit"
	"github.com/adm-chura3inter/medisync/services/core/internal/testutil"
)

// ── Fake Token Parser ────────────────────────────────────────────

type fakeTokenParser struct {
	claims Claims
	err    error
}

func (p *fakeTokenParser) Parse(tokenString string) (TokenClaimser, error) {
	return p.claims, p.err
}

func adminClaims() Claims { return Claims{Subject: "user-1", Role: "ADMIN", ProjectID: "proj-1"} }

func newAuthedRequest[T any](msg *T) *connect.Request[T] {
	req := connect.NewRequest(msg)
	req.Header().Set("Authorization", "Bearer test-token")
	return req
}

// ── Fake DrugStore ────────────────────────────────────────────────

type fakeDrugStore struct {
	createResult     *Drug
	createErr        error
	getByIDResult    *Drug
	getByIDErr       error
	getBarcodeResult *Drug
	getBarcodeErr    error
	listResult       []*Drug
	listNextToken    string
	listTotalCount   int64
	listErr          error
	updateResult     *Drug
	updateErr        error
	deactivateResult *Drug
	deactivateErr    error
	// call recording
	createCalls  []Drug
	updateCalls  []Drug
	barcodeCalls []string
}

func (s *fakeDrugStore) Create(_ context.Context, d Drug) (*Drug, error) {
	s.createCalls = append(s.createCalls, d)
	return s.createResult, s.createErr
}

func (s *fakeDrugStore) GetByID(_ context.Context, id string) (*Drug, error) {
	return s.getByIDResult, s.getByIDErr
}

func (s *fakeDrugStore) GetByCode(_ context.Context, code string) (*Drug, error) {
	return nil, errors.New("not implemented")
}

func (s *fakeDrugStore) GetByBarcode(_ context.Context, barcode string) (*Drug, error) {
	s.barcodeCalls = append(s.barcodeCalls, barcode)
	return s.getBarcodeResult, s.getBarcodeErr
}

func (s *fakeDrugStore) List(_ context.Context, query string, includeInactive bool, pageSize int32, pageToken, projectID string) ([]*Drug, string, int64, error) {
	return s.listResult, s.listNextToken, s.listTotalCount, s.listErr
}

func (s *fakeDrugStore) Update(_ context.Context, d Drug) (*Drug, error) {
	s.updateCalls = append(s.updateCalls, d)
	return s.updateResult, s.updateErr
}

func (s *fakeDrugStore) Deactivate(_ context.Context, id string) (*Drug, error) {
	return s.deactivateResult, s.deactivateErr
}

// ── Fake audit.Writer DB ────────────────────────────────────────────

type fakeAuditDB struct {
	*testutil.FakeExecer
}

// ── Handler tests ───────────────────────────────────────────────────

func TestHandlerCreateDrugSuccess(t *testing.T) {
	now := time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC)
	fakeStore := &fakeDrugStore{
		createResult: &Drug{
			ID:          "new-id",
			Code:        "PARA-500",
			Name:        "Paracetamol",
			GenericName: "Paracetamol",
			Form:        "tablet",
			Strength:    "500mg",
			Unit:        "tab",
			StickerNote: "With food",
			Active:      true,
			CreatedAt:   now,
			UpdatedAt:   now,
		},
	}
	auditDB := &fakeAuditDB{FakeExecer: &testutil.FakeExecer{}}
	auditWriter := audit.NewWriterWithDB(auditDB)
	server := NewCatalogServerWithAuth(fakeStore, auditWriter, &fakeTokenParser{claims: adminClaims()})

	resp, err := server.CreateDrug(context.Background(), newAuthedRequest(&catalogv1.CreateDrugRequest{
		Code:        "PARA-500",
		Name:        "Paracetamol",
		GenericName: "Paracetamol",
		Form:        "tablet",
		Strength:    "500mg",
		Unit:        "tab",
		StickerNote: "With food",
	}))
	if err != nil {
		t.Fatalf("CreateDrug: %v", err)
	}
	if resp.Msg.Drug == nil {
		t.Fatal("expected drug in response")
	}
	if resp.Msg.Drug.Id != "new-id" {
		t.Errorf("Id = %q, want new-id", resp.Msg.Drug.Id)
	}
	if resp.Msg.Drug.Code != "PARA-500" {
		t.Errorf("Code = %q, want PARA-500", resp.Msg.Drug.Code)
	}
	if resp.Msg.Drug.CreatedAt == nil {
		t.Error("CreatedAt should be set")
	}
	if resp.Msg.Drug.UpdatedAt == nil {
		t.Error("UpdatedAt should be set")
	}
	// Verify audit was written.
	if len(auditDB.Calls) != 1 {
		t.Fatalf("expected 1 audit call, got %d", len(auditDB.Calls))
	}
}

func TestHandlerCreateDrugMissingCode(t *testing.T) {
	server := NewCatalogServerWithAuth(&fakeDrugStore{}, nil, &fakeTokenParser{claims: adminClaims()})
	_, err := server.CreateDrug(context.Background(), newAuthedRequest(&catalogv1.CreateDrugRequest{}))
	if err == nil {
		t.Fatal("expected error for missing code")
	}
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("code = %v, want CodeInvalidArgument", connect.CodeOf(err))
	}
}

func TestHandlerCreateDrugMissingName(t *testing.T) {
	server := NewCatalogServerWithAuth(&fakeDrugStore{}, nil, &fakeTokenParser{claims: adminClaims()})
	_, err := server.CreateDrug(context.Background(), newAuthedRequest(&catalogv1.CreateDrugRequest{
		Code: "CODE",
	}))
	if err == nil {
		t.Fatal("expected error for missing name")
	}
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("code = %v, want CodeInvalidArgument", connect.CodeOf(err))
	}
}

func TestHandlerCreateDrugStoreError(t *testing.T) {
	fakeStore := &fakeDrugStore{createErr: errors.New("db down")}
	server := NewCatalogServerWithAuth(fakeStore, nil, &fakeTokenParser{claims: adminClaims()})

	_, err := server.CreateDrug(context.Background(), newAuthedRequest(&catalogv1.CreateDrugRequest{
		Code: "C",
		Name: "N",
	}))
	if err == nil {
		t.Fatal("expected error")
	}
	if connect.CodeOf(err) != connect.CodeInternal {
		t.Errorf("code = %v, want CodeInternal", connect.CodeOf(err))
	}
}

func TestHandlerGetDrugSuccess(t *testing.T) {
	now := time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC)
	fakeStore := &fakeDrugStore{
		getByIDResult: &Drug{
			ID:        "drug-1",
			Code:      "PARA-500",
			Name:      "Paracetamol",
			Active:    true,
			CreatedAt: now,
			UpdatedAt: now,
		},
	}
	server := NewCatalogServerWithAuth(fakeStore, nil, &fakeTokenParser{claims: adminClaims()})

	resp, err := server.GetDrug(context.Background(), newAuthedRequest(&catalogv1.GetDrugRequest{
		Id: "drug-1",
	}))
	if err != nil {
		t.Fatalf("GetDrug: %v", err)
	}
	if resp.Msg.Drug == nil {
		t.Fatal("expected drug")
	}
	if resp.Msg.Drug.Id != "drug-1" {
		t.Errorf("Id = %q, want drug-1", resp.Msg.Drug.Id)
	}
}

func TestHandlerGetDrugMissingID(t *testing.T) {
	server := NewCatalogServerWithAuth(&fakeDrugStore{}, nil, &fakeTokenParser{claims: adminClaims()})
	_, err := server.GetDrug(context.Background(), newAuthedRequest(&catalogv1.GetDrugRequest{}))
	if err == nil {
		t.Fatal("expected error for missing id")
	}
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("code = %v, want CodeInvalidArgument", connect.CodeOf(err))
	}
}

func TestHandlerGetDrugNotFound(t *testing.T) {
	fakeStore := &fakeDrugStore{getByIDResult: nil}
	server := NewCatalogServerWithAuth(fakeStore, nil, &fakeTokenParser{claims: adminClaims()})

	_, err := server.GetDrug(context.Background(), newAuthedRequest(&catalogv1.GetDrugRequest{
		Id: "ghost",
	}))
	if err == nil {
		t.Fatal("expected error")
	}
	if connect.CodeOf(err) != connect.CodeNotFound {
		t.Errorf("code = %v, want CodeNotFound", connect.CodeOf(err))
	}
}

func TestHandlerGetByBarcodeSuccess(t *testing.T) {
	fakeStore := &fakeDrugStore{
		getBarcodeResult: &Drug{ID: "drug-1", Code: "PARA-500", Name: "Paracetamol", Barcode: "8851234567890", Active: true},
	}
	server := NewCatalogServerWithAuth(fakeStore, nil, &fakeTokenParser{claims: adminClaims()})

	resp, err := server.GetByBarcode(context.Background(), newAuthedRequest(&catalogv1.GetByBarcodeRequest{
		Barcode: "8851234567890",
	}))
	if err != nil {
		t.Fatalf("GetByBarcode: %v", err)
	}
	if resp.Msg.Drug == nil || resp.Msg.Drug.Id != "drug-1" {
		t.Fatalf("unexpected drug response: %+v", resp.Msg.Drug)
	}
	if resp.Msg.Drug.Barcode != "8851234567890" {
		t.Errorf("Barcode = %q, want 8851234567890", resp.Msg.Drug.Barcode)
	}
	if len(fakeStore.barcodeCalls) != 1 || fakeStore.barcodeCalls[0] != "8851234567890" {
		t.Errorf("barcode calls = %v", fakeStore.barcodeCalls)
	}
}

func TestHandlerGetByBarcodeMissingBarcode(t *testing.T) {
	server := NewCatalogServerWithAuth(&fakeDrugStore{}, nil, &fakeTokenParser{claims: adminClaims()})
	_, err := server.GetByBarcode(context.Background(), newAuthedRequest(&catalogv1.GetByBarcodeRequest{}))
	if err == nil {
		t.Fatal("expected error for missing barcode")
	}
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("code = %v, want CodeInvalidArgument", connect.CodeOf(err))
	}
}

func TestHandlerGetByBarcodeNotFound(t *testing.T) {
	server := NewCatalogServerWithAuth(&fakeDrugStore{}, nil, &fakeTokenParser{claims: adminClaims()})
	_, err := server.GetByBarcode(context.Background(), newAuthedRequest(&catalogv1.GetByBarcodeRequest{
		Barcode: "missing",
	}))
	if err == nil {
		t.Fatal("expected error")
	}
	if connect.CodeOf(err) != connect.CodeNotFound {
		t.Errorf("code = %v, want CodeNotFound", connect.CodeOf(err))
	}
}

func TestHandlerListDrugs(t *testing.T) {
	fakeStore := &fakeDrugStore{
		listResult: []*Drug{
			{ID: "d1", Code: "A", Name: "Drug A", Active: true},
			{ID: "d2", Code: "B", Name: "Drug B", Active: true},
		},
		listNextToken:  "d2",
		listTotalCount: 3,
	}
	server := NewCatalogServerWithAuth(fakeStore, nil, &fakeTokenParser{claims: adminClaims()})

	resp, err := server.ListDrugs(context.Background(), newAuthedRequest(&catalogv1.ListDrugsRequest{
		PageSize: 2,
	}))
	if err != nil {
		t.Fatalf("ListDrugs: %v", err)
	}
	if len(resp.Msg.Drugs) != 2 {
		t.Errorf("expected 2 drugs, got %d", len(resp.Msg.Drugs))
	}
	if resp.Msg.NextPageToken != "d2" {
		t.Errorf("NextPageToken = %q, want d2", resp.Msg.NextPageToken)
	}
	if resp.Msg.TotalCount != 3 {
		t.Errorf("TotalCount = %d, want 3", resp.Msg.TotalCount)
	}
}

func TestHandlerUpdateDrugSuccess(t *testing.T) {
	now := time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC)
	fakeStore := &fakeDrugStore{
		updateResult: &Drug{
			ID: "drug-1", Code: "CODE", Name: "Updated", Active: true,
			CreatedAt: now, UpdatedAt: now,
		},
	}
	auditDB := &fakeAuditDB{FakeExecer: &testutil.FakeExecer{}}
	auditWriter := audit.NewWriterWithDB(auditDB)
	server := NewCatalogServerWithAuth(fakeStore, auditWriter, &fakeTokenParser{claims: adminClaims()})

	resp, err := server.UpdateDrug(context.Background(), newAuthedRequest(&catalogv1.UpdateDrugRequest{
		Drug: &catalogv1.Drug{
			Id:   "drug-1",
			Code: "CODE",
			Name: "Updated",
		},
	}))
	if err != nil {
		t.Fatalf("UpdateDrug: %v", err)
	}
	if resp.Msg.Drug.Name != "Updated" {
		t.Errorf("Name = %q, want Updated", resp.Msg.Drug.Name)
	}
	// Verify audit was written.
	if len(auditDB.Calls) != 1 {
		t.Errorf("expected 1 audit call, got %d", len(auditDB.Calls))
	}
}

func TestHandlerUpdateDrugMissingID(t *testing.T) {
	server := NewCatalogServerWithAuth(&fakeDrugStore{}, nil, &fakeTokenParser{claims: adminClaims()})
	_, err := server.UpdateDrug(context.Background(), newAuthedRequest(&catalogv1.UpdateDrugRequest{}))
	if err == nil {
		t.Fatal("expected error")
	}
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("code = %v, want CodeInvalidArgument", connect.CodeOf(err))
	}
}

func TestHandlerUpdateDrugNotFound(t *testing.T) {
	fakeStore := &fakeDrugStore{updateResult: nil}
	server := NewCatalogServerWithAuth(fakeStore, nil, &fakeTokenParser{claims: adminClaims()})

	_, err := server.UpdateDrug(context.Background(), newAuthedRequest(&catalogv1.UpdateDrugRequest{
		Drug: &catalogv1.Drug{Id: "ghost"},
	}))
	if err == nil {
		t.Fatal("expected error")
	}
	if connect.CodeOf(err) != connect.CodeNotFound {
		t.Errorf("code = %v, want CodeNotFound", connect.CodeOf(err))
	}
}

func TestHandlerDeactivateDrugSuccess(t *testing.T) {
	now := time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC)
	fakeStore := &fakeDrugStore{
		deactivateResult: &Drug{
			ID: "drug-1", Code: "CODE", Name: "Deactivated", Active: false,
			CreatedAt: now, UpdatedAt: now,
		},
	}
	auditDB := &fakeAuditDB{FakeExecer: &testutil.FakeExecer{}}
	auditWriter := audit.NewWriterWithDB(auditDB)
	server := NewCatalogServerWithAuth(fakeStore, auditWriter, &fakeTokenParser{claims: adminClaims()})

	resp, err := server.DeactivateDrug(context.Background(), newAuthedRequest(&catalogv1.DeactivateDrugRequest{
		Id: "drug-1",
	}))
	if err != nil {
		t.Fatalf("DeactivateDrug: %v", err)
	}
	if resp.Msg.Drug.Active {
		t.Error("drug should be inactive")
	}
	// Verify audit was written.
	if len(auditDB.Calls) != 1 {
		t.Errorf("expected 1 audit call, got %d", len(auditDB.Calls))
	}
}

func TestHandlerDeactivateDrugMissingID(t *testing.T) {
	server := NewCatalogServerWithAuth(&fakeDrugStore{}, nil, &fakeTokenParser{claims: adminClaims()})
	_, err := server.DeactivateDrug(context.Background(), newAuthedRequest(&catalogv1.DeactivateDrugRequest{}))
	if err == nil {
		t.Fatal("expected error")
	}
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("code = %v, want CodeInvalidArgument", connect.CodeOf(err))
	}
}

func TestHandlerDeactivateDrugNotFound(t *testing.T) {
	fakeStore := &fakeDrugStore{deactivateResult: nil}
	server := NewCatalogServerWithAuth(fakeStore, nil, &fakeTokenParser{claims: adminClaims()})

	_, err := server.DeactivateDrug(context.Background(), newAuthedRequest(&catalogv1.DeactivateDrugRequest{
		Id: "ghost",
	}))
	if err == nil {
		t.Fatal("expected error")
	}
	if connect.CodeOf(err) != connect.CodeNotFound {
		t.Errorf("code = %v, want CodeNotFound", connect.CodeOf(err))
	}
}

// ── toProtoDrug tests ───────────────────────────────────────────────

func TestToProtoDrugNil(t *testing.T) {
	pb := toProtoDrug(nil)
	if pb != nil {
		t.Errorf("expected nil, got %+v", pb)
	}
}

func TestToProtoDrugFull(t *testing.T) {
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	d := &Drug{
		ID:          "drug-id",
		Code:        "CODE-123",
		Name:        "Test Drug",
		GenericName: "Test Generic",
		Form:        "tablet",
		Strength:    "100mg",
		Unit:        "tab",
		StickerNote: "Take with water",
		Active:      true,
		Barcode:     "8851234567890",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	pb := toProtoDrug(d)
	if pb.Id != "drug-id" {
		t.Errorf("Id = %q", pb.Id)
	}
	if pb.Code != "CODE-123" {
		t.Errorf("Code = %q", pb.Code)
	}
	if pb.Name != "Test Drug" {
		t.Errorf("Name = %q", pb.Name)
	}
	if pb.GenericName != "Test Generic" {
		t.Errorf("GenericName = %q", pb.GenericName)
	}
	if pb.Form != "tablet" {
		t.Errorf("Form = %q", pb.Form)
	}
	if pb.Strength != "100mg" {
		t.Errorf("Strength = %q", pb.Strength)
	}
	if pb.Unit != "tab" {
		t.Errorf("Unit = %q", pb.Unit)
	}
	if pb.StickerNote != "Take with water" {
		t.Errorf("StickerNote = %q", pb.StickerNote)
	}
	if !pb.Active {
		t.Error("Active should be true")
	}
	if pb.Barcode != "8851234567890" {
		t.Errorf("Barcode = %q", pb.Barcode)
	}
	if pb.CreatedAt == nil {
		t.Error("CreatedAt should not be nil")
	}
	if pb.UpdatedAt == nil {
		t.Error("UpdatedAt should not be nil")
	}
}

func TestToProtoDrugZeroTime(t *testing.T) {
	d := &Drug{
		ID:     "drug-id",
		Code:   "CODE",
		Active: true,
	}
	pb := toProtoDrug(d)
	if pb.CreatedAt != nil {
		t.Error("CreatedAt should be nil for zero time")
	}
	if pb.UpdatedAt != nil {
		t.Error("UpdatedAt should be nil for zero time")
	}
}

// ── Domain tests ────────────────────────────────────────────────────

func TestDrugDefaultValues(t *testing.T) {
	d := Drug{Code: "TEST", Name: "Test"}
	if d.Active {
		t.Error("Active should default to false (zero value)")
	}
	if d.ID != "" {
		t.Error("ID should be empty by default")
	}
	if d.CreatedAt.IsZero() == false {
		t.Error("CreatedAt should be zero by default")
	}
}

// ── Interface compliance ────────────────────────────────────────────

func TestStoreImplementsDrugStore(t *testing.T) {
	var _ DrugStore = (*Store)(nil)
}

// ── Connect-RPC handler registration (compile-time) ─────────────────

func TestCatalogServerImplementsHandler(t *testing.T) {
	// Compile-time check is at the top of handler.go.
	// This test confirms the runtime factory works.
	server := NewCatalogServerWithAuth(&fakeDrugStore{}, nil, &fakeTokenParser{claims: adminClaims()})
	_, err := server.GetDrug(context.Background(), newAuthedRequest(&catalogv1.GetDrugRequest{}))
	// We expect an error because the fake returns a nil audit error or something.
	// The point is the call compiled and ran.
	_ = err
}
