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
	dispenseID := uniqueID(t, "DISP-PIPE")
	drugCode := uniqueID(t, "DRUG")
	cabinetID := uniqueID(t, "CAB")
	slotCode := uniqueID(t, "SLOT")
	seedPipelinePrescription(t, ctx, pool, store, prescriptionID, drugCode)
	seedPipelineSlot(t, ctx, pool, cabinetID, slotCode, drugCode)

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

	fulfillmentConsumer := vending.NewConsumer(js, vending.NewClient(vendingURL, "pipeline-token"), nil, logger)
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

	requested := &eventsv1.DispenseRequested{
		DispenseId:     dispenseID,
		PrescriptionId: prescriptionID,
		SlotCode:       slotCode,
		Quantity:       2,
		TraceId:        "trace-pipeline",
	}
	payload, err := protojson.Marshal(requested)
	if err != nil {
		t.Fatalf("marshal dispense.requested: %v", err)
	}
	if _, err := js.Publish(ctx, natsx.SubjectDispenseRequested, payload); err != nil {
		t.Fatalf("publish dispense.requested: %v", err)
	}

	observed := collectPipelineEvents(t, ctx, events, 5)
	assertPipelineEventOrder(t, observed)
	assertPipelineEventPayloads(t, observed, dispenseID, prescriptionID, slotCode)

	select {
	case request := <-dispenseRequests:
		if request.Prescription != prescriptionID {
			t.Errorf("vending prescription = %q, want %q", request.Prescription, prescriptionID)
		}
		if len(request.Items) != 1 || request.Items[0].Quantity != 1 {
			t.Errorf("vending items = %+v, want one item with quantity 1", request.Items)
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
		cabinetID, slotCode,
	).Scan(&quantity); err != nil {
		t.Fatalf("read slot quantity: %v", err)
	}
	if state != string(StateDispensed) {
		t.Errorf("prescription state = %q, want %q", state, StateDispensed)
	}
	if quantity != 8 {
		t.Errorf("slot quantity = %d, want 8", quantity)
	}
}

func seedPipelinePrescription(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	store *Store,
	prescriptionID string,
	drugCode string,
) {
	t.Helper()
	inserted, err := store.Insert(ctx, Prescription{
		PrescriptionID: prescriptionID,
		SourceSystem:   "pipeline-test",
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
	if _, err := pool.Exec(ctx,
		`UPDATE medisync.prescription SET state = 'DISPENSING' WHERE prescription_id = $1`,
		prescriptionID,
	); err != nil {
		t.Fatalf("set prescription DISPENSING: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(),
			`DELETE FROM medisync.prescription WHERE prescription_id = $1 AND source_system = 'pipeline-test'`,
			prescriptionID,
		)
	})
}

func seedPipelineSlot(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	cabinetID string,
	slotCode string,
	drugCode string,
) {
	t.Helper()
	if _, err := pool.Exec(ctx,
		`INSERT INTO medisync.slot
		   (cabinet_id, code, drug_id, drug_code, drug_name, capacity, quantity, low_threshold, project_id)
		 VALUES ($1, $2, $3, $3, 'Pipeline Drug', 20, 10, 2, '00000000-0000-0000-0000-000000000001')`,
		cabinetID, slotCode, drugCode,
	); err != nil {
		t.Fatalf("seed inventory slot: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(),
			`DELETE FROM medisync.slot WHERE cabinet_id = $1 AND code = $2`,
			cabinetID, slotCode,
		)
	})
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
