//go:build integration

// Package e2e holds cross-context end-to-end integration tests that drive
// the full M2 chain against a real PostgreSQL 16 database. These tests
// exercise identity, catalog, inventory, and dispensing together with
// real JWT tokens — no fake parsers or mocked stores.
//
// This file is the pre-M3 safety net: it covers the M2-complete portion
// of CANONICAL_FLOW.md steps 1–3 (up to dispense.requested outbox).
// Do NOT add M3 dependencies (fulfillment, printing, vending) here.
package e2e

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/protobuf/encoding/protojson"

	dispensingv1 "github.com/adm-chura3inter/medisync/services/core/internal/gen/medisync/dispensing/v1"
	eventsv1 "github.com/adm-chura3inter/medisync/services/core/internal/gen/medisync/events/v1"

	"github.com/adm-chura3inter/medisync/services/core/internal/catalog"
	"github.com/adm-chura3inter/medisync/services/core/internal/dispensing"
	"github.com/adm-chura3inter/medisync/services/core/internal/identity"
	"github.com/adm-chura3inter/medisync/services/core/internal/inventory"
	"github.com/adm-chura3inter/medisync/services/core/internal/platform/audit"
)

// ---------------------------------------------------------------------------
// Test harness
// ---------------------------------------------------------------------------

func e2ePool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Fatal("TEST_DATABASE_URL is required for E2E integration tests. Set it to a test database URL.")
	}
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatalf("connect to test database: %v", err)
	}
	if err := pool.Ping(context.Background()); err != nil {
		pool.Close()
		t.Fatalf("ping test database: %v", err)
	}
	t.Cleanup(func() { pool.Close() })
	return pool
}

func e2eUnique(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

// jwtTokenParser adapts identity.JWTManager to dispensing.TokenParser.
// This is the same adapter wired in cmd/core/main.go.
type jwtTokenParser struct {
	mgr *identity.JWTManager
}

func (p *jwtTokenParser) Parse(tokenString string) (*dispensing.TokenClaims, error) {
	claims, err := p.mgr.Parse(tokenString)
	if err != nil {
		return nil, err
	}
	return &dispensing.TokenClaims{
		Subject: claims.Subject,
		Role:    claims.Role,
		WardIDs: claims.WardIDs,
	}, nil
}

// ---------------------------------------------------------------------------
// The end-to-end test
// ---------------------------------------------------------------------------

// TestFullM2ChainE2E drives the verified M2 chain from identity → catalog →
// inventory → dispensing → audit + outbox, with real JWT-based ward isolation.
//
// Steps exercised:
//  1. Seed identity — create two users (WARD-3A nurse, WARD-9Z nurse), issue real JWTs.
//  2. Catalog — create a drug, assert it is active.
//  3. Inventory — create a slot, assign the drug, refill stock, assert quantity.
//  4. Dispensing intake — insert a READY prescription for WARD-3A.
//  5. Dispense — call the real handler with WARD-3A JWT; assert READY→DISPENSING,
//     audit entry written, dispense.requested outbox row in same transaction.
//  6. Ward isolation — attempt GetPrescription with WARD-9Z JWT on the WARD-3A
//     prescription; assert CodeNotFound (closes F1 fake-parser gap).
func TestFullM2ChainE2E(t *testing.T) {
	ctx := context.Background()
	pool := e2ePool(t)

	// ---- shared infrastructure ----------------------------------------

	jwtMgr, err := identity.NewJWTManager(
		"medisync-e2e-test-secret-32-bytes!!", // 32+ bytes required
		1*time.Hour,                           // long enough for the test
		nil,                                   // use real clock
	)
	if err != nil {
		t.Fatalf("NewJWTManager: %v", err)
	}

	auditWriter := audit.NewWriter(pool)
	parser := &jwtTokenParser{mgr: jwtMgr}

	// Use a unique per-test run suffix so repeated runs don't collide.
	// All rows are cleaned up at the end regardless.
	testRun := e2eUnique("e2e")

	// ---- step 1: seed identity (two users, different wards) -----------

	ward3A := "WARD-3A"
	ward9Z := "WARD-9Z"

	var (
		user3AID string
		user9ZID string
	)

	// Create WARD-3A nurse.
	err = pool.QueryRow(ctx,
		`INSERT INTO medisync.users (username, password_hash, display_name, role, ward_ids, active)
		 VALUES ($1, '$2a$10$aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa', 'Nurse 3A', 'NURSE', $2, true)
		 RETURNING id`,
		testRun+"-nurse-3a", []string{ward3A},
	).Scan(&user3AID)
	if err != nil {
		t.Fatalf("seed nurse-3a: %v", err)
	}

	// Create WARD-9Z nurse.
	err = pool.QueryRow(ctx,
		`INSERT INTO medisync.users (username, password_hash, display_name, role, ward_ids, active)
		 VALUES ($1, '$2a$10$aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa', 'Nurse 9Z', 'NURSE', $2, true)
		 RETURNING id`,
		testRun+"-nurse-9z", []string{ward9Z},
	).Scan(&user9ZID)
	if err != nil {
		t.Fatalf("seed nurse-9z: %v", err)
	}

	// Issue real JWTs (NOT fake parsers — this is the point of F1 closure).
	user3A, err := identity.NewStore(pool).GetByID(ctx, user3AID)
	if err != nil || user3A == nil {
		t.Fatalf("GetByID 3A: %v / nil=%v", err, user3A == nil)
	}

	user9Z, err := identity.NewStore(pool).GetByID(ctx, user9ZID)
	if err != nil || user9Z == nil {
		t.Fatalf("GetByID 9Z: %v / nil=%v", err, user9Z == nil)
	}

	jwt3A, _, err := jwtMgr.Issue(user3A)
	if err != nil {
		t.Fatalf("Issue JWT 3A: %v", err)
	}
	jwt9Z, _, err := jwtMgr.Issue(user9Z)
	if err != nil {
		t.Fatalf("Issue JWT 9Z: %v", err)
	}

	t.Log("step 1 PASS: two users seeded, real JWTs issued")

	// ---- step 2: catalog — create a drug -------------------------------

	drugStore := catalog.NewStore(pool, auditWriter)
	drug, err := drugStore.Create(ctx, catalog.Drug{
		Code:     testRun + "-PARA500",
		Name:     "Paracetamol 500 mg",
		Form:     "tablet",
		Strength: "500 mg",
		Unit:     "tablet",
	})
	if err != nil {
		t.Fatalf("create drug: %v", err)
	}
	if drug == nil {
		t.Fatal("created drug is nil")
	}
	if !drug.Active {
		t.Error("drug should be active by default")
	}

	t.Logf("step 2 PASS: drug created id=%s code=%s", drug.ID, drug.Code)

	// ---- step 3: inventory — slot + assign + refill --------------------

	slotCode := testRun + "-S1"
	cabinetID := "CAB-01"

	// Create a bare slot via raw SQL (inventory.Store has no CreateSlot method).
	var slotID string
	err = pool.QueryRow(ctx,
		`INSERT INTO medisync.slot (cabinet_id, code, capacity, quantity, low_threshold)
		 VALUES ($1, $2, 0, 0, 0)
		 RETURNING id`,
		cabinetID, slotCode,
	).Scan(&slotID)
	if err != nil {
		t.Fatalf("seed slot: %v", err)
	}

	// Assign the drug to the slot.
	invStore := inventory.NewStore(pool, auditWriter)
	slot, err := invStore.AssignDrug(ctx, slotID, drug.ID, drug.Code, drug.Name, 100, 5)
	if err != nil {
		t.Fatalf("assign drug: %v", err)
	}
	if slot == nil {
		t.Fatal("slot is nil after assign")
	}
	if slot.DrugID != drug.ID {
		t.Errorf("slot DrugID = %q, want %q", slot.DrugID, drug.ID)
	}
	if slot.Capacity != 100 {
		t.Errorf("slot Capacity = %d, want 100", slot.Capacity)
	}

	// Refill stock.
	refilled, err := invStore.Refill(ctx, slotID, 50, nil)
	if err != nil {
		t.Fatalf("refill slot: %v", err)
	}
	if refilled == nil {
		t.Fatal("refilled slot is nil")
	}
	if refilled.Quantity != 50 {
		t.Errorf("refilled Quantity = %d, want 50", refilled.Quantity)
	}

	t.Logf("step 3 PASS: slot %s assigned drug %s, refilled to qty=%d",
		slotCode, drug.Code, refilled.Quantity)

	// ---- step 4: dispensing intake — insert READY prescription ---------

	dispStore := dispensing.NewStore(pool)
	prescriptionID := testRun + "-RX"
	now := time.Now()
	pr := dispensing.Prescription{
		PrescriptionID: prescriptionID,
		SourceSystem:   "e2e-test",
		HN:             "HN-E2E-001",
		PatientName:    "E2E Test Patient",
		WardID:         ward3A,
		IssuedAt:       &now,
		Items: []dispensing.Item{
			{DrugCode: drug.Code, DrugName: drug.Name, Quantity: 2, DosageText: "twice daily"},
		},
	}

	inserted, err := dispStore.Insert(ctx, pr)
	if err != nil {
		t.Fatalf("insert prescription: %v", err)
	}
	if !inserted {
		t.Fatal("prescription insert should succeed (first time)")
	}

	// Idempotency check — duplicate insert must return false, not error.
	dupInserted, err := dispStore.Insert(ctx, pr)
	if err != nil {
		t.Fatalf("duplicate insert: %v", err)
	}
	if dupInserted {
		t.Error("duplicate insert should return false (deduplicated)")
	}

	// Read back the stored row to get its internal UUID.
	var storedID string
	var storedState string
	err = pool.QueryRow(ctx,
		`SELECT id, state FROM medisync.prescription
		 WHERE prescription_id = $1 AND source_system = $2`,
		prescriptionID, "e2e-test",
	).Scan(&storedID, &storedState)
	if err != nil {
		t.Fatalf("read back prescription: %v", err)
	}
	if storedState != "READY" {
		t.Errorf("prescription state after insert = %q, want READY", storedState)
	}

	t.Logf("step 4 PASS: prescription %s inserted as READY (internal id=%s)", prescriptionID, storedID)

	// ---- step 5: dispense via real handler (READY → DISPENSING) --------

	// Wire the dispensing handler with a real pool, real store, real JWT parser,
	// and real audit writer — no fakes anywhere.
	dispHandler := dispensing.NewDispensingServer(dispStore, pool, parser, auditWriter)

	// Build the Connect request with the WARD-3A user's JWT.
	req := connect.NewRequest(&dispensingv1.DispenseRequest{
		PrescriptionId: prescriptionID,
		TraceId:        testRun + "-trace",
	})
	req.Header().Set("Authorization", "Bearer "+jwt3A)

	resp, err := dispHandler.Dispense(ctx, req)
	if err != nil {
		t.Fatalf("Dispense: %v", err)
	}
	if resp == nil || resp.Msg == nil || resp.Msg.Prescription == nil {
		t.Fatal("Dispense response has no prescription")
	}

	updatedRx := resp.Msg.Prescription
	if updatedRx.State != dispensingv1.PrescriptionState_PRESCRIPTION_STATE_DISPENSING {
		t.Errorf("state after dispense = %v, want DISPENSING", updatedRx.State)
	}

	t.Logf("step 5a PASS: dispense transitioned READY → DISPENSING")

	// ---- step 5b: assert audit entry was written -----------------------

	var auditCount int
	err = pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM medisync.audit_log
		 WHERE action = 'prescription.dispense.requested'
		   AND entity_id = $1`,
		prescriptionID,
	).Scan(&auditCount)
	if err != nil {
		t.Fatalf("count audit: %v", err)
	}
	if auditCount != 1 {
		t.Errorf("expected 1 audit entry for dispense, got %d", auditCount)
	}

	t.Logf("step 5b PASS: audit entry written for dispense.requested")

	// ---- step 5c: assert outbox row in same transaction ----------------

	var outboxCount int
	err = pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM medisync.outbox
		 WHERE subject = 'medisync.dispense.requested'
		   AND payload ->> 'prescriptionId' = $1`,
		prescriptionID,
	).Scan(&outboxCount)
	if err != nil {
		t.Fatalf("count outbox: %v", err)
	}
	if outboxCount != 1 {
		t.Errorf("expected 1 outbox row for dispense.requested, got %d", outboxCount)
	}

	// Verify the outbox payload is valid proto JSON.
	var payloadRaw []byte
	err = pool.QueryRow(ctx,
		`SELECT payload FROM medisync.outbox
		 WHERE subject = 'medisync.dispense.requested'
		   AND payload ->> 'prescriptionId' = $1
		 LIMIT 1`,
		prescriptionID,
	).Scan(&payloadRaw)
	if err != nil {
		t.Fatalf("read outbox payload: %v", err)
	}
	var outboxEvent eventsv1.DispenseRequested
	if err := protojson.Unmarshal(payloadRaw, &outboxEvent); err != nil {
		t.Fatalf("unmarshal outbox payload: %v", err)
	}
	if outboxEvent.GetPrescriptionId() != prescriptionID {
		t.Errorf("outbox prescription_id = %q, want %q",
			outboxEvent.GetPrescriptionId(), prescriptionID)
	}

	t.Logf("step 5c PASS: outbox dispense.requested row present with valid proto JSON")

	// ---- step 6: ward isolation — WARD-9Z access gets CodeNotFound -----

	req9Z := connect.NewRequest(&dispensingv1.GetPrescriptionRequest{
		Id: storedID,
	})
	req9Z.Header().Set("Authorization", "Bearer "+jwt9Z)

	_, err9Z := dispHandler.GetPrescription(ctx, req9Z)
	if err9Z == nil {
		t.Fatal("expected error for cross-ward access, got nil")
	}

	connectErr, ok := err9Z.(*connect.Error)
	if !ok {
		t.Fatalf("expected *connect.Error, got %T: %v", err9Z, err9Z)
	}
	if connectErr.Code() != connect.CodeNotFound {
		t.Errorf("cross-ward error code = %v, want CodeNotFound (closes F1)", connectErr.Code())
	}
	// The message must not distinguish between "doesn't exist" and "wrong ward".
	if connectErr.Message() != "prescription not found" {
		t.Errorf("cross-ward message = %q, want 'prescription not found'", connectErr.Message())
	}

	t.Logf("step 6 PASS: WARD-9Z JWT rejected with CodeNotFound (ward isolation verified with real JWT)")

	// ---- also verify: own-ward GetPrescription works (positive control)

	reqOwn := connect.NewRequest(&dispensingv1.GetPrescriptionRequest{
		Id: storedID,
	})
	reqOwn.Header().Set("Authorization", "Bearer "+jwt3A)

	ownResp, err := dispHandler.GetPrescription(ctx, reqOwn)
	if err != nil {
		t.Fatalf("own-ward GetPrescription: %v", err)
	}
	if ownResp.Msg.Prescription.State != dispensingv1.PrescriptionState_PRESCRIPTION_STATE_DISPENSING {
		t.Errorf("own-ward state = %v, want DISPENSING", ownResp.Msg.Prescription.State)
	}

	t.Log("positive control PASS: own-ward GetPrescription returns DISPENSING prescription")

	// ---- cleanup (deferred so mid-test t.Fatalf won't leak rows) -------

	t.Cleanup(func() {
		// Remove all test rows so repeated runs don't collide on unique constraints.
		// Order matters: FK-style references (none enforced, but be safe).
		pool.Exec(context.Background(), `DELETE FROM medisync.outbox WHERE payload ->> 'prescriptionId' = $1`, prescriptionID)
		pool.Exec(context.Background(), `DELETE FROM medisync.prescription WHERE prescription_id = $1`, prescriptionID)
		pool.Exec(context.Background(), `DELETE FROM medisync.slot WHERE code = $1`, slotCode)
		pool.Exec(context.Background(), `DELETE FROM medisync.drug WHERE code = $1`, drug.Code)
		pool.Exec(context.Background(), `DELETE FROM medisync.users WHERE id IN ($1, $2)`, user3AID, user9ZID)
		pool.Exec(context.Background(), `DELETE FROM medisync.audit_log WHERE entity_id = $1`, prescriptionID)
	})
}
