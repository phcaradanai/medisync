package cabinet

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"connectrpc.com/connect"
	cabinetv1 "github.com/adm-chura3inter/medisync/services/core/internal/gen/medisync/cabinet/v1"
	cabinetv1connect "github.com/adm-chura3inter/medisync/services/core/internal/gen/medisync/cabinet/v1/cabinetv1connect"
	"github.com/adm-chura3inter/medisync/services/core/internal/platform/audit"
	"github.com/adm-chura3inter/medisync/services/core/internal/platform/pagination"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Compile-time check.
var _ cabinetv1connect.CabinetServiceHandler = (*Server)(nil)

// TokenParser validates admin bearer tokens.
type TokenParser interface {
	Parse(tokenString string) (*TokenClaims, error)
}

// TokenClaims mirrors identity.TokenClaims to avoid import cycle.
type TokenClaims struct {
	Subject   string
	Role      string
	ProjectID string
	WardIDs   []string
}

// Server is the Connect-RPC handler for CabinetService.
type Server struct {
	store  *Store
	parser TokenParser
	audit  *audit.Writer
}

// NewServer creates a CabinetService handler.
func NewServer(store *Store, parser TokenParser, aw *audit.Writer) *Server {
	return &Server{store: store, parser: parser, audit: aw}
}

// ListCabinets returns cabinets scoped to the caller's project.
// Requires admin role. SYSADMIN sees all cabinets.
func (s *Server) ListCabinets(
	ctx context.Context,
	req *connect.Request[cabinetv1.ListCabinetsRequest],
) (*connect.Response[cabinetv1.ListCabinetsResponse], error) {
	claims, err := s.authenticate(req.Header())
	if err != nil {
		return nil, err
	}
	if claims.Role != "ADMIN" {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("admin role required"))
	}

	pageSize, pageToken := pagination.DefaultPageSize, ""
	if req.Msg != nil && req.Msg.Pagination != nil {
		pageSize = pagination.NormalizePageSize(req.Msg.Pagination.PageSize)
		pageToken = req.Msg.Pagination.PageToken
	}

	cabinets, nextToken, totalCount, err := s.store.List(ctx, claims.ProjectID, pageSize, pageToken)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.New("internal error"))
	}

	pb := make([]*cabinetv1.Cabinet, len(cabinets))
	for i, c := range cabinets {
		pb[i] = toProto(c)
	}
	return connect.NewResponse(&cabinetv1.ListCabinetsResponse{
		Cabinets:      pb,
		NextPageToken: nextToken,
		TotalCount:    totalCount,
	}), nil
}

// CreateCabinet registers a new cabinet. Requires admin role.
func (s *Server) CreateCabinet(
	ctx context.Context,
	req *connect.Request[cabinetv1.CreateCabinetRequest],
) (*connect.Response[cabinetv1.CreateCabinetResponse], error) {
	claims, err := s.authenticate(req.Header())
	if err != nil {
		return nil, err
	}

	msg := req.Msg
	if msg == nil || msg.Code == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("cabinet code is required"))
	}
	if msg.Name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("cabinet name is required"))
	}

	projectID := claims.ProjectID
	if projectID == "" {
		projectID = msg.ProjectId
	}
	if projectID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("project_id is required"))
	}

	displayName := msg.DisplayName
	if displayName == "" {
		displayName = msg.Name
	}

	c, err := s.store.Create(ctx, msg.Code, msg.Name, displayName, projectID)
	if err != nil {
		if errors.Is(err, ErrDuplicateCode) {
			return nil, connect.NewError(connect.CodeAlreadyExists, ErrDuplicateCode)
		}
		return nil, connect.NewError(connect.CodeInternal, errors.New("internal error"))
	}
	s.writeAudit(ctx, audit.Entry{
		Actor:     claims.Subject,
		Action:    "create_cabinet",
		Entity:    "cabinet",
		EntityID:  c.ID,
		ProjectID: claims.ProjectID,
		Detail:    map[string]string{"actor_type": claims.Role},
	})

	return connect.NewResponse(&cabinetv1.CreateCabinetResponse{Cabinet: toProto(c)}), nil
}

// UpdateCabinet modifies name and/or active flag. Requires admin role.
func (s *Server) UpdateCabinet(
	ctx context.Context,
	req *connect.Request[cabinetv1.UpdateCabinetRequest],
) (*connect.Response[cabinetv1.UpdateCabinetResponse], error) {
	claims, authErr := s.authenticate(req.Header())
	if authErr != nil {
		return nil, authErr
	}
	if claims.Role != "ADMIN" {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("admin role required"))
	}

	msg := req.Msg
	if msg == nil || msg.Id == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("cabinet id is required"))
	}

	existing, err := s.store.GetByID(ctx, msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.New("internal error"))
	}
	if existing == nil {
		return nil, connect.NewError(connect.CodeNotFound, ErrNotFound)
	}

	var name *string
	if msg.Name != nil {
		nm := *msg.Name
		name = &nm
	}

	updated, err := s.store.Update(ctx, msg.Id, name, msg.Active)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.New("internal error"))
	}
	if updated == nil {
		return nil, connect.NewError(connect.CodeNotFound, ErrNotFound)
	}
	s.writeAudit(ctx, audit.Entry{
		Actor:     claims.Subject,
		Action:    "update_cabinet",
		Entity:    "cabinet",
		EntityID:  updated.ID,
		ProjectID: claims.ProjectID,
		Detail:    map[string]string{"actor_type": claims.Role},
	})

	return connect.NewResponse(&cabinetv1.UpdateCabinetResponse{Cabinet: toProto(updated)}), nil
}

func (s *Server) writeAudit(ctx context.Context, entry audit.Entry) {
	if s.audit == nil {
		return
	}
	if entry.TraceID == "" {
		entry.TraceID = uuid.NewString()
	}
	_ = s.audit.Write(ctx, entry)
}

// requireAdmin validates the admin bearer token.
func (s *Server) requireAdmin(header http.Header) error {
	claims, err := s.authenticate(header)
	if err != nil {
		return err
	}
	if claims.Role != "ADMIN" {
		return connect.NewError(connect.CodePermissionDenied, errors.New("admin role required"))
	}
	return nil
}

func (s *Server) authenticate(header http.Header) (*TokenClaims, error) {
	tokenStr, err := extractBearer(header)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, err)
	}
	claims, err := s.parser.Parse(tokenStr)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("invalid token"))
	}
	return claims, nil
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

func toProto(c *Cabinet) *cabinetv1.Cabinet {
	if c == nil {
		return nil
	}
	createdAt := timestamppb.New(c.CreatedAt)
	updatedAt := timestamppb.New(c.UpdatedAt)
	if c.CreatedAt.IsZero() {
		createdAt = nil
	}
	if c.UpdatedAt.IsZero() {
		updatedAt = nil
	}
	return &cabinetv1.Cabinet{
		Id:          c.ID,
		Code:        c.Code,
		Name:        c.Name,
		DisplayName: c.DisplayName,
		Active:      c.Active,
		ProjectId:   c.ProjectID,
		CreatedAt:   createdAt,
		UpdatedAt:   updatedAt,
	}
}
