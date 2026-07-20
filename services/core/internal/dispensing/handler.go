package dispensing

import (
	"context"
	"errors"
	"fmt"
	"strings"

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

// Dispense transitions a READY prescription to DISPENSING and writes the
// medisync.dispense.requested outbox event in the same transaction.
// Authorization is enforced on the prescription's ward.
func (s *DispensingServer) Dispense(
	ctx context.Context,
	req *connect.Request[dispensingv1.DispenseRequest],
) (*connect.Response[dispensingv1.DispenseResponse], error) {
	claims, err := s.authenticate(req.Header())
	if err != nil {
		return nil, err
	}

	msg := req.Msg
	if msg == nil || msg.PrescriptionId == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, ErrPrescriptionIDRequired)
	}

	// Fetch the prescription by external ID. Source system is inferred from
	// stored data (the dispense endpoint is called by kiosk, not the feeder).
	// We look up by prescription_id; if multiple source systems, we take the
	// most recent READY one. For M2, the common case is a single source.
	pr, err := s.findReadyByPrescriptionID(ctx, msg.PrescriptionId)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("find prescription: %w", err))
	}
	if pr == nil {
		return nil, connect.NewError(connect.CodeNotFound, ErrPrescriptionNotFound)
	}

	if !s.authorizeWard(claims, pr.WardID) {
		return nil, connect.NewError(connect.CodeNotFound, ErrPrescriptionNotFound)
	}

	if pr.State != StateReady {
		return nil, connect.NewError(connect.CodeFailedPrecondition,
			fmt.Errorf("prescription is in state %s, must be READY to dispense", pr.State))
	}

	// Build the outbox payload.
	traceID := msg.TraceId
	if traceID == "" {
		traceID = uuid.New().String()
	}
	dispenseID := uuid.New().String()

	outboxEvent := &eventsv1.DispenseRequested{
		DispenseId:     dispenseID,
		PrescriptionId: pr.PrescriptionID,
		TraceId:        traceID,
	}
	outboxPayload, err := protojson.Marshal(outboxEvent)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("marshal outbox event: %w", err))
	}

	// Transition state with outbox in a single transaction.
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("begin tx: %w", err))
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// We need the full store (not the narrow DispensingStore) for TransitionState.
	fullStore, ok := s.store.(*Store)
	if !ok {
		return nil, connect.NewError(connect.CodeInternal, errors.New("store does not support TransitionState"))
	}

	updated, err := fullStore.TransitionState(ctx, tx, pr.ID, StateReady, StateDispensing, outboxPayload)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("transition state: %w", err))
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("commit tx: %w", err))
	}

	// Write audit entry.
	s.writeAudit(ctx, audit.Entry{
		TraceID:  traceID,
		Actor:    claims.Subject,
		Action:   "prescription.dispense.requested",
		Entity:   "prescription",
		EntityID: pr.PrescriptionID,
		Detail: map[string]any{
			"dispense_id": dispenseID,
			"ward_id":     pr.WardID,
			"from_state":  string(StateReady),
			"to_state":    string(StateDispensing),
		},
	})

	return connect.NewResponse(&dispensingv1.DispenseResponse{
		Prescription: toProtoPrescription(updated),
	}), nil
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

// ListEmergencyDrugs returns drugs marked as emergency-accessible.
func (s *DispensingServer) ListEmergencyDrugs(ctx context.Context, req *connect.Request[dispensingv1.ListEmergencyDrugsRequest]) (*connect.Response[dispensingv1.ListEmergencyDrugsResponse], error) {
	rows, err := s.pool.Query(ctx,
		`SELECT s.id, s.code, s.drug_code, COALESCE(s.drug_name,''), COALESCE(s.drug_type,''), s.quantity, s.capacity
		   FROM medisync.slot s WHERE s.emergency_drug = TRUE AND s.quantity > 0 ORDER BY s.code`)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("query emergency: %w", err))
	}
	defer rows.Close()

	var drugs []*dispensingv1.EmergencyDrug
	for rows.Next() {
		d := &dispensingv1.EmergencyDrug{}
		if err := rows.Scan(&d.SlotId, &d.SlotCode, &d.DrugCode, &d.DrugName, &d.DrugType, &d.Quantity, &d.MaxDispense); err != nil {
			continue
		}
		drugs = append(drugs, d)
	}
	return connect.NewResponse(&dispensingv1.ListEmergencyDrugsResponse{Drugs: drugs, TotalCount: int64(len(drugs))}), nil
}

// EmergencyDispense performs sticker-less dispensing after card verification.
func (s *DispensingServer) EmergencyDispense(ctx context.Context, req *connect.Request[dispensingv1.EmergencyDispenseRequest]) (*connect.Response[dispensingv1.EmergencyDispenseResponse], error) {
	msg := req.Msg

	// Verify card token against user
	var userID string
	err := s.pool.QueryRow(ctx,
		`SELECT id FROM medisync.users WHERE card_token_hash = crypt($1, card_token_hash) AND emergency_access = TRUE`, msg.CardToken).Scan(&userID)
	if err != nil {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("invalid card or no emergency access"))
	}

	// Verify slot exists and is emergency-accessible
	var slotCode, drugName string
	err = s.pool.QueryRow(ctx,
		`SELECT code, drug_name FROM medisync.slot WHERE id = $1 AND emergency_drug = TRUE AND quantity >= $2`,
		msg.SlotId, msg.Quantity).Scan(&slotCode, &drugName)
	if err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("slot not available for emergency dispense"))
	}

	// Decrement stock
	tag, err := s.pool.Exec(ctx,
		`UPDATE medisync.slot SET quantity = quantity - $1, updated_at = now() WHERE id = $2 AND quantity >= $1`,
		msg.Quantity, msg.SlotId)
	if err != nil || tag.RowsAffected() == 0 {
		return nil, connect.NewError(connect.CodeInternal, errors.New("stock decrement failed"))
	}

	// Log emergency
	s.pool.Exec(ctx,
		`INSERT INTO medisync.emergency_log (user_id, slot_id, drug_code, quantity, reason, kiosk_id) VALUES ($1,$2,$3,$4,$5,$6)`,
		userID, msg.SlotId, msg.DrugCode, msg.Quantity, msg.Reason, msg.KioskId)

	// Audit
	s.writeAudit(ctx, audit.Entry{Actor: userID, Action: "emergency.dispense", Entity: "slot", EntityID: msg.SlotId})

	return connect.NewResponse(&dispensingv1.EmergencyDispenseResponse{
		DispenseId: userID[:8], SlotCode: slotCode, DrugName: drugName,
		Quantity: msg.Quantity, Status: "DISPENSED",
	}), nil
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
