package dispensing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	eventsv1 "github.com/adm-chura3inter/medisync/services/core/internal/gen/medisync/events/v1"
)

type TransactionStatus string

const (
	TransactionAwaitingIdentity TransactionStatus = "AWAITING_IDENTITY"
	TransactionQueued           TransactionStatus = "QUEUED"
	TransactionDispensing       TransactionStatus = "DISPENSING"
	TransactionDispensed        TransactionStatus = "DISPENSED"
	TransactionFailed           TransactionStatus = "FAILED"
	TransactionCancelled        TransactionStatus = "CANCELLED"
	TransactionExpired          TransactionStatus = "EXPIRED"
)

var (
	ErrDispenseNotFound           = errors.New("dispense transaction not found")
	ErrDispenseWrongKiosk         = errors.New("dispense transaction belongs to another kiosk")
	ErrDispenseNotAwaitingID      = errors.New("dispense transaction is not awaiting identity")
	ErrDispenseExpired            = errors.New("dispense transaction expired")
	ErrInsufficientStock          = errors.New("insufficient stock in this kiosk")
	ErrEmergencyEmployeeNotFound  = errors.New("employee code is inactive or outside this project")
	ErrEmergencyDrugNotConfigured = errors.New("drug is not configured for emergency dispense at this kiosk")
	ErrEmergencyQuantityInvalid   = errors.New("emergency quantity is outside the configured limit")
)

type TransactionRecord struct {
	ID                  string
	PrescriptionRowID   string
	PrescriptionRef     string
	SourceSystem        string
	KioskCode           string
	ProjectID           string
	OperatorUserID      string
	OperatorDisplayName string
	Status              TransactionStatus
	TraceID             string
	FailureCode         string
	FailureDetail       string
	HardwareRequest     json.RawMessage
	HardwareResponse    json.RawMessage
	StickerScannedAt    time.Time
	IdentityConfirmedAt *time.Time
	QueuedAt            *time.Time
	StartedAt           *time.Time
	CompletedAt         *time.Time
	FailedAt            *time.Time
	CancelledAt         *time.Time
	ExpiresAt           time.Time
	CreatedAt           time.Time
	UpdatedAt           time.Time
	Items               []*TransactionItemRecord
}

type TransactionItemRecord struct {
	ID                string
	SequenceNo        int32
	DrugCode          string
	DrugName          string
	RequestedQuantity int32
	AllocatedQuantity int32
	DispensedQuantity int32
	Status            string
	Allocations       []*AllocationRecord
}

type AllocationRecord struct {
	ID                  string
	ItemID              string
	SlotID              string
	SlotCode            string
	BatchID             string
	LotNumber           string
	ExpiryDate          *time.Time
	Quantity            int32
	DispensedQuantity   int32
	DoorNo              int32
	HardwareLayer       int32
	ChannelStart        int32
	ChannelEnd          int32
	Status              string
	HardwareAttemptedAt *time.Time
	HardwareSuccess     bool
	HardwareDetail      string
	HardwareResponse    json.RawMessage
}

type allocationCandidate struct {
	BatchID      string
	SlotID       string
	SlotCode     string
	LotNumber    string
	ExpiryDate   *time.Time
	Available    int32
	DoorNo       int32
	Layer        int32
	ChannelStart int32
	ChannelEnd   int32
}

type plannedItem struct {
	PrescriptionItem Item
	Candidates       []plannedAllocation
}

type plannedAllocation struct {
	allocationCandidate
	Quantity int32
}

// PrepareTransaction reserves FIFO batches from exactly one immutable kiosk
// code and records the sticker scan. The caller owns the database transaction.
func (s *Store) PrepareTransaction(
	ctx context.Context,
	tx pgx.Tx,
	pr *PrescriptionRow,
	kioskCode, projectID, traceID string,
) (*TransactionRecord, error) {
	if pr == nil {
		return nil, ErrPrescriptionNotFound
	}

	var kioskActive bool
	if err := tx.QueryRow(ctx,
		`SELECT active FROM medisync.kiosks WHERE code = $1 AND project_id = $2`,
		kioskCode, projectID).Scan(&kioskActive); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrDispenseWrongKiosk
		}
		return nil, fmt.Errorf("verify kiosk: %w", err)
	}
	if !kioskActive {
		return nil, fmt.Errorf("kiosk %s is inactive", kioskCode)
	}

	var prescriptionState string
	if err := tx.QueryRow(ctx,
		`SELECT state FROM medisync.prescription
		  WHERE id = $1 AND project_id = $2 FOR UPDATE`, pr.ID, projectID).Scan(&prescriptionState); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrPrescriptionNotFound
		}
		return nil, fmt.Errorf("lock prescription: %w", err)
	}
	if State(prescriptionState) != StateReady {
		return nil, fmt.Errorf("prescription must be READY, got %s", prescriptionState)
	}

	plans := make([]plannedItem, 0, len(pr.Items))
	for _, item := range pr.Items {
		if item.DrugCode == "" || item.Quantity <= 0 {
			return nil, fmt.Errorf("invalid prescription item %q", item.DrugCode)
		}
		rows, err := tx.Query(ctx,
			`SELECT b.id, s.id, s.code, b.lot_number, b.expiry_date,
			        (b.quantity - b.reserved_quantity) AS available,
			        s.door_no, s.hardware_layer, s.channel_start, s.channel_end
			   FROM medisync.slot s
			   JOIN medisync.slot_batch b ON b.slot_id = s.id
			  WHERE s.cabinet_id = $1
			    AND s.project_id = $2
			    AND s.drug_code = $3
			    AND s.is_active = true
			    AND b.quantity > b.reserved_quantity
			  ORDER BY b.expiry_date ASC NULLS LAST, b.created_at ASC, s.code ASC
			  FOR UPDATE OF b, s`, kioskCode, projectID, item.DrugCode)
		if err != nil {
			return nil, fmt.Errorf("find stock for %s: %w", item.DrugCode, err)
		}

		remaining := item.Quantity
		plan := plannedItem{PrescriptionItem: item}
		for rows.Next() && remaining > 0 {
			var candidate allocationCandidate
			if err := rows.Scan(
				&candidate.BatchID, &candidate.SlotID, &candidate.SlotCode,
				&candidate.LotNumber, &candidate.ExpiryDate, &candidate.Available,
				&candidate.DoorNo, &candidate.Layer, &candidate.ChannelStart, &candidate.ChannelEnd,
			); err != nil {
				rows.Close()
				return nil, fmt.Errorf("scan stock candidate: %w", err)
			}
			take := candidate.Available
			if take > remaining {
				take = remaining
			}
			plan.Candidates = append(plan.Candidates, plannedAllocation{allocationCandidate: candidate, Quantity: take})
			remaining -= take
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("iterate stock for %s: %w", item.DrugCode, err)
		}
		if remaining > 0 {
			return nil, fmt.Errorf("%w: kiosk=%s drug=%s requested=%d missing=%d",
				ErrInsufficientStock, kioskCode, item.DrugCode, item.Quantity, remaining)
		}
		plans = append(plans, plan)
	}

	var dispenseID string
	err := tx.QueryRow(ctx,
		`INSERT INTO medisync.dispense_transaction
		   (prescription_id, prescription_ref, source_system, kiosk_code, project_id, trace_id)
		 VALUES ($1,$2,$3,$4,$5,$6)
		 RETURNING id`,
		pr.ID, pr.PrescriptionID, pr.SourceSystem, kioskCode, projectID, traceID).Scan(&dispenseID)
	if err != nil {
		return nil, fmt.Errorf("insert dispense transaction: %w", err)
	}

	for itemIndex, plan := range plans {
		var itemID string
		err := tx.QueryRow(ctx,
			`INSERT INTO medisync.dispense_transaction_item
			   (dispense_id, sequence_no, drug_code, drug_name, requested_quantity, allocated_quantity)
			 VALUES ($1,$2,$3,$4,$5,$5) RETURNING id`,
			dispenseID, itemIndex+1, plan.PrescriptionItem.DrugCode,
			plan.PrescriptionItem.DrugName, plan.PrescriptionItem.Quantity).Scan(&itemID)
		if err != nil {
			return nil, fmt.Errorf("insert dispense item: %w", err)
		}

		for _, allocation := range plan.Candidates {
			if _, err := tx.Exec(ctx,
				`INSERT INTO medisync.dispense_allocation
				   (dispense_id, item_id, slot_id, slot_code, batch_id, lot_number, expiry_date,
				    quantity, door_no, hardware_layer, channel_start, channel_end)
				 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
				dispenseID, itemID, allocation.SlotID, allocation.SlotCode,
				allocation.BatchID, allocation.LotNumber, allocation.ExpiryDate,
				allocation.Quantity, allocation.DoorNo, allocation.Layer,
				allocation.ChannelStart, allocation.ChannelEnd); err != nil {
				return nil, fmt.Errorf("insert dispense allocation: %w", err)
			}
			tag, err := tx.Exec(ctx,
				`UPDATE medisync.slot_batch
				    SET reserved_quantity = reserved_quantity + $1, updated_at = now()
				  WHERE id = $2 AND quantity - reserved_quantity >= $1`,
				allocation.Quantity, allocation.BatchID)
			if err != nil {
				return nil, fmt.Errorf("reserve batch %s: %w", allocation.BatchID, err)
			}
			if tag.RowsAffected() != 1 {
				return nil, fmt.Errorf("reserve batch %s: stock changed concurrently", allocation.BatchID)
			}
			tag, err = tx.Exec(ctx,
				`UPDATE medisync.slot
				    SET reserved_quantity = reserved_quantity + $1, updated_at = now()
				  WHERE id = $2 AND quantity - reserved_quantity >= $1`,
				allocation.Quantity, allocation.SlotID)
			if err != nil {
				return nil, fmt.Errorf("reserve slot %s: %w", allocation.SlotCode, err)
			}
			if tag.RowsAffected() != 1 {
				return nil, fmt.Errorf("reserve slot %s: stock changed concurrently", allocation.SlotCode)
			}
		}
	}

	return getTransaction(ctx, tx, dispenseID, false)
}

func (s *Store) GetTransaction(ctx context.Context, dispenseID string) (*TransactionRecord, error) {
	return getTransaction(ctx, s.db, dispenseID, false)
}

func (s *Store) GetTransactionForUpdate(ctx context.Context, tx pgx.Tx, dispenseID string) (*TransactionRecord, error) {
	return getTransaction(ctx, tx, dispenseID, true)
}

func getTransaction(ctx context.Context, db dbConn, dispenseID string, forUpdate bool) (*TransactionRecord, error) {
	query := `SELECT id, prescription_id, prescription_ref, source_system, kiosk_code,
	                 COALESCE(project_id::text,''), COALESCE(operator_user_id::text,''),
	                 operator_display_name, status, trace_id, failure_code, failure_detail,
	                 hardware_request, hardware_response, sticker_scanned_at,
	                 identity_confirmed_at, queued_at, started_at, completed_at, failed_at,
	                 cancelled_at, expires_at, created_at, updated_at
	            FROM medisync.dispense_transaction WHERE id = $1`
	if forUpdate {
		query += " FOR UPDATE"
	}
	var record TransactionRecord
	var status string
	err := db.QueryRow(ctx, query, dispenseID).Scan(
		&record.ID, &record.PrescriptionRowID, &record.PrescriptionRef, &record.SourceSystem,
		&record.KioskCode, &record.ProjectID, &record.OperatorUserID,
		&record.OperatorDisplayName, &status, &record.TraceID, &record.FailureCode,
		&record.FailureDetail, &record.HardwareRequest, &record.HardwareResponse,
		&record.StickerScannedAt, &record.IdentityConfirmedAt, &record.QueuedAt,
		&record.StartedAt, &record.CompletedAt, &record.FailedAt, &record.CancelledAt,
		&record.ExpiresAt, &record.CreatedAt, &record.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get dispense transaction: %w", err)
	}
	record.Status = TransactionStatus(status)

	itemRows, err := db.Query(ctx,
		`SELECT id, sequence_no, drug_code, drug_name, requested_quantity,
		        allocated_quantity, dispensed_quantity, status
		   FROM medisync.dispense_transaction_item
		  WHERE dispense_id = $1 ORDER BY sequence_no`, dispenseID)
	if err != nil {
		return nil, fmt.Errorf("list dispense items: %w", err)
	}
	defer itemRows.Close()
	itemByID := make(map[string]*TransactionItemRecord)
	for itemRows.Next() {
		item := &TransactionItemRecord{}
		if err := itemRows.Scan(&item.ID, &item.SequenceNo, &item.DrugCode, &item.DrugName,
			&item.RequestedQuantity, &item.AllocatedQuantity, &item.DispensedQuantity,
			&item.Status); err != nil {
			return nil, fmt.Errorf("scan dispense item: %w", err)
		}
		record.Items = append(record.Items, item)
		itemByID[item.ID] = item
	}
	if err := itemRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate dispense items: %w", err)
	}

	allocationRows, err := db.Query(ctx,
		`SELECT id, item_id, slot_id, slot_code, batch_id, lot_number, expiry_date,
		        quantity, dispensed_quantity, door_no, hardware_layer,
		        channel_start, channel_end, status, hardware_attempted_at,
		        COALESCE(hardware_success,false), hardware_detail, hardware_response
		   FROM medisync.dispense_allocation
		  WHERE dispense_id = $1 ORDER BY created_at, id`, dispenseID)
	if err != nil {
		return nil, fmt.Errorf("list dispense allocations: %w", err)
	}
	defer allocationRows.Close()
	for allocationRows.Next() {
		allocation := &AllocationRecord{}
		if err := allocationRows.Scan(
			&allocation.ID, &allocation.ItemID, &allocation.SlotID, &allocation.SlotCode,
			&allocation.BatchID, &allocation.LotNumber, &allocation.ExpiryDate,
			&allocation.Quantity, &allocation.DispensedQuantity, &allocation.DoorNo,
			&allocation.HardwareLayer, &allocation.ChannelStart, &allocation.ChannelEnd,
			&allocation.Status, &allocation.HardwareAttemptedAt, &allocation.HardwareSuccess,
			&allocation.HardwareDetail, &allocation.HardwareResponse,
		); err != nil {
			return nil, fmt.Errorf("scan dispense allocation: %w", err)
		}
		if item := itemByID[allocation.ItemID]; item != nil {
			item.Allocations = append(item.Allocations, allocation)
		}
	}
	if err := allocationRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate dispense allocations: %w", err)
	}
	return &record, nil
}

// QueueTransaction binds the authenticated operator, transitions the
// prescription, and writes the outbox event atomically.
func (s *Store) QueueTransaction(
	ctx context.Context,
	tx pgx.Tx,
	record *TransactionRecord,
	operatorUserID, operatorDisplayName string,
	outboxPayload []byte,
) error {
	if record == nil {
		return ErrDispenseNotFound
	}
	if record.Status != TransactionAwaitingIdentity {
		return ErrDispenseNotAwaitingID
	}
	if time.Now().After(record.ExpiresAt) {
		return ErrDispenseExpired
	}

	tag, err := tx.Exec(ctx,
		`UPDATE medisync.dispense_transaction
		    SET operator_user_id = $1, operator_display_name = $2,
		        status = 'QUEUED', identity_confirmed_at = now(), queued_at = now(),
		        hardware_request = $3, updated_at = now()
		  WHERE id = $4 AND status = 'AWAITING_IDENTITY' AND expires_at > now()`,
		operatorUserID, operatorDisplayName, outboxPayload, record.ID)
	if err != nil {
		return fmt.Errorf("queue dispense transaction: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return ErrDispenseNotAwaitingID
	}
	tag, err = tx.Exec(ctx,
		`UPDATE medisync.prescription SET state = 'DISPENSING', updated_at = now()
		  WHERE id = $1 AND state = 'READY'`, record.PrescriptionRowID)
	if err != nil {
		return fmt.Errorf("transition prescription to DISPENSING: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return ErrInvalidTransition
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO medisync.outbox (subject, payload, project_id, created_by)
		 VALUES ('medisync.dispense.requested', $1, $2, $3)`,
		outboxPayload, nullableString(record.ProjectID), operatorUserID); err != nil {
		return fmt.Errorf("insert dispense outbox: %w", err)
	}
	return nil
}

func (s *Store) MarkTransactionStarted(ctx context.Context, dispenseID, kioskCode string) error {
	tag, err := s.db.Exec(ctx,
		`UPDATE medisync.dispense_transaction
		    SET status = 'DISPENSING', started_at = COALESCE(started_at, now()), updated_at = now()
		  WHERE id = $1 AND kiosk_code = $2 AND status IN ('QUEUED','DISPENSING')`, dispenseID, kioskCode)
	if err != nil {
		return fmt.Errorf("mark dispense started: %w", err)
	}
	if tag.RowsAffected() == 0 {
		tag, err = s.db.Exec(ctx,
			`UPDATE medisync.emergency_dispense_transaction
			    SET status='DISPENSING',started_at=COALESCE(started_at,now()),updated_at=now()
			  WHERE id=$1 AND kiosk_code=$2 AND status IN ('QUEUED','DISPENSING')`,
			dispenseID, kioskCode)
		if err != nil {
			return fmt.Errorf("mark emergency dispense started: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrDispenseNotFound
		}
		_, _ = s.db.Exec(ctx,
			`UPDATE medisync.emergency_dispense_allocation
			    SET status='DISPENSING',updated_at=now()
			  WHERE emergency_dispense_id=$1 AND status='RESERVED'`, dispenseID)
		return nil
	}
	_, _ = s.db.Exec(ctx,
		`UPDATE medisync.dispense_transaction_item SET status='DISPENSING', updated_at=now()
		  WHERE dispense_id=$1 AND status='RESERVED'`, dispenseID)
	_, _ = s.db.Exec(ctx,
		`UPDATE medisync.dispense_allocation SET status='DISPENSING', updated_at=now()
		  WHERE dispense_id=$1 AND status='RESERVED'`, dispenseID)
	return nil
}

func (s *Store) TransactionExecutionStatus(ctx context.Context, dispenseID, kioskCode string) (string, error) {
	var status string
	if err := s.db.QueryRow(ctx,
		`SELECT status FROM medisync.dispense_transaction WHERE id=$1 AND kiosk_code=$2`,
		dispenseID, kioskCode).Scan(&status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			if emergencyErr := s.db.QueryRow(ctx,
				`SELECT status FROM medisync.emergency_dispense_transaction WHERE id=$1 AND kiosk_code=$2`,
				dispenseID, kioskCode).Scan(&status); emergencyErr != nil {
				if errors.Is(emergencyErr, pgx.ErrNoRows) {
					return "", ErrDispenseNotFound
				}
				return "", emergencyErr
			}
			return status, nil
		}
		return "", err
	}
	return status, nil
}

// LoadHardwareResults returns durable per-allocation outcomes. A redelivered
// NATS message uses these records instead of physically dispensing twice.
func (s *Store) LoadHardwareResults(ctx context.Context, dispenseID, kioskCode string) (map[string]*eventsv1.DispenseAllocationResult, error) {
	rows, err := s.db.Query(ctx,
		`SELECT a.id, a.hardware_success, a.hardware_detail
		   FROM medisync.dispense_allocation a
		   JOIN medisync.dispense_transaction d ON d.id=a.dispense_id
		  WHERE a.dispense_id=$1 AND d.kiosk_code=$2 AND a.hardware_attempted_at IS NOT NULL
		 UNION ALL
		 SELECT a.id, a.hardware_success, a.hardware_detail
		   FROM medisync.emergency_dispense_allocation a
		   JOIN medisync.emergency_dispense_transaction d ON d.id=a.emergency_dispense_id
		  WHERE a.emergency_dispense_id=$1 AND d.kiosk_code=$2 AND a.hardware_attempted_at IS NOT NULL`,
		dispenseID, kioskCode)
	if err != nil {
		return nil, fmt.Errorf("load hardware results: %w", err)
	}
	defer rows.Close()
	results := make(map[string]*eventsv1.DispenseAllocationResult)
	for rows.Next() {
		result := &eventsv1.DispenseAllocationResult{}
		if err := rows.Scan(&result.AllocationId, &result.Success, &result.Detail); err != nil {
			return nil, fmt.Errorf("scan hardware result: %w", err)
		}
		results[result.AllocationId] = result
	}
	return results, rows.Err()
}

func (s *Store) RecordHardwareResult(ctx context.Context, dispenseID, kioskCode string, result *eventsv1.DispenseAllocationResult, responseJSON string) error {
	if result == nil || result.AllocationId == "" {
		return errors.New("allocation hardware result is required")
	}
	if strings.TrimSpace(responseJSON) == "" {
		responseJSON = "{}"
	}
	tag, err := s.db.Exec(ctx,
		`UPDATE medisync.dispense_allocation a
		    SET hardware_attempted_at=COALESCE(a.hardware_attempted_at,now()),
		        hardware_success=COALESCE(a.hardware_success,$1),
		        hardware_detail=CASE WHEN a.hardware_success IS NULL THEN $2 ELSE a.hardware_detail END,
		        hardware_response=CASE WHEN a.hardware_success IS NULL THEN $3::jsonb ELSE a.hardware_response END,
		        updated_at=now()
		   FROM medisync.dispense_transaction d
		  WHERE a.id=$4 AND a.dispense_id=$5 AND d.id=a.dispense_id AND d.kiosk_code=$6`,
		result.Success, result.Detail, responseJSON, result.AllocationId, dispenseID, kioskCode)
	if err != nil {
		return fmt.Errorf("record hardware result: %w", err)
	}
	if tag.RowsAffected() != 1 {
		tag, err = s.db.Exec(ctx,
			`UPDATE medisync.emergency_dispense_allocation a
			    SET hardware_attempted_at=COALESCE(a.hardware_attempted_at,now()),
			        hardware_success=COALESCE(a.hardware_success,$1),
			        hardware_detail=CASE WHEN a.hardware_success IS NULL THEN $2 ELSE a.hardware_detail END,
			        hardware_response=CASE WHEN a.hardware_success IS NULL THEN $3::jsonb ELSE a.hardware_response END,
			        updated_at=now()
			   FROM medisync.emergency_dispense_transaction d
			  WHERE a.id=$4 AND a.emergency_dispense_id=$5
			    AND d.id=a.emergency_dispense_id AND d.kiosk_code=$6`,
			result.Success, result.Detail, responseJSON, result.AllocationId, dispenseID, kioskCode)
		if err != nil {
			return fmt.Errorf("record emergency hardware result: %w", err)
		}
		if tag.RowsAffected() != 1 {
			return ErrDispenseNotFound
		}
	}
	return nil
}

// MarkHardwareAttempt is committed before issuing a physical command. If the
// process dies after sending but before persisting the outcome, redelivery sees
// an attempted allocation with an unknown result and fails closed instead of
// dispensing the same medicine twice.
func (s *Store) MarkHardwareAttempt(ctx context.Context, dispenseID, kioskCode, allocationID string) error {
	tag, err := s.db.Exec(ctx,
		`UPDATE medisync.dispense_allocation a
		    SET hardware_attempted_at=COALESCE(a.hardware_attempted_at,now()),
		        hardware_detail=CASE WHEN a.hardware_attempted_at IS NULL THEN 'command dispatched; outcome pending' ELSE a.hardware_detail END,
		        updated_at=now()
		   FROM medisync.dispense_transaction d
		  WHERE a.id=$1 AND a.dispense_id=$2 AND d.id=a.dispense_id AND d.kiosk_code=$3`,
		allocationID, dispenseID, kioskCode)
	if err != nil {
		return fmt.Errorf("mark hardware attempt: %w", err)
	}
	if tag.RowsAffected() != 1 {
		tag, err = s.db.Exec(ctx,
			`UPDATE medisync.emergency_dispense_allocation a
			    SET hardware_attempted_at=COALESCE(a.hardware_attempted_at,now()),
			        hardware_detail=CASE WHEN a.hardware_attempted_at IS NULL THEN 'command dispatched; outcome pending' ELSE a.hardware_detail END,
			        updated_at=now()
			   FROM medisync.emergency_dispense_transaction d
			  WHERE a.id=$1 AND a.emergency_dispense_id=$2
			    AND d.id=a.emergency_dispense_id AND d.kiosk_code=$3`,
			allocationID, dispenseID, kioskCode)
		if err != nil {
			return fmt.Errorf("mark emergency hardware attempt: %w", err)
		}
		if tag.RowsAffected() != 1 {
			return ErrDispenseNotFound
		}
	}
	return nil
}

func releaseReservations(ctx context.Context, tx pgx.Tx, dispenseID, terminalStatus string) error {
	rows, err := tx.Query(ctx,
		`SELECT slot_id, batch_id, quantity FROM medisync.dispense_allocation
		  WHERE dispense_id=$1 AND status IN ('RESERVED','DISPENSING') FOR UPDATE`, dispenseID)
	if err != nil {
		return fmt.Errorf("lock dispense reservations: %w", err)
	}
	type reservation struct {
		slotID, batchID string
		quantity        int32
	}
	var reservations []reservation
	for rows.Next() {
		var r reservation
		if err := rows.Scan(&r.slotID, &r.batchID, &r.quantity); err != nil {
			rows.Close()
			return fmt.Errorf("scan reservation: %w", err)
		}
		reservations = append(reservations, r)
	}
	rows.Close()
	for _, r := range reservations {
		if _, err := tx.Exec(ctx,
			`UPDATE medisync.slot_batch SET reserved_quantity=GREATEST(0,reserved_quantity-$1), updated_at=now() WHERE id=$2`,
			r.quantity, r.batchID); err != nil {
			return fmt.Errorf("release batch reservation: %w", err)
		}
		if _, err := tx.Exec(ctx,
			`UPDATE medisync.slot SET reserved_quantity=GREATEST(0,reserved_quantity-$1), updated_at=now() WHERE id=$2`,
			r.quantity, r.slotID); err != nil {
			return fmt.Errorf("release slot reservation: %w", err)
		}
	}
	_, err = tx.Exec(ctx,
		`UPDATE medisync.dispense_allocation SET status=$2, updated_at=now()
		  WHERE dispense_id=$1 AND status IN ('RESERVED','DISPENSING')`, dispenseID, terminalStatus)
	if err != nil {
		return fmt.Errorf("release allocations: %w", err)
	}
	_, err = tx.Exec(ctx,
		`UPDATE medisync.dispense_transaction_item SET status=$2, updated_at=now()
		  WHERE dispense_id=$1 AND status IN ('RESERVED','DISPENSING')`, dispenseID, terminalStatus)
	return err
}

func (s *Store) CancelTransaction(ctx context.Context, poolBegin func(context.Context) (pgx.Tx, error), dispenseID, kioskCode, reason string) (*TransactionRecord, error) {
	tx, err := poolBegin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	record, err := getTransaction(ctx, tx, dispenseID, true)
	if err != nil || record == nil {
		if err == nil {
			err = ErrDispenseNotFound
		}
		return nil, err
	}
	if record.KioskCode != kioskCode {
		return nil, ErrDispenseWrongKiosk
	}
	if record.Status != TransactionAwaitingIdentity {
		return nil, ErrDispenseNotAwaitingID
	}
	if err := releaseReservations(ctx, tx, dispenseID, "RELEASED"); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE medisync.dispense_transaction
		    SET status='CANCELLED', failure_detail=$2, cancelled_at=now(), updated_at=now()
		  WHERE id=$1`, dispenseID, reason); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.GetTransaction(ctx, dispenseID)
}

func (s *Store) ExpireStaleTransactions(ctx context.Context, poolBegin func(context.Context) (pgx.Tx, error)) (int, error) {
	rows, err := s.db.Query(ctx, `SELECT id FROM medisync.dispense_transaction WHERE status='AWAITING_IDENTITY' AND expires_at <= now() ORDER BY expires_at LIMIT 100`)
	if err != nil {
		return 0, fmt.Errorf("list expired dispense transactions: %w", err)
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	expired := 0
	for _, id := range ids {
		tx, err := poolBegin(ctx)
		if err != nil {
			return expired, err
		}
		record, err := getTransaction(ctx, tx, id, true)
		if err != nil {
			_ = tx.Rollback(ctx)
			return expired, err
		}
		if record == nil || record.Status != TransactionAwaitingIdentity || time.Now().Before(record.ExpiresAt) {
			_ = tx.Rollback(ctx)
			continue
		}
		if err := releaseReservations(ctx, tx, id, "RELEASED"); err != nil {
			_ = tx.Rollback(ctx)
			return expired, err
		}
		if _, err := tx.Exec(ctx, `UPDATE medisync.dispense_transaction SET status='EXPIRED', failure_code='identity_timeout', failure_detail='identity was not confirmed before reservation expiry', failed_at=now(), updated_at=now() WHERE id=$1`, id); err != nil {
			_ = tx.Rollback(ctx)
			return expired, err
		}
		if err := tx.Commit(ctx); err != nil {
			return expired, err
		}
		expired++
	}
	return expired, nil
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func (s *Store) FinalizeTransactionSuccess(ctx context.Context, tx pgx.Tx, dispenseID, kioskCode, hardwareResponse string, outcomes map[string]bool) (*TransactionRecord, bool, error) {
	record, err := getTransaction(ctx, tx, dispenseID, true)
	if err != nil || record == nil {
		if err == nil {
			err = ErrDispenseNotFound
		}
		return nil, false, err
	}
	if record.KioskCode != kioskCode {
		return nil, false, ErrDispenseWrongKiosk
	}
	if record.Status == TransactionDispensed {
		return record, false, nil
	}
	if record.Status != TransactionDispensing && record.Status != TransactionQueued {
		return nil, false, fmt.Errorf("cannot complete transaction in status %s", record.Status)
	}
	for _, item := range record.Items {
		for _, allocation := range item.Allocations {
			if !outcomes[allocation.ID] {
				return nil, false, fmt.Errorf("allocation %s lacks hardware success", allocation.ID)
			}
			if err := consumeAllocation(ctx, tx, allocation); err != nil {
				return nil, false, err
			}
		}
	}
	if err := updateItemOutcomes(ctx, tx, dispenseID, "DISPENSED"); err != nil {
		return nil, false, err
	}
	tag, err := tx.Exec(ctx,
		`UPDATE medisync.dispense_transaction
		    SET status='DISPENSED', hardware_response=$2, completed_at=now(), updated_at=now()
		  WHERE id=$1 AND status IN ('QUEUED','DISPENSING')`, dispenseID, []byte(hardwareResponse))
	if err != nil {
		return nil, false, fmt.Errorf("complete dispense transaction: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return nil, false, fmt.Errorf("complete dispense transaction: state changed concurrently")
	}
	tag, err = tx.Exec(ctx,
		`UPDATE medisync.prescription SET state='DISPENSED', failure_reason='', updated_at=now()
		  WHERE id=$1 AND state='DISPENSING'`, record.PrescriptionRowID)
	if err != nil {
		return nil, false, fmt.Errorf("complete prescription: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return nil, false, fmt.Errorf("complete prescription: state changed concurrently")
	}
	updated, err := getTransaction(ctx, tx, dispenseID, false)
	return updated, true, err
}

func (s *Store) FinalizeTransactionFailure(ctx context.Context, tx pgx.Tx, dispenseID, kioskCode, reason, detail string, outcomes map[string]bool) (*TransactionRecord, bool, error) {
	record, err := getTransaction(ctx, tx, dispenseID, true)
	if err != nil || record == nil {
		if err == nil {
			err = ErrDispenseNotFound
		}
		return nil, false, err
	}
	if record.KioskCode != kioskCode {
		return nil, false, ErrDispenseWrongKiosk
	}
	if record.Status == TransactionFailed {
		return record, false, nil
	}
	if record.Status != TransactionDispensing && record.Status != TransactionQueued {
		return nil, false, fmt.Errorf("cannot fail transaction in status %s", record.Status)
	}
	for _, item := range record.Items {
		for _, allocation := range item.Allocations {
			if outcomes[allocation.ID] {
				if err := consumeAllocation(ctx, tx, allocation); err != nil {
					return nil, false, err
				}
				continue
			}
			if err := releaseAllocation(ctx, tx, allocation, "FAILED"); err != nil {
				return nil, false, err
			}
		}
	}
	if err := updateItemOutcomes(ctx, tx, dispenseID, "FAILED"); err != nil {
		return nil, false, err
	}
	tag, err := tx.Exec(ctx,
		`UPDATE medisync.dispense_transaction
		    SET status='FAILED', failure_code=$2, failure_detail=$3, failed_at=now(), updated_at=now()
		  WHERE id=$1 AND status IN ('QUEUED','DISPENSING')`, dispenseID, reason, detail)
	if err != nil {
		return nil, false, fmt.Errorf("fail dispense transaction: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return nil, false, fmt.Errorf("fail dispense transaction: state changed concurrently")
	}
	tag, err = tx.Exec(ctx,
		`UPDATE medisync.prescription SET state='FAILED', failure_reason=$2, updated_at=now()
		  WHERE id=$1 AND state='DISPENSING'`, record.PrescriptionRowID, reason+": "+detail)
	if err != nil {
		return nil, false, fmt.Errorf("fail prescription: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return nil, false, fmt.Errorf("fail prescription: state changed concurrently")
	}
	updated, err := getTransaction(ctx, tx, dispenseID, false)
	return updated, true, err
}

func consumeAllocation(ctx context.Context, tx pgx.Tx, allocation *AllocationRecord) error {
	tag, err := tx.Exec(ctx,
		`UPDATE medisync.slot_batch
		    SET quantity=quantity-$1, reserved_quantity=reserved_quantity-$1, updated_at=now()
		  WHERE id=$2 AND quantity >= $1 AND reserved_quantity >= $1`, allocation.Quantity, allocation.BatchID)
	if err != nil {
		return fmt.Errorf("consume reserved batch %s: %w", allocation.BatchID, err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("consume reserved batch %s: reservation changed concurrently", allocation.BatchID)
	}
	tag, err = tx.Exec(ctx,
		`UPDATE medisync.slot
		    SET quantity=quantity-$1, reserved_quantity=reserved_quantity-$1, updated_at=now()
		  WHERE id=$2 AND quantity >= $1 AND reserved_quantity >= $1`, allocation.Quantity, allocation.SlotID)
	if err != nil {
		return fmt.Errorf("consume reserved slot %s: %w", allocation.SlotCode, err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("consume reserved slot %s: reservation changed concurrently", allocation.SlotCode)
	}
	if _, err := tx.Exec(ctx,
		`UPDATE medisync.dispense_allocation SET status='DISPENSED', dispensed_quantity=quantity, updated_at=now() WHERE id=$1`, allocation.ID); err != nil {
		return fmt.Errorf("complete allocation %s: %w", allocation.ID, err)
	}
	return nil
}

func releaseAllocation(ctx context.Context, tx pgx.Tx, allocation *AllocationRecord, status string) error {
	if _, err := tx.Exec(ctx, `UPDATE medisync.slot_batch SET reserved_quantity=GREATEST(0,reserved_quantity-$1), updated_at=now() WHERE id=$2`, allocation.Quantity, allocation.BatchID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE medisync.slot SET reserved_quantity=GREATEST(0,reserved_quantity-$1), updated_at=now() WHERE id=$2`, allocation.Quantity, allocation.SlotID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE medisync.dispense_allocation SET status=$2, updated_at=now() WHERE id=$1`, allocation.ID, status)
	return err
}

func updateItemOutcomes(ctx context.Context, tx pgx.Tx, dispenseID, failureStatus string) error {
	_, err := tx.Exec(ctx,
		`UPDATE medisync.dispense_transaction_item i
		    SET dispensed_quantity = a.dispensed_quantity,
		        status = CASE
		          WHEN a.dispensed_quantity = i.requested_quantity THEN 'DISPENSED'
		          WHEN $2 = 'FAILED' THEN 'FAILED'
		          ELSE $2
		        END,
		        updated_at=now()
		   FROM (SELECT item_id, COALESCE(sum(dispensed_quantity),0)::integer AS dispensed_quantity
		           FROM medisync.dispense_allocation WHERE dispense_id=$1 GROUP BY item_id) a
		  WHERE i.id=a.item_id`, dispenseID, failureStatus)
	return err
}

type TransactionFilter struct {
	ProjectID      string
	KioskCode      string
	SlotID         string
	DrugCode       string
	Statuses       []TransactionStatus
	Prescription   string
	OperatorUserID string
	CreatedFrom    *time.Time
	CreatedTo      *time.Time
	PageSize       int32
	PageToken      string
}

func (s *Store) ListTransactions(ctx context.Context, filter TransactionFilter) ([]*TransactionRecord, string, int64, error) {
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
		add("project_id = $%d", filter.ProjectID)
	}
	if filter.KioskCode != "" {
		add("kiosk_code = $%d", filter.KioskCode)
	}
	switch {
	case filter.SlotID != "" && filter.DrugCode != "":
		args = append(args, filter.SlotID, filter.DrugCode)
		clauses = append(clauses, fmt.Sprintf(`EXISTS (
			SELECT 1
			  FROM medisync.dispense_allocation allocation
			  JOIN medisync.dispense_transaction_item item ON item.id = allocation.item_id
			 WHERE allocation.dispense_id = medisync.dispense_transaction.id
			   AND allocation.slot_id = $%d
			   AND item.drug_code = $%d
		)`, len(args)-1, len(args)))
	case filter.SlotID != "":
		add(`EXISTS (
			SELECT 1 FROM medisync.dispense_allocation allocation
			 WHERE allocation.dispense_id = medisync.dispense_transaction.id
			   AND allocation.slot_id = $%d
		)`, filter.SlotID)
	case filter.DrugCode != "":
		add(`EXISTS (
			SELECT 1 FROM medisync.dispense_transaction_item item
			 WHERE item.dispense_id = medisync.dispense_transaction.id
			   AND item.drug_code = $%d
		)`, filter.DrugCode)
	}
	if len(filter.Statuses) > 0 {
		values := make([]string, 0, len(filter.Statuses))
		for _, status := range filter.Statuses {
			values = append(values, string(status))
		}
		add("status = ANY($%d)", values)
	}
	if filter.Prescription != "" {
		add("prescription_ref = $%d", filter.Prescription)
	}
	if filter.OperatorUserID != "" {
		add("operator_user_id = $%d", filter.OperatorUserID)
	}
	if filter.CreatedFrom != nil {
		add("created_at >= $%d", *filter.CreatedFrom)
	}
	if filter.CreatedTo != nil {
		add("created_at < $%d", *filter.CreatedTo)
	}

	where := " WHERE " + joinClauses(clauses)
	var total int64
	if err := s.db.QueryRow(ctx, `SELECT count(*) FROM medisync.dispense_transaction`+where, args...).Scan(&total); err != nil {
		return nil, "", 0, fmt.Errorf("count dispense transactions: %w", err)
	}
	if filter.PageToken != "" {
		add("created_at < (SELECT created_at FROM medisync.dispense_transaction WHERE id = $%d)", filter.PageToken)
		where = " WHERE " + joinClauses(clauses)
	}
	args = append(args, pageSize+1)
	rows, err := s.db.Query(ctx,
		fmt.Sprintf(`SELECT id FROM medisync.dispense_transaction%s ORDER BY created_at DESC, id DESC LIMIT $%d`, where, len(args)),
		args...)
	if err != nil {
		return nil, "", 0, fmt.Errorf("list dispense transaction ids: %w", err)
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
	records := make([]*TransactionRecord, 0, len(ids))
	for _, id := range ids {
		record, err := s.GetTransaction(ctx, id)
		if err != nil {
			return nil, "", 0, err
		}
		if record != nil {
			records = append(records, record)
		}
	}
	return records, next, total, nil
}

func joinClauses(clauses []string) string {
	result := ""
	for i, clause := range clauses {
		if i > 0 {
			result += " AND "
		}
		result += clause
	}
	return result
}
