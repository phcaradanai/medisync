package dispensing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/encoding/protojson"

	eventsv1 "github.com/adm-chura3inter/medisync/services/core/internal/gen/medisync/events/v1"
)

const (
	EmergencyAuthCard         = "CARD"
	EmergencyAuthEmployeeCode = "EMPLOYEE_CODE"
)

type EmergencyTransactionRecord struct {
	ID                  string
	KioskCode           string
	ProjectID           string
	HN                  string
	EmployeeCode        string
	OperatorUserID      string
	OperatorDisplayName string
	OperatorAuthMethod  string
	SlotCode            string
	DrugCode            string
	DrugName            string
	RequestedQuantity   int32
	DispensedQuantity   int32
	Reason              string
	Status              TransactionStatus
	TraceID             string
	FailureCode         string
	FailureDetail       string
	HardwareRequest     json.RawMessage
	HardwareResponse    json.RawMessage
	QueuedAt            time.Time
	StartedAt           *time.Time
	CompletedAt         *time.Time
	FailedAt            *time.Time
	CreatedAt           time.Time
	UpdatedAt           time.Time
	Allocations         []*AllocationRecord
}

type EmergencyTransactionFilter struct {
	ProjectID    string
	KioskCode    string
	HN           string
	EmployeeCode string
	DrugCode     string
	Statuses     []TransactionStatus
	AuthMethods  []string
	CreatedFrom  *time.Time
	CreatedTo    *time.Time
	PageSize     int32
	PageToken    string
}

// CreateEmergencyTransaction validates the operator identity, reserves stock
// in one configured slot, and queues an immutable emergency transaction.
func (s *Store) CreateEmergencyTransaction(
	ctx context.Context,
	tx pgx.Tx,
	kioskCode, projectID, hn, employeeCode, slotCode, drugCode string,
	quantity int32,
	reason, traceID, authMethod string,
) (*EmergencyTransactionRecord, error) {
	if authMethod != EmergencyAuthCard && authMethod != EmergencyAuthEmployeeCode {
		return nil, fmt.Errorf("invalid emergency operator auth method %q", authMethod)
	}
	var kioskActive bool
	if err := tx.QueryRow(ctx,
		`SELECT active FROM medisync.kiosks WHERE code=$1 AND project_id=$2`,
		kioskCode, projectID).Scan(&kioskActive); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrDispenseWrongKiosk
		}
		return nil, fmt.Errorf("verify emergency kiosk: %w", err)
	}
	if !kioskActive {
		return nil, fmt.Errorf("kiosk %s is inactive", kioskCode)
	}

	employeeCode = strings.ToUpper(strings.TrimSpace(employeeCode))
	var operatorID, operatorName string
	if err := tx.QueryRow(ctx,
		`SELECT id, display_name FROM medisync.users
		  WHERE project_id=$1 AND upper(employee_code)=$2 AND active=true`,
		projectID, employeeCode).Scan(&operatorID, &operatorName); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrEmergencyEmployeeNotFound
		}
		return nil, fmt.Errorf("resolve emergency employee: %w", err)
	}

	var slotID, resolvedDrugName string
	var maximum int32
	var doorNo, layer, channelStart, channelEnd int32
	if err := tx.QueryRow(ctx,
		`SELECT id, drug_name, emergency_max_quantity,
		        door_no, hardware_layer, channel_start, channel_end
		   FROM medisync.slot
		  WHERE cabinet_id=$1 AND project_id=$2 AND code=$3 AND drug_code=$4
		    AND emergency_drug=true AND is_active=true
		  FOR UPDATE`,
		kioskCode, projectID, slotCode, drugCode).Scan(
		&slotID, &resolvedDrugName, &maximum,
		&doorNo, &layer, &channelStart, &channelEnd,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrEmergencyDrugNotConfigured
		}
		return nil, fmt.Errorf("lock emergency slot: %w", err)
	}
	if quantity <= 0 || quantity > maximum {
		return nil, fmt.Errorf("%w: maximum=%d", ErrEmergencyQuantityInvalid, maximum)
	}

	rows, err := tx.Query(ctx,
		`SELECT id, lot_number, expiry_date, quantity-reserved_quantity
		   FROM medisync.slot_batch
		  WHERE slot_id=$1 AND quantity > reserved_quantity
		  ORDER BY expiry_date ASC NULLS LAST, created_at ASC
		  FOR UPDATE`, slotID)
	if err != nil {
		return nil, fmt.Errorf("find emergency stock: %w", err)
	}
	type batchPlan struct {
		id, lot string
		expiry  *time.Time
		qty     int32
	}
	remaining := quantity
	var plans []batchPlan
	for rows.Next() && remaining > 0 {
		var plan batchPlan
		var available int32
		if err := rows.Scan(&plan.id, &plan.lot, &plan.expiry, &available); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan emergency stock: %w", err)
		}
		plan.qty = available
		if plan.qty > remaining {
			plan.qty = remaining
		}
		plans = append(plans, plan)
		remaining -= plan.qty
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate emergency stock: %w", err)
	}
	if remaining > 0 {
		return nil, ErrInsufficientStock
	}

	var emergencyID string
	if err := tx.QueryRow(ctx,
		`INSERT INTO medisync.emergency_dispense_transaction
		   (kiosk_code,project_id,hn,employee_code,operator_user_id,
		    operator_display_name,slot_code,drug_code,drug_name,
		    requested_quantity,reason,trace_id,operator_auth_method)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
		 RETURNING id`,
		kioskCode, projectID, hn, employeeCode, operatorID, operatorName,
		slotCode, drugCode, resolvedDrugName, quantity, reason, traceID, authMethod,
	).Scan(&emergencyID); err != nil {
		return nil, fmt.Errorf("insert emergency transaction: %w", err)
	}

	for _, plan := range plans {
		if _, err := tx.Exec(ctx,
			`INSERT INTO medisync.emergency_dispense_allocation
			   (emergency_dispense_id,slot_id,slot_code,batch_id,lot_number,
			    expiry_date,quantity,door_no,hardware_layer,channel_start,channel_end)
			 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
			emergencyID, slotID, slotCode, plan.id, plan.lot, plan.expiry,
			plan.qty, doorNo, layer, channelStart, channelEnd); err != nil {
			return nil, fmt.Errorf("insert emergency allocation: %w", err)
		}
		if tag, err := tx.Exec(ctx,
			`UPDATE medisync.slot_batch
			    SET reserved_quantity=reserved_quantity+$1, updated_at=now()
			  WHERE id=$2 AND quantity-reserved_quantity >= $1`, plan.qty, plan.id); err != nil {
			return nil, fmt.Errorf("reserve emergency batch: %w", err)
		} else if tag.RowsAffected() != 1 {
			return nil, ErrInsufficientStock
		}
		if tag, err := tx.Exec(ctx,
			`UPDATE medisync.slot
			    SET reserved_quantity=reserved_quantity+$1, updated_at=now()
			  WHERE id=$2 AND quantity-reserved_quantity >= $1`, plan.qty, slotID); err != nil {
			return nil, fmt.Errorf("reserve emergency slot: %w", err)
		} else if tag.RowsAffected() != 1 {
			return nil, ErrInsufficientStock
		}
	}

	record, err := getEmergencyTransaction(ctx, tx, emergencyID, false)
	if err != nil {
		return nil, err
	}
	event := emergencyRequestedEvent(record)
	payload, err := protojson.Marshal(event)
	if err != nil {
		return nil, fmt.Errorf("marshal emergency request: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`UPDATE medisync.emergency_dispense_transaction SET hardware_request=$2 WHERE id=$1`,
		emergencyID, payload); err != nil {
		return nil, fmt.Errorf("snapshot emergency hardware request: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO medisync.outbox (subject,payload,project_id,created_by)
		 VALUES ('medisync.dispense.requested',$1,$2,$3)`,
		payload, projectID, operatorID); err != nil {
		return nil, fmt.Errorf("queue emergency outbox: %w", err)
	}
	record.HardwareRequest = payload
	return record, nil
}

func emergencyRequestedEvent(record *EmergencyTransactionRecord) *eventsv1.DispenseRequested {
	event := &eventsv1.DispenseRequested{
		// The hardware API calls this field prescription_id, but emergency
		// withdrawals have no prescription. Use the emergency transaction ID as
		// the hardware order reference and never expose the patient's HN here.
		DispenseId: record.ID, PrescriptionId: "EMERGENCY:" + record.ID,
		TraceId: record.TraceID, KioskCode: record.KioskCode, ProjectId: record.ProjectID,
		SlotCode: record.SlotCode, Quantity: record.RequestedQuantity,
	}
	for _, allocation := range record.Allocations {
		event.Allocations = append(event.Allocations, &eventsv1.DispenseAllocation{
			AllocationId: allocation.ID, DrugCode: record.DrugCode,
			SlotCode: allocation.SlotCode, BatchId: allocation.BatchID,
			Quantity: allocation.Quantity, DoorNo: allocation.DoorNo,
			HardwareLayer: allocation.HardwareLayer, ChannelStart: allocation.ChannelStart,
			ChannelEnd: allocation.ChannelEnd,
		})
	}
	event.Allocations = orderHardwareAllocations(event.Allocations)
	return event
}

func (s *Store) GetEmergencyTransaction(ctx context.Context, dispenseID string) (*EmergencyTransactionRecord, error) {
	return getEmergencyTransaction(ctx, s.db, dispenseID, false)
}

func getEmergencyTransaction(ctx context.Context, db dbConn, dispenseID string, forUpdate bool) (*EmergencyTransactionRecord, error) {
	query := `SELECT id,kiosk_code,project_id,hn,employee_code,operator_user_id,
	                 operator_display_name,operator_auth_method,slot_code,drug_code,drug_name,
	                 requested_quantity,dispensed_quantity,reason,status,trace_id,
	                 failure_code,failure_detail,hardware_request,hardware_response,
	                 queued_at,started_at,completed_at,failed_at,created_at,updated_at
	            FROM medisync.emergency_dispense_transaction WHERE id=$1`
	if forUpdate {
		query += " FOR UPDATE"
	}
	var record EmergencyTransactionRecord
	var status string
	if err := db.QueryRow(ctx, query, dispenseID).Scan(
		&record.ID, &record.KioskCode, &record.ProjectID, &record.HN,
		&record.EmployeeCode, &record.OperatorUserID, &record.OperatorDisplayName,
		&record.OperatorAuthMethod,
		&record.SlotCode, &record.DrugCode, &record.DrugName,
		&record.RequestedQuantity, &record.DispensedQuantity, &record.Reason,
		&status, &record.TraceID, &record.FailureCode, &record.FailureDetail,
		&record.HardwareRequest, &record.HardwareResponse, &record.QueuedAt,
		&record.StartedAt, &record.CompletedAt, &record.FailedAt,
		&record.CreatedAt, &record.UpdatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get emergency transaction: %w", err)
	}
	record.Status = TransactionStatus(status)
	rows, err := db.Query(ctx,
		`SELECT id,slot_id,slot_code,batch_id,lot_number,expiry_date,quantity,
		        dispensed_quantity,door_no,hardware_layer,channel_start,channel_end,
		        status,hardware_attempted_at,COALESCE(hardware_success,false),
		        hardware_detail,hardware_response
		   FROM medisync.emergency_dispense_allocation
		  WHERE emergency_dispense_id=$1 ORDER BY created_at,id`, dispenseID)
	if err != nil {
		return nil, fmt.Errorf("list emergency allocations: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		allocation := &AllocationRecord{}
		if err := rows.Scan(
			&allocation.ID, &allocation.SlotID, &allocation.SlotCode,
			&allocation.BatchID, &allocation.LotNumber, &allocation.ExpiryDate,
			&allocation.Quantity, &allocation.DispensedQuantity, &allocation.DoorNo,
			&allocation.HardwareLayer, &allocation.ChannelStart, &allocation.ChannelEnd,
			&allocation.Status, &allocation.HardwareAttemptedAt, &allocation.HardwareSuccess,
			&allocation.HardwareDetail, &allocation.HardwareResponse,
		); err != nil {
			return nil, fmt.Errorf("scan emergency allocation: %w", err)
		}
		record.Allocations = append(record.Allocations, allocation)
	}
	return &record, rows.Err()
}

func (s *Store) FinalizeEmergencySuccess(ctx context.Context, tx pgx.Tx, dispenseID, kioskCode, hardwareResponse string, outcomes map[string]bool) (*EmergencyTransactionRecord, bool, error) {
	record, err := getEmergencyTransaction(ctx, tx, dispenseID, true)
	if err != nil || record == nil {
		return record, false, err
	}
	if record.KioskCode != kioskCode {
		return nil, false, ErrDispenseWrongKiosk
	}
	if record.Status == TransactionDispensed {
		return record, false, nil
	}
	if record.Status != TransactionQueued && record.Status != TransactionDispensing {
		return nil, false, fmt.Errorf("cannot complete emergency transaction in status %s", record.Status)
	}
	for _, allocation := range record.Allocations {
		if !outcomes[allocation.ID] {
			return nil, false, fmt.Errorf("emergency allocation %s lacks hardware success", allocation.ID)
		}
		if err := consumeEmergencyAllocation(ctx, tx, allocation); err != nil {
			return nil, false, err
		}
	}
	if _, err := tx.Exec(ctx,
		`UPDATE medisync.emergency_dispense_transaction
		    SET status='DISPENSED',dispensed_quantity=requested_quantity,
		        hardware_response=$2,completed_at=now(),updated_at=now()
		  WHERE id=$1`, dispenseID, []byte(hardwareResponse)); err != nil {
		return nil, false, fmt.Errorf("complete emergency transaction: %w", err)
	}
	updated, err := getEmergencyTransaction(ctx, tx, dispenseID, false)
	return updated, true, err
}

func (s *Store) FinalizeEmergencyFailure(ctx context.Context, tx pgx.Tx, dispenseID, kioskCode, reason, detail string, outcomes map[string]bool) (*EmergencyTransactionRecord, bool, error) {
	record, err := getEmergencyTransaction(ctx, tx, dispenseID, true)
	if err != nil || record == nil {
		return record, false, err
	}
	if record.KioskCode != kioskCode {
		return nil, false, ErrDispenseWrongKiosk
	}
	if record.Status == TransactionFailed {
		return record, false, nil
	}
	if record.Status != TransactionQueued && record.Status != TransactionDispensing {
		return nil, false, fmt.Errorf("cannot fail emergency transaction in status %s", record.Status)
	}
	var dispensed int32
	for _, allocation := range record.Allocations {
		if outcomes[allocation.ID] {
			if err := consumeEmergencyAllocation(ctx, tx, allocation); err != nil {
				return nil, false, err
			}
			dispensed += allocation.Quantity
			continue
		}
		if err := releaseEmergencyAllocation(ctx, tx, allocation, "FAILED"); err != nil {
			return nil, false, err
		}
	}
	if _, err := tx.Exec(ctx,
		`UPDATE medisync.emergency_dispense_transaction
		    SET status='FAILED',dispensed_quantity=$2,failure_code=$3,
		        failure_detail=$4,failed_at=now(),updated_at=now()
		  WHERE id=$1`, dispenseID, dispensed, reason, detail); err != nil {
		return nil, false, fmt.Errorf("fail emergency transaction: %w", err)
	}
	updated, err := getEmergencyTransaction(ctx, tx, dispenseID, false)
	return updated, true, err
}

func consumeEmergencyAllocation(ctx context.Context, tx pgx.Tx, allocation *AllocationRecord) error {
	if tag, err := tx.Exec(ctx,
		`UPDATE medisync.slot_batch SET quantity=quantity-$1,
		        reserved_quantity=reserved_quantity-$1,updated_at=now()
		  WHERE id=$2 AND quantity >= $1 AND reserved_quantity >= $1`,
		allocation.Quantity, allocation.BatchID); err != nil {
		return err
	} else if tag.RowsAffected() != 1 {
		return fmt.Errorf("consume emergency batch %s: reservation changed", allocation.BatchID)
	}
	if tag, err := tx.Exec(ctx,
		`UPDATE medisync.slot SET quantity=quantity-$1,
		        reserved_quantity=reserved_quantity-$1,updated_at=now()
		  WHERE id=$2 AND quantity >= $1 AND reserved_quantity >= $1`,
		allocation.Quantity, allocation.SlotID); err != nil {
		return err
	} else if tag.RowsAffected() != 1 {
		return fmt.Errorf("consume emergency slot %s: reservation changed", allocation.SlotCode)
	}
	_, err := tx.Exec(ctx,
		`UPDATE medisync.emergency_dispense_allocation
		    SET status='DISPENSED',dispensed_quantity=quantity,updated_at=now()
		  WHERE id=$1`, allocation.ID)
	return err
}

func releaseEmergencyAllocation(ctx context.Context, tx pgx.Tx, allocation *AllocationRecord, status string) error {
	if _, err := tx.Exec(ctx,
		`UPDATE medisync.slot_batch SET reserved_quantity=GREATEST(0,reserved_quantity-$1),updated_at=now() WHERE id=$2`,
		allocation.Quantity, allocation.BatchID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE medisync.slot SET reserved_quantity=GREATEST(0,reserved_quantity-$1),updated_at=now() WHERE id=$2`,
		allocation.Quantity, allocation.SlotID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx,
		`UPDATE medisync.emergency_dispense_allocation SET status=$2,updated_at=now() WHERE id=$1`,
		allocation.ID, status)
	return err
}

func (s *Store) ListEmergencyTransactions(ctx context.Context, filter EmergencyTransactionFilter) ([]*EmergencyTransactionRecord, string, int64, error) {
	pageSize := filter.PageSize
	if pageSize <= 0 {
		pageSize = 50
	}
	if pageSize > 200 {
		pageSize = 200
	}
	clauses := []string{"1=1"}
	args := []any{}
	add := func(clause string, value any) {
		args = append(args, value)
		clauses = append(clauses, fmt.Sprintf(clause, len(args)))
	}
	if filter.ProjectID != "" {
		add("project_id=$%d", filter.ProjectID)
	}
	if filter.KioskCode != "" {
		add("kiosk_code=$%d", filter.KioskCode)
	}
	if filter.HN != "" {
		add("hn=$%d", filter.HN)
	}
	if filter.EmployeeCode != "" {
		add("upper(employee_code)=upper($%d)", filter.EmployeeCode)
	}
	if filter.DrugCode != "" {
		add("drug_code=$%d", filter.DrugCode)
	}
	if len(filter.Statuses) > 0 {
		values := make([]string, 0, len(filter.Statuses))
		for _, status := range filter.Statuses {
			values = append(values, string(status))
		}
		add("status=ANY($%d)", values)
	}
	if len(filter.AuthMethods) > 0 {
		add("operator_auth_method=ANY($%d)", filter.AuthMethods)
	}
	if filter.CreatedFrom != nil {
		add("created_at >= $%d", *filter.CreatedFrom)
	}
	if filter.CreatedTo != nil {
		add("created_at < $%d", *filter.CreatedTo)
	}
	where := " WHERE " + joinClauses(clauses)
	var total int64
	if err := s.db.QueryRow(ctx,
		`SELECT count(*) FROM medisync.emergency_dispense_transaction`+where,
		args...).Scan(&total); err != nil {
		return nil, "", 0, fmt.Errorf("count emergency transactions: %w", err)
	}
	if filter.PageToken != "" {
		add("created_at < (SELECT created_at FROM medisync.emergency_dispense_transaction WHERE id=$%d)", filter.PageToken)
		where = " WHERE " + joinClauses(clauses)
	}
	args = append(args, pageSize+1)
	rows, err := s.db.Query(ctx,
		fmt.Sprintf(`SELECT id FROM medisync.emergency_dispense_transaction%s ORDER BY created_at DESC,id DESC LIMIT $%d`, where, len(args)),
		args...)
	if err != nil {
		return nil, "", 0, fmt.Errorf("list emergency transaction ids: %w", err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, "", 0, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, "", 0, err
	}
	next := ""
	if len(ids) > int(pageSize) {
		next = ids[pageSize-1]
		ids = ids[:pageSize]
	}
	records := make([]*EmergencyTransactionRecord, 0, len(ids))
	for _, id := range ids {
		record, err := s.GetEmergencyTransaction(ctx, id)
		if err != nil {
			return nil, "", 0, err
		}
		if record != nil {
			records = append(records, record)
		}
	}
	return records, next, total, nil
}
