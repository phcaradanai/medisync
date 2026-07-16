package cabinet

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"connectrpc.com/connect"
	cabinetv1 "github.com/adm-chura3inter/medisync/services/core/internal/gen/medisync/cabinet/v1"
	cabinetv1connect "github.com/adm-chura3inter/medisync/services/core/internal/gen/medisync/cabinet/v1/cabinetv1connect"
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
	Subject string
	Role    string
	WardIDs []string
}

// Server is the Connect-RPC handler for CabinetService.
type Server struct {
	store  *Store
	parser TokenParser
}

// NewServer creates a CabinetService handler.
func NewServer(store *Store, parser TokenParser) *Server {
	return &Server{store: store, parser: parser}
}

// ListCabinets returns all cabinets. Requires admin role.
func (s *Server) ListCabinets(
	ctx context.Context,
	req *connect.Request[cabinetv1.ListCabinetsRequest],
) (*connect.Response[cabinetv1.ListCabinetsResponse], error) {
	if err := s.requireAdmin(req.Header()); err != nil {
		return nil, err
	}

	cabinets, err := s.store.List(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.New("internal error"))
	}

	pb := make([]*cabinetv1.Cabinet, len(cabinets))
	for i, c := range cabinets {
		pb[i] = toProto(c)
	}
	return connect.NewResponse(&cabinetv1.ListCabinetsResponse{Cabinets: pb}), nil
}

// CreateCabinet registers a new cabinet. Requires admin role.
func (s *Server) CreateCabinet(
	ctx context.Context,
	req *connect.Request[cabinetv1.CreateCabinetRequest],
) (*connect.Response[cabinetv1.CreateCabinetResponse], error) {
	if err := s.requireAdmin(req.Header()); err != nil {
		return nil, err
	}

	msg := req.Msg
	if msg == nil || msg.Code == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("cabinet code is required"))
	}
	if msg.Name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("cabinet name is required"))
	}

	c, err := s.store.Create(ctx, msg.Code, msg.Name)
	if err != nil {
		if errors.Is(err, ErrDuplicateCode) {
			return nil, connect.NewError(connect.CodeAlreadyExists, ErrDuplicateCode)
		}
		return nil, connect.NewError(connect.CodeInternal, errors.New("internal error"))
	}

	return connect.NewResponse(&cabinetv1.CreateCabinetResponse{Cabinet: toProto(c)}), nil
}

// UpdateCabinet modifies name and/or active flag. Requires admin role.
func (s *Server) UpdateCabinet(
	ctx context.Context,
	req *connect.Request[cabinetv1.UpdateCabinetRequest],
) (*connect.Response[cabinetv1.UpdateCabinetResponse], error) {
	if err := s.requireAdmin(req.Header()); err != nil {
		return nil, err
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

	return connect.NewResponse(&cabinetv1.UpdateCabinetResponse{Cabinet: toProto(updated)}), nil
}

// requireAdmin validates the admin bearer token.
func (s *Server) requireAdmin(header http.Header) error {
	tokenStr, err := extractBearer(header)
	if err != nil {
		return connect.NewError(connect.CodeUnauthenticated, err)
	}
	claims, err := s.parser.Parse(tokenStr)
	if err != nil {
		return connect.NewError(connect.CodeUnauthenticated, errors.New("invalid token"))
	}
	if claims.Role != "ADMIN" {
		return connect.NewError(connect.CodePermissionDenied, errors.New("admin role required"))
	}
	return nil
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
		Id:        c.ID,
		Code:      c.Code,
		Name:      c.Name,
		Active:    c.Active,
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
	}
}
