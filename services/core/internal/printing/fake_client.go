package printing

import (
	"context"
	"fmt"
	"sync"
)

// NewFakeClient returns a no-op fake print_ops client for dev/testing.
// Records all submitted jobs for verification in tests.
func NewFakeClient() *FakeClient {
	return &FakeClient{}
}

// FakeClient is an in-memory print_ops client that always succeeds.
// Use SubmitJobErr to simulate failures in tests.
type FakeClient struct {
	mu          sync.Mutex
	Jobs        []PrintJobRequest
	SubmitJobFn func(ctx context.Context, req PrintJobRequest) (*PrintJobResponse, error)
}

func (f *FakeClient) SubmitJob(ctx context.Context, req PrintJobRequest) (*PrintJobResponse, error) {
	f.mu.Lock()
	f.Jobs = append(f.Jobs, req)
	f.mu.Unlock()

	if f.SubmitJobFn != nil {
		return f.SubmitJobFn(ctx, req)
	}

	// Derive an id from the request_id.
	id := fmt.Sprintf("fake-job-%s", req.RequestID)
	return &PrintJobResponse{
		ID:        id,
		Status:    "PENDING",
		Duplicate: false,
	}, nil
}

// LastJob returns the most recently submitted job, or an empty request if none.
func (f *FakeClient) LastJob() PrintJobRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.Jobs) == 0 {
		return PrintJobRequest{}
	}
	return f.Jobs[len(f.Jobs)-1]
}

// JobCount returns the number of submitted jobs.
func (f *FakeClient) JobCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.Jobs)
}

// Reset clears all recorded jobs.
func (f *FakeClient) Reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Jobs = nil
	f.SubmitJobFn = nil
}

// Ensure FakeClient satisfies Client.
var _ Client = (*FakeClient)(nil)

// NewFailClient returns a fake that always fails with the given reason.
func NewFailClient(reason string) *FakeClient {
	f := NewFakeClient()
	f.SubmitJobFn = func(_ context.Context, _ PrintJobRequest) (*PrintJobResponse, error) {
		return nil, fmt.Errorf("print_ops fake failure: %s", reason)
	}
	return f
}

// NewFailClientOnce returns a fake that succeeds N times then fails.
func NewFailClientOnce(succeedN int, reason string) *FakeClient {
	f := NewFakeClient()
	var callCount int
	var mu sync.Mutex
	f.SubmitJobFn = func(_ context.Context, _ PrintJobRequest) (*PrintJobResponse, error) {
		mu.Lock()
		defer mu.Unlock()
		callCount++
		if callCount <= succeedN {
			return &PrintJobResponse{
				ID:     fmt.Sprintf("fake-job-%d", callCount),
				Status: "PENDING",
			}, nil
		}
		return nil, fmt.Errorf("print_ops fake failure: %s", reason)
	}
	return f
}
