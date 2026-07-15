// Command demoseed provisions deterministic demo data into the MediSync
// database for local development and testing. It is idempotent — re-running
// with the same demo identifiers does not duplicate rows.
//
// Usage:
//
//	go run ./cmd/demoseed          # seed demo data (idempotent)
//	go run ./cmd/demoseed --reset  # clear demo data first, then re-seed
//
// Environment:
//
//	DATABASE_URL  PostgreSQL connection string (required)
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/adm-chura3inter/medisync/services/core/internal/identity"
	"github.com/adm-chura3inter/medisync/services/core/internal/platform/postgres"
	"github.com/adm-chura3inter/medisync/services/core/migrations"
)

// Demo identifiers — kept as constants so downstream tests can reference them.
// All demo data is prefixed "DEMO-" for easy cleanup.
const (
	demoPrefix    = "DEMO-"
	demoSource    = "demo-seed"
	demoKioskCode = "DEMO-K1"
	demoKioskPIN  = "123456"
	demoWard      = "WARD-3A"
	demoCabinetID = "CAB1"
)

// demoUser holds the fields for a seed user.
type demoUser struct {
	Username    string
	Password    string
	DisplayName string
	Role        string // ADMIN, PHARMACIST, NURSE, REFILLER
}

// demoDrug holds the fields for a seed drug.
type demoDrug struct {
	Code        string
	Name        string
	GenericName string
	Form        string
	Strength    string
	Unit        string
}

// demoSlot holds the fields for a seed slot.
type demoSlot struct {
	Code         string
	DrugCode     string
	Capacity     int32
	Quantity     int32
	LowThreshold int32
}

// demoRxItem is a single line item in the demo prescription.
type demoRxItem struct {
	DrugCode   string `json:"drug_code"`
	DrugName   string `json:"drug_name"`
	Quantity   int32  `json:"quantity"`
	DosageText string `json:"dosage_text"`
}

var (
	demoUsers = []demoUser{
		{Username: "admin", Password: "medisync-local-admin-2026", DisplayName: "Administrator", Role: "ADMIN"},
		{Username: "pharmacist", Password: "demo-pharmacist-2026", DisplayName: "Demo Pharmacist", Role: "PHARMACIST"},
		{Username: "nurse", Password: "demo-nurse-2026", DisplayName: "Demo Nurse", Role: "NURSE"},
		{Username: "refiller", Password: "demo-refiller-2026", DisplayName: "Demo Refiller", Role: "REFILLER"},
	}

	demoDrugs = []demoDrug{
		{Code: "DEMO-PARA500", Name: "Paracetamol 500 mg", GenericName: "พาราเซตามอล 500 มก.", Form: "tablet", Strength: "500 mg", Unit: "tablet"},
		{Code: "DEMO-AMOX500", Name: "Amoxicillin 500 mg", GenericName: "อะม็อกซีซิลลิน 500 มก.", Form: "capsule", Strength: "500 mg", Unit: "capsule"},
		{Code: "DEMO-OME20", Name: "Omeprazole 20 mg", GenericName: "โอมีพราโซล 20 มก.", Form: "capsule", Strength: "20 mg", Unit: "capsule"},
	}

	demoSlots = []demoSlot{
		{Code: "S01", DrugCode: "DEMO-PARA500", Capacity: 100, Quantity: 80, LowThreshold: 20},
		{Code: "S02", DrugCode: "DEMO-AMOX500", Capacity: 100, Quantity: 60, LowThreshold: 20},
		{Code: "S03", DrugCode: "DEMO-OME20", Capacity: 50, Quantity: 45, LowThreshold: 10},
	}

	demoRxItems = []demoRxItem{
		{DrugCode: "DEMO-PARA500", DrugName: "Paracetamol 500 mg", Quantity: 2, DosageText: "รับประทานครั้งละ 1 เม็ด ทุก 6 ชั่วโมง เวลาปวดหรือมีไข้"},
		{DrugCode: "DEMO-AMOX500", DrugName: "Amoxicillin 500 mg", Quantity: 3, DosageText: "รับประทานครั้งละ 1 แคปซูล วันละ 3 ครั้ง หลังอาหาร"},
	}
)

func main() {
	reset := flag.Bool("reset", false, "delete existing demo data before seeding")
	flag.Parse()

	url := os.Getenv("DATABASE_URL")
	if url == "" {
		fmt.Fprintln(os.Stderr, "DATABASE_URL is required (e.g. postgres://medisync:medisync@localhost:5432/medisync?sslmode=disable)")
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		fmt.Fprintf(os.Stderr, "connect to database: %v\n", err)
		os.Exit(1)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "ping database: %v\n", err)
		os.Exit(1)
	}

	// Apply migrations first so the schema exists.
	discardLog := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError + 1}))
	if err := postgres.Migrate(ctx, pool, migrations.FS, discardLog); err != nil {
		fmt.Fprintf(os.Stderr, "migrate: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Migrations applied.")

	if *reset {
		if err := clearDemoData(ctx, pool); err != nil {
			fmt.Fprintf(os.Stderr, "clear demo data: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Demo data cleared.")
	}

	if err := seed(ctx, pool); err != nil {
		fmt.Fprintf(os.Stderr, "seed: %v\n", err)
		os.Exit(1)
	}

	printCredentials()
}

// clearDemoData removes all rows with demo identifiers. Order respects
// foreign keys: prescriptions and slots reference drugs, so drugs are
// deleted last.
func clearDemoData(ctx context.Context, pool *pgxpool.Pool) error {
	cmds := []string{
		`DELETE FROM dispensing.outbox WHERE subject LIKE 'medisync.demo%'`,
		`DELETE FROM dispensing.prescription WHERE source_system = '` + demoSource + `'`,
		`DELETE FROM inventory.slot WHERE cabinet_id = '` + demoCabinetID + `'`,
		`DELETE FROM catalog.drug WHERE code LIKE '` + demoPrefix + `%'`,
		`DELETE FROM identity.users WHERE username IN ('pharmacist','nurse','refiller')`,
		`DELETE FROM identity.kiosks WHERE code = '` + demoKioskCode + `'`,
		`DELETE FROM audit.audit_log WHERE entity_id LIKE '` + demoPrefix + `%'`,
	}
	for _, cmd := range cmds {
		if _, err := pool.Exec(ctx, cmd); err != nil {
			return fmt.Errorf("%q: %w", cmd, err)
		}
	}
	return nil
}

// seed inserts demo data. All inserts use ON CONFLICT DO NOTHING to
// ensure idempotency across repeated runs.
func seed(ctx context.Context, pool *pgxpool.Pool) error {
	if err := seedUsers(ctx, pool); err != nil {
		return fmt.Errorf("users: %w", err)
	}
	if err := seedKiosk(ctx, pool); err != nil {
		return fmt.Errorf("kiosk: %w", err)
	}
	drugIDs, err := seedDrugs(ctx, pool)
	if err != nil {
		return fmt.Errorf("drugs: %w", err)
	}
	if err := seedSlots(ctx, pool, drugIDs); err != nil {
		return fmt.Errorf("slots: %w", err)
	}
	if err := seedPrescription(ctx, pool, drugIDs); err != nil {
		return fmt.Errorf("prescription: %w", err)
	}
	return nil
}

func seedUsers(ctx context.Context, pool *pgxpool.Pool) error {
	for _, u := range demoUsers {
		hash, err := identity.HashPassword(u.Password)
		if err != nil {
			return fmt.Errorf("hash password for %q: %w", u.Username, err)
		}
		// admin is seeded by core/main.go with SeedAdmin — we use
		// ON CONFLICT DO NOTHING so re-running is safe.
		wardIDs := "{}"
		if u.Role != "ADMIN" {
			wardIDs = fmt.Sprintf(`{"%s"}`, demoWard)
		}
		sql := fmt.Sprintf(
			`INSERT INTO identity.users (username, password_hash, display_name, role, ward_ids, active)
			 VALUES ('%s', '%s', '%s', '%s', '%s', true)
			 ON CONFLICT (username) DO NOTHING`,
			u.Username, hash, u.DisplayName, u.Role, wardIDs)
		if _, err := pool.Exec(ctx, sql); err != nil {
			return fmt.Errorf("insert user %q: %w", u.Username, err)
		}
		fmt.Printf("  user %-12s (%s)\n", u.Username, u.Role)
	}
	return nil
}

func seedKiosk(ctx context.Context, pool *pgxpool.Pool) error {
	pinHash, err := identity.HashPassword(demoKioskPIN)
	if err != nil {
		return fmt.Errorf("hash kiosk PIN: %w", err)
	}
	sql := fmt.Sprintf(
		`INSERT INTO identity.kiosks (code, display_name, pin_hash, active)
		 VALUES ('%s', 'Demo Cabinet K1', '%s', true)
		 ON CONFLICT (code) DO NOTHING`,
		demoKioskCode, pinHash)
	if _, err := pool.Exec(ctx, sql); err != nil {
		return fmt.Errorf("insert kiosk: %w", err)
	}
	fmt.Printf("  kiosk %s (active)\n", demoKioskCode)
	return nil
}

// seedDrugs inserts the demo drugs and returns a map of drug_code → id
// for use by slot and prescription seeding.
func seedDrugs(ctx context.Context, pool *pgxpool.Pool) (map[string]string, error) {
	ids := make(map[string]string)
	for _, d := range demoDrugs {
		var id string
		err := pool.QueryRow(ctx,
			`INSERT INTO catalog.drug (code, name, generic_name, form, strength, unit)
			 VALUES ($1, $2, $3, $4, $5, $6)
			 ON CONFLICT (code) DO UPDATE SET name = EXCLUDED.name,
			   generic_name = EXCLUDED.generic_name, form = EXCLUDED.form,
			   strength = EXCLUDED.strength, unit = EXCLUDED.unit,
			   updated_at = now()
			 RETURNING id`,
			d.Code, d.Name, d.GenericName, d.Form, d.Strength, d.Unit).Scan(&id)
		if err != nil {
			return nil, fmt.Errorf("insert drug %q: %w", d.Code, err)
		}
		ids[d.Code] = id
		fmt.Printf("  drug  %-14s %s\n", d.Code, d.Name)
	}
	return ids, nil
}

func seedSlots(ctx context.Context, pool *pgxpool.Pool, drugIDs map[string]string) error {
	for _, s := range demoSlots {
		drugID := drugIDs[s.DrugCode]
		drugName := ""
		for _, d := range demoDrugs {
			if d.Code == s.DrugCode {
				drugName = d.Name
				break
			}
		}
		_, err := pool.Exec(ctx,
			`INSERT INTO inventory.slot (cabinet_id, code, drug_id, drug_code, drug_name, capacity, quantity, low_threshold)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			 ON CONFLICT (cabinet_id, code) DO UPDATE SET
			   drug_id = EXCLUDED.drug_id,
			   drug_code = EXCLUDED.drug_code,
			   drug_name = EXCLUDED.drug_name,
			   capacity = EXCLUDED.capacity,
			   quantity = EXCLUDED.quantity,
			   low_threshold = EXCLUDED.low_threshold,
			   updated_at = now()`,
			demoCabinetID, s.Code, drugID, s.DrugCode, drugName,
			s.Capacity, s.Quantity, s.LowThreshold)
		if err != nil {
			return fmt.Errorf("insert slot %q: %w", s.Code, err)
		}
		fmt.Printf("  slot  %-5s %s (qty=%d)\n", s.Code, s.DrugCode, s.Quantity)
	}
	return nil
}

func seedPrescription(ctx context.Context, pool *pgxpool.Pool, drugIDs map[string]string) error {
	// Build the items JSON from demoRxItems — no drugIDs needed,
	// the prescription items reference drug_code directly.
	itemsJSON, err := json.Marshal(demoRxItems)
	if err != nil {
		return fmt.Errorf("marshal items: %w", err)
	}

	rxID := demoPrefix + "RX-001"
	now := time.Now()

	_, err = pool.Exec(ctx,
		`INSERT INTO dispensing.prescription
		   (prescription_id, source_system, hn, patient_name, ward_id, items, state, issued_at)
		 VALUES ($1, $2, $3, $4, $5, $6, 'READY', $7)
		 ON CONFLICT ON CONSTRAINT prescription_external_key DO UPDATE SET
		   hn = EXCLUDED.hn,
		   patient_name = EXCLUDED.patient_name,
		   ward_id = EXCLUDED.ward_id,
		   items = EXCLUDED.items,
		   updated_at = now()`,
		rxID, demoSource, "HN100001", "Demo Patient", demoWard, itemsJSON, now)
	if err != nil {
		return fmt.Errorf("insert prescription: %w", err)
	}
	fmt.Printf("  rx    %-15s READY (ward=%s, patient=HN100001)\n", rxID, demoWard)
	return nil
}

// printCredentials writes the demo credentials to stdout with clear
// local-development-only warnings.
func printCredentials() {
	fmt.Println()
	fmt.Println("══════════════════════════════════════════════════════════════")
	fmt.Println("  DEMO CREDENTIALS — LOCAL DEVELOPMENT ONLY")
	fmt.Println("  DO NOT USE THESE IN PRODUCTION")
	fmt.Println("══════════════════════════════════════════════════════════════")
	fmt.Println()
	fmt.Println("Core API:        http://localhost:8080")
	fmt.Println("Admin app:       http://localhost:5173  (Vite dev)")
	fmt.Println("Kiosk app:       http://localhost:5174  (Vite dev)")
	fmt.Println()
	fmt.Println("── Staff logins (password auth) ──")
	fmt.Println()
	for _, u := range demoUsers {
		fmt.Printf("  %-12s / %-25s  (%s)\n", u.Username, u.Password, u.Role)
	}
	fmt.Println()
	fmt.Println("── Kiosk login ──")
	fmt.Println()
	fmt.Printf("  Code: %s\n", demoKioskCode)
	fmt.Printf("  PIN:  %s\n", demoKioskPIN)
	fmt.Println()
	fmt.Println("── Prescription ──")
	fmt.Println()
	fmt.Printf("  ID:     DEMO-RX-001\n")
	fmt.Printf("  Ward:   %s\n", demoWard)
	fmt.Printf("  Patient: HN100001 (Demo Patient)\n")
	fmt.Printf("  Items:  Paracetamol 500mg x2, Amoxicillin 500mg x3\n")
	fmt.Println()
	fmt.Println("── Reset & re-seed ──")
	fmt.Println()
	fmt.Println("  go run ./cmd/demoseed --reset")
	fmt.Println()
	fmt.Println("══════════════════════════════════════════════════════════════")
}
