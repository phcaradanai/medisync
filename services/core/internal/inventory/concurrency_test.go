//go:build integration

package inventory

import (
	"context"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// concurrencyPool returns a dedicated pool for concurrency tests.
// Each goroutine acquires its own connection to avoid the pgx
// single-connection concurrency restriction.
func concurrencyPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool := integrationPool(t)
	// Clean up any leftover test data from prior runs.
	_, _ = pool.Exec(context.Background(), `DELETE FROM medisync.slot`)
	return pool
}

// slotID is returned by seedSlotConcurrent and used across goroutines.
func seedSlotConcurrent(t *testing.T, pool *pgxpool.Pool, cabinetID, code string) string {
	t.Helper()
	var id string
	err := pool.QueryRow(context.Background(),
		`INSERT INTO medisync.slot (cabinet_id, code, capacity, quantity, low_threshold)
		 VALUES ($1, $2, 10000, 0, 0)
		 RETURNING id`, cabinetID, code).Scan(&id)
	if err != nil {
		t.Fatalf("seed slot: %v", err)
	}
	return id
}

// TestRefillConcurrency proves that concurrent Refill operations do not
// lose updates. Each goroutine uses its own Store backed by the pool,
// ensuring each has its own connection. The atomic UPDATE ... SET
// quantity = quantity + $delta prevents lost updates at the DB level.
func TestRefillConcurrency(t *testing.T) {
	pool := concurrencyPool(t)
	defer pool.Close()

	cabID, code := uniqueCabinetCode(t, "CONC")
	slotID := seedSlotConcurrent(t, pool, cabID, code)
	// Assign drug.
	store := NewStore(pool, nil)
	_, err := store.AssignDrug(context.Background(), slotID, "drug-1", "PARA-500", "Paracetamol", 10000, 10)
	if err != nil {
		t.Fatalf("AssignDrug: %v", err)
	}

	const numWorkers = 50
	const deltaPerWorker int32 = 1

	var wg sync.WaitGroup
	errs := make(chan error, numWorkers)

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Each goroutine gets its own Store backed by the pool.
			// pgxpool.Pool handles connection multiplexing safely.
			s := NewStore(pool, nil)
			_, err := s.Refill(context.Background(), slotID, deltaPerWorker, nil)
			if err != nil {
				errs <- err
			}
		}()
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent Refill error: %v", err)
	}

	got, err := store.GetByID(context.Background(), slotID)
	if err != nil {
		t.Fatalf("GetByID after concurrency test: %v", err)
	}
	if got == nil {
		t.Fatal("slot not found after concurrency test")
	}

	expected := int32(numWorkers)
	if got.Quantity != expected {
		t.Errorf("Quantity = %d, want %d (lost %d updates)",
			got.Quantity, expected, expected-got.Quantity)
	}
}

// TestRefillConcurrencyMixedDelta proves that concurrent positive and
// negative deltas produce the correct net result without lost updates.
func TestRefillConcurrencyMixedDelta(t *testing.T) {
	pool := concurrencyPool(t)
	defer pool.Close()

	cabID, code := uniqueCabinetCode(t, "MIXD")
	slotID := seedSlotConcurrent(t, pool, cabID, code)
	store := NewStore(pool, nil)
	_, err := store.AssignDrug(context.Background(), slotID, "drug-1", "PARA-500", "Paracetamol", 10000, 10)
	if err != nil {
		t.Fatalf("AssignDrug: %v", err)
	}

	// Start with 100 units.
	_, err = store.Refill(context.Background(), slotID, 100, nil)
	if err != nil {
		t.Fatalf("initial refill: %v", err)
	}

	const numAdders = 25
	const numSubtractors = 25
	const deltaPerWorker int32 = 1

	var wg sync.WaitGroup
	errs := make(chan error, numAdders+numSubtractors)

	for i := 0; i < numAdders; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s := NewStore(pool, nil)
			_, err := s.Refill(context.Background(), slotID, deltaPerWorker, nil)
			if err != nil {
				errs <- err
			}
		}()
	}

	for i := 0; i < numSubtractors; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s := NewStore(pool, nil)
			_, err := s.Refill(context.Background(), slotID, -deltaPerWorker, nil)
			if err != nil {
				errs <- err
			}
		}()
	}

	wg.Wait()
	close(errs)

	failedCount := 0
	for err := range errs {
		t.Logf("concurrent Refill error (expected for some subtractors): %v", err)
		failedCount++
	}
	// Record failures but don't fail on them — some subtractors may
	// legitimately fail if they race and see insufficient stock.

	got, err := store.GetByID(context.Background(), slotID)
	if err != nil {
		t.Fatalf("GetByID after concurrency test: %v", err)
	}
	if got == nil {
		t.Fatal("slot not found after concurrency test")
	}

	// Net = 100 + 25 - 25 = 100. Verify no lost updates.
	expected := int32(100)
	if got.Quantity != expected {
		t.Errorf("Quantity = %d, want %d (concurrent mixed deltas lost updates)",
			got.Quantity, expected)
	}
}

// TestRefillConcurrencyHighContention stresses the atomic increment with
// 200 goroutines and confirms the final quantity is exact.
func TestRefillConcurrencyHighContention(t *testing.T) {
	pool := concurrencyPool(t)
	defer pool.Close()

	cabID, code := uniqueCabinetCode(t, "HIGH")
	slotID := seedSlotConcurrent(t, pool, cabID, code)
	store := NewStore(pool, nil)
	_, err := store.AssignDrug(context.Background(), slotID, "drug-1", "PARA-500", "Paracetamol", 100000, 10)
	if err != nil {
		t.Fatalf("AssignDrug: %v", err)
	}

	const numWorkers = 200
	const deltaPerWorker int32 = 1

	var wg sync.WaitGroup
	errs := make(chan error, numWorkers)

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s := NewStore(pool, nil)
			_, err := s.Refill(context.Background(), slotID, deltaPerWorker, nil)
			if err != nil {
				errs <- err
			}
		}()
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent Refill error: %v", err)
	}

	got, err := store.GetByID(context.Background(), slotID)
	if err != nil {
		t.Fatalf("GetByID after high-contention test: %v", err)
	}
	if got == nil {
		t.Fatal("slot not found after high-contention test")
	}

	expected := int32(200)
	if got.Quantity != expected {
		t.Errorf("Quantity = %d, want %d (lost %d updates in high-contention)",
			got.Quantity, expected, expected-got.Quantity)
	}
}
