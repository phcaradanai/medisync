package identity

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	identityv1 "github.com/adm-chura3inter/medisync/services/core/internal/gen/medisync/identity/v1"
	identityv1connect "github.com/adm-chura3inter/medisync/services/core/internal/gen/medisync/identity/v1/identityv1connect"
	"github.com/golang-jwt/jwt/v5"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// --- extractBearer tests ---

func TestExtractBearerValid(t *testing.T) {
	h := http.Header{}
	h.Set("Authorization", "Bearer abc.def.ghi")
	token, err := extractBearer(h)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token != "abc.def.ghi" {
		t.Errorf("token = %q, want abc.def.ghi", token)
	}
}

func TestExtractBearerMissingHeader(t *testing.T) {
	_, err := extractBearer(http.Header{})
	if err == nil {
		t.Fatal("expected error for missing header")
	}
}

func TestExtractBearerNonBearerScheme(t *testing.T) {
	h := http.Header{}
	h.Set("Authorization", "Basic dXNlcjpwYXNz")
	_, err := extractBearer(h)
	if err == nil {
		t.Fatal("expected error for non-Bearer scheme")
	}
	if !strings.Contains(err.Error(), "Bearer") {
		t.Errorf("error should mention Bearer: %v", err)
	}
}

func TestExtractBearerEmptyToken(t *testing.T) {
	h := http.Header{}
	h.Set("Authorization", "Bearer ")
	_, err := extractBearer(h)
	if err == nil {
		t.Fatal("expected error for empty Bearer token")
	}
}

func TestExtractBearerNoSpace(t *testing.T) {
	h := http.Header{}
	h.Set("Authorization", "BearerOnly")
	_, err := extractBearer(h)
	if err == nil {
		t.Fatal("expected error for missing space after Bearer")
	}
}

func TestExtractBearerCaseInsensitive(t *testing.T) {
	h := http.Header{}
	h.Set("Authorization", "bearer mytoken")
	token, err := extractBearer(h)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token != "mytoken" {
		t.Errorf("token = %q", token)
	}
}

// --- toProtoUser tests ---

func TestToProtoUserStripsPasswordAndCardToken(t *testing.T) {
	u := &User{
		ID:           "proto-1",
		Username:     "testuser",
		DisplayName:  "Test User",
		PasswordHash: "secret-hash",
		Role:         RoleNurse,
		WardIDs:      []string{"WARD-3A"},
		Active:       true,
		CreatedAt:    time.Date(2026, 7, 13, 10, 0, 0, 0, time.UTC),
	}

	pb := toProtoUser(u)
	if pb.Id != "proto-1" {
		t.Errorf("Id = %q", pb.Id)
	}
	if pb.Username != "testuser" {
		t.Errorf("Username = %q", pb.Username)
	}
	if pb.DisplayName != "Test User" {
		t.Errorf("DisplayName = %q", pb.DisplayName)
	}
	if pb.Role != identityv1.Role_ROLE_NURSE {
		t.Errorf("Role = %v", pb.Role)
	}
	if pb.Active != true {
		t.Error("Active should be true")
	}
	if len(pb.WardIds) != 1 || pb.WardIds[0] != "WARD-3A" {
		t.Errorf("WardIds = %v", pb.WardIds)
	}
	// Verify that PasswordHash and CardToken are NOT in the proto.
	// The proto User has no field for them, so there's nothing to
	// check other than that toProtoUser doesn't panic.
}

func TestToProtoUserAllRoles(t *testing.T) {
	tests := []struct {
		domain Role
		proto  identityv1.Role
	}{
		{RoleAdmin, identityv1.Role_ROLE_ADMIN},
		{RolePharmacist, identityv1.Role_ROLE_PHARMACIST},
		{RoleNurse, identityv1.Role_ROLE_NURSE},
		{RoleRefiller, identityv1.Role_ROLE_REFILLER},
	}
	for _, tt := range tests {
		u := &User{ID: "x", Role: tt.domain}
		pb := toProtoUser(u)
		if pb.Role != tt.proto {
			t.Errorf("domain %v -> proto %v, want %v", tt.domain, pb.Role, tt.proto)
		}
	}
}

func TestToProtoUserNil(t *testing.T) {
	pb := toProtoUser(nil)
	if pb != nil {
		t.Errorf("expected nil, got %+v", pb)
	}
}

// --- Handler tests ---

func setupHandler(t *testing.T, store *fakeUserStore, tm *fakeTokenManager) *IdentityServer {
	t.Helper()
	svc := &AuthService{
		store:  store,
		passwd: &passwordHelper{Hash: HashPassword, Verify: VerifyPassword},
		jwt:    tm,
	}
	return NewIdentityServer(svc, nil)
}

func TestHandlerLoginSuccess(t *testing.T) {
	pwHash := makeHash(t, "secret123")
	store := &fakeUserStore{
		usersByUsername: map[string]*User{
			"nurse1": {ID: "u1", Username: "nurse1", PasswordHash: pwHash, Role: RoleNurse, Active: true, WardIDs: []string{"WARD-3A"}},
		},
	}
	tm := &fakeTokenManager{
		fixedToken:     "jwt-handler-login",
		fixedExpiresAt: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
	}
	h := setupHandler(t, store, tm)

	req := connect.NewRequest(&identityv1.LoginRequest{
		Username: "nurse1",
		Password: "secret123",
	})
	resp, err := h.Login(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Msg.AccessToken != "jwt-handler-login" {
		t.Errorf("AccessToken = %q", resp.Msg.AccessToken)
	}
	if resp.Msg.User == nil {
		t.Fatal("expected user in response")
	}
	if resp.Msg.User.Username != "nurse1" {
		t.Errorf("Username = %q", resp.Msg.User.Username)
	}
}

func TestHandlerLoginInvalidCredentials(t *testing.T) {
	store := &fakeUserStore{usersByUsername: map[string]*User{}}
	h := setupHandler(t, store, &fakeTokenManager{})

	req := connect.NewRequest(&identityv1.LoginRequest{
		Username: "ghost",
		Password: "boo",
	})
	_, err := h.Login(context.Background(), req)
	if err == nil {
		t.Fatal("expected error")
	}
	connectErr := new(connect.Error)
	if !errors.As(err, &connectErr) {
		t.Fatalf("expected *connect.Error, got %T", err)
	}
	if connectErr.Code() != connect.CodeUnauthenticated {
		t.Errorf("code = %v, want CodeUnauthenticated", connectErr.Code())
	}
}

func TestHandlerLoginMissingUsername(t *testing.T) {
	h := setupHandler(t, &fakeUserStore{}, &fakeTokenManager{})
	req := connect.NewRequest(&identityv1.LoginRequest{Username: "", Password: "pw"})
	_, err := h.Login(context.Background(), req)
	assertConnectCode(t, err, connect.CodeInvalidArgument)
}

func TestHandlerLoginMissingPassword(t *testing.T) {
	h := setupHandler(t, &fakeUserStore{}, &fakeTokenManager{})
	req := connect.NewRequest(&identityv1.LoginRequest{Username: "u", Password: ""})
	_, err := h.Login(context.Background(), req)
	assertConnectCode(t, err, connect.CodeInvalidArgument)
}

func TestHandlerLoginInactiveUser(t *testing.T) {
	store := &fakeUserStore{
		usersByUsername: map[string]*User{
			"inactive": {ID: "i1", Username: "inactive", PasswordHash: makeHash(t, "pw"), Role: RoleNurse, Active: false},
		},
	}
	h := setupHandler(t, store, &fakeTokenManager{})
	req := connect.NewRequest(&identityv1.LoginRequest{Username: "inactive", Password: "pw"})
	_, err := h.Login(context.Background(), req)
	assertConnectCode(t, err, connect.CodePermissionDenied)
}

func TestHandlerCardLoginSuccess(t *testing.T) {
	store := &fakeUserStore{
		usersByCardToken: map[string]*User{
			"card-ok": {ID: "cu1", Username: "carduser", Role: RolePharmacist, Active: true},
		},
	}
	tm := &fakeTokenManager{
		fixedToken:     "jwt-card-handler",
		fixedExpiresAt: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
	}
	h := setupHandler(t, store, tm)

	req := connect.NewRequest(&identityv1.CardLoginRequest{CardToken: "card-ok"})
	resp, err := h.CardLogin(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Msg.AccessToken != "jwt-card-handler" {
		t.Errorf("AccessToken = %q", resp.Msg.AccessToken)
	}
}

func TestHandlerCardLoginMissingToken(t *testing.T) {
	h := setupHandler(t, &fakeUserStore{}, &fakeTokenManager{})
	req := connect.NewRequest(&identityv1.CardLoginRequest{CardToken: ""})
	_, err := h.CardLogin(context.Background(), req)
	assertConnectCode(t, err, connect.CodeInvalidArgument)
}

func TestHandlerCardLoginInvalidToken(t *testing.T) {
	store := &fakeUserStore{usersByCardToken: map[string]*User{}}
	h := setupHandler(t, store, &fakeTokenManager{})
	req := connect.NewRequest(&identityv1.CardLoginRequest{CardToken: "no-such"})
	_, err := h.CardLogin(context.Background(), req)
	assertConnectCode(t, err, connect.CodeUnauthenticated)
}

func TestHandlerWhoAmISuccess(t *testing.T) {
	store := &fakeUserStore{
		usersByID: map[string]*User{
			"who-1": {ID: "who-1", Username: "whoami-user", Role: RoleNurse, Active: true},
		},
	}
	tm := &fakeTokenManager{
		parseResult: &TokenClaims{
			RegisteredClaims: jwt.RegisteredClaims{Subject: "who-1"},
		},
	}
	h := setupHandler(t, store, tm)

	req := connect.NewRequest(&identityv1.WhoAmIRequest{})
	req.Header().Set("Authorization", "Bearer valid-who-token")
	resp, err := h.WhoAmI(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Msg.User == nil {
		t.Fatal("expected user in response")
	}
	if resp.Msg.User.Username != "whoami-user" {
		t.Errorf("Username = %q", resp.Msg.User.Username)
	}
}

func TestHandlerWhoAmIMissingAuthHeader(t *testing.T) {
	h := setupHandler(t, &fakeUserStore{}, &fakeTokenManager{})
	req := connect.NewRequest(&identityv1.WhoAmIRequest{})
	_, err := h.WhoAmI(context.Background(), req)
	assertConnectCode(t, err, connect.CodeUnauthenticated)
}

func TestHandlerWhoAmIExpiredToken(t *testing.T) {
	tm := &fakeTokenManager{parseErr: errors.New("token expired")}
	h := setupHandler(t, &fakeUserStore{}, tm)

	req := connect.NewRequest(&identityv1.WhoAmIRequest{})
	req.Header().Set("Authorization", "Bearer expired-token")
	_, err := h.WhoAmI(context.Background(), req)
	assertConnectCode(t, err, connect.CodeUnauthenticated)
}

func TestHandlerWhoAmIInactiveUser(t *testing.T) {
	store := &fakeUserStore{
		usersByID: map[string]*User{
			"who-inact": {ID: "who-inact", Role: RoleNurse, Active: false},
		},
	}
	tm := &fakeTokenManager{
		parseResult: &TokenClaims{
			RegisteredClaims: jwt.RegisteredClaims{Subject: "who-inact"},
		},
	}
	h := setupHandler(t, store, tm)

	req := connect.NewRequest(&identityv1.WhoAmIRequest{})
	req.Header().Set("Authorization", "Bearer inact-token")
	_, err := h.WhoAmI(context.Background(), req)
	assertConnectCode(t, err, connect.CodePermissionDenied)
}

// assertConnectCode checks that err is a *connect.Error with the expected code.
func assertConnectCode(t *testing.T, err error, want connect.Code) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected connect error with code %v, got nil", want)
	}
	var ce *connect.Error
	if !errors.As(err, &ce) {
		t.Fatalf("expected *connect.Error, got %T: %v", err, err)
	}
	if ce.Code() != want {
		t.Errorf("code = %v, want %v", ce.Code(), want)
	}
}

// --- Domain-to-proto round-trip smoke ---

func TestToProtoTimestampZero(t *testing.T) {
	ts := toProtoTimestamp(time.Time{})
	if ts != nil {
		t.Error("expected nil for zero time")
	}
}

func TestToProtoTimestampValid(t *testing.T) {
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	ts := toProtoTimestamp(now)
	if ts == nil {
		t.Fatal("expected non-nil timestamp")
	}
	if !ts.AsTime().Equal(now) {
		t.Errorf("ts = %v, want %v", ts.AsTime(), now)
	}
}

// Ensure unused timestamp import is used in tests.
var _ = timestamppb.Now

// ── extractIP tests ────────────────────────────────────────────────

func TestExtractIPWithPort(t *testing.T) {
	ip := extractIP("192.168.1.1:54321")
	if ip != "192.168.1.1" {
		t.Errorf("extractIP = %q, want 192.168.1.1", ip)
	}
}

func TestExtractIPWithoutPort(t *testing.T) {
	ip := extractIP("10.0.0.1")
	if ip != "10.0.0.1" {
		t.Errorf("extractIP = %q, want 10.0.0.1", ip)
	}
}

func TestExtractIPEmpty(t *testing.T) {
	ip := extractIP("")
	if ip != "" {
		t.Errorf("extractIP should return empty for empty input, got %q", ip)
	}
}

func TestExtractIPIPv6(t *testing.T) {
	ip := extractIP("[::1]:12345")
	if ip != "::1" {
		t.Errorf("extractIP = %q, want ::1", ip)
	}
}

// ── Rate-limit handler tests ───────────────────────────────────────

// fakeLimiter is a deterministic rate limiter for tests.
type fakeLimiter struct {
	allow bool
	calls []string
}

func (f *fakeLimiter) Allow(key string) bool {
	f.calls = append(f.calls, key)
	return f.allow
}

func (f *fakeLimiter) Reset() {
	f.calls = nil
}

func setupHandlerWithRateLimit(t *testing.T, store *fakeUserStore, tm *fakeTokenManager, idLim, ipLim LoginRateLimiter) *IdentityServer {
	t.Helper()
	svc := &AuthService{
		store:  store,
		passwd: &passwordHelper{Hash: HashPassword, Verify: VerifyPassword},
		jwt:    tm,
	}
	return NewIdentityServerWithRateLimit(svc, nil, idLim, ipLim)
}

func TestHandlerLoginRateLimitedByIdentifier(t *testing.T) {
	pwHash := makeHash(t, "secret123")
	store := &fakeUserStore{
		usersByUsername: map[string]*User{
			"nurse1": {ID: "u1", Username: "nurse1", PasswordHash: pwHash, Role: RoleNurse, Active: true},
		},
	}
	tm := &fakeTokenManager{
		fixedToken:     "jwt-token",
		fixedExpiresAt: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
	}
	idLim := &fakeLimiter{allow: false} // Always deny
	h := setupHandlerWithRateLimit(t, store, tm, idLim, nil)

	req := connect.NewRequest(&identityv1.LoginRequest{
		Username: "nurse1",
		Password: "secret123",
	})
	_, err := h.Login(context.Background(), req)
	assertConnectCode(t, err, connect.CodeResourceExhausted)

	// Verify the limiter was called with the username.
	if len(idLim.calls) != 1 || idLim.calls[0] != "nurse1" {
		t.Errorf("id limiter calls = %v, want [nurse1]", idLim.calls)
	}
}

func TestHandlerLoginRateLimitedByIP(t *testing.T) {
	// IP rate limiting is tested via HTTP round-trip because
	// connect.NewRequest doesn't expose peer address in unit tests.
	pwHash := makeHash(t, "secret123")
	store := &fakeUserStore{
		usersByUsername: map[string]*User{
			"nurse1": {ID: "u1", Username: "nurse1", PasswordHash: pwHash, Role: RoleNurse, Active: true, CreatedAt: time.Now()},
		},
	}
	tm := &fakeTokenManager{
		fixedToken:     "jwt-ip-test",
		fixedExpiresAt: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
	}

	// Create a server with a limiter that blocks the IP "127.0.0.1".
	svc := &AuthService{
		store:  store,
		passwd: &passwordHelper{Hash: HashPassword, Verify: VerifyPassword},
		jwt:    tm,
	}
	ipLim := &fakeLimiter{allow: false}
	server := NewIdentityServerWithRateLimit(svc, nil, &fakeLimiter{allow: true}, ipLim)
	mux := http.NewServeMux()
	path, handler := identityv1connect.NewIdentityServiceHandler(server)
	mux.Handle(path, handler)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	client := identityv1connect.NewIdentityServiceClient(http.DefaultClient, ts.URL)
	_, err := client.Login(context.Background(),
		connect.NewRequest(&identityv1.LoginRequest{Username: "nurse1", Password: "secret123"}))
	assertConnectCode(t, err, connect.CodeResourceExhausted)

	// Verify IP was checked.
	if len(ipLim.calls) != 1 || ipLim.calls[0] != "127.0.0.1" {
		t.Errorf("ip limiter calls = %v, want [127.0.0.1]", ipLim.calls)
	}
}

func TestHandlerCardLoginRateLimitedByIdentifier(t *testing.T) {
	store := &fakeUserStore{
		usersByCardToken: map[string]*User{
			"card-ok": {ID: "cu1", Username: "carduser", Role: RolePharmacist, Active: true},
		},
	}
	tm := &fakeTokenManager{
		fixedToken:     "jwt-card",
		fixedExpiresAt: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
	}
	idLim := &fakeLimiter{allow: false}
	h := setupHandlerWithRateLimit(t, store, tm, idLim, nil)

	req := connect.NewRequest(&identityv1.CardLoginRequest{CardToken: "card-ok"})
	_, err := h.CardLogin(context.Background(), req)
	assertConnectCode(t, err, connect.CodeResourceExhausted)
}

func TestHandlerLoginNoLimitersPassThrough(t *testing.T) {
	// When no limiters are set (nil, nil), login should work normally.
	pwHash := makeHash(t, "secret123")
	store := &fakeUserStore{
		usersByUsername: map[string]*User{
			"nurse1": {ID: "u1", Username: "nurse1", PasswordHash: pwHash, Role: RoleNurse, Active: true},
		},
	}
	tm := &fakeTokenManager{
		fixedToken:     "jwt-no-lim",
		fixedExpiresAt: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
	}
	h := setupHandlerWithRateLimit(t, store, tm, nil, nil)

	req := connect.NewRequest(&identityv1.LoginRequest{
		Username: "nurse1",
		Password: "secret123",
	})
	resp, err := h.Login(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error with nil limiters: %v", err)
	}
	if resp.Msg.AccessToken != "jwt-no-lim" {
		t.Errorf("AccessToken = %q", resp.Msg.AccessToken)
	}
}

func TestHandlerCardLoginNoLimitersPassThrough(t *testing.T) {
	store := &fakeUserStore{
		usersByCardToken: map[string]*User{
			"card-ok": {ID: "cu1", Username: "carduser", Role: RolePharmacist, Active: true},
		},
	}
	tm := &fakeTokenManager{
		fixedToken:     "jwt-card",
		fixedExpiresAt: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
	}
	h := setupHandlerWithRateLimit(t, store, tm, nil, nil)

	req := connect.NewRequest(&identityv1.CardLoginRequest{CardToken: "card-ok"})
	resp, err := h.CardLogin(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error with nil limiters: %v", err)
	}
	if resp.Msg.AccessToken != "jwt-card" {
		t.Errorf("AccessToken = %q", resp.Msg.AccessToken)
	}
}

func TestHandlerLoginRateLimitedUniformError(t *testing.T) {
	// Both identifier and IP produce the same error — clients can't distinguish.
	// Tested via HTTP round-trips so peer addresses are present.

	// Block by identifier (allow IP).
	idBlockStore := &fakeUserStore{
		usersByUsername: map[string]*User{
			"nurse1": {ID: "u1", Username: "nurse1", PasswordHash: makeHash(t, "pw"), Role: RoleNurse, Active: true, CreatedAt: time.Now()},
		},
	}
	idBlockSvc := &AuthService{
		store:  idBlockStore,
		passwd: &passwordHelper{Hash: HashPassword, Verify: VerifyPassword},
		jwt:    &fakeTokenManager{fixedToken: "t", fixedExpiresAt: time.Now()},
	}
	idBlockServer := NewIdentityServerWithRateLimit(idBlockSvc, nil, &fakeLimiter{allow: false}, &fakeLimiter{allow: true})

	mux1 := http.NewServeMux()
	p1, h1 := identityv1connect.NewIdentityServiceHandler(idBlockServer)
	mux1.Handle(p1, h1)
	ts1 := httptest.NewServer(mux1)
	defer ts1.Close()
	c1 := identityv1connect.NewIdentityServiceClient(http.DefaultClient, ts1.URL)
	_, err1 := c1.Login(context.Background(),
		connect.NewRequest(&identityv1.LoginRequest{Username: "nurse1", Password: "pw"}))

	// Block by IP (allow identifier).
	ipBlockStore := &fakeUserStore{
		usersByUsername: map[string]*User{
			"nurse1": {ID: "u1", Username: "nurse1", PasswordHash: makeHash(t, "pw"), Role: RoleNurse, Active: true, CreatedAt: time.Now()},
		},
	}
	ipBlockSvc := &AuthService{
		store:  ipBlockStore,
		passwd: &passwordHelper{Hash: HashPassword, Verify: VerifyPassword},
		jwt:    &fakeTokenManager{fixedToken: "t", fixedExpiresAt: time.Now()},
	}
	ipBlockServer := NewIdentityServerWithRateLimit(ipBlockSvc, nil, &fakeLimiter{allow: true}, &fakeLimiter{allow: false})

	mux2 := http.NewServeMux()
	p2, h2 := identityv1connect.NewIdentityServiceHandler(ipBlockServer)
	mux2.Handle(p2, h2)
	ts2 := httptest.NewServer(mux2)
	defer ts2.Close()
	c2 := identityv1connect.NewIdentityServiceClient(http.DefaultClient, ts2.URL)
	_, err2 := c2.Login(context.Background(),
		connect.NewRequest(&identityv1.LoginRequest{Username: "nurse1", Password: "pw"}))

	ce1 := new(connect.Error)
	ce2 := new(connect.Error)
	if !errors.As(err1, &ce1) || !errors.As(err2, &ce2) {
		t.Fatal("both errors should be *connect.Error")
	}
	if ce1.Code() != ce2.Code() {
		t.Errorf("codes differ: id-limit=%v ip-limit=%v", ce1.Code(), ce2.Code())
	}
	if ce1.Message() != ce2.Message() {
		t.Errorf("messages differ: id-limit=%q ip-limit=%q", ce1.Message(), ce2.Message())
	}
}
