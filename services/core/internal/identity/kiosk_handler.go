package identity

import (
	"context"
	"errors"
	"net/http"
	"time"

	"connectrpc.com/connect"
	kioskv1 "github.com/adm-chura3inter/medisync/services/core/internal/gen/medisync/kiosk/v1"
	kioskv1connect "github.com/adm-chura3inter/medisync/services/core/internal/gen/medisync/kiosk/v1/kioskv1connect"
	"github.com/adm-chura3inter/medisync/services/core/internal/platform/audit"
	"github.com/adm-chura3inter/medisync/services/core/internal/platform/pagination"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Compile-time check: KioskServer implements the generated handler interface.
var _ kioskv1connect.KioskServiceHandler = (*KioskServer)(nil)

// KioskTokenManager is the narrow JWT interface for kiosk tokens.
type KioskTokenManager interface {
	IssueKiosk(k *Kiosk) (string, time.Time, error)
	ParseKiosk(tokenString string) (*KioskTokenClaims, error)
}

// UserTokenParser is the narrow interface for validating admin bearer
// tokens. The kiosk handler only needs to parse user JWTs to verify the
// caller is an admin — it doesn't need the full AuthService.
type UserTokenParser interface {
	Parse(tokenString string) (*TokenClaims, error)
}

// KioskServer is the Connect-RPC handler for KioskService.
type KioskServer struct {
	store      KioskStore
	passwd     *passwordHelper
	jwt        KioskTokenManager
	userParser UserTokenParser
	idLimiter  LoginRateLimiter
	ipLimiter  LoginRateLimiter
	audit      *audit.Writer
}

// NewKioskServer creates a KioskServer without rate limiting.
func NewKioskServer(store KioskStore, jwt KioskTokenManager, userParser UserTokenParser) *KioskServer {
	return &KioskServer{
		store:      store,
		passwd:     &passwordHelper{Hash: HashPassword, Verify: VerifyPassword},
		jwt:        jwt,
		userParser: userParser,
	}
}

// NewKioskServerWithRateLimit creates a KioskServer with rate limiting
// on the kiosk login endpoint.
func NewKioskServerWithRateLimit(store KioskStore, jwt KioskTokenManager, userParser UserTokenParser, idLimiter, ipLimiter LoginRateLimiter, aw *audit.Writer) *KioskServer {
	return &KioskServer{
		store:      store,
		passwd:     &passwordHelper{Hash: HashPassword, Verify: VerifyPassword},
		jwt:        jwt,
		userParser: userParser,
		idLimiter:  idLimiter,
		ipLimiter:  ipLimiter,
		audit:      aw,
	}
}

// ── Admin RPCs ──────────────────────────────────────────────────────

// ListKiosks returns all kiosks. Requires admin role.
func (s *KioskServer) ListKiosks(
	ctx context.Context,
	req *connect.Request[kioskv1.ListKiosksRequest],
) (*connect.Response[kioskv1.ListKiosksResponse], error) {
	claims, err := s.authenticate(req.Header())
	if err != nil {
		return nil, err
	}
	if claims.Role != string(RoleAdmin) {
		return nil, connect.NewError(connect.CodePermissionDenied, ErrNotAdmin)
	}

	pageSize, pageToken := pagination.DefaultPageSize, ""
	if req.Msg != nil && req.Msg.Pagination != nil {
		pageSize = pagination.NormalizePageSize(req.Msg.Pagination.PageSize)
		pageToken = req.Msg.Pagination.PageToken
	}

	kiosks, nextToken, totalCount, listErr := s.store.List(
		ctx, claims.ProjectID, pageSize, pageToken,
	)
	if listErr != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.New("internal error"))
	}

	pbKiosks := make([]*kioskv1.Kiosk, len(kiosks))
	for i, k := range kiosks {
		pbKiosks[i] = toProtoKiosk(k, false) // never include PIN in list
	}

	return connect.NewResponse(&kioskv1.ListKiosksResponse{
		Kiosks:        pbKiosks,
		NextPageToken: nextToken,
		TotalCount:    totalCount,
	}), nil
}

// CreateKiosk creates a new kiosk. Requires admin role. Returns the
// plaintext PIN in the response — this is the only time the PIN is
// readable. The admin must record or relay it.
func (s *KioskServer) CreateKiosk(
	ctx context.Context,
	req *connect.Request[kioskv1.CreateKioskRequest],
) (*connect.Response[kioskv1.CreateKioskResponse], error) {
	claims, err := s.authenticate(req.Header())
	if err != nil {
		return nil, err
	}
	if claims.Role != string(RoleAdmin) {
		return nil, connect.NewError(connect.CodePermissionDenied, ErrNotAdmin)
	}

	msg := req.Msg
	if msg == nil || msg.Code == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, ErrKioskCodeRequired)
	}
	if msg.Pin == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, ErrKioskPinRequired)
	}

	pinHash, hashErr := s.passwd.Hash(msg.Pin)
	if hashErr != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.New("internal error"))
	}

	projectID := claims.ProjectID
	if projectID == "" {
		projectID = msg.ProjectId
	}
	if projectID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("project_id is required"))
	}

	k := &Kiosk{
		Code:        msg.Code,
		DisplayName: msg.DisplayName,
		Name:        msg.Name,
		PinHash:     pinHash,
		ProjectID:   projectID,
	}
	if err := s.store.Create(ctx, k); err != nil {
		if errors.Is(err, ErrDuplicateKioskCode) {
			return nil, connect.NewError(connect.CodeAlreadyExists, ErrDuplicateKioskCode)
		}
		return nil, connect.NewError(connect.CodeInternal, errors.New("internal error"))
	}

	pb := toProtoKiosk(k, true)
	if pb != nil {
		pb.Pin = &msg.Pin
	}
	s.writeAudit(ctx, audit.Entry{
		Actor:     claims.Subject,
		Action:    "create_kiosk",
		Entity:    "kiosk",
		EntityID:  k.ID,
		ProjectID: claims.ProjectID,
		Detail:    map[string]string{"actor_type": claims.Role},
	})
	return connect.NewResponse(&kioskv1.CreateKioskResponse{
		Kiosk: pb,
	}), nil
}

// UpdateKiosk modifies display_name and/or active flag. Requires admin role.
func (s *KioskServer) UpdateKiosk(
	ctx context.Context,
	req *connect.Request[kioskv1.UpdateKioskRequest],
) (*connect.Response[kioskv1.UpdateKioskResponse], error) {
	claims, authErr := s.authenticate(req.Header())
	if authErr != nil {
		return nil, authErr
	}
	if claims.Role != string(RoleAdmin) {
		return nil, connect.NewError(connect.CodePermissionDenied, ErrNotAdmin)
	}

	msg := req.Msg
	if msg == nil || msg.Id == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("kiosk id is required"))
	}

	// Fetch existing kiosk to merge updates.
	existing, err := s.store.GetByID(ctx, msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.New("internal error"))
	}
	if existing == nil {
		return nil, connect.NewError(connect.CodeNotFound, ErrKioskNotFound)
	}

	if msg.DisplayName != nil {
		existing.DisplayName = *msg.DisplayName
	}
	if msg.Name != nil {
		existing.Name = *msg.Name
	}
	if msg.Active != nil {
		existing.Active = *msg.Active
	}

	if err := s.store.Update(ctx, existing); err != nil {
		if errors.Is(err, ErrKioskNotFound) {
			return nil, connect.NewError(connect.CodeNotFound, ErrKioskNotFound)
		}
		return nil, connect.NewError(connect.CodeInternal, errors.New("internal error"))
	}

	// Re-fetch to get fresh timestamps.
	updated, err := s.store.GetByID(ctx, msg.Id)
	if err != nil || updated == nil {
		return nil, connect.NewError(connect.CodeInternal, errors.New("internal error"))
	}
	s.writeAudit(ctx, audit.Entry{
		Actor:     claims.Subject,
		Action:    "update_kiosk",
		Entity:    "kiosk",
		EntityID:  updated.ID,
		ProjectID: claims.ProjectID,
		Detail:    map[string]string{"actor_type": claims.Role},
	})

	return connect.NewResponse(&kioskv1.UpdateKioskResponse{
		Kiosk: toProtoKiosk(updated, false),
	}), nil
}

// ResetKioskPin replaces the PIN for a kiosk. Requires admin role.
// Returns the new plaintext PIN in the response — the admin must relay it.
func (s *KioskServer) ResetKioskPin(
	ctx context.Context,
	req *connect.Request[kioskv1.ResetKioskPinRequest],
) (*connect.Response[kioskv1.ResetKioskPinResponse], error) {
	claims, authErr := s.authenticate(req.Header())
	if authErr != nil {
		return nil, authErr
	}
	if claims.Role != string(RoleAdmin) {
		return nil, connect.NewError(connect.CodePermissionDenied, ErrNotAdmin)
	}

	msg := req.Msg
	if msg == nil || msg.Id == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("kiosk id is required"))
	}
	if msg.NewPin == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, ErrKioskPinRequired)
	}

	// Verify kiosk exists.
	existing, err := s.store.GetByID(ctx, msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.New("internal error"))
	}
	if existing == nil {
		return nil, connect.NewError(connect.CodeNotFound, ErrKioskNotFound)
	}

	pinHash, err := s.passwd.Hash(msg.NewPin)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.New("internal error"))
	}

	if err := s.store.UpdatePIN(ctx, msg.Id, pinHash); err != nil {
		if errors.Is(err, ErrKioskNotFound) {
			return nil, connect.NewError(connect.CodeNotFound, ErrKioskNotFound)
		}
		return nil, connect.NewError(connect.CodeInternal, errors.New("internal error"))
	}

	pb := toProtoKiosk(existing, true)
	if pb != nil {
		pb.Pin = &msg.NewPin
	}
	s.writeAudit(ctx, audit.Entry{
		Actor:     claims.Subject,
		Action:    "reset_kiosk_pin",
		Entity:    "kiosk",
		EntityID:  existing.ID,
		ProjectID: claims.ProjectID,
		Detail:    map[string]string{"actor_type": claims.Role},
	})
	return connect.NewResponse(&kioskv1.ResetKioskPinResponse{
		Kiosk: pb,
	}), nil
}

// ── Kiosk RPCs ─────────────────────────────────────────────────────

// KioskLogin authenticates a kiosk by code and PIN. Returns a JWT with
// kiosk identity claims on success.
func (s *KioskServer) KioskLogin(
	ctx context.Context,
	req *connect.Request[kioskv1.KioskLoginRequest],
) (*connect.Response[kioskv1.KioskLoginResponse], error) {
	msg := req.Msg
	if msg == nil || msg.Code == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, ErrKioskCodeRequired)
	}
	if msg.Pin == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, ErrKioskPinRequired)
	}

	// Rate-limit check: per-identifier (code) and per-IP.
	if err := s.checkKioskLoginRateLimit(req.Peer().Addr, msg.Code); err != nil {
		return nil, err
	}

	k, err := s.store.GetByCode(ctx, msg.Code)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.New("internal error"))
	}

	// Use dummy hash for non-existent codes to prevent timing enumeration.
	pinHash := dummyPasswordHash
	if k != nil {
		pinHash = k.PinHash
	}
	if err := s.passwd.Verify(pinHash, msg.Pin); k == nil || err != nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, ErrInvalidKioskCode)
	}

	if !k.Active {
		return nil, connect.NewError(connect.CodePermissionDenied, ErrInactiveKiosk)
	}

	token, expiresAt, err := s.jwt.IssueKiosk(k)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.New("internal error"))
	}

	return connect.NewResponse(&kioskv1.KioskLoginResponse{
		AccessToken: token,
		ExpiresAt:   timestamppb.New(expiresAt),
		Kiosk:       toProtoKiosk(k, false),
	}), nil
}

// KioskValidate verifies a kiosk bearer token and returns the kiosk
// identity. The kiosk calls this on boot to restore its session.
func (s *KioskServer) KioskValidate(
	ctx context.Context,
	req *connect.Request[kioskv1.KioskValidateRequest],
) (*connect.Response[kioskv1.KioskValidateResponse], error) {
	tokenStr, err := extractBearer(req.Header())
	if err != nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, err)
	}

	claims, err := s.jwt.ParseKiosk(tokenStr)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, ErrInvalidKioskCode)
	}
	if claims.Subject == "" {
		return nil, connect.NewError(connect.CodeUnauthenticated, ErrInvalidKioskCode)
	}

	k, err := s.store.GetByID(ctx, claims.Subject)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.New("internal error"))
	}
	if k == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, ErrInvalidKioskCode)
	}
	if !k.Active {
		return nil, connect.NewError(connect.CodePermissionDenied, ErrInactiveKiosk)
	}

	return connect.NewResponse(&kioskv1.KioskValidateResponse{
		Kiosk: toProtoKiosk(k, false),
	}), nil
}

// ── Internal helpers ────────────────────────────────────────────────

// authenticate extracts and validates a user Bearer token, returning
// the claims. Returns a Connect error on failure.
func (s *KioskServer) authenticate(header http.Header) (*TokenClaims, *connect.Error) {
	tokenStr, err := extractBearer(header)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, err)
	}
	claims, err := s.userParser.Parse(tokenStr)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, ErrInvalidCredentials)
	}
	return claims, nil
}

// requireAdmin extracts the Bearer token from the request headers,
// validates it via the user token parser, and checks that the caller has
// the ADMIN role. Returns a Connect error if any check fails.
func (s *KioskServer) requireAdmin(header http.Header) *connect.Error {
	tokenStr, err := extractBearer(header)
	if err != nil {
		return connect.NewError(connect.CodeUnauthenticated, err)
	}

	claims, err := s.userParser.Parse(tokenStr)
	if err != nil {
		return connect.NewError(connect.CodeUnauthenticated, ErrInvalidCredentials)
	}

	if claims.Role != string(RoleAdmin) {
		return connect.NewError(connect.CodePermissionDenied, ErrNotAdmin)
	}

	return nil
}

func (s *KioskServer) writeAudit(ctx context.Context, entry audit.Entry) {
	if s.audit == nil {
		return
	}
	if entry.TraceID == "" {
		entry.TraceID = uuid.NewString()
	}
	_ = s.audit.Write(ctx, entry)
}

// checkKioskLoginRateLimit checks both the per-identifier (kiosk code)
// and per-IP rate limiters. Returns a uniform connect.Error when either
// limit is exceeded.
func (s *KioskServer) checkKioskLoginRateLimit(peerAddr, code string) *connect.Error {
	if s.idLimiter != nil && !s.idLimiter.Allow(code) {
		return connect.NewError(connect.CodeResourceExhausted, ErrRateLimitExceeded)
	}

	ip := extractIP(peerAddr)
	if ip != "" && s.ipLimiter != nil && !s.ipLimiter.Allow(ip) {
		return connect.NewError(connect.CodeResourceExhausted, ErrRateLimitExceeded)
	}

	return nil
}

// toProtoKiosk converts a domain Kiosk to a proto Kiosk. When
// includePin is true, a nil optional pin field is returned (the caller
// sets the plaintext PIN on the response). The pin_hash is never
// included in the proto.
func toProtoKiosk(k *Kiosk, includePin bool) *kioskv1.Kiosk {
	if k == nil {
		return nil
	}

	createdAt := timestamppb.New(k.CreatedAt)
	if k.CreatedAt.IsZero() {
		createdAt = nil
	}

	out := &kioskv1.Kiosk{
		Id:          k.ID,
		Code:        k.Code,
		DisplayName: k.DisplayName,
		Name:        k.Name,
		Active:      k.Active,
		CreatedAt:   createdAt,
	}
	return out
}

// Ensure unused imports compile away.
var _ = timestamppb.Now
