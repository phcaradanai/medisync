// Package testutil provides test helpers shared across bounded contexts.
package testutil

import (
	"context"

	"github.com/jackc/pgx/v5/pgconn"
)

// Execer is the narrow database interface used by Store and audit.Writer.
// *pgxpool.Pool satisfies this interface, allowing production code to pass
// the pool directly while tests inject a deterministic FakeExecer.
type Execer interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
}

// FakeExecer is an in-memory Execer for unit tests. It records every
// Exec call and returns values configured through ReturnTag and ReturnErr.
type FakeExecer struct {
	Calls     []ExecCall
	ReturnTag pgconn.CommandTag
	ReturnErr error
}

// ExecCall records the arguments of a single Exec call.
type ExecCall struct {
	SQL  string
	Args []any
}

func (f *FakeExecer) Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
	f.Calls = append(f.Calls, ExecCall{SQL: sql, Args: arguments})
	return f.ReturnTag, f.ReturnErr
}

// LastCall returns the most recent ExecCall, or an empty ExecCall if none exist.
func (f *FakeExecer) LastCall() ExecCall {
	if len(f.Calls) == 0 {
		return ExecCall{}
	}
	return f.Calls[len(f.Calls)-1]
}
