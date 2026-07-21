package dispensing

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"

	dispensingv1 "github.com/adm-chura3inter/medisync/services/core/internal/gen/medisync/dispensing/v1"
	dispensingv1connect "github.com/adm-chura3inter/medisync/services/core/internal/gen/medisync/dispensing/v1/dispensingv1connect"
	eventsv1 "github.com/adm-chura3inter/medisync/services/core/internal/gen/medisync/events/v1"
	"github.com/adm-chura3inter/medisync/services/core/internal/platform/audit"
	"github.com/adm-chura3inter/medisync/services/core/internal/platform/pagination"
)

// TokenParser is the narrow JWT interface consumed by the dispensing handler.
type TokenParser interface {
	Parse(tokenString string) (*TokenClaims, error)
}

// TokenClaims mirrors identity.TokenClaims for the dispensing handler's
// authorization needs (role + ward_ids). We define our own narrow interface
// to avoid a circular dependency on package identity.
type TokenClaims struct {
	Subject   string
	Role      string
	ProjectID string
	WardIDs   []string
}

// UserResolver resolves the current user from a JWT token. The identity
// module's AuthService satisfies this.
type UserResolver interface {
	WhoAmI(ctx context.Context, tokenString string) (*UserInfo, error)
}

// UserInfo carries the minimal user info needed for authorization.
type UserInfo struct {
	ID      string
	Role    string
	WardIDs []string
}

// Common dispensing errors returned to callers.
var (
	ErrPrescriptionNotFound   = errors.New("prescription not found")
	ErrPrescriptionIDRequired = errors.New("prescription_id is required")
	ErrInvalidTransition      = errors.New("invalid state transition")
	ErrUnauthorized           = errors.New("unauthorized")
	ErrWardRequired           = errors.New("ward_id is required for scoped access")
	ErrAuthorizationRequired  = errors.New("authorization header is required")
	ErrBearerSchemeRequired   = errors.New("authorization header must use Bearer scheme")
	ErrBearerTokenRequired    = errors.New("bearer token is required")
)

// Compile-time check: DispensingServer implements the generated handler interface.
var _ dispensingv1connect.DispensingServiceHandler = (*DispensingServer)(nil)

// DispensingStore is the narrow interface consumed by the handler.
type DispensingStore interface {
	GetByID(ctx context.Context, id string) (*PrescriptionRow, error)
	GetByPrescriptionID(ctx context.Context, prescriptionID, sourceSystem string) (*PrescriptionRow, error)
	ListByWard(ctx context.Context, wardIDs []string, states []State, pageSize int32, pageToken string) ([]*PrescriptionRow, string, int64, error)
	// TransitionState requires a caller-provided tx for atomic outbox insert.
}

// DispensingServer is the Connect-RPC handler for DispensingService.
type DispensingServer struct {
	store  DispensingStore
	pool   *pgxpool.Pool // for starting transactions on Dispense
	parser TokenParser
	audit  *audit.Writer
}

// NewDispensingServer creates a DispensingServer.
func NewDispensingServer(store DispensingStore, pool *pgxpool.Pool, parser TokenParser, aw *audit.Writer) *DispensingServer {
	return &DispensingServer{store: store, pool: pool, parser: parser, audit: aw}
}

// ListPrescriptions returns prescriptions filtered by the caller's ward scope.
// ADMIN role sees all wards; other roles are restricted to their assigned wards.
// The ward_id request parameter is ignored for non-ADMIN users (their JWT wards
// are the authoritative source). When ward_id is empty for ADMIN, all wards are
// returned. States filter is optional; empty means non-terminal states only.
func (s *DispensingServer) ListPrescriptions(
	ctx context.Context,
	req *connect.Request[dispensingv1.ListPrescriptionsRequest],
) (*connect.Response[dispensingv1.ListPrescriptionsResponse], error) {
	claims, err := s.authenticate(req.Header())
	if err != nil {
		return nil, err
	}

	msg := req.Msg
	pageSize := pagination.DefaultPageSize
	pageToken := ""
	if msg != nil {
		pageSize = pagination.NormalizePageSize(msg.PageSize)
		pageToken = msg.PageToken
	}

	// Resolve ward scope from JWT claims.
	wardIDs := s.resolveWardScope(claims, msg)

	// Convert proto states to domain states.
	var states []State
	if msg != nil {
		for _, ps := range msg.States {
			states = append(states, protoStateToDomain(ps))
		}
	}

	prescriptions, nextToken, totalCount, err := s.store.ListByWard(
		ctx, wardIDs, states, pageSize, pageToken,
	)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("list prescriptions: %w", err))
	}

	pbPrescriptions := make([]*dispensingv1.Prescription, 0, len(prescriptions))
	for _, pr := range prescriptions {
		pbPrescriptions = append(pbPrescriptions, toProtoPrescription(pr))
	}

	return connect.NewResponse(&dispensingv1.ListPrescriptionsResponse{
		Prescriptions: pbPrescriptions,
		NextPageToken: nextToken,
		TotalCount:    totalCount,
	}), nil
}

// GetPrescription fetches a single prescription by ID. The caller must be
// authorized for the prescription's ward.
func (s *DispensingServer) GetPrescription(
	ctx context.Context,
	req *connect.Request[dispensingv1.GetPrescriptionRequest],
) (*connect.Response[dispensingv1.GetPrescriptionResponse], error) {
	claims, err := s.authenticate(req.Header())
	if err != nil {
		return nil, err
	}

	msg := req.Msg
	if msg == nil || msg.Id == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("id is required"))
	}

	pr, err := s.store.GetByID(ctx, msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("get prescription: %w", err))
	}
	if pr == nil {
		return nil, connect.NewError(connect.CodeNotFound, ErrPrescriptionNotFound)
	}

	if !s.authorizeWard(claims, pr.WardID) {
		return nil, connect.NewError(connect.CodeNotFound, ErrPrescriptionNotFound)
	}

	return connect.NewResponse(&dispensingv1.GetPrescriptionResponse{
		Prescription: toProtoPrescription(pr),
	}), nil
}

// Dispense is the retired one-step endpoint. Physical dispensing must pass
// through PrepareDispense and ConfirmDispense so kiosk identity, stock
// reservation, and operator identity are durably bound before hardware starts.
func (s *DispensingServer) Dispense(
	ctx context.Context,
	req *connect.Request[dispensingv1.DispenseRequest],
) (*connect.Response[dispensingv1.DispenseResponse], error) {
	if _, err := s.authenticate(req.Header()); err != nil {
		return nil, err
	}
	return nil, connect.NewError(connect.CodeFailedPrecondition,
		errors.New("one-step dispense is disabled; scan with PrepareDispense then confirm identity with ConfirmDispense"))
}

func (s *DispensingServer) PrepareDispense(ctx context.Context, req *connect.Request[dispensingv1.PrepareDispenseRequest]) (*connect.Response[dispensingv1.PrepareDispenseResponse], error) {
	claims, err := s.authenticate(req.Header())
	if err != nil {
		return nil, err
	}
	if claims.Role != "KIOSK" || claims.Subject == "" {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("kiosk authentication required"))
	}
	msg := req.Msg
	if msg == nil || strings.TrimSpace(msg.StickerCode) == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("sticker_code is required"))
	}
	pr, err := s.findReadyByPrescriptionIDForProject(ctx, strings.TrimSpace(msg.StickerCode), claims.ProjectID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("find prescription: %w", err))
	}
	if pr == nil {
		return nil, connect.NewError(connect.CodeNotFound, ErrPrescriptionNotFound)
	}
	traceID := strings.TrimSpace(msg.TraceId)
	if traceID == "" {
		traceID = uuid.New().String()
	}
	fullStore, ok := s.store.(*Store)
	if !ok {
		return nil, connect.NewError(connect.CodeInternal, errors.New("transaction store unavailable"))
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("begin prepare: %w", err))
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	record, err := fullStore.PrepareTransaction(ctx, tx, pr, claims.Subject, claims.ProjectID, traceID)
	if err != nil {
		code := connect.CodeFailedPrecondition
		if errors.Is(err, ErrDispenseWrongKiosk) {
			code = connect.CodePermissionDenied
		}
		return nil, connect.NewError(code, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("commit prepare: %w", err))
	}
	s.writeAudit(ctx, audit.Entry{TraceID: traceID, Actor: claims.Subject, Action: "dispense.sticker_scanned", Entity: "dispense_transaction", EntityID: record.ID, ProjectID: claims.ProjectID, Detail: map[string]any{"kiosk_code": claims.Subject, "prescription_id": pr.PrescriptionID}})
	return connect.NewResponse(&dispensingv1.PrepareDispenseResponse{Transaction: toProtoTransaction(record), Prescription: toProtoPrescription(pr)}), nil
}

func (s *DispensingServer) ConfirmDispense(ctx context.Context, req *connect.Request[dispensingv1.ConfirmDispenseRequest]) (*connect.Response[dispensingv1.ConfirmDispenseResponse], error) {
	staff, err := s.authenticate(req.Header())
	if err != nil {
		return nil, err
	}
	if staff.Role == "KIOSK" {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("staff authentication required"))
	}
	kiosk, err := s.authenticateNamed(req.Header(), "X-Kiosk-Authorization")
	if err != nil {
		return nil, err
	}
	if kiosk.Role != "KIOSK" || kiosk.Subject == "" {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("kiosk authentication required"))
	}
	msg := req.Msg
	if msg == nil || msg.DispenseId == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("dispense_id is required"))
	}
	fullStore, ok := s.store.(*Store)
	if !ok {
		return nil, connect.NewError(connect.CodeInternal, errors.New("transaction store unavailable"))
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	record, err := fullStore.GetTransactionForUpdate(ctx, tx, msg.DispenseId)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if record == nil {
		return nil, connect.NewError(connect.CodeNotFound, ErrDispenseNotFound)
	}
	if record.KioskCode != kiosk.Subject {
		return nil, connect.NewError(connect.CodePermissionDenied, ErrDispenseWrongKiosk)
	}
	if staff.ProjectID != record.ProjectID || kiosk.ProjectID != record.ProjectID {
		return nil, connect.NewError(connect.CodePermissionDenied, ErrDispenseWrongKiosk)
	}
	pr, err := s.store.GetByID(ctx, record.PrescriptionRowID)
	if err != nil || pr == nil {
		return nil, connect.NewError(connect.CodeNotFound, ErrPrescriptionNotFound)
	}
	if !s.authorizeWard(staff, pr.WardID) {
		return nil, connect.NewError(connect.CodePermissionDenied, ErrUnauthorized)
	}
	var operatorName string
	if err := tx.QueryRow(ctx, `SELECT display_name FROM medisync.users WHERE id=$1 AND project_id=$2 AND active=true`, staff.Subject, staff.ProjectID).Scan(&operatorName); err != nil {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("operator is inactive or outside this project"))
	}
	event := transactionRequestedEvent(record)
	payload, err := protojson.Marshal(event)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if err := fullStore.QueueTransaction(ctx, tx, record, staff.Subject, operatorName, payload); err != nil {
		code := connect.CodeFailedPrecondition
		if errors.Is(err, ErrDispenseExpired) {
			code = connect.CodeDeadlineExceeded
		}
		return nil, connect.NewError(code, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	updated, err := fullStore.GetTransaction(ctx, record.ID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	s.writeAudit(ctx, audit.Entry{TraceID: record.TraceID, Actor: staff.Subject, Action: "dispense.identity_confirmed", Entity: "dispense_transaction", EntityID: record.ID, ProjectID: record.ProjectID, Detail: map[string]any{"kiosk_code": record.KioskCode, "operator": operatorName}})
	return connect.NewResponse(&dispensingv1.ConfirmDispenseResponse{Transaction: toProtoTransaction(updated)}), nil
}

func (s *DispensingServer) CancelDispense(ctx context.Context, req *connect.Request[dispensingv1.CancelDispenseRequest]) (*connect.Response[dispensingv1.CancelDispenseResponse], error) {
	kiosk, err := s.authenticate(req.Header())
	if err != nil {
		return nil, err
	}
	if kiosk.Role != "KIOSK" {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("kiosk authentication required"))
	}
	msg := req.Msg
	if msg == nil || msg.DispenseId == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("dispense_id is required"))
	}
	fullStore, ok := s.store.(*Store)
	if !ok {
		return nil, connect.NewError(connect.CodeInternal, errors.New("transaction store unavailable"))
	}
	record, err := fullStore.CancelTransaction(ctx, s.pool.Begin, msg.DispenseId, kiosk.Subject, msg.Reason)
	if err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, err)
	}
	s.writeAudit(ctx, audit.Entry{TraceID: record.TraceID, Actor: kiosk.Subject, Action: "dispense.cancelled", Entity: "dispense_transaction", EntityID: record.ID, ProjectID: record.ProjectID, Detail: map[string]any{"kiosk_code": record.KioskCode, "reason": msg.Reason}})
	return connect.NewResponse(&dispensingv1.CancelDispenseResponse{Transaction: toProtoTransaction(record)}), nil
}

func (s *DispensingServer) GetDispenseTransaction(ctx context.Context, req *connect.Request[dispensingv1.GetDispenseTransactionRequest]) (*connect.Response[dispensingv1.GetDispenseTransactionResponse], error) {
	claims, err := s.authenticate(req.Header())
	if err != nil {
		return nil, err
	}
	if req.Msg == nil || req.Msg.DispenseId == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("dispense_id is required"))
	}
	fullStore, ok := s.store.(*Store)
	if !ok {
		return nil, connect.NewError(connect.CodeInternal, errors.New("transaction store unavailable"))
	}
	record, err := fullStore.GetTransaction(ctx, req.Msg.DispenseId)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if record == nil {
		return nil, connect.NewError(connect.CodeNotFound, ErrDispenseNotFound)
	}
	if claims.ProjectID != record.ProjectID || (claims.Role == "KIOSK" && claims.Subject != record.KioskCode) {
		return nil, connect.NewError(connect.CodeNotFound, ErrDispenseNotFound)
	}
	return connect.NewResponse(&dispensingv1.GetDispenseTransactionResponse{Transaction: toProtoTransaction(record)}), nil
}

func (s *DispensingServer) ListDispenseTransactions(ctx context.Context, req *connect.Request[dispensingv1.ListDispenseTransactionsRequest]) (*connect.Response[dispensingv1.ListDispenseTransactionsResponse], error) {
	claims, err := s.authenticate(req.Header())
	if err != nil {
		return nil, err
	}
	msg := req.Msg
	filter := TransactionFilter{ProjectID: claims.ProjectID, PageSize: 50}
	if msg != nil {
		filter.KioskCode, filter.Prescription, filter.OperatorUserID = msg.KioskCode, msg.PrescriptionId, msg.OperatorUserId
		filter.SlotID, filter.DrugCode = msg.SlotId, msg.DrugCode
		filter.PageSize, filter.PageToken = msg.PageSize, msg.PageToken
		if msg.CreatedFrom != nil {
			value := msg.CreatedFrom.AsTime()
			filter.CreatedFrom = &value
		}
		if msg.CreatedTo != nil {
			value := msg.CreatedTo.AsTime()
			filter.CreatedTo = &value
		}
		for _, status := range msg.Statuses {
			filter.Statuses = append(filter.Statuses, protoTransactionStatus(status))
		}
	}
	if claims.Role == "KIOSK" {
		filter.KioskCode = claims.Subject
	}
	fullStore, ok := s.store.(*Store)
	if !ok {
		return nil, connect.NewError(connect.CodeInternal, errors.New("transaction store unavailable"))
	}
	records, next, total, err := fullStore.ListTransactions(ctx, filter)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	result := make([]*dispensingv1.DispenseTransaction, 0, len(records))
	for _, record := range records {
		result = append(result, toProtoTransaction(record))
	}
	return connect.NewResponse(&dispensingv1.ListDispenseTransactionsResponse{Transactions: result, NextPageToken: next, TotalCount: total}), nil
}

// findReadyByPrescriptionID looks up a prescription by its external ID.
// It queries the most recent one in READY state across source systems.
func (s *DispensingServer) findReadyByPrescriptionID(ctx context.Context, prescriptionID string) (*PrescriptionRow, error) {
	// Try a few common source systems. The kiosk doesn't know source_system,
	// so we search across known sources. For M2, we query the DB directly.
	// A more robust approach (M3+) would use a dedicated store method.
	query := `SELECT id, prescription_id, source_system, hn, patient_name, ward_id,
	                 items, state, failure_reason, issued_at, created_at, updated_at
	            FROM medisync.prescription
	           WHERE prescription_id = $1 AND state = 'READY'
	           ORDER BY created_at DESC LIMIT 1`

	row := s.pool.QueryRow(ctx, query, prescriptionID)
	return scanPrescription(row)
}

func (s *DispensingServer) findReadyByPrescriptionIDForProject(ctx context.Context, prescriptionID, projectID string) (*PrescriptionRow, error) {
	query := `SELECT id, prescription_id, source_system, hn, patient_name, ward_id,
	                 items, state, failure_reason, issued_at, created_at, updated_at
	            FROM medisync.prescription
	           WHERE prescription_id = $1 AND project_id = $2 AND state = 'READY'
	           ORDER BY created_at DESC LIMIT 1`
	return scanPrescription(s.pool.QueryRow(ctx, query, prescriptionID, projectID))
}

// authenticate extracts and validates the Bearer token from the request headers.
// Returns the token claims or a Connect error (Unauthenticated).
func (s *DispensingServer) authenticate(header interface{ Get(string) string }) (*TokenClaims, error) {
	tokenStr, err := extractBearer(header)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, err)
	}

	claims, err := s.parser.Parse(tokenStr)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, ErrUnauthorized)
	}

	return claims, nil
}

func (s *DispensingServer) authenticateNamed(header interface{ Get(string) string }, name string) (*TokenClaims, error) {
	auth := header.Get(name)
	if auth == "" {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("%s header is required", name))
	}
	parts := strings.SplitN(auth, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") || strings.TrimSpace(parts[1]) == "" {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("%s must use Bearer scheme", name))
	}
	claims, err := s.parser.Parse(strings.TrimSpace(parts[1]))
	if err != nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, ErrUnauthorized)
	}
	return claims, nil
}

// resolveWardScope returns the ward IDs the caller is authorized to see.
// ADMIN sees all requested wards (or all if none specified). Other roles
// are restricted to the wards in their JWT claims.
func (s *DispensingServer) resolveWardScope(claims *TokenClaims, msg *dispensingv1.ListPrescriptionsRequest) []string {
	if claims.Role == "ADMIN" || claims.Role == "KIOSK" {
		// Admins and kiosks can optionally filter by a specific ward.
		if msg != nil && msg.WardId != "" {
			return []string{msg.WardId}
		}
		// Empty means "all wards" — we return all wards from claims.
		// If the admin has no specific wards, they see everything
		// (no ward filter applied by ListByWard when wardID is empty).
		if len(claims.WardIDs) > 0 {
			return claims.WardIDs
		}
		// Admin with empty ward list: return a sentinel that matches everything.
		// ListByWard with empty ward_id would match nothing (WHERE ward_id = ''),
		// so we return a wildcard by using the admin's universal scope.
		// Practically this means querying with an empty ward set; the handler
		// will query without ward filter.
		return []string{""}
	}

	// Non-admin: strict ward scoping from JWT claims.
	return claims.WardIDs
}

// authorizeWard returns true if the claims allow access to the given ward.
// ADMIN can access any ward; other roles must have the ward in their WardIDs.
func (s *DispensingServer) authorizeWard(claims *TokenClaims, wardID string) bool {
	if claims.Role == "ADMIN" || claims.Role == "KIOSK" {
		return true
	}
	for _, w := range claims.WardIDs {
		if w == wardID {
			return true
		}
	}
	return false
}

// ── Emergency Dispensing ───────────────────────────────────────────

// ListEmergencyDrugs returns configured emergency stock for this kiosk only.
func (s *DispensingServer) ListEmergencyDrugs(ctx context.Context, req *connect.Request[dispensingv1.ListEmergencyDrugsRequest]) (*connect.Response[dispensingv1.ListEmergencyDrugsResponse], error) {
	claims, err := s.authenticate(req.Header())
	if err != nil {
		return nil, err
	}
	if claims.Role != "KIOSK" || claims.Subject == "" {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("kiosk authentication required"))
	}
	msg := req.Msg
	if msg != nil && strings.TrimSpace(msg.KioskCode) != "" && strings.TrimSpace(msg.KioskCode) != claims.Subject {
		return nil, connect.NewError(connect.CodePermissionDenied, ErrDispenseWrongKiosk)
	}
	pageSize := int32(50)
	pageToken := ""
	if msg != nil {
		pageSize = pagination.NormalizePageSize(msg.PageSize)
		pageToken = strings.TrimSpace(msg.PageToken)
	}
	var total int64
	if err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM medisync.slot
		  WHERE cabinet_id=$1 AND project_id=$2 AND emergency_drug=true AND is_active=true`,
		claims.Subject, claims.ProjectID).Scan(&total); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("count emergency drugs: %w", err))
	}
	rows, err := s.pool.Query(ctx,
		`SELECT s.code,s.drug_code,COALESCE(s.drug_name,''),COALESCE(s.drug_type,''),
		        GREATEST(0,s.quantity-s.reserved_quantity),
		        LEAST(s.emergency_max_quantity,GREATEST(0,s.quantity-s.reserved_quantity))
		   FROM medisync.slot s
		  WHERE s.cabinet_id=$1 AND s.project_id=$2 AND s.emergency_drug=true
		    AND s.is_active=true AND s.code > $3
		  ORDER BY s.code LIMIT $4`,
		claims.Subject, claims.ProjectID, pageToken, pageSize+1)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("query emergency: %w", err))
	}
	defer rows.Close()

	var drugs []*dispensingv1.EmergencyDrug
	for rows.Next() {
		d := &dispensingv1.EmergencyDrug{}
		if err := rows.Scan(&d.SlotCode, &d.DrugCode, &d.DrugName, &d.DrugType, &d.Quantity, &d.MaxDispense); err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("scan emergency drug: %w", err))
		}
		drugs = append(drugs, d)
	}
	if err := rows.Err(); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("iterate emergency drugs: %w", err))
	}
	next := ""
	if len(drugs) > int(pageSize) {
		next = drugs[pageSize-1].SlotCode
		drugs = drugs[:pageSize]
	}
	return connect.NewResponse(&dispensingv1.ListEmergencyDrugsResponse{Drugs: drugs, TotalCount: total, NextPageToken: next}), nil
}

// EmergencyDispense creates a separate, hardware-backed emergency transaction.
func (s *DispensingServer) EmergencyDispense(ctx context.Context, req *connect.Request[dispensingv1.EmergencyDispenseRequest]) (*connect.Response[dispensingv1.EmergencyDispenseResponse], error) {
	caller, err := s.authenticate(req.Header())
	if err != nil {
		return nil, err
	}
	msg := req.Msg
	if msg == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("emergency dispense request is required"))
	}

	kiosk := caller
	authMethod := EmergencyAuthEmployeeCode
	employeeCode := strings.ToUpper(strings.TrimSpace(msg.EmployeeCode))
	if caller.Role != "KIOSK" {
		kiosk, err = s.authenticateNamed(req.Header(), "X-Kiosk-Authorization")
		if err != nil {
			return nil, err
		}
		if kiosk.Role != "KIOSK" || kiosk.Subject == "" {
			return nil, connect.NewError(connect.CodePermissionDenied, errors.New("kiosk authentication required"))
		}
		if caller.Subject == "" || caller.ProjectID == "" || caller.ProjectID != kiosk.ProjectID {
			return nil, connect.NewError(connect.CodePermissionDenied, errors.New("card operator is outside this kiosk project"))
		}
		var cardEmployeeCode string
		if err := s.pool.QueryRow(ctx,
			`SELECT employee_code FROM medisync.users
			  WHERE id=$1 AND project_id=$2 AND active=true AND employee_code IS NOT NULL`,
			caller.Subject, caller.ProjectID).Scan(&cardEmployeeCode); err != nil {
			return nil, connect.NewError(connect.CodePermissionDenied, errors.New("card operator is inactive or has no employee code"))
		}
		cardEmployeeCode = strings.ToUpper(strings.TrimSpace(cardEmployeeCode))
		if employeeCode != "" && employeeCode != cardEmployeeCode {
			return nil, connect.NewError(connect.CodePermissionDenied, errors.New("card identity does not match employee_code"))
		}
		employeeCode = cardEmployeeCode
		authMethod = EmergencyAuthCard
	}
	if kiosk.Role != "KIOSK" || kiosk.Subject == "" {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("kiosk authentication required"))
	}
	if code := strings.TrimSpace(msg.KioskCode); code != "" && code != kiosk.Subject {
		return nil, connect.NewError(connect.CodePermissionDenied, ErrDispenseWrongKiosk)
	}
	hn := strings.TrimSpace(msg.Hn)
	slotCode := strings.TrimSpace(msg.SlotCode)
	drugCode := strings.TrimSpace(msg.DrugCode)
	reason := strings.TrimSpace(msg.Reason)
	if hn == "" || len(hn) > 64 {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("hn is required and must not exceed 64 characters"))
	}
	if employeeCode == "" || len(employeeCode) > 64 {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("employee_code is required and must not exceed 64 characters"))
	}
	if slotCode == "" || drugCode == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("slot_code and drug_code are required"))
	}
	if len(reason) > 500 {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("reason must not exceed 500 characters"))
	}
	traceID := strings.TrimSpace(msg.TraceId)
	if traceID == "" {
		traceID = uuid.New().String()
	}
	fullStore, ok := s.store.(*Store)
	if !ok {
		return nil, connect.NewError(connect.CodeInternal, errors.New("transaction store unavailable"))
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("begin emergency dispense: %w", err))
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	record, err := fullStore.CreateEmergencyTransaction(
		ctx, tx, kiosk.Subject, kiosk.ProjectID, hn, employeeCode,
		slotCode, drugCode, msg.Quantity, reason, traceID, authMethod,
	)
	if err != nil {
		code := connect.CodeFailedPrecondition
		if errors.Is(err, ErrEmergencyEmployeeNotFound) {
			code = connect.CodePermissionDenied
		} else if errors.Is(err, ErrDispenseWrongKiosk) {
			code = connect.CodePermissionDenied
		}
		return nil, connect.NewError(code, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("commit emergency dispense: %w", err))
	}
	s.writeAudit(ctx, audit.Entry{TraceID: record.TraceID, Actor: record.EmployeeCode, Action: "emergency_dispense.queued", Entity: "emergency_dispense_transaction", EntityID: record.ID, ProjectID: record.ProjectID, Detail: map[string]any{"kiosk_code": record.KioskCode, "hn": record.HN, "drug_code": record.DrugCode, "quantity": record.RequestedQuantity, "operator_auth_method": record.OperatorAuthMethod}})
	return connect.NewResponse(&dispensingv1.EmergencyDispenseResponse{
		DispenseId: record.ID, SlotCode: record.SlotCode, DrugName: record.DrugName,
		Quantity: record.RequestedQuantity, Status: string(record.Status),
		Transaction: toProtoEmergencyTransaction(record),
	}), nil
}

func (s *DispensingServer) GetEmergencyDispenseTransaction(ctx context.Context, req *connect.Request[dispensingv1.GetEmergencyDispenseTransactionRequest]) (*connect.Response[dispensingv1.GetEmergencyDispenseTransactionResponse], error) {
	claims, err := s.authenticate(req.Header())
	if err != nil {
		return nil, err
	}
	if req.Msg == nil || strings.TrimSpace(req.Msg.DispenseId) == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("dispense_id is required"))
	}
	fullStore, ok := s.store.(*Store)
	if !ok {
		return nil, connect.NewError(connect.CodeInternal, errors.New("transaction store unavailable"))
	}
	record, err := fullStore.GetEmergencyTransaction(ctx, strings.TrimSpace(req.Msg.DispenseId))
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if record == nil || record.ProjectID != claims.ProjectID || (claims.Role == "KIOSK" && record.KioskCode != claims.Subject) {
		return nil, connect.NewError(connect.CodeNotFound, ErrDispenseNotFound)
	}
	return connect.NewResponse(&dispensingv1.GetEmergencyDispenseTransactionResponse{Transaction: toProtoEmergencyTransaction(record)}), nil
}

func (s *DispensingServer) ListEmergencyDispenseTransactions(ctx context.Context, req *connect.Request[dispensingv1.ListEmergencyDispenseTransactionsRequest]) (*connect.Response[dispensingv1.ListEmergencyDispenseTransactionsResponse], error) {
	claims, err := s.authenticate(req.Header())
	if err != nil {
		return nil, err
	}
	filter := EmergencyTransactionFilter{ProjectID: claims.ProjectID, PageSize: 50}
	if msg := req.Msg; msg != nil {
		filter.KioskCode, filter.HN = strings.TrimSpace(msg.KioskCode), strings.TrimSpace(msg.Hn)
		filter.EmployeeCode, filter.DrugCode = strings.TrimSpace(msg.EmployeeCode), strings.TrimSpace(msg.DrugCode)
		filter.PageSize, filter.PageToken = msg.PageSize, strings.TrimSpace(msg.PageToken)
		if msg.CreatedFrom != nil {
			value := msg.CreatedFrom.AsTime()
			filter.CreatedFrom = &value
		}
		if msg.CreatedTo != nil {
			value := msg.CreatedTo.AsTime()
			filter.CreatedTo = &value
		}
		for _, status := range msg.Statuses {
			filter.Statuses = append(filter.Statuses, protoEmergencyStatus(status))
		}
		for _, method := range msg.OperatorAuthMethods {
			filter.AuthMethods = append(filter.AuthMethods, emergencyAuthMethodDomain(method))
		}
	}
	if claims.Role == "KIOSK" {
		filter.KioskCode = claims.Subject
	}
	fullStore, ok := s.store.(*Store)
	if !ok {
		return nil, connect.NewError(connect.CodeInternal, errors.New("transaction store unavailable"))
	}
	records, next, total, err := fullStore.ListEmergencyTransactions(ctx, filter)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	result := make([]*dispensingv1.EmergencyDispenseTransaction, 0, len(records))
	for _, record := range records {
		result = append(result, toProtoEmergencyTransaction(record))
	}
	return connect.NewResponse(&dispensingv1.ListEmergencyDispenseTransactionsResponse{Transactions: result, NextPageToken: next, TotalCount: total}), nil
}

// writeAudit records an audit entry. Audit failures are logged but do not
// cause the RPC to fail.
func (s *DispensingServer) writeAudit(ctx context.Context, e audit.Entry) {
	if s.audit == nil {
		return
	}
	_ = s.audit.Write(ctx, e)
}

// extractBearer parses "Authorization: Bearer ***". It returns an error
// when the header is missing, malformed, or uses a non-Bearer scheme.
// This is a copy of identity.extractBearer to avoid a package dependency.
func extractBearer(header interface{ Get(string) string }) (string, error) {
	auth := header.Get("Authorization")
	if auth == "" {
		return "", ErrAuthorizationRequired
	}
	parts := strings.SplitN(auth, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
		return "", ErrBearerSchemeRequired
	}
	token := strings.TrimSpace(parts[1])
	if token == "" {
		return "", ErrBearerTokenRequired
	}
	return token, nil
}

// toProtoPrescription converts a domain PrescriptionRow to a proto Prescription.
func toProtoPrescription(pr *PrescriptionRow) *dispensingv1.Prescription {
	if pr == nil {
		return nil
	}

	items := make([]*dispensingv1.PrescriptionItem, 0, len(pr.Items))
	for _, it := range pr.Items {
		items = append(items, &dispensingv1.PrescriptionItem{
			DrugCode:   it.DrugCode,
			DrugName:   it.DrugName,
			Quantity:   it.Quantity,
			DosageText: it.DosageText,
		})
	}

	var issuedAt *timestamppb.Timestamp
	if pr.IssuedAt != nil {
		issuedAt = timestamppb.New(*pr.IssuedAt)
	}

	return &dispensingv1.Prescription{
		Id:             pr.ID,
		PrescriptionId: pr.PrescriptionID,
		SourceSystem:   pr.SourceSystem,
		Hn:             pr.HN,
		PatientName:    pr.PatientName,
		WardId:         pr.WardID,
		Items:          items,
		State:          domainStateToProto(pr.State),
		FailureReason:  pr.FailureReason,
		IssuedAt:       issuedAt,
		CreatedAt:      timestamppb.New(pr.CreatedAt),
		UpdatedAt:      timestamppb.New(pr.UpdatedAt),
	}
}

func transactionRequestedEvent(record *TransactionRecord) *eventsv1.DispenseRequested {
	event := &eventsv1.DispenseRequested{
		DispenseId: record.ID, PrescriptionId: record.PrescriptionRef,
		TraceId: record.TraceID, KioskCode: record.KioskCode, ProjectId: record.ProjectID,
	}
	for _, item := range record.Items {
		for _, allocation := range item.Allocations {
			event.Allocations = append(event.Allocations, &eventsv1.DispenseAllocation{
				AllocationId: allocation.ID, ItemId: item.ID, DrugCode: item.DrugCode,
				SlotCode: allocation.SlotCode, BatchId: allocation.BatchID,
				Quantity: allocation.Quantity, DoorNo: allocation.DoorNo,
				HardwareLayer: allocation.HardwareLayer, ChannelStart: allocation.ChannelStart,
				ChannelEnd: allocation.ChannelEnd,
			})
		}
	}
	if len(event.Allocations) > 0 {
		event.SlotCode = event.Allocations[0].SlotCode
		for _, allocation := range event.Allocations {
			event.Quantity += allocation.Quantity
		}
	}
	return event
}

func toProtoTransaction(record *TransactionRecord) *dispensingv1.DispenseTransaction {
	if record == nil {
		return nil
	}
	result := &dispensingv1.DispenseTransaction{
		DispenseId: record.ID, PrescriptionId: record.PrescriptionRef,
		SourceSystem: record.SourceSystem, KioskCode: record.KioskCode,
		OperatorUserId: record.OperatorUserID, OperatorDisplayName: record.OperatorDisplayName,
		Status: transactionStatusProto(record.Status), TraceId: record.TraceID,
		FailureCode: record.FailureCode, FailureDetail: record.FailureDetail,
		StickerScannedAt: timestamppb.New(record.StickerScannedAt), ExpiresAt: timestamppb.New(record.ExpiresAt),
		CreatedAt: timestamppb.New(record.CreatedAt), UpdatedAt: timestamppb.New(record.UpdatedAt),
	}
	result.IdentityConfirmedAt = optionalTimestamp(record.IdentityConfirmedAt)
	result.QueuedAt = optionalTimestamp(record.QueuedAt)
	result.StartedAt = optionalTimestamp(record.StartedAt)
	result.CompletedAt = optionalTimestamp(record.CompletedAt)
	result.FailedAt = optionalTimestamp(record.FailedAt)
	result.CancelledAt = optionalTimestamp(record.CancelledAt)
	for _, item := range record.Items {
		pbItem := &dispensingv1.DispenseTransactionItem{
			Id: item.ID, SequenceNo: item.SequenceNo, DrugCode: item.DrugCode,
			DrugName: item.DrugName, RequestedQuantity: item.RequestedQuantity,
			AllocatedQuantity: item.AllocatedQuantity, DispensedQuantity: item.DispensedQuantity,
			Status: item.Status,
		}
		for _, allocation := range item.Allocations {
			pbItem.Allocations = append(pbItem.Allocations, &dispensingv1.DispenseAllocation{
				Id: allocation.ID, SlotId: allocation.SlotID, SlotCode: allocation.SlotCode, BatchId: allocation.BatchID,
				LotNumber: allocation.LotNumber, ExpiryDate: optionalTimestamp(allocation.ExpiryDate),
				Quantity: allocation.Quantity, DispensedQuantity: allocation.DispensedQuantity,
				DoorNo: allocation.DoorNo, HardwareLayer: allocation.HardwareLayer,
				ChannelStart: allocation.ChannelStart, ChannelEnd: allocation.ChannelEnd,
				Status: allocation.Status, HardwareAttemptedAt: optionalTimestamp(allocation.HardwareAttemptedAt),
				HardwareSuccess: allocation.HardwareSuccess, HardwareDetail: allocation.HardwareDetail,
				HardwareResponse: string(allocation.HardwareResponse),
			})
		}
		result.Items = append(result.Items, pbItem)
	}
	return result
}

func emergencyAuthMethodProto(method string) dispensingv1.EmergencyOperatorAuthMethod {
	switch method {
	case EmergencyAuthCard:
		return dispensingv1.EmergencyOperatorAuthMethod_EMERGENCY_OPERATOR_AUTH_METHOD_CARD
	case EmergencyAuthEmployeeCode:
		return dispensingv1.EmergencyOperatorAuthMethod_EMERGENCY_OPERATOR_AUTH_METHOD_EMPLOYEE_CODE
	default:
		return dispensingv1.EmergencyOperatorAuthMethod_EMERGENCY_OPERATOR_AUTH_METHOD_UNSPECIFIED
	}
}

func emergencyAuthMethodDomain(method dispensingv1.EmergencyOperatorAuthMethod) string {
	switch method {
	case dispensingv1.EmergencyOperatorAuthMethod_EMERGENCY_OPERATOR_AUTH_METHOD_CARD:
		return EmergencyAuthCard
	case dispensingv1.EmergencyOperatorAuthMethod_EMERGENCY_OPERATOR_AUTH_METHOD_EMPLOYEE_CODE:
		return EmergencyAuthEmployeeCode
	default:
		return ""
	}
}

func optionalTimestamp(value *time.Time) *timestamppb.Timestamp {
	if value == nil {
		return nil
	}
	return timestamppb.New(*value)
}

func transactionStatusProto(status TransactionStatus) dispensingv1.DispenseTransactionStatus {
	switch status {
	case TransactionAwaitingIdentity:
		return dispensingv1.DispenseTransactionStatus_DISPENSE_TRANSACTION_STATUS_AWAITING_IDENTITY
	case TransactionQueued:
		return dispensingv1.DispenseTransactionStatus_DISPENSE_TRANSACTION_STATUS_QUEUED
	case TransactionDispensing:
		return dispensingv1.DispenseTransactionStatus_DISPENSE_TRANSACTION_STATUS_DISPENSING
	case TransactionDispensed:
		return dispensingv1.DispenseTransactionStatus_DISPENSE_TRANSACTION_STATUS_DISPENSED
	case TransactionFailed:
		return dispensingv1.DispenseTransactionStatus_DISPENSE_TRANSACTION_STATUS_FAILED
	case TransactionCancelled:
		return dispensingv1.DispenseTransactionStatus_DISPENSE_TRANSACTION_STATUS_CANCELLED
	case TransactionExpired:
		return dispensingv1.DispenseTransactionStatus_DISPENSE_TRANSACTION_STATUS_EXPIRED
	default:
		return dispensingv1.DispenseTransactionStatus_DISPENSE_TRANSACTION_STATUS_UNSPECIFIED
	}
}

func protoTransactionStatus(status dispensingv1.DispenseTransactionStatus) TransactionStatus {
	switch status {
	case dispensingv1.DispenseTransactionStatus_DISPENSE_TRANSACTION_STATUS_AWAITING_IDENTITY:
		return TransactionAwaitingIdentity
	case dispensingv1.DispenseTransactionStatus_DISPENSE_TRANSACTION_STATUS_QUEUED:
		return TransactionQueued
	case dispensingv1.DispenseTransactionStatus_DISPENSE_TRANSACTION_STATUS_DISPENSING:
		return TransactionDispensing
	case dispensingv1.DispenseTransactionStatus_DISPENSE_TRANSACTION_STATUS_DISPENSED:
		return TransactionDispensed
	case dispensingv1.DispenseTransactionStatus_DISPENSE_TRANSACTION_STATUS_FAILED:
		return TransactionFailed
	case dispensingv1.DispenseTransactionStatus_DISPENSE_TRANSACTION_STATUS_CANCELLED:
		return TransactionCancelled
	case dispensingv1.DispenseTransactionStatus_DISPENSE_TRANSACTION_STATUS_EXPIRED:
		return TransactionExpired
	default:
		return ""
	}
}

func toProtoEmergencyTransaction(record *EmergencyTransactionRecord) *dispensingv1.EmergencyDispenseTransaction {
	if record == nil {
		return nil
	}
	return &dispensingv1.EmergencyDispenseTransaction{
		DispenseId: record.ID, KioskCode: record.KioskCode, ProjectId: record.ProjectID,
		Hn: record.HN, EmployeeCode: record.EmployeeCode,
		OperatorUserId: record.OperatorUserID, OperatorDisplayName: record.OperatorDisplayName,
		OperatorAuthMethod: emergencyAuthMethodProto(record.OperatorAuthMethod),
		SlotCode:           record.SlotCode, DrugCode: record.DrugCode, DrugName: record.DrugName,
		RequestedQuantity: record.RequestedQuantity, DispensedQuantity: record.DispensedQuantity,
		Status: emergencyStatusProto(record.Status), Reason: record.Reason,
		FailureCode: record.FailureCode, FailureDetail: record.FailureDetail,
		TraceId: record.TraceID, QueuedAt: timestamppb.New(record.QueuedAt),
		StartedAt: optionalTimestamp(record.StartedAt), CompletedAt: optionalTimestamp(record.CompletedAt),
		FailedAt: optionalTimestamp(record.FailedAt), CreatedAt: timestamppb.New(record.CreatedAt),
		UpdatedAt: timestamppb.New(record.UpdatedAt),
	}
}

func emergencyStatusProto(status TransactionStatus) dispensingv1.EmergencyDispenseStatus {
	switch status {
	case TransactionQueued:
		return dispensingv1.EmergencyDispenseStatus_EMERGENCY_DISPENSE_STATUS_QUEUED
	case TransactionDispensing:
		return dispensingv1.EmergencyDispenseStatus_EMERGENCY_DISPENSE_STATUS_DISPENSING
	case TransactionDispensed:
		return dispensingv1.EmergencyDispenseStatus_EMERGENCY_DISPENSE_STATUS_DISPENSED
	case TransactionFailed:
		return dispensingv1.EmergencyDispenseStatus_EMERGENCY_DISPENSE_STATUS_FAILED
	default:
		return dispensingv1.EmergencyDispenseStatus_EMERGENCY_DISPENSE_STATUS_UNSPECIFIED
	}
}

func protoEmergencyStatus(status dispensingv1.EmergencyDispenseStatus) TransactionStatus {
	switch status {
	case dispensingv1.EmergencyDispenseStatus_EMERGENCY_DISPENSE_STATUS_QUEUED:
		return TransactionQueued
	case dispensingv1.EmergencyDispenseStatus_EMERGENCY_DISPENSE_STATUS_DISPENSING:
		return TransactionDispensing
	case dispensingv1.EmergencyDispenseStatus_EMERGENCY_DISPENSE_STATUS_DISPENSED:
		return TransactionDispensed
	case dispensingv1.EmergencyDispenseStatus_EMERGENCY_DISPENSE_STATUS_FAILED:
		return TransactionFailed
	default:
		return ""
	}
}

func domainStateToProto(s State) dispensingv1.PrescriptionState {
	switch s {
	case StateReceived:
		return dispensingv1.PrescriptionState_PRESCRIPTION_STATE_RECEIVED
	case StateReady:
		return dispensingv1.PrescriptionState_PRESCRIPTION_STATE_READY
	case StateDispensing:
		return dispensingv1.PrescriptionState_PRESCRIPTION_STATE_DISPENSING
	case StateDispensed:
		return dispensingv1.PrescriptionState_PRESCRIPTION_STATE_DISPENSED
	case StateFailed:
		return dispensingv1.PrescriptionState_PRESCRIPTION_STATE_FAILED
	case StateCancelled:
		return dispensingv1.PrescriptionState_PRESCRIPTION_STATE_CANCELLED
	case StateExpired:
		return dispensingv1.PrescriptionState_PRESCRIPTION_STATE_EXPIRED
	default:
		return dispensingv1.PrescriptionState_PRESCRIPTION_STATE_UNSPECIFIED
	}
}

func protoStateToDomain(ps dispensingv1.PrescriptionState) State {
	switch ps {
	case dispensingv1.PrescriptionState_PRESCRIPTION_STATE_RECEIVED:
		return StateReceived
	case dispensingv1.PrescriptionState_PRESCRIPTION_STATE_READY:
		return StateReady
	case dispensingv1.PrescriptionState_PRESCRIPTION_STATE_DISPENSING:
		return StateDispensing
	case dispensingv1.PrescriptionState_PRESCRIPTION_STATE_DISPENSED:
		return StateDispensed
	case dispensingv1.PrescriptionState_PRESCRIPTION_STATE_FAILED:
		return StateFailed
	case dispensingv1.PrescriptionState_PRESCRIPTION_STATE_CANCELLED:
		return StateCancelled
	case dispensingv1.PrescriptionState_PRESCRIPTION_STATE_EXPIRED:
		return StateExpired
	default:
		return State("")
	}
}
