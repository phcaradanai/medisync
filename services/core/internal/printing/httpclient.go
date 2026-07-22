package printing

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/adm-chura3inter/medisync/services/core/internal/platform/config"
)

// httpClient is the real print_ops HTTP client.
type httpClient struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

// NewClient creates the real print_ops HTTP client from config.
// Callers should use NewClientFromConfig or NewFakeClient for tests.
func NewClient(cfg config.Config) Client {
	return &httpClient{
		baseURL: cfg.PrintOpsURL,
		apiKey:  cfg.PrintOpsAPIKey,
		http: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// SubmitJob posts a print job to POST /api/v1/print-jobs.
// Uses X-Api-Key header and an idempotent request_id.
func (c *httpClient) SubmitJob(ctx context.Context, req PrintJobRequest) (*PrintJobResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal print job request: %w", err)
	}

	url := c.baseURL + "/api/v1/print-jobs"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-Api-Key", c.apiKey)

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("post print_jobs: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}

	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("print_ops returned %d: %s", resp.StatusCode, string(respBody))
	}

	var jobResp PrintJobResponse
	if err := json.Unmarshal(respBody, &jobResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}
	return &jobResp, nil
}
