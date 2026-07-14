package identity

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	"connectrpc.com/connect"
	identityv1 "github.com/adm-chura3inter/medisync/services/core/internal/gen/medisync/identity/v1"
	identityv1connect "github.com/adm-chura3inter/medisync/services/core/internal/gen/medisync/identity/v1/identityv1connect"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// LoginRateLimiter is the interface IdentityServer uses to check login rate
// limits. It is a subset of the real ratelimit.Limiter so tests can inject
// deterministic behaviour without importing the full ratelimit package.
type LoginRateLimiter interface {
	Allow(key string) bool
	Reset()
}

// Compile-time check: IdentityServer implements the generated handler interface.
var _ identityv1connect.IdentityServiceHandler = (*IdentityServer)(nil)

// IdentityServer is the Connect-RPC handler for IdentityService.
// It owns no HTTP server startup logic — just the RPC mapping.
type IdentityServer struct {
	auth         *AuthService
	idLimiter    LoginRateLimiter // per-identifier (username / card_token)
	ipLimiter    LoginRateLimiter // per-remote-IP
}

// NewIdentityServer creates an IdentityServer without rate limiting.
func NewIdentityServer(auth *AuthService) *IdentityServer {
	return &IdentityServer{auth: auth}
}

// NewIdentityServerWithRateLimit creates an IdentityServer with rate
// limiting on login endpoints. Both idLimiter and ipLimiter may be nil
// (or a noop limiter) to disable the corresponding check.
func NewIdentityServerWithRateLimit(auth *AuthService, idLimiter, ipLimiter LoginRateLimiter) *IdentityServer {
	return &IdentityServer{auth: auth, idLimiter: idLimiter, ipLimiter: ipLimiter}
}

// Login handles password-based login. It maps domain errors to safe Connect
// error codes that do not leak whether a username exists.
func (s *IdentityServer) Login(
	ctx context.Context,
	req *connect.Request[identityv1.LoginRequest],
) (*connect.Response[identityv1.LoginResponse], error) {
	msg := req.Msg
	if msg == nil || msg.Username == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, ErrMissingUsername)
	}
	if msg.Password == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, ErrMissingPassword)
	}

	// Rate-limit check: per-identifier and per-IP. Both must pass.
	// The same error is returned regardless of which limit triggered.
	if err := s.checkLoginRateLimit(req.Peer().Addr, msg.Username); err != nil {
		return nil, err
	}

	token, expiresAt, user, err := s.auth.LoginPassword(ctx, msg.Username, msg.Password)
	if err != nil {
		return nil, authErrorToConnect(err)
	}

	return connect.NewResponse(&identityv1.LoginResponse{
		AccessToken: token,
		ExpiresAt:   timestamppb.New(expiresAt),
		User:        toProtoUser(user),
	}), nil
}

// CardLogin handles card-token-based login. It maps domain errors to
// safe Connect error codes.
func (s *IdentityServer) CardLogin(
	ctx context.Context,
	req *connect.Request[identityv1.CardLoginRequest],
) (*connect.Response[identityv1.CardLoginResponse], error) {
	msg := req.Msg
	if msg == nil || msg.CardToken == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, ErrMissingCardToken)
	}

	// Rate-limit check: per-identifier (card token) and per-IP.
	if err := s.checkLoginRateLimit(req.Peer().Addr, msg.CardToken); err != nil {
		return nil, err
	}

	token, expiresAt, user, err := s.auth.LoginCard(ctx, msg.CardToken)
	if err != nil {
		return nil, authErrorToConnect(err)
	}

	return connect.NewResponse(&identityv1.CardLoginResponse{
		AccessToken: token,
		ExpiresAt:   timestamppb.New(expiresAt),
		User:        toProtoUser(user),
	}), nil
}

// checkLoginRateLimit checks both the per-identifier and per-IP rate
// limiters. It returns a uniform connect.Error (CodeResourceExhausted)
// with a safe message when either limit is exceeded.
// peerAddr is the raw address string from Connect's req.Peer().Addr.
func (s *IdentityServer) checkLoginRateLimit(peerAddr, identifier string) *connect.Error {
	// Per-identifier limit.
	if s.idLimiter != nil && !s.idLimiter.Allow(identifier) {
		return connect.NewError(connect.CodeResourceExhausted, ErrRateLimitExceeded)
	}

	// Per-IP limit.
	ip := extractIP(peerAddr)
	if ip != "" && s.ipLimiter != nil && !s.ipLimiter.Allow(ip) {
		return connect.NewError(connect.CodeResourceExhausted, ErrRateLimitExceeded)
	}

	return nil
}

// extractIP strips the port from an addr string if present.
func extractIP(addr string) string {
	if addr == "" {
		return ""
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		// No port — use the raw address.
		return addr
	}
	return host
}

// WhoAmI returns the current user from the JWT in the Authorization header.
func (s *IdentityServer) WhoAmI(
	ctx context.Context,
	req *connect.Request[identityv1.WhoAmIRequest],
) (*connect.Response[identityv1.WhoAmIResponse], error) {
	tokenStr, err := extractBearer(req.Header())
	if err != nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, err)
	}

	user, err := s.auth.WhoAmI(ctx, tokenStr)
	if err != nil {
		return nil, authErrorToConnect(err)
	}

	return connect.NewResponse(&identityv1.WhoAmIResponse{
		User: toProtoUser(user),
	}), nil
}

// extractBearer parses "Authorization: Bearer <token>". It returns an
// error when the header is missing, malformed, or uses a non-Bearer scheme.
func extractBearer(header http.Header) (string, error) {
	auth := header.Get("Authorization")
	if auth == "" {
		return "", errors.New("authorization header is required")
	}
	parts := strings.SplitN(auth, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
		return "", errors.New("authorization header must use Bearer scheme")
	}
	token := strings.TrimSpace(parts[1])
	if token == "" {
		return "", errors.New("bearer token is required")
	}
	return token, nil
}

// authErrorToConnect maps domain authentication errors to safe Connect
// error codes. It never returns a message that distinguishes between an
// unknown user and a wrong password.
func authErrorToConnect(err error) *connect.Error {
	switch {
	case errors.Is(err, ErrInvalidCredentials):
		return connect.NewError(connect.CodeUnauthenticated, ErrInvalidCredentials)
	case errors.Is(err, ErrInactiveUser):
		return connect.NewError(connect.CodePermissionDenied, ErrInactiveUser)
	case errors.Is(err, ErrMissingUsername),
		errors.Is(err, ErrMissingPassword),
		errors.Is(err, ErrMissingCardToken),
		errors.Is(err, ErrMissingToken):
		return connect.NewError(connect.CodeInvalidArgument, err)
	default:
		// All other errors are internal — DB failures, JWT signing, etc.
		// We return a generic message so internals stay hidden.
		return connect.NewError(connect.CodeInternal, errors.New("internal error"))
	}
}

// toProtoUser converts a domain User to a proto User. It strips the
// password hash and card token — these must never leave the server.
func toProtoUser(u *User) *identityv1.User {
	if u == nil {
		return nil
	}
	var pbRole identityv1.Role
	switch u.Role {
	case RoleAdmin:
		pbRole = identityv1.Role_ROLE_ADMIN
	case RolePharmacist:
		pbRole = identityv1.Role_ROLE_PHARMACIST
	case RoleNurse:
		pbRole = identityv1.Role_ROLE_NURSE
	case RoleRefiller:
		pbRole = identityv1.Role_ROLE_REFILLER
	}

	createdAt := timestamppb.New(u.CreatedAt)
	// If CreatedAt is zero (e.g. in unit tests), set a sentinel.
	if u.CreatedAt.IsZero() {
		createdAt = nil
	}

	return &identityv1.User{
		Id:          u.ID,
		Username:    u.Username,
		DisplayName: u.DisplayName,
		Role:        pbRole,
		WardIds:     u.WardIDs,
		Active:      u.Active,
		CreatedAt:   createdAt,
	}
}

// toProtoTimestamp wraps a time in the proto timestamp helper used by tests
// that bypass the full handler. Exported for test use only.
func toProtoTimestamp(t time.Time) *timestamppb.Timestamp {
	if t.IsZero() {
		return nil
	}
	return timestamppb.New(t)
}
