//go:build integration

package dispensing

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	server "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"google.golang.org/protobuf/encoding/protojson"

	eventsv1 "github.com/adm-chura3inter/medisync/services/core/internal/gen/medisync/events/v1"
	"github.com/adm-chura3inter/medisync/services/core/internal/platform/natsx"
	"github.com/adm-chura3inter/medisync/services/core/internal/testutil"
	"github.com/adm-chura3inter/medisync/services/core/internal/vending"
)

func TestDispensingToVendingPipeline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	pool := integrationPool(t)
	store := NewStore(pool)
	prescriptionID := uniqueID(t, "RX-PIPE")
	drugCode := uniqueID(t, "DRUG")
	slotCode := uniqueID(t, "SLOT")
	var projectID string
	if err := pool.QueryRow(ctx, `SELECT id FROM medisync.projects WHERE code='0001'`).Scan(&projectID); err != nil {
		t.Fatalf("load project 0001: %v", err)
	}
	kioskCode := seedPipelineKiosk(t, ctx, pool, projectID)
	operatorID := seedPipelineOperator(t, ctx, pool, projectID)
	pr := seedPipelinePrescription(t, ctx, pool, store, prescriptionID, drugCode, projectID)
	seedPipelineSlot(t, ctx, pool, kioskCode, slotCode, drugCode, projectID)

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin prepare: %v", err)
	}
	record, err := store.PrepareTransaction(ctx, tx, pr, kioskCode, projectID, "trace-pipeline")
	if err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("prepare transaction: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit prepare: %v", err)
	}
	dispenseID := record.ID
	requested := transactionRequestedEvent(record)
	payload, err := protojson.Marshal(requested)
	if err != nil {
		t.Fatalf("marshal dispense.requested: %v", err)
	}
	tx, err = pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin queue: %v", err)
	}
	if err := store.QueueTransaction(ctx, tx, record, operatorID, "Pipeline Pharmacist", payload); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("queue transaction: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit queue: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM medisync.outbox WHERE created_by=$1`, operatorID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM medisync.dispense_transaction WHERE id=$1`, dispenseID)
	})

	js, nc := startEmbeddedJetStream(t, ctx)
	events := subscribeToPipelineEvents(t, nc)

	dispenseRequests := make(chan vending.DispenseRequest, 1)
	vendingURL, closeVending := testutil.StartHTTPServer(map[string]http.HandlerFunc{
		"GET /api/v1/health": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"status":"ok"}`)
		},
		"POST /api/v1/vending/drugDispenser": func(w http.ResponseWriter, r *http.Request) {
			if got := r.Header.Get("Authorization"); got != "Bearer pipeline-token" {
				http.Error(w, "missing bearer token", http.StatusUnauthorized)
				return
			}
			var request vending.DispenseRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			dispenseRequests <- request
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(vending.DispenseResponse{
				OK: 1,
				Data: vending.DispenseData{
					PrescriptionNo: request.Prescription,
					Status:         "success",
					Door:           1,
					Steps: []vending.DispenseStep{
						{Phase: "dispense", Layer: 1, Success: true},
					},
				},
			})
		},
	})
	defer closeVending()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	completionConsumer := NewCompletionConsumer(js, pool, store, nil, logger)
	stopCompletion, err := completionConsumer.Start(ctx)
	if err != nil {
		t.Fatalf("start completion consumer: %v", err)
	}
	defer stopCompletion()

	fulfillmentConsumer := vending.NewRoutedConsumer(js, pipelineRouter{client: vending.NewClient(vendingURL, "pipeline-token")}, store, nil, logger)
	stopFulfillment, err := fulfillmentConsumer.Start(ctx)
	if err != nil {
		t.Fatalf("start fulfillment consumer: %v", err)
	}
	defer stopFulfillment()

	dispenseConsumer := NewDispenseRequestedConsumer(js, logger)
	stopDispense, err := dispenseConsumer.Start(ctx)
	if err != nil {
		t.Fatalf("start dispense.requested consumer: %v", err)
	}
	defer stopDispense()

	if _, err := js.Publish(ctx, natsx.SubjectDispenseRequested, payload); err != nil {
		t.Fatalf("publish dispense.requested: %v", err)
	}

	observed := collectPipelineEvents(t, ctx, events, 5)
	assertPipelineEventOrder(t, observed)
	assertPipelineEventPayloads(t, observed, dispenseID, prescriptionID, slotCode)

	select {
	case request := <-dispenseRequests:
		wantRequestID := prescriptionID + ":" + record.Items[0].Allocations[0].ID
		if request.Prescription != wantRequestID {
			t.Errorf("vending prescription = %q, want %q", request.Prescription, wantRequestID)
		}
		if len(request.Items) != 1 || request.Items[0].Quantity != 2 {
			t.Errorf("vending items = %+v, want one item with quantity 2", request.Items)
		}
	case <-ctx.Done():
		t.Fatal("vending agent did not receive a dispense request")
	}

	var state string
	var quantity int
	if err := pool.QueryRow(ctx,
		`SELECT state FROM medisync.prescription WHERE prescription_id = $1`,
		prescriptionID,
	).Scan(&state); err != nil {
		t.Fatalf("read prescription state: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`SELECT quantity FROM medisync.slot WHERE cabinet_id = $1 AND code = $2`,
		kioskCode, slotCode,
	).Scan(&quantity); err != nil {
		t.Fatalf("read slot quantity: %v", err)
	}
	if state != string(StateDispensed) {
		t.Errorf("prescription state = %q, want %q", state, StateDispensed)
	}
	if quantity != 8 {
		t.Errorf("slot quantity = %d, want 8", quantity)
	}
	var txStatus string
	var hardwareSuccess bool
	var reserved int
	if err := pool.QueryRow(ctx,
		`SELECT d.status, a.hardware_success, s.reserved_quantity
		   FROM medisync.dispense_transaction d
		   JOIN medisync.dispense_allocation a ON a.dispense_id=d.id
		   JOIN medisync.slot s ON s.id=a.slot_id
		  WHERE d.id=$1`, dispenseID).Scan(&txStatus, &hardwareSuccess, &reserved); err != nil {
		t.Fatalf("read transaction tracking: %v", err)
	}
	if txStatus != "DISPENSED" || !hardwareSuccess || reserved != 0 {
		t.Errorf("transaction status=%s hardware_success=%v reserved=%d", txStatus, hardwareSuccess, reserved)
	}

	slotID := record.Items[0].Allocations[0].SlotID
	history, _, historyTotal, err := store.ListTransactions(ctx, TransactionFilter{
		ProjectID: projectID, KioskCode: kioskCode, SlotID: slotID, DrugCode: drugCode, PageSize: 7,
	})
	if err != nil {
		t.Fatalf("list slot drug history: %v", err)
	}
	if historyTotal != 1 || len(history) != 1 || history[0].ID != dispenseID {
		t.Errorf("slot drug history total=%d records=%v, want dispense %s", historyTotal, history, dispenseID)
	}
	wrongDrugHistory, _, wrongDrugTotal, err := store.ListTransactions(ctx, TransactionFilter{
		ProjectID: projectID, KioskCode: kioskCode, SlotID: slotID, DrugCode: "NOT-THIS-DRUG", PageSize: 7,
	})
	if err != nil {
		t.Fatalf("list mismatched slot drug history: %v", err)
	}
	if wrongDrugTotal != 0 || len(wrongDrugHistory) != 0 {
		t.Errorf("mismatched slot drug history total=%d records=%v, want empty", wrongDrugTotal, wrongDrugHistory)
	}
}

func TestListTransactionsFiltersExactKioskSlotAndDrug(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool := integrationPool(t)
	store := NewStore(pool)
	var projectID string
	if err := pool.QueryRow(ctx, `SELECT id FROM medisync.projects WHERE code='0001'`).Scan(&projectID); err != nil {
		t.Fatalf("load project 0001: %v", err)
	}
	kioskCode := seedPipelineKiosk(t, ctx, pool, projectID)
	prescriptionID := uniqueID(t, "RX-HISTORY")
	drugCode := uniqueID(t, "DRUG-HISTORY")
	slotCode := uniqueID(t, "SLOT-HISTORY")
	prescription := seedPipelinePrescription(t, ctx, pool, store, prescriptionID, drugCode, projectID)
	seedPipelineSlot(t, ctx, pool, kioskCode, slotCode, drugCode, projectID)

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin prepare history transaction: %v", err)
	}
	record, err := store.PrepareTransaction(ctx, tx, prescription, kioskCode, projectID, uniqueID(t, "trace-history"))
	if err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("prepare history transaction: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit history transaction: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM medisync.dispense_transaction WHERE id=$1`, record.ID)
	})

	slotID := record.Items[0].Allocations[0].SlotID
	cabinetHistory, _, cabinetTotal, err := store.ListTransactions(ctx, TransactionFilter{
		ProjectID: projectID, KioskCode: kioskCode, DrugCode: drugCode, PageSize: 7,
	})
	if err != nil {
		t.Fatalf("list kiosk drug history: %v", err)
	}
	if cabinetTotal != 1 || len(cabinetHistory) != 1 || cabinetHistory[0].ID != record.ID {
		t.Fatalf("kiosk drug history total=%d records=%v, want transaction %s", cabinetTotal, cabinetHistory, record.ID)
	}
	otherKioskHistory, _, otherKioskTotal, err := store.ListTransactions(ctx, TransactionFilter{
		ProjectID: projectID, KioskCode: "99999999", DrugCode: drugCode, PageSize: 7,
	})
	if err != nil {
		t.Fatalf("list other kiosk drug history: %v", err)
	}
	if otherKioskTotal != 0 || len(otherKioskHistory) != 0 {
		t.Fatalf("other kiosk history total=%d records=%v, want empty", otherKioskTotal, otherKioskHistory)
	}

	history, _, total, err := store.ListTransactions(ctx, TransactionFilter{
		ProjectID: projectID, KioskCode: kioskCode, SlotID: slotID, DrugCode: drugCode, PageSize: 7,
	})
	if err != nil {
		t.Fatalf("list exact slot drug history: %v", err)
	}
	if total != 1 || len(history) != 1 || history[0].ID != record.ID {
		t.Fatalf("exact history total=%d records=%v, want transaction %s", total, history, record.ID)
	}

	mismatch, _, mismatchTotal, err := store.ListTransactions(ctx, TransactionFilter{
		ProjectID: projectID, KioskCode: kioskCode, SlotID: slotID, DrugCode: "NOT-THIS-DRUG", PageSize: 7,
	})
	if err != nil {
		t.Fatalf("list mismatched slot drug history: %v", err)
	}
	if mismatchTotal != 0 || len(mismatch) != 0 {
		t.Fatalf("mismatched history total=%d records=%v, want empty", mismatchTotal, mismatch)
	}
}

type pipelineRouter struct{ client vending.Client }

func (r pipelineRouter) ClientFor(string) (vending.Client, error) { return r.client, nil }

func seedPipelinePrescription(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	store *Store,
	prescriptionID string,
	drugCode string,
	projectID string,
) *PrescriptionRow {
	t.Helper()
	inserted, err := store.Insert(ctx, Prescription{
		PrescriptionID: prescriptionID,
		SourceSystem:   "pipeline-test",
		ProjectID:      projectID,
		HN:             "HN-PIPELINE",
		PatientName:    "Pipeline Test",
		WardID:         "WARD-PIPELINE",
		Items: []Item{
			{DrugCode: drugCode, DrugName: "Pipeline Drug", Quantity: 2},
		},
	})
	if err != nil || !inserted {
		t.Fatalf("seed prescription: inserted=%v err=%v", inserted, err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(),
			`DELETE FROM medisync.prescription WHERE prescription_id = $1 AND source_system = 'pipeline-test'`,
			prescriptionID,
		)
	})
	row, err := store.GetByPrescriptionID(ctx, prescriptionID, "pipeline-test")
	if err != nil || row == nil {
		t.Fatalf("load seeded prescription: row=%v err=%v", row, err)
	}
	return row
}

func seedPipelineSlot(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	kioskCode string,
	slotCode string,
	drugCode string,
	projectID string,
) {
	t.Helper()
	if _, err := pool.Exec(ctx,
		`INSERT INTO medisync.slot
		   (cabinet_id, code, drug_id, drug_code, drug_name, capacity, quantity, low_threshold,
		    project_id, door_no, hardware_layer, channel_start, channel_end)
		 VALUES ($1, $2, $3, $3, 'Pipeline Drug', 20, 10, 2, $4, 1, 2, 3, 3)`,
		kioskCode, slotCode, drugCode, projectID,
	); err != nil {
		t.Fatalf("seed inventory slot: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO medisync.slot_batch (slot_id,lot_number,expiry_date,quantity)
		 SELECT id,'PIPELINE-LOT',now()+interval '1 year',quantity
		   FROM medisync.slot WHERE cabinet_id=$1 AND code=$2`, kioskCode, slotCode); err != nil {
		t.Fatalf("seed inventory batch: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(),
			`DELETE FROM medisync.slot WHERE cabinet_id = $1 AND code = $2`,
			kioskCode, slotCode,
		)
	})
}

func seedPipelineKiosk(t *testing.T, ctx context.Context, pool *pgxpool.Pool, projectID string) string {
	t.Helper()
	var code string
	displayName := uniqueID(t, "Pipeline Kiosk")
	if err := pool.QueryRow(ctx,
		`INSERT INTO medisync.kiosks (display_name,pin_hash,active,project_id)
		 VALUES ($1,'integration-only',true,$2) RETURNING code`, displayName, projectID).Scan(&code); err != nil {
		t.Fatalf("seed kiosk: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM medisync.kiosks WHERE code=$1`, code)
	})
	return code
}

func seedPipelineOperator(t *testing.T, ctx context.Context, pool *pgxpool.Pool, projectID string) string {
	t.Helper()
	var id string
	username := uniqueID(t, "pipeline-user")
	if err := pool.QueryRow(ctx,
		`INSERT INTO medisync.users (username,password_hash,display_name,role,ward_ids,project_id,active)
		 VALUES ($1,'integration-only','Pipeline Pharmacist','PHARMACIST','{WARD-PIPELINE}',$2,true)
		 RETURNING id`, username, projectID).Scan(&id); err != nil {
		t.Fatalf("seed operator: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM medisync.users WHERE id=$1`, id)
	})
	return id
}

func startEmbeddedJetStream(t *testing.T, ctx context.Context) (jetstream.JetStream, *nats.Conn) {
	t.Helper()
	ns, err := server.NewServer(&server.Options{
		JetStream: true,
		StoreDir:  t.TempDir(),
		Port:      -1,
		NoLog:     true,
		NoSigs:    true,
	})
	if err != nil {
		t.Fatalf("create embedded NATS server: %v", err)
	}
	go ns.Start()
	if !ns.ReadyForConnections(5 * time.Second) {
		ns.Shutdown()
		t.Fatal("embedded NATS server did not become ready")
	}
	t.Cleanup(func() {
		ns.Shutdown()
		ns.WaitForShutdown()
	})

	nc, err := nats.Connect(ns.ClientURL())
	if err != nil {
		t.Fatalf("connect embedded NATS: %v", err)
	}
	t.Cleanup(nc.Close)

	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatalf("create JetStream context: %v", err)
	}
	if err := natsx.EnsureStreams(ctx, js); err != nil {
		t.Fatalf("ensure JetStream streams: %v", err)
	}
	return js, nc
}

type pipelineEvent struct {
	subject string
	payload []byte
}

func subscribeToPipelineEvents(t *testing.T, nc *nats.Conn) <-chan pipelineEvent {
	t.Helper()
	events := make(chan pipelineEvent, 8)
	if _, err := nc.Subscribe("medisync.>", func(msg *nats.Msg) {
		events <- pipelineEvent{subject: msg.Subject, payload: append([]byte(nil), msg.Data...)}
	}); err != nil {
		t.Fatalf("subscribe pipeline events: %v", err)
	}
	if err := nc.Flush(); err != nil {
		t.Fatalf("flush pipeline subscription: %v", err)
	}
	return events
}

func collectPipelineEvents(t *testing.T, ctx context.Context, events <-chan pipelineEvent, count int) []pipelineEvent {
	t.Helper()
	observed := make([]pipelineEvent, 0, count)
	for len(observed) < count {
		select {
		case event := <-events:
			observed = append(observed, event)
		case <-ctx.Done():
			t.Fatalf("timed out after events %v", pipelineSubjects(observed))
		}
	}
	return observed
}

func assertPipelineEventOrder(t *testing.T, events []pipelineEvent) {
	t.Helper()
	want := []string{
		natsx.SubjectDispenseRequested,
		natsx.SubjectFulfillmentRequested,
		natsx.SubjectDispenseCompleted,
		natsx.SubjectStockChanged,
		natsx.SubjectPrintRequested,
	}
	got := pipelineSubjects(events)
	if !slices.Equal(got, want) {
		t.Fatalf("event order = %v, want %v", got, want)
	}
}

func assertPipelineEventPayloads(t *testing.T, events []pipelineEvent, dispenseID, prescriptionID, slotCode string) {
	t.Helper()
	var requested eventsv1.DispenseRequested
	if err := protojson.Unmarshal(events[0].payload, &requested); err != nil {
		t.Fatalf("decode dispense.requested: %v", err)
	}
	if requested.GetDispenseId() != dispenseID || requested.GetPrescriptionId() != prescriptionID ||
		requested.GetSlotCode() != slotCode || requested.GetQuantity() != 2 {
		t.Errorf("dispense.requested = %+v", &requested)
	}

	var fulfillment eventsv1.FulfillmentRequested
	if err := protojson.Unmarshal(events[1].payload, &fulfillment); err != nil {
		t.Fatalf("decode fulfillment.requested: %v", err)
	}
	if fulfillment.GetFulfillmentId() != dispenseID || fulfillment.GetPrescriptionId() != prescriptionID ||
		fulfillment.GetTraceId() != "trace-pipeline" {
		t.Errorf("fulfillment.requested = %+v", &fulfillment)
	}

	var completed eventsv1.DispenseCompleted
	if err := protojson.Unmarshal(events[2].payload, &completed); err != nil {
		t.Fatalf("decode dispense.completed: %v", err)
	}
	if completed.GetDispenseId() != dispenseID || completed.GetPrescriptionId() != prescriptionID ||
		completed.GetCompletedAt() == nil || completed.GetTraceId() != "trace-pipeline" {
		t.Errorf("dispense.completed = %+v", &completed)
	}

	var stockChanged eventsv1.StockChanged
	if err := protojson.Unmarshal(events[3].payload, &stockChanged); err != nil {
		t.Fatalf("decode stock.changed: %v", err)
	}
	if stockChanged.GetReason() != eventsv1.StockChangeReason_STOCK_CHANGE_REASON_DISPENSE ||
		stockChanged.GetTraceId() != "trace-pipeline" {
		t.Errorf("stock.changed = %+v", &stockChanged)
	}

	var printRequested eventsv1.PrintRequested
	if err := protojson.Unmarshal(events[4].payload, &printRequested); err != nil {
		t.Fatalf("decode print.requested: %v", err)
	}
	if printRequested.GetPrintId() != dispenseID || printRequested.GetPrescriptionId() != prescriptionID ||
		printRequested.GetTraceId() != "trace-pipeline" {
		t.Errorf("print.requested = %+v", &printRequested)
	}
}

func pipelineSubjects(events []pipelineEvent) []string {
	subjects := make([]string, len(events))
	for i, event := range events {
		subjects[i] = event.subject
	}
	return subjects
}
