//go:build integration

package dispensing

import (
	"context"
	"fmt"
	"testing"
	"time"

	"connectrpc.com/connect"
	dispensingv1 "github.com/adm-chura3inter/medisync/services/core/internal/gen/medisync/dispensing/v1"
	eventsv1 "github.com/adm-chura3inter/medisync/services/core/internal/gen/medisync/events/v1"
	"google.golang.org/protobuf/encoding/protojson"
)

type emergencyIntegrationTokenParser map[string]*TokenClaims

func (p emergencyIntegrationTokenParser) Parse(token string) (*TokenClaims, error) {
	claims := p[token]
	if claims == nil {
		return nil, fmt.Errorf("unknown integration token")
	}
	return claims, nil
}

func TestEmergencyTransactionIsSeparateAndConsumesOnlyAfterHardwareSuccess(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool := integrationPool(t)
	store := NewStore(pool)

	var projectID string
	if err := pool.QueryRow(ctx, `SELECT id FROM medisync.projects WHERE code='0001'`).Scan(&projectID); err != nil {
		t.Fatalf("load project: %v", err)
	}
	kioskCode := seedPipelineKiosk(t, ctx, pool, projectID)
	operatorID := seedPipelineOperator(t, ctx, pool, projectID)
	drugCode := uniqueID(t, "EMERGENCY-DRUG")
	slotCode := uniqueID(t, "EMERGENCY-SLOT")
	employeeCode := uniqueID(t, "EMP")
	hn := uniqueID(t, "HN-EMERGENCY")
	seedPipelineSlot(t, ctx, pool, kioskCode, slotCode, drugCode, projectID)
	if _, err := pool.Exec(ctx, `UPDATE medisync.users SET employee_code=$2 WHERE id=$1`, operatorID, employeeCode); err != nil {
		t.Fatalf("set employee code: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`UPDATE medisync.slot SET emergency_drug=true,emergency_max_quantity=2
		  WHERE cabinet_id=$1 AND code=$2`, kioskCode, slotCode); err != nil {
		t.Fatalf("configure emergency drug: %v", err)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin emergency transaction: %v", err)
	}
	record, err := store.CreateEmergencyTransaction(
		ctx, tx, kioskCode, projectID, hn, employeeCode,
		slotCode, drugCode, 2, "integration test", uniqueID(t, "trace-emergency"), EmergencyAuthEmployeeCode,
	)
	if err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("create emergency transaction: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit emergency transaction: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM medisync.outbox WHERE created_by=$1`, operatorID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM medisync.emergency_dispense_transaction WHERE id=$1`, record.ID)
	})

	if record.Status != TransactionQueued {
		t.Fatalf("initial status = %s, want QUEUED", record.Status)
	}
	if record.KioskCode != kioskCode || record.EmployeeCode != employeeCode {
		t.Fatalf("transaction scope = kiosk %q employee %q", record.KioskCode, record.EmployeeCode)
	}
	if record.OperatorAuthMethod != EmergencyAuthEmployeeCode {
		t.Fatalf("operator auth method = %q, want %q", record.OperatorAuthMethod, EmergencyAuthEmployeeCode)
	}
	var normalCount, emergencyCount, quantity, reserved int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM medisync.dispense_transaction WHERE id=$1`, record.ID).Scan(&normalCount); err != nil {
		t.Fatalf("count normal transactions: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM medisync.emergency_dispense_transaction WHERE id=$1`, record.ID).Scan(&emergencyCount); err != nil {
		t.Fatalf("count emergency transactions: %v", err)
	}
	if normalCount != 0 || emergencyCount != 1 {
		t.Fatalf("normal=%d emergency=%d, want normal=0 emergency=1", normalCount, emergencyCount)
	}
	var prescriptionCount int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM medisync.prescription WHERE project_id=$1 AND hn=$2`,
		projectID, hn).Scan(&prescriptionCount); err != nil {
		t.Fatalf("count prescriptions for emergency HN: %v", err)
	}
	if prescriptionCount != 0 {
		t.Fatalf("emergency dispense created %d prescription rows, want 0", prescriptionCount)
	}
	var hardwareRequest eventsv1.DispenseRequested
	if err := protojson.Unmarshal(record.HardwareRequest, &hardwareRequest); err != nil {
		t.Fatalf("decode emergency hardware request: %v", err)
	}
	if hardwareRequest.PrescriptionId != "EMERGENCY:"+record.ID {
		t.Fatalf("hardware order reference = %q, want emergency transaction reference", hardwareRequest.PrescriptionId)
	}
	if hardwareRequest.PrescriptionId == "EMERGENCY:"+hn {
		t.Fatal("hardware order reference must not contain the patient HN")
	}
	if err := pool.QueryRow(ctx,
		`SELECT quantity,reserved_quantity FROM medisync.slot WHERE cabinet_id=$1 AND code=$2`,
		kioskCode, slotCode).Scan(&quantity, &reserved); err != nil {
		t.Fatalf("read reserved stock: %v", err)
	}
	if quantity != 10 || reserved != 2 {
		t.Fatalf("before hardware quantity=%d reserved=%d, want 10/2", quantity, reserved)
	}

	if err := store.MarkTransactionStarted(ctx, record.ID, kioskCode); err != nil {
		t.Fatalf("mark emergency started: %v", err)
	}
	outcomes := make(map[string]bool, len(record.Allocations))
	for _, allocation := range record.Allocations {
		if err := store.MarkHardwareAttempt(ctx, record.ID, kioskCode, allocation.ID); err != nil {
			t.Fatalf("mark hardware attempt: %v", err)
		}
		result := &eventsv1.DispenseAllocationResult{AllocationId: allocation.ID, Success: true, Detail: "success"}
		if err := store.RecordHardwareResult(ctx, record.ID, kioskCode, result, `{"status":"success"}`); err != nil {
			t.Fatalf("record hardware result: %v", err)
		}
		outcomes[allocation.ID] = true
	}
	tx, err = pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin finalize: %v", err)
	}
	completed, applied, err := store.FinalizeEmergencySuccess(ctx, tx, record.ID, kioskCode, `[{"status":"success"}]`, outcomes)
	if err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("finalize emergency success: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit emergency completion: %v", err)
	}
	if !applied || completed.Status != TransactionDispensed || completed.DispensedQuantity != 2 {
		t.Fatalf("completion = applied %v status %s quantity %d", applied, completed.Status, completed.DispensedQuantity)
	}
	if err := pool.QueryRow(ctx,
		`SELECT quantity,reserved_quantity FROM medisync.slot WHERE cabinet_id=$1 AND code=$2`,
		kioskCode, slotCode).Scan(&quantity, &reserved); err != nil {
		t.Fatalf("read completed stock: %v", err)
	}
	if quantity != 8 || reserved != 0 {
		t.Fatalf("after hardware quantity=%d reserved=%d, want 8/0", quantity, reserved)
	}

	listed, _, total, err := store.ListEmergencyTransactions(ctx, EmergencyTransactionFilter{
		ProjectID: projectID, KioskCode: kioskCode, HN: hn, EmployeeCode: employeeCode,
	})
	if err != nil {
		t.Fatalf("list emergency transactions: %v", err)
	}
	if total != 1 || len(listed) != 1 || listed[0].ID != record.ID {
		t.Fatalf("list returned total=%d records=%d", total, len(listed))
	}
}

func TestEmergencyCardAuthenticationUsesStaffAndKioskIdentity(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool := integrationPool(t)
	store := NewStore(pool)

	var projectID string
	if err := pool.QueryRow(ctx, `SELECT id FROM medisync.projects WHERE code='0001'`).Scan(&projectID); err != nil {
		t.Fatalf("load project: %v", err)
	}
	kioskCode := seedPipelineKiosk(t, ctx, pool, projectID)
	operatorID := seedPipelineOperator(t, ctx, pool, projectID)
	employeeCode := uniqueID(t, "EMP-CARD")
	drugCode := uniqueID(t, "EMERGENCY-CARD-DRUG")
	slotCode := uniqueID(t, "EMERGENCY-CARD-SLOT")
	seedPipelineSlot(t, ctx, pool, kioskCode, slotCode, drugCode, projectID)
	if _, err := pool.Exec(ctx, `UPDATE medisync.users SET employee_code=$2 WHERE id=$1`, operatorID, employeeCode); err != nil {
		t.Fatalf("set employee code: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`UPDATE medisync.slot SET emergency_drug=true,emergency_max_quantity=1
		  WHERE cabinet_id=$1 AND code=$2`, kioskCode, slotCode); err != nil {
		t.Fatalf("configure emergency drug: %v", err)
	}

	server := NewDispensingServer(store, pool, emergencyIntegrationTokenParser{
		"staff-token": {Subject: operatorID, Role: "NURSE", ProjectID: projectID},
		"kiosk-token": {Subject: kioskCode, Role: "KIOSK", ProjectID: projectID},
	}, nil)
	req := connect.NewRequest(&dispensingv1.EmergencyDispenseRequest{
		KioskCode: kioskCode, Hn: uniqueID(t, "HN-CARD"), EmployeeCode: employeeCode,
		SlotCode: slotCode, DrugCode: drugCode, Quantity: 1,
	})
	req.Header().Set("Authorization", "Bearer staff-token")
	req.Header().Set("X-Kiosk-Authorization", "Bearer kiosk-token")
	response, err := server.EmergencyDispense(ctx, req)
	if err != nil {
		t.Fatalf("card-authenticated emergency dispense: %v", err)
	}
	if response.Msg.Transaction == nil {
		t.Fatal("card-authenticated response has no transaction")
	}
	record := response.Msg.Transaction
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM medisync.outbox WHERE created_by=$1`, operatorID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM medisync.emergency_dispense_transaction WHERE id=$1`, record.DispenseId)
	})
	if record.OperatorAuthMethod != dispensingv1.EmergencyOperatorAuthMethod_EMERGENCY_OPERATOR_AUTH_METHOD_CARD {
		t.Fatalf("operator auth method = %v, want CARD", record.OperatorAuthMethod)
	}
	if record.OperatorUserId != operatorID || record.EmployeeCode != employeeCode || record.KioskCode != kioskCode {
		t.Fatalf("card attribution = operator %q employee %q kiosk %q", record.OperatorUserId, record.EmployeeCode, record.KioskCode)
	}
}
