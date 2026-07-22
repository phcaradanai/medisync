// Command sync-relay bridges PrintOps sticker printer results back to MediSync.
// It polls PrintOps API for completed jobs and publishes real PrintCompleted events to NATS.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/adm-chura3inter/medisync/services/sync-relay/internal/config"
	"github.com/adm-chura3inter/medisync/services/sync-relay/internal/cursor"
)

const subjectPrintCompleted = "medisync.print.completed"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg := config.Load()
	log.SetFlags(log.LstdFlags | log.Lmsgprefix)
	log.SetPrefix("[sync-relay] ")

	// Connect NATS
	nc, err := nats.Connect(cfg.NATSURL, nats.RetryOnFailedConnect(true), nats.MaxReconnects(-1))
	if err != nil {
		return fmt.Errorf("nats connect: %w", err)
	}
	defer nc.Drain()
	js, err := nc.JetStream()
	if err != nil {
		return fmt.Errorf("jetstream: %w", err)
	}

	// Open cursor DB
	store, err := cursor.Open(cfg.CursorDBPath)
	if err != nil {
		return fmt.Errorf("cursor db: %w", err)
	}
	defer store.Close()

	// Poll PrintOps for completed jobs + relay to NATS
	client := &http.Client{Timeout: 30 * time.Second}
	ticker := time.NewTicker(cfg.PollInterval)
	defer ticker.Stop()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Initial poll immediately
	poll(ctx, cfg, store, client, js)

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			poll(ctx, cfg, store, client, js)
		}
	}
}

type printJob struct {
	ID               string `json:"id"`
	RequestID        string `json:"request_id"`
	SourceSystem     string `json:"source_system"`
	SourceReference  string `json:"source_reference"`
	ProjectID        string `json:"project_id"`
	Status           string `json:"status"`
	CompletedAt      string `json:"completedAt"`
	ErrorCode        string `json:"errorCode"`
	ErrorMessage     string `json:"errorMessage"`
}

type printCompleted struct {
	PrintID        string `json:"print_id"`
	PrescriptionID string `json:"prescription_id"`
	Success        bool   `json:"success"`
	Detail         string `json:"detail"`
	TraceID        string `json:"trace_id"`
	ProjectID      string `json:"project_id"`
}

func poll(ctx context.Context, cfg config.Config, store *cursor.Store, client *http.Client, js nats.JetStreamContext) {
	c := store.Cursor(cfg.StartupLookbackMinutes)

	// FIX #1: cursor is sent as `since` query param to PrintOps
	url := fmt.Sprintf("%s/api/v1/print-jobs?status=SUCCESS,FAILED&limit=%d&since=%s",
		cfg.PrintOpsURL, cfg.PollPageSize, c)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		log.Printf("ERROR build request: %v", err)
		return
	}
	req.Header.Set("X-Api-Key", cfg.PrintOpsAPIKey)

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("WARN PrintOps unreachable: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		log.Printf("WARN PrintOps returned %d: %s", resp.StatusCode, string(body))
		return
	}

	var jobs []printJob
	if err := json.NewDecoder(resp.Body).Decode(&jobs); err != nil {
		log.Printf("ERROR decode response: %v", err)
		return
	}

	var newCursor string
	relayed := 0
	for _, job := range jobs {
		if job.SourceSystem != "medisync" {
			continue
		}
		// FIX #5: extract project_id from job metadata
		projectID := job.ProjectID
		if projectID == "" {
			projectID = "default"
		}

		// FIX #2: dedup check before publish
		if relayed, _ := store.AlreadyRelayed(job.RequestID); relayed {
			continue
		}

		payload := printCompleted{
			PrintID:        job.RequestID,
			PrescriptionID: job.SourceReference,
			Success:        job.Status == "SUCCESS",
			Detail:         job.Status,
			TraceID:        job.ID,
			ProjectID:      projectID,
		}
		if job.ErrorMessage != "" {
			payload.Detail = job.Status + ": " + job.ErrorMessage
		}

		data, _ := json.Marshal(payload)

		// Publish with NATS dedup window (2 min) — idempotent replay protection
		if _, err := js.Publish(subjectPrintCompleted, data, nats.MsgId(job.RequestID)); err != nil {
			log.Printf("ERROR publish %s: %v", job.RequestID, err)
			continue
		}

		// Record relay (best-effort — NATS dedup is the real guard)
		_ = store.RecordRelay(job.ID, job.RequestID, projectID, job.Status, job.SourceReference)
		relayed++

		if job.CompletedAt > newCursor {
			newCursor = job.CompletedAt
		}
	}

	if newCursor != "" {
		_ = store.UpdateCursor(newCursor)
	}
	if relayed > 0 {
		log.Printf("INFO relayed %d jobs, cursor=%s", relayed, newCursor)
	}
}
