package vending

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	defaultTimeout    = 30 * time.Second
	defaultDoorNo     = 1
)

// httpClient is the real vending-3d-ctl-agent HTTP client.
type httpClient struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

// NewClient creates the real vending HTTP client.
func NewClient(baseURL, apiKey string) *httpClient {
	return &httpClient{
		baseURL: baseURL,
		apiKey:  apiKey,
		http: &http.Client{
			Timeout: defaultTimeout,
		},
	}
}

// Health calls GET /api/v1/health and returns an error when the agent
// is unreachable or reports degraded status.
func (c *httpClient) Health(ctx context.Context) error {
	url := c.baseURL + "/api/v1/health"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("vending health: build request: %w", err)
	}

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return fmt.Errorf("vending health: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("vending health: status %d: %s", resp.StatusCode, string(body))
	}

	// Verify the agent reports healthy. The response envelope is:
	// {"status":"ok"|"degraded", ...}
	var health struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		return fmt.Errorf("vending health: decode response: %w", err)
	}
	if !strings.EqualFold(health.Status, "ok") {
		return fmt.Errorf("vending health: agent status is %q, expected \"ok\"", health.Status)
	}
	return nil
}

// Dispense calls POST /api/v1/vending/drugDispenser with Bearer auth.
// HTTP timeouts are enforced by the global client timeout; the vending
// agent enforces its own internal timeout via SERIAL_API_TIMEOUT_MS.
// A 502 or 504 from the agent indicates an internal hardware timeout
// and must be treated as FAILED by callers.
func (c *httpClient) Dispense(ctx context.Context, req DispenseRequest) (*DispenseResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("vending dispense: marshal request: %w", err)
	}

	url := c.baseURL + "/api/v1/vending/drugDispenser"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("vending dispense: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("vending dispense: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("vending dispense: read body: %w", err)
	}

	if resp.StatusCode >= 300 {
		// 504 = hardware timeout, 502 = hardware/parse error.
		// Both signal a failed dispense, not a transient client error.
		return nil, fmt.Errorf("vending dispense: status %d: %s", resp.StatusCode, string(respBody))
	}

	var dr DispenseResponse
	if err := json.Unmarshal(respBody, &dr); err != nil {
		return nil, fmt.Errorf("vending dispense: unmarshal response: %w", err)
	}
	return &dr, nil
}
