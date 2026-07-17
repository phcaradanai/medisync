package catalog

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"connectrpc.com/connect"
	catalogv1 "github.com/adm-chura3inter/medisync/services/core/internal/gen/medisync/catalog/v1"
	catalogv1connect "github.com/adm-chura3inter/medisync/services/core/internal/gen/medisync/catalog/v1/catalogv1connect"
	"github.com/adm-chura3inter/medisync/services/core/internal/platform/audit"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Common catalog errors returned to callers.
var (
	ErrDrugCodeRequired = errors.New("drug code is required")
	ErrDrugNameRequired = errors.New("drug name is required")
	ErrDrugIDRequired   = errors.New("drug id is required")
	ErrBarcodeRequired  = errors.New("barcode is required")
	ErrDrugNotFound     = errors.New("drug not found")
	ErrNotAuthenticated = errors.New("authentication required")
	ErrNotAdmin         = errors.New("admin role required")
)

// TokenClaimser is the narrow JWT claims interface consumed by catalog.
// Avoids circular dependency on package identity.
type TokenClaimser interface {
	GetSubject() string
	GetRole() string
	GetProjectID() string
}

// Claims adapter wraps identity TokenClaims for catalog.
type Claims struct {
	Subject   string
	Role      string
	ProjectID string
}

func (c Claims) GetSubject() string   { return c.Subject }
func (c Claims) GetRole() string      { return c.Role }
func (c Claims) GetProjectID() string { return c.ProjectID }

// TokenParser validates a Bearer token and returns claims.
type TokenParser interface {
	Parse(tokenString string) (TokenClaimser, error)
}

// DrugStore is the narrow drug-persistence interface.
type DrugStore interface {
	Create(ctx context.Context, d Drug) (*Drug, error)
	GetByID(ctx context.Context, id string) (*Drug, error)
	GetByCode(ctx context.Context, code string) (*Drug, error)
	GetByBarcode(ctx context.Context, barcode string) (*Drug, error)
	List(ctx context.Context, query string, includeInactive bool, pageSize int32, pageToken, projectID string) ([]*Drug, string, error)
	Update(ctx context.Context, d Drug) (*Drug, error)
	Deactivate(ctx context.Context, id string) (*Drug, error)
}

var _ catalogv1connect.CatalogServiceHandler = (*CatalogServer)(nil)
var _ DrugStore = (*Store)(nil)

// CatalogServer is the Connect-RPC handler for CatalogService.
type CatalogServer struct {
	store  DrugStore
	audit  *audit.Writer
	parser TokenParser
}

// NewCatalogServer creates a CatalogServer with the given store and audit writer.
func NewCatalogServer(store DrugStore, aw *audit.Writer) *CatalogServer {
	return &CatalogServer{store: store, audit: aw}
}

// NewCatalogServerWithAuth creates a CatalogServer with JWT auth.
func NewCatalogServerWithAuth(store DrugStore, aw *audit.Writer, parser TokenParser) *CatalogServer {
	return &CatalogServer{store: store, audit: aw, parser: parser}
}

// authenticate extracts and validates JWT claims. Returns connect.Error on failure.
func (s *CatalogServer) authenticate(header http.Header) (TokenClaimser, *connect.Error) {
	if s.parser == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, ErrNotAuthenticated)
	}
	auth := header.Get("Authorization")
	if auth == "" {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("authorization header is required"))
	}
	parts := strings.SplitN(auth, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("authorization header must use Bearer scheme"))
	}
	claims, err := s.parser.Parse(strings.TrimSpace(parts[1]))
	if err != nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("invalid credentials"))
	}
	if claims.GetRole() != "ADMIN" {
		return nil, connect.NewError(connect.CodePermissionDenied, ErrNotAdmin)
	}
	return claims, nil
}

func (s *CatalogServer) CreateDrug(ctx context.Context, req *connect.Request[catalogv1.CreateDrugRequest]) (*connect.Response[catalogv1.CreateDrugResponse], error) {
	claims, cerr := s.authenticate(req.Header())
	if cerr != nil {
		return nil, cerr
	}
	msg := req.Msg
	if msg == nil || msg.Code == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, ErrDrugCodeRequired)
	}
	if msg.Name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, ErrDrugNameRequired)
	}

	projectID := claims.GetProjectID()
	if projectID == "" {
		projectID = msg.ProjectId
	}
	if projectID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("project_id is required"))
	}

	drug, err := s.store.Create(ctx, Drug{
		Code:        msg.Code,
		Name:        msg.Name,
		DisplayName: msg.DisplayName,
		GenericName: msg.GenericName,
		Form:        msg.Form,
		Strength:    msg.Strength,
		Unit:        msg.Unit,
		StickerNote: msg.StickerNote,
		ProjectID:   projectID,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("create drug: %w", err))
	}

	s.writeAudit(ctx, audit.Entry{
		Actor:     claims.GetSubject(),
		Action:    "drug.created",
		Entity:    "drug",
		EntityID:  drug.ID,
		ProjectID: claims.GetProjectID(),
		Detail:    auditDetail{Code: drug.Code, Name: drug.Name},
	})

	return connect.NewResponse(&catalogv1.CreateDrugResponse{Drug: toProtoDrug(drug)}), nil
}

func (s *CatalogServer) GetDrug(ctx context.Context, req *connect.Request[catalogv1.GetDrugRequest]) (*connect.Response[catalogv1.GetDrugResponse], error) {
	if _, cerr := s.authenticate(req.Header()); cerr != nil {
		return nil, cerr
	}
	msg := req.Msg
	if msg == nil || msg.Id == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, ErrDrugIDRequired)
	}

	drug, err := s.store.GetByID(ctx, msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("get drug: %w", err))
	}
	if drug == nil {
		return nil, connect.NewError(connect.CodeNotFound, ErrDrugNotFound)
	}

	return connect.NewResponse(&catalogv1.GetDrugResponse{Drug: toProtoDrug(drug)}), nil
}

func (s *CatalogServer) GetByBarcode(ctx context.Context, req *connect.Request[catalogv1.GetByBarcodeRequest]) (*connect.Response[catalogv1.GetByBarcodeResponse], error) {
	if _, cerr := s.authenticate(req.Header()); cerr != nil {
		return nil, cerr
	}
	msg := req.Msg
	if msg == nil || msg.Barcode == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, ErrBarcodeRequired)
	}

	drug, err := s.store.GetByBarcode(ctx, msg.Barcode)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("get drug by barcode: %w", err))
	}
	if drug == nil {
		return nil, connect.NewError(connect.CodeNotFound, ErrDrugNotFound)
	}

	return connect.NewResponse(&catalogv1.GetByBarcodeResponse{Drug: toProtoDrug(drug)}), nil
}

func (s *CatalogServer) ListDrugs(ctx context.Context, req *connect.Request[catalogv1.ListDrugsRequest]) (*connect.Response[catalogv1.ListDrugsResponse], error) {
	claims, cerr := s.authenticate(req.Header())
	if cerr != nil {
		return nil, cerr
	}
	msg := req.Msg
	query := ""
	includeInactive := false
	pageSize := int32(50)
	pageToken := ""

	if msg != nil {
		query = msg.Query
		includeInactive = msg.IncludeInactive
		if msg.PageSize > 0 {
			pageSize = msg.PageSize
		}
		pageToken = msg.PageToken
	}

	projectID := claims.GetProjectID()
	drugs, nextToken, err := s.store.List(ctx, query, includeInactive, pageSize, pageToken, projectID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("list drugs: %w", err))
	}

	pbDrugs := make([]*catalogv1.Drug, 0, len(drugs))
	for _, d := range drugs {
		pbDrugs = append(pbDrugs, toProtoDrug(d))
	}

	return connect.NewResponse(&catalogv1.ListDrugsResponse{Drugs: pbDrugs, NextPageToken: nextToken}), nil
}

func (s *CatalogServer) UpdateDrug(ctx context.Context, req *connect.Request[catalogv1.UpdateDrugRequest]) (*connect.Response[catalogv1.UpdateDrugResponse], error) {
	claims, cerr := s.authenticate(req.Header())
	if cerr != nil {
		return nil, cerr
	}
	msg := req.Msg
	if msg == nil || msg.Drug == nil || msg.Drug.Id == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, ErrDrugIDRequired)
	}

	pb := msg.Drug
	drug, err := s.store.Update(ctx, Drug{
		ID:          pb.Id,
		Code:        pb.Code,
		Name:        pb.Name,
		GenericName: pb.GenericName,
		Form:        pb.Form,
		Strength:    pb.Strength,
		Unit:        pb.Unit,
		StickerNote: pb.StickerNote,
		Active:      pb.Active,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("update drug: %w", err))
	}
	if drug == nil {
		return nil, connect.NewError(connect.CodeNotFound, ErrDrugNotFound)
	}

	s.writeAudit(ctx, audit.Entry{
		Actor:     claims.GetSubject(),
		Action:    "drug.updated",
		Entity:    "drug",
		EntityID:  drug.ID,
		ProjectID: claims.GetProjectID(),
		Detail:    auditDetail{Code: drug.Code, Name: drug.Name},
	})

	return connect.NewResponse(&catalogv1.UpdateDrugResponse{Drug: toProtoDrug(drug)}), nil
}

func (s *CatalogServer) DeactivateDrug(ctx context.Context, req *connect.Request[catalogv1.DeactivateDrugRequest]) (*connect.Response[catalogv1.DeactivateDrugResponse], error) {
	claims, cerr := s.authenticate(req.Header())
	if cerr != nil {
		return nil, cerr
	}
	msg := req.Msg
	if msg == nil || msg.Id == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, ErrDrugIDRequired)
	}

	drug, err := s.store.Deactivate(ctx, msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("deactivate drug: %w", err))
	}
	if drug == nil {
		return nil, connect.NewError(connect.CodeNotFound, ErrDrugNotFound)
	}

	s.writeAudit(ctx, audit.Entry{
		Actor:     claims.GetSubject(),
		Action:    "drug.deactivated",
		Entity:    "drug",
		EntityID:  drug.ID,
		ProjectID: claims.GetProjectID(),
		Detail:    auditDetail{Code: drug.Code, Name: drug.Name},
	})

	return connect.NewResponse(&catalogv1.DeactivateDrugResponse{Drug: toProtoDrug(drug)}), nil
}

func (s *CatalogServer) writeAudit(ctx context.Context, e audit.Entry) {
	if s.audit == nil {
		return
	}
	_ = s.audit.Write(ctx, e)
}

func toProtoDrug(d *Drug) *catalogv1.Drug {
	if d == nil {
		return nil
	}
	var createdAt, updatedAt *timestamppb.Timestamp
	if !d.CreatedAt.IsZero() {
		createdAt = timestamppb.New(d.CreatedAt)
	}
	if !d.UpdatedAt.IsZero() {
		updatedAt = timestamppb.New(d.UpdatedAt)
	}
	return &catalogv1.Drug{
		Id:          d.ID,
		Code:        d.Code,
		Name:        d.Name,
		DisplayName: d.DisplayName,
		GenericName: d.GenericName,
		Form:        d.Form,
		Strength:    d.Strength,
		Unit:        d.Unit,
		StickerNote: d.StickerNote,
		Active:      d.Active,
		ProjectId:   d.ProjectID,
		Barcode:     d.Barcode,
		CreatedAt:   createdAt,
		UpdatedAt:   updatedAt,
	}
}
