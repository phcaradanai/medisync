package catalog

import (
	"context"
	"errors"
	"fmt"

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
	ErrDrugNotFound     = errors.New("drug not found")
)

// DrugStore is the narrow drug-persistence interface consumed by the
// catalog handler. The concrete Store satisfies this interface.
type DrugStore interface {
	Create(ctx context.Context, d Drug) (*Drug, error)
	GetByID(ctx context.Context, id string) (*Drug, error)
	GetByCode(ctx context.Context, code string) (*Drug, error)
	List(ctx context.Context, query string, includeInactive bool, pageSize int32, pageToken string) ([]*Drug, string, error)
	Update(ctx context.Context, d Drug) (*Drug, error)
	Deactivate(ctx context.Context, id string) (*Drug, error)
}

// Compile-time checks.
var _ catalogv1connect.CatalogServiceHandler = (*CatalogServer)(nil)
var _ DrugStore = (*Store)(nil)

// CatalogServer is the Connect-RPC handler for CatalogService.
type CatalogServer struct {
	store DrugStore
	audit *audit.Writer
}

// NewCatalogServer creates a CatalogServer with the given store and audit writer.
func NewCatalogServer(store DrugStore, aw *audit.Writer) *CatalogServer {
	return &CatalogServer{store: store, audit: aw}
}

// CreateDrug handles drug creation. It validates required fields, creates
// the drug via the store, and writes an audit entry.
func (s *CatalogServer) CreateDrug(
	ctx context.Context,
	req *connect.Request[catalogv1.CreateDrugRequest],
) (*connect.Response[catalogv1.CreateDrugResponse], error) {
	msg := req.Msg
	if msg == nil || msg.Code == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, ErrDrugCodeRequired)
	}
	if msg.Name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, ErrDrugNameRequired)
	}

	drug, err := s.store.Create(ctx, Drug{
		Code:        msg.Code,
		Name:        msg.Name,
		GenericName: msg.GenericName,
		Form:        msg.Form,
		Strength:    msg.Strength,
		Unit:        msg.Unit,
		StickerNote: msg.StickerNote,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("create drug: %w", err))
	}

	s.writeAudit(ctx, audit.Entry{
		Actor:    "system",
		Action:   "drug.created",
		Entity:   "drug",
		EntityID: drug.ID,
		Detail:   auditDetail{Code: drug.Code, Name: drug.Name},
	})

	return connect.NewResponse(&catalogv1.CreateDrugResponse{
		Drug: toProtoDrug(drug),
	}), nil
}

// GetDrug fetches a single drug by ID. Returns NotFound when the drug
// does not exist.
func (s *CatalogServer) GetDrug(
	ctx context.Context,
	req *connect.Request[catalogv1.GetDrugRequest],
) (*connect.Response[catalogv1.GetDrugResponse], error) {
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

	return connect.NewResponse(&catalogv1.GetDrugResponse{
		Drug: toProtoDrug(drug),
	}), nil
}

// ListDrugs searches and paginates the drug catalog. An empty query
// returns all drugs. By default only active drugs are returned.
func (s *CatalogServer) ListDrugs(
	ctx context.Context,
	req *connect.Request[catalogv1.ListDrugsRequest],
) (*connect.Response[catalogv1.ListDrugsResponse], error) {
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

	drugs, nextToken, err := s.store.List(ctx, query, includeInactive, pageSize, pageToken)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("list drugs: %w", err))
	}

	pbDrugs := make([]*catalogv1.Drug, 0, len(drugs))
	for _, d := range drugs {
		pbDrugs = append(pbDrugs, toProtoDrug(d))
	}

	return connect.NewResponse(&catalogv1.ListDrugsResponse{
		Drugs:         pbDrugs,
		NextPageToken: nextToken,
	}), nil
}

// UpdateDrug modifies an existing drug. The request Drug must include
// the id. All mutable fields are applied; timestamps are managed server-side.
func (s *CatalogServer) UpdateDrug(
	ctx context.Context,
	req *connect.Request[catalogv1.UpdateDrugRequest],
) (*connect.Response[catalogv1.UpdateDrugResponse], error) {
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
		Actor:    "system",
		Action:   "drug.updated",
		Entity:   "drug",
		EntityID: drug.ID,
		Detail:   auditDetail{Code: drug.Code, Name: drug.Name},
	})

	return connect.NewResponse(&catalogv1.UpdateDrugResponse{
		Drug: toProtoDrug(drug),
	}), nil
}

// DeactivateDrug soft-deletes a drug by setting active=false. It is
// idempotent — deactivating an already-inactive drug returns NotFound.
func (s *CatalogServer) DeactivateDrug(
	ctx context.Context,
	req *connect.Request[catalogv1.DeactivateDrugRequest],
) (*connect.Response[catalogv1.DeactivateDrugResponse], error) {
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
		Actor:    "system",
		Action:   "drug.deactivated",
		Entity:   "drug",
		EntityID: drug.ID,
		Detail:   auditDetail{Code: drug.Code, Name: drug.Name},
	})

	return connect.NewResponse(&catalogv1.DeactivateDrugResponse{
		Drug: toProtoDrug(drug),
	}), nil
}

// writeAudit records an audit entry when the audit writer is configured.
// Audit failures are logged but do not cause the RPC to fail.
func (s *CatalogServer) writeAudit(ctx context.Context, e audit.Entry) {
	if s.audit == nil {
		return
	}
	_ = s.audit.Write(ctx, e)
}

// toProtoDrug converts a domain Drug to a proto Drug. Safe for nil input.
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
		GenericName: d.GenericName,
		Form:        d.Form,
		Strength:    d.Strength,
		Unit:        d.Unit,
		StickerNote: d.StickerNote,
		Active:      d.Active,
		CreatedAt:   createdAt,
		UpdatedAt:   updatedAt,
	}
}
