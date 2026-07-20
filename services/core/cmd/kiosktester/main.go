// Command kiosktester drives the kiosk withdraw/dispense flow end-to-end
// against a running core, exactly the way real hardware would.
//
// Modes:
//
//	create  — publish rx.prescription.created to NATS (the real hospital
//	          producer path). Items are drawn from a chosen cabinet's real
//	          slots so the kiosk cart resolves drug positions. Prints a
//	          scannable prescription_id (the "sticker").
//	confirm — log in as staff and call DispensingService/Dispense for a
//	          prescription_id, exactly like a user scanning to confirm at the
//	          cabinet face, then poll GetPrescription until DISPENSED/FAILED.
//	flow    — create then confirm back-to-back = full E2E like hardware.
//	serve   — a tiny local web console: change values in a form and click to
//	          run create / confirm / flow. Does the NATS + core work
//	          server-side (a browser cannot reach NATS directly).
//
// Dev/testing only. Reuses the same wire contracts as production.
//
//	go run ./cmd/kiosktester -mode=serve
//	go run ./cmd/kiosktester -mode=flow -cabinet=CAB1
package main

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"

	eventsv1 "github.com/adm-chura3inter/medisync/services/core/internal/gen/medisync/events/v1"
	"github.com/adm-chura3inter/medisync/services/core/internal/platform/natsx"
)

func main() {
	cfg := parseFlags()
	if err := run(cfg); err != nil {
		log.Fatal(err)
	}
}

type config struct {
	mode      string
	addr      string
	core      string
	natsURL   string
	kioskCode string
	kioskPIN  string
	staff     string
	staffPass string
	staffRole string
	cabinet   string
	ward      string
	hn        string
	patient   string
	source    string
	drugsFlag string
	items     int
	fixedID   string
	timeout   time.Duration
}

func parseFlags() config {
	var c config
	flag.StringVar(&c.mode, "mode", "create", "create | confirm | flow | serve")
	flag.StringVar(&c.addr, "addr", ":8899", "serve mode: listen address")
	flag.StringVar(&c.core, "core", "http://localhost:8080", "core Connect API base URL")
	flag.StringVar(&c.natsURL, "nats", "nats://localhost:4222", "NATS server URL")
	flag.StringVar(&c.kioskCode, "kiosk-code", "DEMO-K1", "kiosk code (for ListSlots auth)")
	flag.StringVar(&c.kioskPIN, "kiosk-pin", "123456", "kiosk PIN")
	flag.StringVar(&c.staff, "staff", "pharmacist", "staff username for the confirm step (pharmacist | nurse | refiller)")
	flag.StringVar(&c.staffPass, "staff-pass", "", "staff password (default: demo-<role>-2026)")
	flag.StringVar(&c.staffRole, "role", "auto", "label for the simulated scan: PHARMACIST | NURSE | REFILLER (auto = infer from username)")
	flag.StringVar(&c.cabinet, "cabinet", "", "cabinet id to draw drugs from (empty = any)")
	flag.StringVar(&c.ward, "ward", "WARD-3A", "ward id stamped on the prescription")
	flag.StringVar(&c.hn, "hn", "HN900001", "patient hospital number")
	flag.StringVar(&c.patient, "patient", "ผู้ป่วยทดสอบ Kiosk", "patient name")
	flag.StringVar(&c.source, "source", "kiosktester", "source_system (idempotency key with prescription_id)")
	flag.StringVar(&c.drugsFlag, "drugs", "", "explicit items CODE:qty,CODE:qty (default: auto-pick from cabinet)")
	flag.IntVar(&c.items, "items", 2, "number of drugs to auto-pick when -drugs is empty")
	flag.StringVar(&c.fixedID, "id", "", "prescription_id: fixed id to create, or the id to confirm")
	flag.DurationVar(&c.timeout, "timeout", 25*time.Second, "how long to poll for a terminal dispense state")
	flag.Parse()
	return c
}

func run(c config) error {
	switch c.mode {
	case "serve":
		return serve(c)
	case "create":
		res := createPrescription(context.Background(), c, c.fixedID)
		fmt.Print(res.text())
		return res.Err
	case "confirm":
		if c.fixedID == "" {
			return fmt.Errorf("-mode=confirm requires -id=<prescription_id>")
		}
		res := confirmPrescription(context.Background(), c, c.fixedID)
		fmt.Print(res.text())
		return res.Err
	case "flow":
		cr := createPrescription(context.Background(), c, c.fixedID)
		fmt.Print(cr.text())
		if cr.Err != nil {
			return cr.Err
		}
		time.Sleep(1500 * time.Millisecond)
		cf := confirmPrescription(context.Background(), c, cr.ID)
		fmt.Print(cf.text())
		return cf.Err
	default:
		return fmt.Errorf("unknown -mode=%q (want create|confirm|flow|serve)", c.mode)
	}
}

// ── Result types (shared by CLI + web) ───────────────────────────────

type result struct {
	OK    bool     `json:"ok"`
	ID    string   `json:"id,omitempty"`
	State string   `json:"state,omitempty"`
	Log   []string `json:"log"`
	Err   error    `json:"-"`
	Error string   `json:"error,omitempty"`
}

func (r *result) log(format string, a ...any) { r.Log = append(r.Log, fmt.Sprintf(format, a...)) }
func (r *result) fail(err error) *result {
	r.OK = false
	r.Err = err
	r.Error = err.Error()
	r.log("❌ %s", err.Error())
	return r
}
func (r *result) text() string { return strings.Join(r.Log, "\n") + "\n" }

// ── create ───────────────────────────────────────────────────────────

func createPrescription(ctx context.Context, c config, fixedID string) *result {
	r := &result{OK: true}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	kioskTok, _, err := kioskLogin(ctx, c.core, c.kioskCode, c.kioskPIN)
	if err != nil {
		return r.fail(fmt.Errorf("kiosk login: %w", err))
	}
	slots, err := listSlots(ctx, c.core, kioskTok, c.cabinet)
	if err != nil {
		return r.fail(fmt.Errorf("list slots: %w", err))
	}
	items, err := chooseItems(slots, c.drugsFlag, c.items)
	if err != nil {
		return r.fail(err)
	}

	id := fixedID
	if id == "" {
		id = fmt.Sprintf("RX-%s", time.Now().Format("20060102-150405"))
	}
	issuedAt := time.Now().UTC()
	ev := &eventsv1.PrescriptionCreated{
		PrescriptionId: id,
		SourceSystem:   c.source,
		Hn:             c.hn,
		PatientName:    c.patient,
		WardId:         c.ward,
		IssuedAt:       timestamppb.New(issuedAt),
		TraceId:        "kiosktester-" + id,
	}
	for _, it := range items {
		qty, _ := it["quantity"].(int)
		ev.Items = append(ev.Items, &eventsv1.PrescriptionItem{
			DrugCode:   fmt.Sprintf("%v", it["drugCode"]),
			DrugName:   fmt.Sprintf("%v", it["drugName"]),
			Quantity:   int32(qty),
			DosageText: fmt.Sprintf("%v", it["dosageText"]),
		})
	}
	if err := publishPrescription(ctx, c.natsURL, c.source, id, ev); err != nil {
		return r.fail(fmt.Errorf("publish to NATS JetStream: %w", err))
	}

	r.ID = id
	r.log("✅ สร้างรายการเบิกยาแล้ว (READY) — สแกน/ป้อนรหัสนี้ที่ตู้")
	r.log("   Prescription ID : %s", id)
	r.log("   Ward            : %s", c.ward)
	r.log("   Cabinet         : %s", orAny(c.cabinet))
	r.log("   Items           : %s", itemsSummary(items))
	return r
}

// chooseItems builds items from the explicit -drugs flag or by auto-picking
// stocked drugs from the cabinet's real slots (names/codes come from core so
// the kiosk cart resolves positions and stock).
func chooseItems(slots []slot, drugsFlag string, want int) ([]map[string]any, error) {
	byCode := map[string]slot{}
	for _, s := range slots {
		if s.DrugCode != "" {
			byCode[s.DrugCode] = s
		}
	}

	if strings.TrimSpace(drugsFlag) != "" {
		var items []map[string]any
		for _, part := range strings.Split(drugsFlag, ",") {
			code, qtyStr, ok := strings.Cut(strings.TrimSpace(part), ":")
			qty := 1
			if ok {
				if n, err := strconv.Atoi(strings.TrimSpace(qtyStr)); err == nil && n > 0 {
					qty = n
				}
			}
			s, found := byCode[strings.TrimSpace(code)]
			if !found {
				return nil, fmt.Errorf("drug %q not found in the selected cabinet's slots", code)
			}
			items = append(items, item(s, qty))
		}
		if len(items) == 0 {
			return nil, fmt.Errorf("no valid items parsed from drugs=%q", drugsFlag)
		}
		return items, nil
	}

	var items []map[string]any
	seen := map[string]bool{}
	for _, s := range slots {
		if len(items) >= want {
			break
		}
		if s.DrugCode == "" || s.DrugName == "" || s.Quantity <= 0 || seen[s.DrugCode] {
			continue
		}
		seen[s.DrugCode] = true
		items = append(items, item(s, 1))
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("no stocked drugs found — seed demo data first (npm run seed:demo)")
	}
	return items, nil
}

func item(s slot, qty int) map[string]any {
	return map[string]any{
		"drugCode":   s.DrugCode,
		"drugName":   s.DrugName,
		"quantity":   qty,
		"dosageText": "ทดสอบ: รับประทานตามแพทย์สั่ง",
	}
}

// staffCredentials resolves the (username, password, role label) for the
// simulated staff scan. Defaults match demoseed users:
//   pharmacist / demo-pharmacist-2026  → PHARMACIST (เบิกยา)
//   nurse      / demo-nurse-2026       → NURSE      (เบิกยา)
//   refiller   / demo-refiller-2026    → REFILLER   (หน้าจอเติมยา)
func staffCredentials(c config) (username, password, role string) {
	username = c.staff
	if username == "" {
		username = "pharmacist"
	}
	password = c.staffPass
	if password == "" {
		password = "demo-" + username + "-2026"
	}
	role = strings.ToUpper(strings.TrimSpace(c.staffRole))
	if role == "" || role == "AUTO" {
		switch username {
		case "pharmacist":
			role = "PHARMACIST"
		case "nurse":
			role = "NURSE"
		case "refiller":
			role = "REFILLER"
		case "admin":
			role = "ADMIN"
		default:
			role = "STAFF"
		}
	}
	return username, password, role
}

// ── confirm ──────────────────────────────────────────────────────────

func confirmPrescription(ctx context.Context, c config, prescriptionID string) *result {
	r := &result{OK: true, ID: prescriptionID}
	ctx, cancel := context.WithTimeout(ctx, c.timeout+15*time.Second)
	defer cancel()

	username, password, role := staffCredentials(c)
	staffTok, err := staffLogin(ctx, c.core, username, password)
	if err != nil {
		return r.fail(fmt.Errorf("staff login (%s/%s): %w", username, role, err))
	}

	r.log("👤 จำลองสแกนบัตรผู้ใช้: %s (role=%s)", username, role)
	r.log("📤 ยืนยันที่ตู้: Dispense(%s)", prescriptionID)
	pr, err := dispense(ctx, c.core, staffTok, prescriptionID)
	if err != nil {
		return r.fail(fmt.Errorf("dispense: %w", err))
	}
	r.log("   → รับเข้าคิว state=%s (internal id=%s)", shortState(pr.State), pr.ID)

	deadline := time.Now().Add(c.timeout)
	last := pr.State
	for time.Now().Before(deadline) {
		time.Sleep(1 * time.Second)
		cur, err := getPrescription(ctx, c.core, staffTok, pr.ID)
		if err != nil {
			r.log("   … poll error: %v", err)
			continue
		}
		if cur.State != last {
			r.log("   → state=%s", shortState(cur.State))
			last = cur.State
		}
		switch shortState(cur.State) {
		case "DISPENSED":
			r.State = "DISPENSED"
			r.log("✅ จ่ายยาสำเร็จ (ครบ flow เหมือน hardware)")
			return r
		case "FAILED":
			r.State = "FAILED"
			return r.fail(fmt.Errorf("dispense failed: %s", cur.FailureReason))
		}
	}
	r.State = shortState(last)
	return r.fail(fmt.Errorf("timed out after %s (last=%s); if parked at DISPENSING check FULFILLMENT_FAKE/VENDING_FAKE + vending agent", c.timeout, shortState(last)))
}

// ── web console (serve mode) ─────────────────────────────────────────

//go:embed console.html
var consoleHTML []byte

func serve(c config) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/" {
			http.NotFound(w, req)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(consoleHTML)
	})
	mux.HandleFunc("/api/slots", func(w http.ResponseWriter, req *http.Request) {
		ctx, cancel := context.WithTimeout(req.Context(), 20*time.Second)
		defer cancel()
		tok, _, err := kioskLogin(ctx, c.core, c.kioskCode, c.kioskPIN)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
			return
		}
		slots, err := listSlots(ctx, c.core, tok, req.URL.Query().Get("cabinet"))
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"slots": slots})
	})
	mux.HandleFunc("/api/run", func(w http.ResponseWriter, req *http.Request) {
		var body struct {
			Mode, Cabinet, Ward, HN, Patient, Source, Drugs, ID, Staff, StaffPass, Role string
			Items                                                                      int
		}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		rc := c // per-request copy of the base config
		override(&rc.cabinet, body.Cabinet)
		override(&rc.ward, body.Ward)
		override(&rc.hn, body.HN)
		override(&rc.patient, body.Patient)
		override(&rc.source, body.Source)
		override(&rc.staff, body.Staff)
		override(&rc.staffPass, body.StaffPass)
		override(&rc.staffRole, body.Role)
		rc.drugsFlag = body.Drugs
		if body.Items > 0 {
			rc.items = body.Items
		}

		var res *result
		switch body.Mode {
		case "create":
			res = createPrescription(req.Context(), rc, body.ID)
		case "confirm":
			if body.ID == "" {
				res = (&result{}).fail(fmt.Errorf("ต้องมี Prescription ID สำหรับ confirm"))
			} else {
				res = confirmPrescription(req.Context(), rc, body.ID)
			}
		case "flow":
			res = createPrescription(req.Context(), rc, body.ID)
			if res.Err == nil {
				id := res.ID
				time.Sleep(1500 * time.Millisecond)
				cf := confirmPrescription(req.Context(), rc, id)
				res.Log = append(res.Log, cf.Log...)
				res.State, res.OK, res.Err, res.Error = cf.State, cf.OK, cf.Err, cf.Error
				res.ID = id
			}
		default:
			res = (&result{}).fail(fmt.Errorf("unknown mode %q", body.Mode))
		}
		writeJSON(w, http.StatusOK, res)
	})

	fmt.Printf("🖥️  Kiosk flow tester console: http://localhost%s\n", displayAddr(c.addr))
	fmt.Printf("    core=%s  nats=%s  kiosk=%s\n", c.core, c.natsURL, c.kioskCode)
	return http.ListenAndServe(c.addr, mux)
}

func override(dst *string, v string) {
	if strings.TrimSpace(v) != "" {
		*dst = v
	}
}

func displayAddr(addr string) string {
	if strings.HasPrefix(addr, ":") {
		return addr
	}
	return addr
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// ── core Connect JSON calls ──────────────────────────────────────────

type slot struct {
	ID        string `json:"id"`
	Code      string `json:"code"`
	CabinetID string `json:"cabinetId"`
	DrugCode  string `json:"drugCode"`
	DrugName  string `json:"drugName"`
	Quantity  int    `json:"quantity"`
	Capacity  int    `json:"capacity"`
}

type prescription struct {
	ID             string `json:"id"`
	PrescriptionID string `json:"prescriptionId"`
	State          string `json:"state"`
	FailureReason  string `json:"failureReason"`
}

func kioskLogin(ctx context.Context, core, code, pin string) (token, kioskID string, err error) {
	var out struct {
		AccessToken string `json:"accessToken"`
		Kiosk       struct {
			ID string `json:"id"`
		} `json:"kiosk"`
	}
	err = connectCall(ctx, core, "medisync.kiosk.v1.KioskService/KioskLogin", "",
		map[string]any{"code": code, "pin": pin}, &out)
	return out.AccessToken, out.Kiosk.ID, err
}

func staffLogin(ctx context.Context, core, username, password string) (string, error) {
	var out struct {
		AccessToken string `json:"accessToken"`
	}
	err := connectCall(ctx, core, "medisync.identity.v1.IdentityService/Login", "",
		map[string]any{"username": username, "password": password}, &out)
	return out.AccessToken, err
}

func listSlots(ctx context.Context, core, token, cabinet string) ([]slot, error) {
	var out struct {
		Slots []slot `json:"slots"`
	}
	err := connectCall(ctx, core, "medisync.inventory.v1.InventoryService/ListSlots", token,
		map[string]any{"cabinetId": cabinet, "lowOnly": false}, &out)
	return out.Slots, err
}

func dispense(ctx context.Context, core, token, prescriptionID string) (*prescription, error) {
	var out struct {
		Prescription prescription `json:"prescription"`
	}
	err := connectCall(ctx, core, "medisync.dispensing.v1.DispensingService/Dispense", token,
		map[string]any{"prescriptionId": prescriptionID, "traceId": "kiosktester-confirm-" + prescriptionID}, &out)
	return &out.Prescription, err
}

func getPrescription(ctx context.Context, core, token, internalID string) (*prescription, error) {
	var out struct {
		Prescription prescription `json:"prescription"`
	}
	err := connectCall(ctx, core, "medisync.dispensing.v1.DispensingService/GetPrescription", token,
		map[string]any{"id": internalID}, &out)
	return &out.Prescription, err
}

// connectCall POSTs a Connect unary JSON request and decodes the reply.
func connectCall(ctx context.Context, core, method, token string, body, out any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	url := strings.TrimRight(core, "/") + "/" + method
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		var ce struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		}
		if json.Unmarshal(raw, &ce) == nil && ce.Message != "" {
			return fmt.Errorf("%s: %s (%s)", method, ce.Message, ce.Code)
		}
		return fmt.Errorf("%s: HTTP %d: %s", method, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(raw, out)
}

// ── NATS producer (real rx.prescription.created path) ────────────────
// Uses JetStream publish + protojson.Marshal — the exact same wire format the
// production hospital feeder uses (see cmd/feeder). Plain NATS publish would
// bypass the RX stream entirely and the dispensing consumer would never see it.

func publishPrescription(ctx context.Context, url, source, id string, ev *eventsv1.PrescriptionCreated) error {
	data, err := protojson.Marshal(ev)
	if err != nil {
		return fmt.Errorf("protojson marshal: %w", err)
	}
	nc, err := nats.Connect(url, nats.Name("medisync-kiosktester"))
	if err != nil {
		return fmt.Errorf("connect nats: %w", err)
	}
	defer nc.Drain()

	js, err := jetstream.New(nc)
	if err != nil {
		return fmt.Errorf("jetstream: %w", err)
	}

	msg := nats.NewMsg(natsx.SubjectPrescriptionCreated)
	msg.Data = data
	msg.Header.Set("Nats-Msg-Id", source+"/"+id)
	ack, err := js.PublishMsg(ctx, msg)
	if err != nil {
		return fmt.Errorf("jetstream publish: %w", err)
	}
	_ = ack
	return nil
}

// ── helpers ──────────────────────────────────────────────────────────

func shortState(s string) string { return strings.TrimPrefix(s, "PRESCRIPTION_STATE_") }

func orAny(s string) string {
	if strings.TrimSpace(s) == "" {
		return "(any)"
	}
	return s
}

func itemsSummary(items []map[string]any) string {
	parts := make([]string, 0, len(items))
	for _, it := range items {
		parts = append(parts, fmt.Sprintf("%v×%v", it["drugCode"], it["quantity"]))
	}
	return strings.Join(parts, ", ")
}
