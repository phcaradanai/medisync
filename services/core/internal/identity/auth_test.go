package identity

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// --- fakeUserStore ---

type fakeUserStore struct {
	usersByUsername  map[string]*User
	usersByCardToken map[string]*User
	usersByID        map[string]*User
	lookupErr        error
}

func (s *fakeUserStore) GetByUsername(_ context.Context, username string) (*User, error) {
	if s.lookupErr != nil {
		return nil, s.lookupErr
	}
	return s.usersByUsername[username], nil
}

func (s *fakeUserStore) GetByCardToken(_ context.Context, token string) (*User, error) {
	if s.lookupErr != nil {
		return nil, s.lookupErr
	}
	return s.usersByCardToken[token], nil
}

func (s *fakeUserStore) GetByID(_ context.Context, id string) (*User, error) {
	if s.lookupErr != nil {
		return nil, s.lookupErr
	}
	return s.usersByID[id], nil
}

// --- fakeTokenManager ---

type fakeTokenManager struct {
	issuedTokens   []*User
	parseResult    *TokenClaims
	parseErr       error
	issueErr       error
	fixedToken     string
	fixedExpiresAt time.Time
}

func (m *fakeTokenManager) Issue(user *User) (string, time.Time, error) {
	if m.issueErr != nil {
		return "", time.Time{}, m.issueErr
	}
	m.issuedTokens = append(m.issuedTokens, user)
	return m.fixedToken, m.fixedExpiresAt, nil
}

func (m *fakeTokenManager) Parse(_ string) (*TokenClaims, error) {
	return m.parseResult, m.parseErr
}

// --- helpers ---

func makeHash(t *testing.T, pw string) string {
	t.Helper()
	h, err := HashPassword(pw)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	return h
}

// --- LoginPassword tests ---

func TestLoginPasswordSuccess(t *testing.T) {
	store := &fakeUserStore{
		usersByUsername: map[string]*User{
			"nurse1": {
				ID:           "user-1",
				Username:     "nurse1",
				PasswordHash: makeHash(t, "secret123"),
				Role:         RoleNurse,
				WardIDs:      []string{"WARD-3A"},
				Active:       true,
			},
		},
	}
	tm := &fakeTokenManager{
		fixedToken:     "jwt-token-abc",
		fixedExpiresAt: time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC),
	}
	svc := &AuthService{
		store:  store,
		passwd: &passwordHelper{Hash: HashPassword, Verify: VerifyPassword},
		jwt:    tm,
	}

	token, expiresAt, user, err := svc.LoginPassword(context.Background(), "nurse1", "secret123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token != "jwt-token-abc" {
		t.Errorf("token = %q, want jwt-token-abc", token)
	}
	if !expiresAt.Equal(tm.fixedExpiresAt) {
		t.Errorf("expiresAt = %v, want %v", expiresAt, tm.fixedExpiresAt)
	}
	if user.ID != "user-1" {
		t.Errorf("user ID = %q, want user-1", user.ID)
	}
}

func TestLoginPasswordUnknownUser(t *testing.T) {
	store := &fakeUserStore{
		usersByUsername: map[string]*User{},
	}
	tm := &fakeTokenManager{}
	svc := &AuthService{
		store:  store,
		passwd: &passwordHelper{Hash: HashPassword, Verify: VerifyPassword},
		jwt:    tm,
	}

	_, _, _, err := svc.LoginPassword(context.Background(), "ghost", "secret123")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("expected ErrInvalidCredentials, got %v", err)
	}
}

func TestLoginPasswordUnknownUserStillVerifiesDummyHash(t *testing.T) {
	verifyCalled := false
	svc := &AuthService{
		store: &fakeUserStore{usersByUsername: map[string]*User{}},
		passwd: &passwordHelper{Verify: func(hash, password string) error {
			verifyCalled = true
			if hash == "" {
				t.Error("dummy password hash must not be empty")
			}
			return errors.New("mismatch")
		}},
		jwt: &fakeTokenManager{},
	}

	_, _, _, err := svc.LoginPassword(context.Background(), "ghost", "secret123")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("expected ErrInvalidCredentials, got %v", err)
	}
	if !verifyCalled {
		t.Fatal("unknown-user login must perform a dummy password verification")
	}
}

func TestLoginPasswordWrongPassword(t *testing.T) {
	store := &fakeUserStore{
		usersByUsername: map[string]*User{
			"nurse1": {
				ID:           "user-1",
				Username:     "nurse1",
				PasswordHash: makeHash(t, "correct"),
				Role:         RoleNurse,
				Active:       true,
			},
		},
	}
	svc := &AuthService{
		store:  store,
		passwd: &passwordHelper{Hash: HashPassword, Verify: VerifyPassword},
		jwt:    &fakeTokenManager{},
	}

	_, _, _, err := svc.LoginPassword(context.Background(), "nurse1", "wrong")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("expected ErrInvalidCredentials for wrong password, got %v", err)
	}
}

func TestLoginPasswordInactiveUser(t *testing.T) {
	store := &fakeUserStore{
		usersByUsername: map[string]*User{
			"inactive": {
				ID:           "user-2",
				Username:     "inactive",
				PasswordHash: makeHash(t, "secret123"),
				Role:         RoleNurse,
				Active:       false,
			},
		},
	}
	svc := &AuthService{
		store:  store,
		passwd: &passwordHelper{Hash: HashPassword, Verify: VerifyPassword},
		jwt:    &fakeTokenManager{},
	}

	_, _, _, err := svc.LoginPassword(context.Background(), "inactive", "secret123")
	if !errors.Is(err, ErrInactiveUser) {
		t.Errorf("expected ErrInactiveUser, got %v", err)
	}
}

func TestLoginPasswordBlankUsername(t *testing.T) {
	svc := NewAuthService(&fakeUserStore{}, &fakeTokenManager{})
	_, _, _, err := svc.LoginPassword(context.Background(), "", "pw")
	if !errors.Is(err, ErrMissingUsername) {
		t.Errorf("expected ErrMissingUsername, got %v", err)
	}
}

func TestLoginPasswordBlankPassword(t *testing.T) {
	svc := NewAuthService(&fakeUserStore{}, &fakeTokenManager{})
	_, _, _, err := svc.LoginPassword(context.Background(), "u", "")
	if !errors.Is(err, ErrMissingPassword) {
		t.Errorf("expected ErrMissingPassword, got %v", err)
	}
}

// --- LoginCard tests ---

func TestLoginCardSuccess(t *testing.T) {
	store := &fakeUserStore{
		usersByCardToken: map[string]*User{
			"card-token-99": {
				ID:       "user-card-1",
				Username: "carduser",
				Role:     RoleNurse,
				Active:   true,
			},
		},
	}
	tm := &fakeTokenManager{
		fixedToken:     "jwt-card",
		fixedExpiresAt: time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC),
	}
	svc := &AuthService{
		store:  store,
		passwd: &passwordHelper{Hash: HashPassword, Verify: VerifyPassword},
		jwt:    tm,
	}

	token, expiresAt, user, err := svc.LoginCard(context.Background(), "card-token-99")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token != "jwt-card" {
		t.Errorf("token = %q", token)
	}
	if !expiresAt.Equal(tm.fixedExpiresAt) {
		t.Errorf("expiresAt = %v", expiresAt)
	}
	if user.Username != "carduser" {
		t.Errorf("Username = %q", user.Username)
	}
}

func TestLoginCardUnknownToken(t *testing.T) {
	store := &fakeUserStore{usersByCardToken: map[string]*User{}}
	svc := &AuthService{
		store:  store,
		passwd: &passwordHelper{Hash: HashPassword, Verify: VerifyPassword},
		jwt:    &fakeTokenManager{},
	}

	_, _, _, err := svc.LoginCard(context.Background(), "no-such-card")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("expected ErrInvalidCredentials, got %v", err)
	}
}

func TestLoginCardInactiveUser(t *testing.T) {
	store := &fakeUserStore{
		usersByCardToken: map[string]*User{
			"card-inactive": {ID: "ci", Username: "ci", Role: RoleNurse, Active: false},
		},
	}
	svc := &AuthService{
		store:  store,
		passwd: &passwordHelper{Hash: HashPassword, Verify: VerifyPassword},
		jwt:    &fakeTokenManager{},
	}

	_, _, _, err := svc.LoginCard(context.Background(), "card-inactive")
	if !errors.Is(err, ErrInactiveUser) {
		t.Errorf("expected ErrInactiveUser, got %v", err)
	}
}

func TestLoginCardBlankToken(t *testing.T) {
	svc := NewAuthService(&fakeUserStore{}, &fakeTokenManager{})
	_, _, _, err := svc.LoginCard(context.Background(), "")
	if !errors.Is(err, ErrMissingCardToken) {
		t.Errorf("expected ErrMissingCardToken, got %v", err)
	}
}

// --- WhoAmI tests ---

func TestWhoAmISuccess(t *testing.T) {
	store := &fakeUserStore{
		usersByID: map[string]*User{
			"user-who": {
				ID:      "user-who",
				Role:    RolePharmacist,
				Active:  true,
				WardIDs: []string{"WARD-ICU"},
			},
		},
	}
	tm := &fakeTokenManager{
		parseResult: &TokenClaims{
			RegisteredClaims: jwt.RegisteredClaims{Subject: "user-who"},
			Role:             "NURSE",
		},
	}
	svc := &AuthService{
		store:  store,
		passwd: &passwordHelper{Hash: HashPassword, Verify: VerifyPassword},
		jwt:    tm,
	}

	user, err := svc.WhoAmI(context.Background(), "valid-token")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if user.ID != "user-who" {
		t.Errorf("ID = %q, want user-who", user.ID)
	}
}

func newJWTClaims(subject string) *TokenClaims {
	return &TokenClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject: subject,
		},
	}
}

func TestWhoAmIMissingToken(t *testing.T) {
	svc := NewAuthService(&fakeUserStore{}, &fakeTokenManager{})
	_, err := svc.WhoAmI(context.Background(), "")
	if !errors.Is(err, ErrMissingToken) {
		t.Errorf("expected ErrMissingToken, got %v", err)
	}
}

func TestWhoAmIInvalidToken(t *testing.T) {
	tm := &fakeTokenManager{parseErr: errors.New("token expired")}
	svc := &AuthService{
		store:  &fakeUserStore{},
		passwd: &passwordHelper{Hash: HashPassword, Verify: VerifyPassword},
		jwt:    tm,
	}

	_, err := svc.WhoAmI(context.Background(), "expired-token")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("expected ErrInvalidCredentials, got %v", err)
	}
}

func TestWhoAmIUserNotFound(t *testing.T) {
	tm := &fakeTokenManager{
		parseResult: newJWTClaims("missing"),
	}
	svc := &AuthService{
		store:  &fakeUserStore{usersByID: map[string]*User{}},
		passwd: &passwordHelper{Hash: HashPassword, Verify: VerifyPassword},
		jwt:    tm,
	}

	_, err := svc.WhoAmI(context.Background(), "token-for-missing")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("expected ErrInvalidCredentials, got %v", err)
	}
}

func TestWhoAmIInactiveUser(t *testing.T) {
	store := &fakeUserStore{
		usersByID: map[string]*User{
			"who-inactive": {ID: "who-inactive", Role: RoleNurse, Active: false},
		},
	}
	tm := &fakeTokenManager{
		parseResult: newJWTClaims("who-inactive"),
	}
	svc := &AuthService{
		store:  store,
		passwd: &passwordHelper{Hash: HashPassword, Verify: VerifyPassword},
		jwt:    tm,
	}

	_, err := svc.WhoAmI(context.Background(), "token-inactive")
	if !errors.Is(err, ErrInactiveUser) {
		t.Errorf("expected ErrInactiveUser, got %v", err)
	}
}

func TestWhoAmIEmptySubject(t *testing.T) {
	tm := &fakeTokenManager{
		parseResult: &TokenClaims{
			RegisteredClaims: jwt.RegisteredClaims{Subject: ""},
		},
	}
	svc := &AuthService{
		store:  &fakeUserStore{},
		passwd: &passwordHelper{Hash: HashPassword, Verify: VerifyPassword},
		jwt:    tm,
	}

	_, err := svc.WhoAmI(context.Background(), "token-no-sub")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("expected ErrInvalidCredentials, got %v", err)
	}
}
