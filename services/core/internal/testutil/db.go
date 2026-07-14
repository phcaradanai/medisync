// Package testutil provides test helpers shared across bounded contexts.
package testutil

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Execer is the narrow write-only database interface used by old-style
// Store and audit.Writer code. New code should prefer dbConn (Exec+Query+QueryRow).
type Execer interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
}

// FakeExecer is an in-memory Execer for unit tests. It records every
// Exec call and returns values configured through ReturnTag and ReturnErr.
// Query and QueryRow are also supported (returning configured values)
// so FakeExecer can be used with the broader dbConn interface.
type FakeExecer struct {
	Calls            []ExecCall
	ReturnTag        pgconn.CommandTag
	ReturnErr        error
	ReturnRows       pgx.Rows  // returned by Query; nil means no mock rows set
	ReturnRowsError  error     // error returned by Query
	ReturnRowScanner RowScanner // returned by QueryRow
	ReturnRowError   error     // error returned by the row's Scan
}

// RowScanner is a function that mimics pgx.Row.Scan behavior.
type RowScanner func(dest ...any) error

// ExecCall records the arguments of a single Exec call.
type ExecCall struct {
	SQL  string
	Args []any
}

func (f *FakeExecer) Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
	f.Calls = append(f.Calls, ExecCall{SQL: sql, Args: arguments})
	return f.ReturnTag, f.ReturnErr
}

// Query records the call and returns configured rows or an error.
func (f *FakeExecer) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	f.Calls = append(f.Calls, ExecCall{SQL: sql, Args: args})
	if f.ReturnRowsError != nil {
		return nil, f.ReturnRowsError
	}
	if f.ReturnRows != nil {
		return f.ReturnRows, nil
	}
	return nil, fmt.Errorf("FakeExecer.Query: ReturnRows not configured for SQL: %s", sql)
}

// QueryRow records the call and returns a fake row scanner.
func (f *FakeExecer) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	f.Calls = append(f.Calls, ExecCall{SQL: sql, Args: args})
	return &fakeRow{scanner: f.ReturnRowScanner, err: f.ReturnRowError}
}

// fakeRow implements pgx.Row.
type fakeRow struct {
	scanner RowScanner
	err     error
}

func (r *fakeRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if r.scanner != nil {
		return r.scanner(dest...)
	}
	return fmt.Errorf("FakeExecer.QueryRow: ReturnRowScanner not configured")
}

// LastCall returns the most recent ExecCall, or an empty ExecCall if none exist.
func (f *FakeExecer) LastCall() ExecCall {
	if len(f.Calls) == 0 {
		return ExecCall{}
	}
	return f.Calls[len(f.Calls)-1]
}
