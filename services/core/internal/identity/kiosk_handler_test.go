package identity

import (
	"context"
	"errors"
	"testing"
	"time"

	"connectrpc.com/connect"
	kioskv1 "github.com/adm-chura3inter/medisync/services/core/internal/gen/medisync/kiosk/v1"
	kioskv1connect "github.com/adm-chura3inter/medisync/services/core/internal/gen/medisync/kiosk/v1/kioskv1connect"
	"github.com/golang-jwt/jwt/v5"
)

// --- Fakes for kiosk handler tests ---

type fakeKioskStore struct {
	kiosksByCode map[string]*Kiosk
	kiosksByID   map[string]*Kiosk
	allKiosks    []*Kiosk

	createErr error
	listErr   error
	updateErr error
}

func (s *fakeKioskStore) List(_ context.Context) ([]*Kiosk, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	return s.allKiosks, nil
}

func (s *fakeKioskStore) ListByProject(_ context.Context, projectID string) ([]*Kiosk, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	var result []*Kiosk
	for _, k := range s.allKiosks {
		if k.ProjectID == projectID {
			result = append(result, k)
		}
	}
	return result, nil
}

func (s *fakeKioskStore) Create(_ context.Context, k *Kiosk) error {
	if s.createErr != nil {
		return s.createErr
	}
	if s.kiosksByCode == nil {
		s.kiosksByCode = map[string]*Kiosk{}
	}
	if s.kiosksByID == nil {
		s.kiosksByID = map[string]*Kiosk{}
	}
	if _, exists := s.kiosksByCode[k.Code]; exists {
		return ErrDuplicateKioskCode
	}
	s.kiosksByCode[k.Code] = k
	s.kiosksByID[k.ID] = k
	s.allKiosks = append(s.allKiosks, k)
	return nil
}

func (s *fakeKioskStore) GetByCode(_ context.Context, code string) (*Kiosk, error) {
	return s.kiosksByCode[code], nil
}

func (s *fakeKioskStore) GetByID(_ context.Context, id string) (*Kiosk, error) {
	return s.kiosksByID[id], nil
}

func (s *fakeKioskStore) Update(_ context.Context, k *Kiosk) error {
	if s.updateErr != nil {
		return s.updateErr
	}
	if existing, ok := s.kiosksByID[k.ID]; ok {
		existing.DisplayName = k.DisplayName
		existing.Active = k.Active
		return nil
	}
	return ErrKioskNotFound
}

func (s *fakeKioskStore) UpdatePIN(_ context.Context, id, pinHash string) error {
	if k, ok := s.kiosksByID[id]; ok {
		k.PinHash = pinHash
		return nil
	}
	return ErrKioskNotFound
}

type fakeKioskJWT struct {
	issueToken    string
	issueExpires  time.Time
	issueErr      error
	parseClaims   *KioskTokenClaims
	parseErr      error
}

func (m *fakeKioskJWT) IssueKiosk(_ *Kiosk) (string, time.Time, error) {
	if m.issueErr != nil {
		return "", time.Time{}, m.issueErr
	}
	return m.issueToken, m.issueExpires, nil
}

func (m *fakeKioskJWT) ParseKiosk(_ string) (*KioskTokenClaims, error) {
	return m.parseClaims, m.parseErr
}

// fakeKioskJWT also implements UserTokenParser for admin auth.
func (m *fakeKioskJWT) Parse(_ string) (*TokenClaims, error) {
	return &TokenClaims{Role: string(RoleAdmin)}, nil
}

type fakeAdminParser struct {
	role Role
	err  error
}

func (p *fakeAdminParser) Parse(_ string) (*TokenClaims, error) {
	if p.err != nil {
		return nil, p.err
	}
	return &TokenClaims{Role: string(p.role)}, nil
}

type alwaysAllowLimiter struct{}

func (alwaysAllowLimiter) Allow(_ string) bool { return true }
func (alwaysAllowLimiter) Reset()              {}

type alwaysDenyLimiter struct{}

func (alwaysDenyLimiter) Allow(_ string) bool { return false }
func (alwaysDenyLimiter) Reset()              {}

func makeKioskPW(t *testing.T, pin string) string {
	t.Helper()
	h, err := HashPassword(pin)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	return h
}

func setupKioskHandler(t *testing.T, store *fakeKioskStore, jwt *fakeKioskJWT, parser UserTokenParser) *KioskServer {
	t.Helper()
	if parser == nil {
		parser = &fakeAdminParser{role: RoleAdmin}
	}
	return &KioskServer{
		store:      store,
		passwd:     &passwordHelper{Hash: HashPassword, Verify: VerifyPassword},
		jwt:        jwt,
		userParser: parser,
	}
}

// ── Compile-time checks ─────────────────────────────────────────────

var _ KioskStore = (*fakeKioskStore)(nil)
var _ KioskTokenManager = (*fakeKioskJWT)(nil)
var _ UserTokenParser = (*fakeKioskJWT)(nil)
var _ UserTokenParser = (*fakeAdminParser)(nil)
var _ LoginRateLimiter = alwaysAllowLimiter{}
var _ LoginRateLimiter = alwaysDenyLimiter{}
var _ kioskv1connect.KioskServiceHandler = (*KioskServer)(nil)

// ── KioskLogin tests ────────────────────────────────────────────────

func TestKioskLoginSuccess(t *testing.T) {
	pwHash := makeKioskPW(t, "123456")
	store := &fakeKioskStore{
		kiosksByCode: map[string]*Kiosk{
			"KIOSK-1": {ID: "k1", Code: "KIOSK-1", DisplayName: "Ward 3A", PinHash: pwHash, Active: true},
		},
		kiosksByID: map[string]*Kiosk{
			"k1": {ID: "k1", Code: "KIOSK-1", DisplayName: "Ward 3A", PinHash: pwHash, Active: true},
		},
	}
	jwt := &fakeKioskJWT{
		issueToken:   "kiosk-jwt-token",
		issueExpires: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
	}
	h := setupKioskHandler(t, store, jwt, nil)

	req := connect.NewRequest(&kioskv1.KioskLoginRequest{Code: "KIOSK-1", Pin: "123456"})
	resp, err := h.KioskLogin(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Msg.AccessToken != "kiosk-jwt-token" {
		t.Errorf("AccessToken = %q, want kiosk-jwt-token", resp.Msg.AccessToken)
	}
	if resp.Msg.Kiosk == nil {
		t.Fatal("expected kiosk in response")
	}
	if resp.Msg.Kiosk.Code != "KIOSK-1" {
		t.Errorf("Kiosk.Code = %q", resp.Msg.Kiosk.Code)
	}
}

func TestKioskLoginWrongPIN(t *testing.T) {
	pwHash := makeKioskPW(t, "correct")
	store := &fakeKioskStore{
		kiosksByCode: map[string]*Kiosk{
			"KIOSK-1": {ID: "k1", Code: "KIOSK-1", DisplayName: "K", PinHash: pwHash, Active: true},
		},
		kiosksByID: map[string]*Kiosk{
			"k1": {ID: "k1", Code: "KIOSK-1", DisplayName: "K", PinHash: pwHash, Active: true},
		},
	}
	jwt := &fakeKioskJWT{issueToken: "t", issueExpires: time.Now()}
	h := setupKioskHandler(t, store, jwt, nil)

	req := connect.NewRequest(&kioskv1.KioskLoginRequest{Code: "KIOSK-1", Pin: "wrong"})
	_, err := h.KioskLogin(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for wrong PIN")
	}
	assertConnectCode(t, err, connect.CodeUnauthenticated)
}

func TestKioskLoginUnknownCode(t *testing.T) {
	store := &fakeKioskStore{
		kiosksByCode: map[string]*Kiosk{},
		kiosksByID:   map[string]*Kiosk{},
	}
	jwt := &fakeKioskJWT{issueToken: "t", issueExpires: time.Now()}
	h := setupKioskHandler(t, store, jwt, nil)

	req := connect.NewRequest(&kioskv1.KioskLoginRequest{Code: "UNKNOWN", Pin: "123456"})
	_, err := h.KioskLogin(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for unknown code")
	}
	assertConnectCode(t, err, connect.CodeUnauthenticated)
}

func TestKioskLoginInactiveKiosk(t *testing.T) {
	pwHash := makeKioskPW(t, "123456")
	store := &fakeKioskStore{
		kiosksByCode: map[string]*Kiosk{
			"KIOSK-1": {ID: "k1", Code: "KIOSK-1", DisplayName: "Inactive", PinHash: pwHash, Active: false},
		},
		kiosksByID: map[string]*Kiosk{
			"k1": {ID: "k1", Code: "KIOSK-1", DisplayName: "Inactive", PinHash: pwHash, Active: false},
		},
	}
	jwt := &fakeKioskJWT{issueToken: "t", issueExpires: time.Now()}
	h := setupKioskHandler(t, store, jwt, nil)

	req := connect.NewRequest(&kioskv1.KioskLoginRequest{Code: "KIOSK-1", Pin: "123456"})
	_, err := h.KioskLogin(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for inactive kiosk")
	}
	assertConnectCode(t, err, connect.CodePermissionDenied)
}

func TestKioskLoginMissingCode(t *testing.T) {
	h := setupKioskHandler(t, &fakeKioskStore{}, &fakeKioskJWT{}, nil)
	req := connect.NewRequest(&kioskv1.KioskLoginRequest{Code: "", Pin: "123456"})
	_, err := h.KioskLogin(context.Background(), req)
	assertConnectCode(t, err, connect.CodeInvalidArgument)
}

func TestKioskLoginMissingPIN(t *testing.T) {
	h := setupKioskHandler(t, &fakeKioskStore{}, &fakeKioskJWT{}, nil)
	req := connect.NewRequest(&kioskv1.KioskLoginRequest{Code: "K1", Pin: ""})
	_, err := h.KioskLogin(context.Background(), req)
	assertConnectCode(t, err, connect.CodeInvalidArgument)
}

func TestKioskLoginRateLimited(t *testing.T) {
	pwHash := makeKioskPW(t, "123456")
	store := &fakeKioskStore{
		kiosksByCode: map[string]*Kiosk{
			"KIOSK-1": {ID: "k1", Code: "KIOSK-1", DisplayName: "K", PinHash: pwHash, Active: true},
		},
		kiosksByID: map[string]*Kiosk{
			"k1": {ID: "k1", Code: "KIOSK-1", DisplayName: "K", PinHash: pwHash, Active: true},
		},
	}
	jwt := &fakeKioskJWT{issueToken: "t", issueExpires: time.Now()}
	h := &KioskServer{
		store:      store,
		passwd:     &passwordHelper{Hash: HashPassword, Verify: VerifyPassword},
		jwt:        jwt,
		userParser: &fakeAdminParser{role: RoleAdmin},
		idLimiter:  alwaysDenyLimiter{},
		ipLimiter:  alwaysDenyLimiter{},
	}

	req := connect.NewRequest(&kioskv1.KioskLoginRequest{Code: "KIOSK-1", Pin: "123456"})
	_, err := h.KioskLogin(context.Background(), req)
	assertConnectCode(t, err, connect.CodeResourceExhausted)
}

// ── KioskValidate tests ─────────────────────────────────────────────

func TestKioskValidateSuccess(t *testing.T) {
	store := &fakeKioskStore{
		kiosksByID: map[string]*Kiosk{
			"k1": {ID: "k1", Code: "KIOSK-1", DisplayName: "Valid", Active: true},
		},
	}
	jwt := &fakeKioskJWT{
		parseClaims: &KioskTokenClaims{
			RegisteredClaims: jwt.RegisteredClaims{Subject: "k1"},
		},
	}
	h := setupKioskHandler(t, store, jwt, nil)

	req := connect.NewRequest(&kioskv1.KioskValidateRequest{})
	req.Header().Set("Authorization", "Bearer valid-token")
	resp, err := h.KioskValidate(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Msg.Kiosk == nil {
		t.Fatal("expected kiosk in response")
	}
	if resp.Msg.Kiosk.Code != "KIOSK-1" {
		t.Errorf("Kiosk.Code = %q", resp.Msg.Kiosk.Code)
	}
}

func TestKioskValidateMissingToken(t *testing.T) {
	h := setupKioskHandler(t, &fakeKioskStore{}, &fakeKioskJWT{}, nil)
	req := connect.NewRequest(&kioskv1.KioskValidateRequest{})
	_, err := h.KioskValidate(context.Background(), req)
	assertConnectCode(t, err, connect.CodeUnauthenticated)
}

func TestKioskValidateInvalidToken(t *testing.T) {
	jwt := &fakeKioskJWT{parseErr: errors.New("invalid token")}
	h := setupKioskHandler(t, &fakeKioskStore{}, jwt, nil)

	req := connect.NewRequest(&kioskv1.KioskValidateRequest{})
	req.Header().Set("Authorization", "Bearer bad-token")
	_, err := h.KioskValidate(context.Background(), req)
	assertConnectCode(t, err, connect.CodeUnauthenticated)
}

func TestKioskValidateKioskNotFound(t *testing.T) {
	store := &fakeKioskStore{
		kiosksByID: map[string]*Kiosk{},
	}
	jwt := &fakeKioskJWT{
		parseClaims: &KioskTokenClaims{
			RegisteredClaims: jwt.RegisteredClaims{Subject: "ghost-id"},
		},
	}
	h := setupKioskHandler(t, store, jwt, nil)

	req := connect.NewRequest(&kioskv1.KioskValidateRequest{})
	req.Header().Set("Authorization", "Bearer ghost-token")
	_, err := h.KioskValidate(context.Background(), req)
	assertConnectCode(t, err, connect.CodeUnauthenticated)
}

func TestKioskValidateInactiveKiosk(t *testing.T) {
	store := &fakeKioskStore{
		kiosksByID: map[string]*Kiosk{
			"k1": {ID: "k1", Code: "KIOSK-1", DisplayName: "Inactive", Active: false},
		},
	}
	jwt := &fakeKioskJWT{
		parseClaims: &KioskTokenClaims{
			RegisteredClaims: jwt.RegisteredClaims{Subject: "k1"},
		},
	}
	h := setupKioskHandler(t, store, jwt, nil)

	req := connect.NewRequest(&kioskv1.KioskValidateRequest{})
	req.Header().Set("Authorization", "Bearer valid-token")
	_, err := h.KioskValidate(context.Background(), req)
	assertConnectCode(t, err, connect.CodePermissionDenied)
}

// ── Admin authorization tests ───────────────────────────────────────

func TestListKiosksRequiresAdmin(t *testing.T) {
	parser := &fakeAdminParser{role: RoleNurse}
	h := setupKioskHandler(t, &fakeKioskStore{}, &fakeKioskJWT{}, parser)

	req := connect.NewRequest(&kioskv1.ListKiosksRequest{})
	req.Header().Set("Authorization", "Bearer nurse-token")
	_, err := h.ListKiosks(context.Background(), req)
	assertConnectCode(t, err, connect.CodePermissionDenied)
}

func TestListKiosksRequiresAuth(t *testing.T) {
	h := setupKioskHandler(t, &fakeKioskStore{}, &fakeKioskJWT{}, nil)
	req := connect.NewRequest(&kioskv1.ListKiosksRequest{})
	_, err := h.ListKiosks(context.Background(), req)
	assertConnectCode(t, err, connect.CodeUnauthenticated)
}

func TestListKiosksSuccess(t *testing.T) {
	store := &fakeKioskStore{
		allKiosks: []*Kiosk{
			{ID: "a", Code: "A", DisplayName: "First"},
			{ID: "b", Code: "B", DisplayName: "Second"},
		},
	}
	h := setupKioskHandler(t, store, &fakeKioskJWT{}, nil)

	req := connect.NewRequest(&kioskv1.ListKiosksRequest{})
	req.Header().Set("Authorization", "Bearer admin-token")
	resp, err := h.ListKiosks(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Msg.Kiosks) != 2 {
		t.Fatalf("expected 2 kiosks, got %d", len(resp.Msg.Kiosks))
	}
}

func TestCreateKioskRequiresAdmin(t *testing.T) {
	parser := &fakeAdminParser{role: RolePharmacist}
	h := setupKioskHandler(t, &fakeKioskStore{}, &fakeKioskJWT{}, parser)

	req := connect.NewRequest(&kioskv1.CreateKioskRequest{Code: "K1", DisplayName: "Test", Pin: "1234"})
	req.Header().Set("Authorization", "Bearer pharma-token")
	_, err := h.CreateKiosk(context.Background(), req)
	assertConnectCode(t, err, connect.CodePermissionDenied)
}

func TestCreateKioskSuccess(t *testing.T) {
	store := &fakeKioskStore{}
	h := setupKioskHandler(t, store, &fakeKioskJWT{}, nil)

	req := connect.NewRequest(&kioskv1.CreateKioskRequest{Code: "NEW-K", DisplayName: "New Kiosk", Pin: "123456"})
	req.Header().Set("Authorization", "Bearer admin-token")
	resp, err := h.CreateKiosk(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Msg.Kiosk == nil {
		t.Fatal("expected kiosk in response")
	}
	if resp.Msg.Kiosk.Code != "NEW-K" {
		t.Errorf("Kiosk.Code = %q", resp.Msg.Kiosk.Code)
	}
	if resp.Msg.Kiosk.Pin == nil || *resp.Msg.Kiosk.Pin != "123456" {
		t.Error("expected plaintext PIN in create response")
	}
}

func TestCreateKioskDuplicateCode(t *testing.T) {
	store := &fakeKioskStore{
		kiosksByCode: map[string]*Kiosk{
			"DUP": {ID: "d", Code: "DUP"},
		},
		createErr: ErrDuplicateKioskCode,
	}
	h := setupKioskHandler(t, store, &fakeKioskJWT{}, nil)

	req := connect.NewRequest(&kioskv1.CreateKioskRequest{Code: "DUP", DisplayName: "Dupe", Pin: "1234"})
	req.Header().Set("Authorization", "Bearer admin-token")
	_, err := h.CreateKiosk(context.Background(), req)
	assertConnectCode(t, err, connect.CodeAlreadyExists)
}

func TestCreateKioskMissingCode(t *testing.T) {
	h := setupKioskHandler(t, &fakeKioskStore{}, &fakeKioskJWT{}, nil)
	req := connect.NewRequest(&kioskv1.CreateKioskRequest{Code: "", DisplayName: "No Code", Pin: "1234"})
	req.Header().Set("Authorization", "Bearer admin-token")
	_, err := h.CreateKiosk(context.Background(), req)
	assertConnectCode(t, err, connect.CodeInvalidArgument)
}

func TestCreateKioskMissingPin(t *testing.T) {
	h := setupKioskHandler(t, &fakeKioskStore{}, &fakeKioskJWT{}, nil)
	req := connect.NewRequest(&kioskv1.CreateKioskRequest{Code: "K1", DisplayName: "No Pin", Pin: ""})
	req.Header().Set("Authorization", "Bearer admin-token")
	_, err := h.CreateKiosk(context.Background(), req)
	assertConnectCode(t, err, connect.CodeInvalidArgument)
}

func TestUpdateKioskRequiresAdmin(t *testing.T) {
	parser := &fakeAdminParser{role: RoleNurse}
	h := setupKioskHandler(t, &fakeKioskStore{}, &fakeKioskJWT{}, parser)

	req := connect.NewRequest(&kioskv1.UpdateKioskRequest{Id: "k1"})
	req.Header().Set("Authorization", "Bearer nurse-token")
	_, err := h.UpdateKiosk(context.Background(), req)
	assertConnectCode(t, err, connect.CodePermissionDenied)
}

func TestResetKioskPinRequiresAdmin(t *testing.T) {
	parser := &fakeAdminParser{role: RoleRefiller}
	h := setupKioskHandler(t, &fakeKioskStore{}, &fakeKioskJWT{}, parser)

	req := connect.NewRequest(&kioskv1.ResetKioskPinRequest{Id: "k1", NewPin: "1234"})
	req.Header().Set("Authorization", "Bearer refill-token")
	_, err := h.ResetKioskPin(context.Background(), req)
	assertConnectCode(t, err, connect.CodePermissionDenied)
}


