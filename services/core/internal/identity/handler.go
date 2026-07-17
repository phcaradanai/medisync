package identity

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"connectrpc.com/connect"
	identityv1 "github.com/adm-chura3inter/medisync/services/core/internal/gen/medisync/identity/v1"
	identityv1connect "github.com/adm-chura3inter/medisync/services/core/internal/gen/medisync/identity/v1/identityv1connect"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// LoginRateLimiter is the interface IdentityServer uses to check login rate limits.
type LoginRateLimiter interface {
	Allow(key string) bool
	Reset()
}

// Compile-time check: IdentityServer implements the generated handler interface.
var _ identityv1connect.IdentityServiceHandler = (*IdentityServer)(nil)

// IdentityServer is the Connect-RPC handler for IdentityService.
type IdentityServer struct {
	auth      *AuthService
	store     *Store
	idLimiter LoginRateLimiter
	ipLimiter LoginRateLimiter
}

func NewIdentityServer(auth *AuthService, store *Store) *IdentityServer {
	return &IdentityServer{auth: auth, store: store}
}

func NewIdentityServerWithRateLimit(auth *AuthService, store *Store, idLimiter, ipLimiter LoginRateLimiter) *IdentityServer {
	return &IdentityServer{auth: auth, store: store, idLimiter: idLimiter, ipLimiter: ipLimiter}
}

// ── Login / WhoAmI ──────────────────────────────────────────────

func (s *IdentityServer) Login(ctx context.Context, req *connect.Request[identityv1.LoginRequest]) (*connect.Response[identityv1.LoginResponse], error) {
	msg := req.Msg
	if msg == nil || msg.Username == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, ErrMissingUsername)
	}
	if msg.Password == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, ErrMissingPassword)
	}
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

func (s *IdentityServer) CardLogin(ctx context.Context, req *connect.Request[identityv1.CardLoginRequest]) (*connect.Response[identityv1.CardLoginResponse], error) {
	msg := req.Msg
	if msg == nil || msg.CardToken == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, ErrMissingCardToken)
	}
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

func (s *IdentityServer) checkLoginRateLimit(peerAddr, identifier string) *connect.Error {
	if s.idLimiter != nil && !s.idLimiter.Allow(identifier) {
		return connect.NewError(connect.CodeResourceExhausted, ErrRateLimitExceeded)
	}
	ip := extractIP(peerAddr)
	if ip != "" && s.ipLimiter != nil && !s.ipLimiter.Allow(ip) {
		return connect.NewError(connect.CodeResourceExhausted, ErrRateLimitExceeded)
	}
	return nil
}

func extractIP(addr string) string {
	if addr == "" {
		return ""
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	return host
}

func (s *IdentityServer) WhoAmI(ctx context.Context, req *connect.Request[identityv1.WhoAmIRequest]) (*connect.Response[identityv1.WhoAmIResponse], error) {
	tokenStr, err := extractBearer(req.Header())
	if err != nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, err)
	}
	user, err := s.auth.WhoAmI(ctx, tokenStr)
	if err != nil {
		return nil, authErrorToConnect(err)
	}
	return connect.NewResponse(&identityv1.WhoAmIResponse{User: toProtoUser(user)}), nil
}

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
		return connect.NewError(connect.CodeInternal, errors.New("internal error"))
	}
}

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
		ProjectId:   u.ProjectIDStr(),
		CreatedAt:   createdAt,
	}
}

func toProtoTimestamp(t time.Time) *timestamppb.Timestamp {
	if t.IsZero() {
		return nil
	}
	return timestamppb.New(t)
}

// ── User management (project-scoped) ────────────────────────────

func (s *IdentityServer) ListUsers(ctx context.Context, req *connect.Request[identityv1.ListUsersRequest]) (*connect.Response[identityv1.ListUsersResponse], error) {
	claims, err := s.requireProjectRole(req.Header())
	if err != nil {
		return nil, err
	}

	projectID := req.Msg.GetProjectId()
	if claims.Role != "ADMIN" || claims.ProjectID != "" {
		// Project admins can only list users in their own project.
		projectID = claims.ProjectID
	}

	users, err := s.store.ListUsers(ctx, req.Msg.GetQuery(), projectID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("list users: %w", err))
	}
	pbUsers := make([]*identityv1.User, len(users))
	for i, u := range users {
		pbUsers[i] = toProtoUser(u)
	}
	return connect.NewResponse(&identityv1.ListUsersResponse{Users: pbUsers}), nil
}

func (s *IdentityServer) CreateUser(ctx context.Context, req *connect.Request[identityv1.CreateUserRequest]) (*connect.Response[identityv1.CreateUserResponse], error) {
	claims, err := s.requireProjectRole(req.Header())
	if err != nil {
		return nil, err
	}

	msg := req.Msg
	if msg.Username == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, ErrMissingUsername)
	}
	if msg.Password == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, ErrMissingPassword)
	}

	projectID := msg.GetProjectId()
	if claims.Role != "ADMIN" || claims.ProjectID != "" {
		projectID = claims.ProjectID
	}
	if projectID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("project_id is required"))
	}

	passwordHash, err := HashPassword(msg.Password)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("hash password: %w", err))
	}

	role := protoRoleToDomain(msg.Role)
	u, err := s.store.CreateUser(ctx, msg.Username, passwordHash, msg.DisplayName, role, msg.WardIds, projectID)
	if err != nil {
		if errors.Is(err, ErrUsernameTaken) {
			return nil, connect.NewError(connect.CodeAlreadyExists, ErrUsernameTaken)
		}
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("create user: %w", err))
	}
	return connect.NewResponse(&identityv1.CreateUserResponse{User: toProtoUser(u)}), nil
}

func (s *IdentityServer) UpdateUser(ctx context.Context, req *connect.Request[identityv1.UpdateUserRequest]) (*connect.Response[identityv1.UpdateUserResponse], error) {
	claims, err := s.requireProjectRole(req.Header())
	if err != nil {
		return nil, err
	}

	msg := req.Msg
	if msg.Id == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("user id is required"))
	}

	var displayName *string
	if msg.DisplayName != nil {
		dn := *msg.DisplayName
		displayName = &dn
	}
	var role *Role
	if msg.Role != nil {
		r := protoRoleToDomain(*msg.Role)
		role = &r
	}
	var projectID *string
	if msg.ProjectId != nil {
		if claims.Role != "ADMIN" || claims.ProjectID != "" {
			return nil, connect.NewError(connect.CodePermissionDenied, errors.New("only SYSADMIN can move users between projects"))
		}
		pid := msg.GetProjectId()
		projectID = &pid
	}

	u, err := s.store.UpdateUser(ctx, msg.Id, displayName, role, msg.Active, msg.WardIds, projectID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("update user: %w", err))
	}
	if u == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("user not found"))
	}
	return connect.NewResponse(&identityv1.UpdateUserResponse{User: toProtoUser(u)}), nil
}

func (s *IdentityServer) SetCardToken(ctx context.Context, req *connect.Request[identityv1.SetCardTokenRequest]) (*connect.Response[identityv1.SetCardTokenResponse], error) {
	if _, err := s.requireProjectRole(req.Header()); err != nil {
		return nil, err
	}
	msg := req.Msg
	if msg.UserId == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("user_id is required"))
	}
	if err := s.store.SetCardToken(ctx, msg.UserId, msg.CardToken); err != nil {
		if errors.Is(err, ErrMissingHasher) {
			return nil, connect.NewError(connect.CodeFailedPrecondition, err)
		}
		if errors.Is(err, ErrMissingCardToken) {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("set card token: %w", err))
	}
	u, err := s.store.GetByID(ctx, msg.UserId)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("get user: %w", err))
	}
	if u == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("user not found"))
	}
	return connect.NewResponse(&identityv1.SetCardTokenResponse{User: toProtoUser(u)}), nil
}

// ── Authorization ───────────────────────────────────────────────

// requireSysAdmin returns claims only if the caller is SYSADMIN (ADMIN + no project).
func (s *IdentityServer) requireSysAdmin(header http.Header) (*TokenClaims, error) {
	claims, err := s.parseClaims(header)
	if err != nil {
		return nil, err
	}
	if claims.Role != "ADMIN" || claims.ProjectID != "" {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("sysadmin role required"))
	}
	return claims, nil
}

// requireProjectRole returns claims if the caller is SYSADMIN or project ADMIN.
// Non-admin roles get permission denied.
func (s *IdentityServer) requireProjectRole(header http.Header) (*TokenClaims, error) {
	claims, err := s.parseClaims(header)
	if err != nil {
		return nil, err
	}
	if claims.Role != "ADMIN" {
		return nil, connect.NewError(connect.CodePermissionDenied, ErrNotAdmin)
	}
	return claims, nil
}

func (s *IdentityServer) parseClaims(header http.Header) (*TokenClaims, error) {
	tokenStr, err := extractBearer(header)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, err)
	}
	claims, err := s.auth.jwt.Parse(tokenStr)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, ErrInvalidCredentials)
	}
	return claims, nil
}

// ── Project service ─────────────────────────────────────────────

// Compile-time check.
var _ identityv1connect.ProjectServiceHandler = (*ProjectServer)(nil)

// ProjectServer handles ProjectService RPCs. SYSADMIN-only.
type ProjectServer struct {
	store *Store
}

func NewProjectServer(store *Store) *ProjectServer {
	return &ProjectServer{store: store}
}

func toProtoProject(p *Project) *identityv1.Project {
	if p == nil {
		return nil
	}
	return &identityv1.Project{
		Id:          p.ID,
		Code:        p.Code,
		Name:        p.Name,
		DisplayName: p.DisplayName,
		Slug:        p.Slug,
		Active:      p.Active,
		CreatedAt:   timestamppb.New(p.CreatedAt),
		UpdatedAt:   timestamppb.New(p.UpdatedAt),
	}
}

func (s *ProjectServer) CreateProject(ctx context.Context, req *connect.Request[identityv1.CreateProjectRequest]) (*connect.Response[identityv1.CreateProjectResponse], error) {
	msg := req.Msg
	if msg.Name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("project name is required"))
	}
	slug := msg.Slug
	if slug == "" {
		slug = strings.ToLower(strings.ReplaceAll(msg.Name, " ", "-"))
	}
	code := msg.Code
	if code == "" {
		code = slug
	}
	p, err := s.store.CreateProject(ctx, msg.Name, slug, code)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("create project: %w", err))
	}
	return connect.NewResponse(&identityv1.CreateProjectResponse{Project: toProtoProject(p)}), nil
}

func (s *ProjectServer) UpdateProject(ctx context.Context, req *connect.Request[identityv1.UpdateProjectRequest]) (*connect.Response[identityv1.UpdateProjectResponse], error) {
	msg := req.Msg
	if msg.Id == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("project id is required"))
	}
	var name *string
	if msg.Name != nil {
		n := msg.GetName()
		name = &n
	}
	p, err := s.store.UpdateProject(ctx, msg.Id, name, msg.Active)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("update project: %w", err))
	}
	if p == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("project not found"))
	}
	return connect.NewResponse(&identityv1.UpdateProjectResponse{Project: toProtoProject(p)}), nil
}

func (s *ProjectServer) ListProjects(ctx context.Context, req *connect.Request[identityv1.ListProjectsRequest]) (*connect.Response[identityv1.ListProjectsResponse], error) {
	projects, err := s.store.ListProjects(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("list projects: %w", err))
	}
	pb := make([]*identityv1.Project, len(projects))
	for i, p := range projects {
		pb[i] = toProtoProject(p)
	}
	return connect.NewResponse(&identityv1.ListProjectsResponse{Projects: pb}), nil
}

func (s *ProjectServer) GetProject(ctx context.Context, req *connect.Request[identityv1.GetProjectRequest]) (*connect.Response[identityv1.GetProjectResponse], error) {
	if req.Msg.Id == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("project id is required"))
	}
	p, err := s.store.GetProject(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("get project: %w", err))
	}
	if p == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("project not found"))
	}
	return connect.NewResponse(&identityv1.GetProjectResponse{Project: toProtoProject(p)}), nil
}

// protoRoleToDomain maps a proto Role enum to the domain Role string.
func protoRoleToDomain(r identityv1.Role) Role {
	switch r {
	case identityv1.Role_ROLE_ADMIN:
		return RoleAdmin
	case identityv1.Role_ROLE_PHARMACIST:
		return RolePharmacist
	case identityv1.Role_ROLE_NURSE:
		return RoleNurse
	case identityv1.Role_ROLE_REFILLER:
		return RoleRefiller
	default:
		return RoleNurse
	}
}
