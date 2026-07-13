package main

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
	"github.com/adm-chura3inter/medisync/services/core/internal/identity"
	"github.com/golang-jwt/jwt/v5"
)

// setupAuthenticatedServer creates a test HTTP server that has the Identity
// Connect handler mounted. It returns the server and a connected client.
// The fake store and token manager allow full control over authentication.
func setupAuthenticatedServer(t *testing.T, store *fakeUserStore, tm *fakeTokenManager) (*httptest.Server, identityv1connect.IdentityServiceClient) {
	t.Helper()

	mux := http.NewServeMux()
	path, handler := newIdentityHandler(store, tm)
	mux.Handle(path, handler)

	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	client := identityv1connect.NewIdentityServiceClient(
		http.DefaultClient,
		ts.URL,
	)
	return ts, client
}

// ── Fake types (replicated from handler/auth tests for package isolation) ──

type fakeUserStore struct {
	usersByUsername  map[string]*identity.User
	usersByCardToken map[string]*identity.User
	usersByID        map[string]*identity.User
	lookupErr        error
}

func (s *fakeUserStore) GetByUsername(_ context.Context, username string) (*identity.User, error) {
	if s.lookupErr != nil {
		return nil, s.lookupErr
	}
	return s.usersByUsername[username], nil
}

func (s *fakeUserStore) GetByCardToken(_ context.Context, token string) (*identity.User, error) {
	if s.lookupErr != nil {
		return nil, s.lookupErr
	}
	return s.usersByCardToken[token], nil
}

func (s *fakeUserStore) GetByID(_ context.Context, id string) (*identity.User, error) {
	if s.lookupErr != nil {
		return nil, s.lookupErr
	}
	return s.usersByID[id], nil
}

type fakeTokenManager struct {
	issuedTokens   []*identity.User
	parseResult    *identity.TokenClaims
	parseErr       error
	issueErr       error
	fixedToken     string
	fixedExpiresAt time.Time
}

func (m *fakeTokenManager) Issue(user *identity.User) (string, time.Time, error) {
	if m.issueErr != nil {
		return "", time.Time{}, m.issueErr
	}
	m.issuedTokens = append(m.issuedTokens, user)
	return m.fixedToken, m.fixedExpiresAt, nil
}

func (m *fakeTokenManager) Parse(_ string) (*identity.TokenClaims, error) {
	return m.parseResult, m.parseErr
}

func makeTestHash(t *testing.T, pw string) string {
	t.Helper()
	h, err := identity.HashPassword(pw)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	return h
}

// ── HTTP-level Connect tests ────────────────────────────────────────────

func TestConnectLoginSuccess(t *testing.T) {
	pw := "secret123"
	pwHash := makeTestHash(t, pw)
	store := &fakeUserStore{
		usersByUsername: map[string]*identity.User{
			"nurse1": {
				ID:           "u1",
				Username:     "nurse1",
				PasswordHash: pwHash,
				Role:         identity.RoleNurse,
				Active:       true,
				WardIDs:      []string{"WARD-3A"},
				CreatedAt:    time.Date(2026, 7, 13, 0, 0, 0, 0, time.UTC),
			},
		},
	}
	tm := &fakeTokenManager{
		fixedToken:     "http-test-jwt",
		fixedExpiresAt: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
	}
	_, client := setupAuthenticatedServer(t, store, tm)

	resp, err := client.Login(context.Background(),
		connect.NewRequest(&identityv1.LoginRequest{
			Username: "nurse1",
			Password: pw,
		}))
	if err != nil {
		t.Fatalf("Login via Connect HTTP: %v", err)
	}
	if resp.Msg.AccessToken != "http-test-jwt" {
		t.Errorf("AccessToken = %q, want http-test-jwt", resp.Msg.AccessToken)
	}
	if resp.Msg.User == nil {
		t.Fatal("expected user in response")
	}
	if resp.Msg.User.Username != "nurse1" {
		t.Errorf("Username = %q, want nurse1", resp.Msg.User.Username)
	}
	if resp.Msg.ExpiresAt == nil {
		t.Error("expected ExpiresAt in response")
	}
}

func TestConnectLoginInvalidCredentials(t *testing.T) {
	store := &fakeUserStore{usersByUsername: map[string]*identity.User{}}
	_, client := setupAuthenticatedServer(t, store, &fakeTokenManager{})

	_, err := client.Login(context.Background(),
		connect.NewRequest(&identityv1.LoginRequest{
			Username: "ghost",
			Password: "boo",
		}))
	if err == nil {
		t.Fatal("expected error for invalid credentials")
	}
	if connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Errorf("code = %v, want CodeUnauthenticated", connect.CodeOf(err))
	}
}

func TestConnectLoginMissingUsername(t *testing.T) {
	_, client := setupAuthenticatedServer(t, &fakeUserStore{}, &fakeTokenManager{})

	_, err := client.Login(context.Background(),
		connect.NewRequest(&identityv1.LoginRequest{
			Username: "",
			Password: "pw",
		}))
	if err == nil {
		t.Fatal("expected error for missing username")
	}
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("code = %v, want CodeInvalidArgument", connect.CodeOf(err))
	}
}

func TestConnectLoginMissingPassword(t *testing.T) {
	_, client := setupAuthenticatedServer(t, &fakeUserStore{}, &fakeTokenManager{})

	_, err := client.Login(context.Background(),
		connect.NewRequest(&identityv1.LoginRequest{
			Username: "u",
			Password: "",
		}))
	if err == nil {
		t.Fatal("expected error for missing password")
	}
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("code = %v, want CodeInvalidArgument", connect.CodeOf(err))
	}
}

func TestConnectWhoAmISuccess(t *testing.T) {
	store := &fakeUserStore{
		usersByID: map[string]*identity.User{
			"who-http-1": {
				ID:        "who-http-1",
				Username:  "whoami-http-user",
				Role:      identity.RoleNurse,
				Active:    true,
				CreatedAt: time.Date(2026, 7, 13, 0, 0, 0, 0, time.UTC),
			},
		},
	}
	tm := &fakeTokenManager{
		parseResult: &identity.TokenClaims{
			RegisteredClaims: jwt.RegisteredClaims{Subject: "who-http-1"},
		},
	}
	_, client := setupAuthenticatedServer(t, store, tm)

	req := connect.NewRequest(&identityv1.WhoAmIRequest{})
	req.Header().Set("Authorization", "Bearer valid-http-who-token")
	resp, err := client.WhoAmI(context.Background(), req)
	if err != nil {
		t.Fatalf("WhoAmI via Connect HTTP: %v", err)
	}
	if resp.Msg.User == nil {
		t.Fatal("expected user in response")
	}
	if resp.Msg.User.Username != "whoami-http-user" {
		t.Errorf("Username = %q, want whoami-http-user", resp.Msg.User.Username)
	}
}

func TestConnectWhoAmIMissingAuth(t *testing.T) {
	_, client := setupAuthenticatedServer(t, &fakeUserStore{}, &fakeTokenManager{})

	_, err := client.WhoAmI(context.Background(),
		connect.NewRequest(&identityv1.WhoAmIRequest{}))
	if err == nil {
		t.Fatal("expected error for missing auth header")
	}
	if connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Errorf("code = %v, want CodeUnauthenticated", connect.CodeOf(err))
	}
}

func TestConnectWhoAmIInvalidToken(t *testing.T) {
	tm := &fakeTokenManager{parseErr: identity.ErrInvalidCredentials}
	_, client := setupAuthenticatedServer(t, &fakeUserStore{}, tm)

	req := connect.NewRequest(&identityv1.WhoAmIRequest{})
	req.Header().Set("Authorization", "Bearer bad-token")
	_, err := client.WhoAmI(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for invalid token")
	}
	if connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Errorf("code = %v, want CodeUnauthenticated", connect.CodeOf(err))
	}
}

// ── Mux path registration test ─────────────────────────────────────────

func TestMuxPathRegistration(t *testing.T) {
	path, handler := newIdentityHandler(&fakeUserStore{}, &fakeTokenManager{})

	// Verify the handler is not nil.
	if handler == nil {
		t.Fatal("expected non-nil handler from NewIdentityServiceHandler")
	}
	// Verify the path contains identity.
	if !strings.Contains(path, "identity") {
		t.Errorf("path %q should contain 'identity'", path)
	}
	// Verify the path ends with / for subtree matching.
	if !strings.HasSuffix(path, "/") {
		t.Errorf("path %q should end with / for Connect subtree routing", path)
	}
}

// ── Password hashing verification ──────────────────────────────────────

type fakeAdminSeeder struct {
	passwordHash string
	created      bool
	err          error
}

func (s *fakeAdminSeeder) SeedAdmin(_ context.Context, passwordHash string) (bool, error) {
	s.passwordHash = passwordHash
	return s.created, s.err
}

func TestBootstrapAdminPassesBcryptHash(t *testing.T) {
	pw := "admin-bootstrap-password"
	store := &fakeAdminSeeder{created: true}
	created, err := bootstrapAdmin(context.Background(), store, pw)
	if err != nil {
		t.Fatalf("bootstrapAdmin: %v", err)
	}
	if !created {
		t.Fatal("created = false, want true")
	}
	if store.passwordHash == pw {
		t.Error("hash must not equal plaintext password")
	}
	if err := identity.VerifyPassword(store.passwordHash, pw); err != nil {
		t.Errorf("VerifyPassword should succeed: %v", err)
	}
}

func TestBootstrapAdminPropagatesStoreError(t *testing.T) {
	want := errors.New("seed failed")
	store := &fakeAdminSeeder{err: want}
	_, err := bootstrapAdmin(context.Background(), store, "admin-bootstrap-password")
	if !errors.Is(err, want) {
		t.Fatalf("error = %v, want wrapped %v", err, want)
	}
}
