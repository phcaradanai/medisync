// Command demoseed creates demo data for local development.
// All demo data is scoped to a project (--project flag).
// Default: uses the Default Project (0000...0001).
// Run: go run ./cmd/demoseed --project=<uuid> --reset
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

// Default project UUID from migration 0011.
var defaultProjectID = "00000000-0000-0000-0000-000000000001"

type demoUser struct {
	Username    string
	Password    string
	DisplayName string
	Role        string
}

type demoDrug struct {
	Code        string `json:"code"`
	Name        string `json:"name"`
	GenericName string `json:"generic_name"`
	Form        string `json:"form"`
	Strength    string `json:"strength"`
	Unit        string `json:"unit"`
}

type demoSlot struct {
	Code         string `json:"code"`
	DrugCode     string `json:"drug_code"`
	Capacity     int    `json:"capacity"`
	Quantity     int    `json:"quantity"`
	LowThreshold int    `json:"low_threshold"`
}

type demoRxItem struct {
	DrugCode   string `json:"drug_code"`
	DrugName   string `json:"drug_name"`
	Quantity   int    `json:"quantity"`
	DosageText string `json:"dosage_text"`
}

var (
	demoUsers = []demoUser{
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
	project := flag.String("project", defaultProjectID, "project UUID to seed into")
	flag.Parse()

	url := os.Getenv("DATABASE_URL")
	if url == "" {
		fmt.Fprintln(os.Stderr, "DATABASE_URL is required")
		os.Exit(1)
	}
	projectID := *project

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		fmt.Fprintf(os.Stderr, "connect: %v\n", err)
		os.Exit(1)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "ping: %v\n", err)
		os.Exit(1)
	}

	discardLog := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError + 1}))
	if err := postgres.Migrate(ctx, pool, migrations.FS, discardLog); err != nil {
		fmt.Fprintf(os.Stderr, "migrate: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Migrations applied.")

	if *reset {
		if err := clearDemoData(ctx, pool); err != nil {
			fmt.Fprintf(os.Stderr, "clear: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Demo data cleared.")
	}

	fmt.Printf("Seeding project: %s\n", projectID)
	if err := seed(ctx, pool, projectID); err != nil {
		fmt.Fprintf(os.Stderr, "seed: %v\n", err)
		os.Exit(1)
	}

	printCredentials(projectID)
}

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

func seed(ctx context.Context, pool *pgxpool.Pool, projectID string) error {
	if err := seedUsers(ctx, pool, projectID); err != nil {
		return fmt.Errorf("users: %w", err)
	}
	if err := seedKiosk(ctx, pool, projectID); err != nil {
		return fmt.Errorf("kiosk: %w", err)
	}
	drugIDs, err := seedDrugs(ctx, pool, projectID)
	if err != nil {
		return fmt.Errorf("drugs: %w", err)
	}
	if err := seedSlots(ctx, pool, drugIDs, projectID); err != nil {
		return fmt.Errorf("slots: %w", err)
	}
	if err := seedPrescription(ctx, pool, projectID); err != nil {
		return fmt.Errorf("prescription: %w", err)
	}
	return nil
}

func seedUsers(ctx context.Context, pool *pgxpool.Pool, projectID string) error {
	pid := "NULL"
	for _, u := range demoUsers {
		hash, err := identity.HashPassword(u.Password)
		if err != nil {
			return fmt.Errorf("hash %q: %w", u.Username, err)
		}
		wardIDs := fmt.Sprintf(`{"%s"}`, demoWard)
		pid = fmt.Sprintf(`'%s'`, projectID)
		sql := fmt.Sprintf(
			`INSERT INTO identity.users (username,password_hash,display_name,role,ward_ids,project_id,active)
			 VALUES ('%s','%s','%s','%s','%s',%s,true) ON CONFLICT (username) DO NOTHING`,
			u.Username, hash, u.DisplayName, u.Role, wardIDs, pid)
		if _, err := pool.Exec(ctx, sql); err != nil {
			return fmt.Errorf("insert %q: %w", u.Username, err)
		}
		fmt.Printf("  user %-12s (%s) project=%s\n", u.Username, u.Role, projectID[:8])
	}
	_ = pid
	return nil
}

func seedKiosk(ctx context.Context, pool *pgxpool.Pool, projectID string) error {
	pinHash, err := identity.HashPassword(demoKioskPIN)
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx,
		`INSERT INTO identity.kiosks (code,display_name,pin_hash,active,project_id)
		 VALUES ($1,$2,$3,true,$4) ON CONFLICT (code) DO NOTHING`,
		demoKioskCode, "Demo Cabinet K1", pinHash, projectID)
	if err != nil {
		return fmt.Errorf("insert kiosk: %w", err)
	}
	fmt.Printf("  kiosk %s (active) project=%s\n", demoKioskCode, projectID[:8])
	return nil
}

func seedDrugs(ctx context.Context, pool *pgxpool.Pool, projectID string) (map[string]string, error) {
	ids := make(map[string]string)
	for _, d := range demoDrugs {
		var id string
		err := pool.QueryRow(ctx,
			`INSERT INTO catalog.drug (code,name,generic_name,form,strength,unit,project_id)
			 VALUES ($1,$2,$3,$4,$5,$6,$7)
			 ON CONFLICT (code,project_id) DO UPDATE SET name=EXCLUDED.name,
			   generic_name=EXCLUDED.generic_name, form=EXCLUDED.form,
			   strength=EXCLUDED.strength, unit=EXCLUDED.unit, updated_at=now()
			 RETURNING id`,
			d.Code, d.Name, d.GenericName, d.Form, d.Strength, d.Unit, projectID).Scan(&id)
		if err != nil {
			return nil, fmt.Errorf("insert drug %q: %w", d.Code, err)
		}
		ids[d.Code] = id
		fmt.Printf("  drug  %-14s %s\n", d.Code, d.Name)
	}
	return ids, nil
}

func seedSlots(ctx context.Context, pool *pgxpool.Pool, drugIDs map[string]string, projectID string) error {
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
			`INSERT INTO inventory.slot (cabinet_id,code,drug_id,drug_code,drug_name,capacity,quantity,low_threshold,project_id)
			 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
			 ON CONFLICT (cabinet_id,code) DO UPDATE SET
			   drug_id=EXCLUDED.drug_id, drug_code=EXCLUDED.drug_code,
			   drug_name=EXCLUDED.drug_name, capacity=EXCLUDED.capacity,
			   quantity=EXCLUDED.quantity, low_threshold=EXCLUDED.low_threshold,
			   project_id=EXCLUDED.project_id, updated_at=now()`,
			demoCabinetID, s.Code, drugID, s.DrugCode, drugName,
			s.Capacity, s.Quantity, s.LowThreshold, projectID)
		if err != nil {
			return fmt.Errorf("slot %q: %w", s.Code, err)
		}
		fmt.Printf("  slot  %-5s %s (qty=%d)\n", s.Code, s.DrugCode, s.Quantity)
	}
	return nil
}

func seedPrescription(ctx context.Context, pool *pgxpool.Pool, projectID string) error {
	itemsJSON, err := json.Marshal(demoRxItems)
	if err != nil {
		return fmt.Errorf("marshal items: %w", err)
	}
	rxID := demoPrefix + "RX-001"
	now := time.Now()
	_, err = pool.Exec(ctx,
		`INSERT INTO dispensing.prescription
		   (prescription_id,source_system,hn,patient_name,ward_id,items,state,project_id,issued_at)
		 VALUES ($1,$2,$3,$4,$5,$6,'READY',$7,$8)
		 ON CONFLICT ON CONSTRAINT prescription_external_key DO UPDATE SET
		   hn=EXCLUDED.hn, patient_name=EXCLUDED.patient_name,
		   ward_id=EXCLUDED.ward_id, items=EXCLUDED.items,
		   project_id=EXCLUDED.project_id, updated_at=now()`,
		rxID, demoSource, "HN100001", "Demo Patient", demoWard, itemsJSON, projectID, now)
	if err != nil {
		return fmt.Errorf("insert rx: %w", err)
	}
	fmt.Printf("  rx    %-15s READY (ward=%s)\n", rxID, demoWard)
	return nil
}

func printCredentials(projectID string) {
	fmt.Println()
	fmt.Println("══════════════════════════════════════════════════════════════")
	fmt.Println("  DEMO CREDENTIALS — LOCAL DEVELOPMENT ONLY")
	fmt.Println("══════════════════════════════════════════════════════════════")
	fmt.Println()
	fmt.Printf("  Project:  %s\n", projectID)
	fmt.Println()
	fmt.Println("── Staff logins ──")
	for _, u := range demoUsers {
		fmt.Printf("  %-12s / %-25s  (%s)\n", u.Username, u.Password, u.Role)
	}
	fmt.Println()
	fmt.Println("── Kiosk ──")
	fmt.Printf("  Code: %s   PIN: %s\n", demoKioskCode, demoKioskPIN)
	fmt.Println()
	fmt.Println("── Prescription ──")
	fmt.Printf("  ID: DEMO-RX-001  Ward: %s  Patient: HN100001\n", demoWard)
	fmt.Println()
	fmt.Println("── Reset & re-seed ──")
	fmt.Printf("  go run ./cmd/demoseed --project=%s --reset\n", projectID)
	fmt.Println()
	fmt.Println("══════════════════════════════════════════════════════════════")
}
