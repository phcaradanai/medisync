//go:build integration

package audit

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func auditIntegrationPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Fatal("TEST_DATABASE_URL is required for integration tests. Set it to a test database URL.")
	}
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatalf("connect to test database: %v", err)
	}
	if err := pool.Ping(context.Background()); err != nil {
		pool.Close()
		t.Fatalf("ping test database: %v", err)
	}
	t.Cleanup(func() {
		pool.Close()
	})
	return pool
}

// txWriter creates a Writer backed by a transaction that is always rolled back.
func txWriter(t *testing.T) (*Writer, pgx.Tx, func()) {
	t.Helper()
	pool := auditIntegrationPool(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	w := NewWriterWithDB(tx)
	cleanup := func() {
		tx.Rollback(ctx) //nolint:errcheck
	}
	return w, tx, cleanup
}

func TestWriteDefaultsActor_Integration(t *testing.T) {
	w, tx, cleanup := txWriter(t)
	defer cleanup()

	e := Entry{
		Action:   "test.default.actor",
		Entity:   "test",
		EntityID: "test-1",
		TraceID:  "trace-001",
	}

	err := w.Write(context.Background(), e)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	var actor string
	err = tx.QueryRow(context.Background(),
		`SELECT actor FROM audit.audit_log WHERE entity_id = $1`,
		e.EntityID,
	).Scan(&actor)
	if err != nil {
		t.Fatalf("read back audit log: %v", err)
	}
	if actor != "system" {
		t.Errorf("actor = %q, want system", actor)
	}
}

func TestWriteWithActor_Integration(t *testing.T) {
	w, tx, cleanup := txWriter(t)
	defer cleanup()

	e := Entry{
		Action:   "test.with.actor",
		Entity:   "test",
		EntityID: "test-2",
		Actor:    "user-42",
		TraceID:  "trace-002",
	}

	err := w.Write(context.Background(), e)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	var actor string
	err = tx.QueryRow(context.Background(),
		`SELECT actor FROM audit.audit_log WHERE entity_id = $1`,
		e.EntityID,
	).Scan(&actor)
	if err != nil {
		t.Fatalf("read back audit log: %v", err)
	}
	if actor != "user-42" {
		t.Errorf("actor = %q, want user-42", actor)
	}
}

func TestWriteWithDetail_Integration(t *testing.T) {
	w, tx, cleanup := txWriter(t)
	defer cleanup()

	e := Entry{
		Action:   "test.with.detail",
		Entity:   "test",
		EntityID: "test-3",
		TraceID:  "trace-003",
		Detail: map[string]any{
			"ward_id": "WARD-3A",
			"items":   2,
			"nested": map[string]any{
				"key": "value",
			},
		},
	}

	err := w.Write(context.Background(), e)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	var detailRaw []byte
	err = tx.QueryRow(context.Background(),
		`SELECT detail FROM audit.audit_log WHERE entity_id = $1`,
		e.EntityID,
	).Scan(&detailRaw)
	if err != nil {
		t.Fatalf("read back audit log: %v", err)
	}

	var detail map[string]any
	if err := json.Unmarshal(detailRaw, &detail); err != nil {
		t.Fatalf("unmarshal detail: %v", err)
	}
	if v, ok := detail["ward_id"]; !ok || v != "WARD-3A" {
		t.Errorf("detail.ward_id = %v, want WARD-3A", v)
	}
}

func TestWriteEmptyDetail_Integration(t *testing.T) {
	w, tx, cleanup := txWriter(t)
	defer cleanup()

	e := Entry{
		Action:   "test.empty.detail",
		Entity:   "test",
		EntityID: "test-4",
		TraceID:  "trace-004",
		Detail:   nil,
	}

	err := w.Write(context.Background(), e)
	if err != nil {
		t.Fatalf("Write with nil detail: %v", err)
	}

	var detailRaw []byte
	err = tx.QueryRow(context.Background(),
		`SELECT detail FROM audit.audit_log WHERE entity_id = $1`,
		e.EntityID,
	).Scan(&detailRaw)
	if err != nil {
		t.Fatalf("read back audit log: %v", err)
	}
	if string(detailRaw) != "{}" {
		t.Errorf("detail = %s, want {}", string(detailRaw))
	}
}

func TestAuditLogWritten_Integration(t *testing.T) {
	w, tx, cleanup := txWriter(t)
	defer cleanup()

	e := Entry{
		Action:   "test.verify.written",
		Entity:   "test",
		EntityID: "test-verify",
		Actor:    "tester",
		TraceID:  "trace-verify-001",
		Detail:   map[string]any{"verified": true},
	}

	err := w.Write(context.Background(), e)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	var actor string
	var detailRaw []byte
	err = tx.QueryRow(context.Background(),
		`SELECT actor, detail FROM audit.audit_log WHERE entity_id = $1 ORDER BY id DESC LIMIT 1`,
		e.EntityID,
	).Scan(&actor, &detailRaw)
	if err != nil {
		t.Fatalf("read back audit log: %v", err)
	}
	if actor != "tester" {
		t.Errorf("actor = %q, want tester", actor)
	}

	var detail map[string]any
	if err := json.Unmarshal(detailRaw, &detail); err != nil {
		t.Fatalf("unmarshal detail: %v", err)
	}
	if v, ok := detail["verified"]; !ok || v != true {
		t.Errorf("detail.verified = %v, want true", v)
	}
}
