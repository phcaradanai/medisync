package printing

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/adm-chura3inter/medisync/services/core/internal/platform/config"
)

// defaultPrintOpsPath is the legacy endpoint used when the configured path
// template contains no dynamic placeholder.
const defaultPrintOpsPath = "/api/v1/print-jobs"

// httpClient is the real print_ops HTTP client.
type httpClient struct {
	baseURL      string
	apiKey       string
	pathTemplate string
	http         *http.Client
}

// NewClient creates the real print_ops HTTP client from config.
// Callers should use NewClientFromConfig or NewFakeClient for tests.
func NewClient(cfg config.Config) Client {
	pathTemplate := cfg.PrintOpsPathTemplate
	if strings.TrimSpace(pathTemplate) == "" {
		pathTemplate = defaultPrintOpsPath
	}
	return &httpClient{
		baseURL:      cfg.PrintOpsURL,
		apiKey:       cfg.PrintOpsAPIKey,
		pathTemplate: pathTemplate,
		http: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// isDynamic reports whether the path template addresses the dynamic endpoint
// (carries the template/profile in the URL path).
func (c *httpClient) isDynamic() bool {
	return strings.Contains(c.pathTemplate, "{{code_template}}") ||
		strings.Contains(c.pathTemplate, "{{code_profile}}")
}

// dynamicPrintBody is the body posted to the dynamic endpoint. The template and
// profile travel in the URL path, so they are omitted here; printer_code is an
// optional override.
type dynamicPrintBody struct {
	RequestID       string         `json:"request_id"`
	SourceSystem    string         `json:"source_system"`
	SourceReference string         `json:"source_reference,omitempty"`
	PrinterCode     string         `json:"printer_code,omitempty"`
	Payload         map[string]any `json:"payload"`
	Copies          int            `json:"copies,omitempty"`
	Metadata        map[string]any `json:"metadata,omitempty"`
}

// SubmitJob posts a print job to print_ops. For the legacy path the full
// PrintJobRequest is sent to /api/v1/print-jobs. For a dynamic path template the
// template/profile codes are substituted into the URL and a reduced body is
// sent. Both use X-Api-Key and an idempotent request_id.
func (c *httpClient) SubmitJob(ctx context.Context, req PrintJobRequest) (*PrintJobResponse, error) {
	path, body, err := c.buildRequest(req)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-Api-Key", c.apiKey)

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("post print job: %w", err)
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

// buildRequest resolves the target path and JSON body for a request.
func (c *httpClient) buildRequest(req PrintJobRequest) (string, []byte, error) {
	if !c.isDynamic() {
		body, err := json.Marshal(req)
		if err != nil {
			return "", nil, fmt.Errorf("marshal print job request: %w", err)
		}
		return c.pathTemplate, body, nil
	}

	codeTemplate := req.CodeTemplate
	if codeTemplate == "" {
		codeTemplate = req.TemplateCode
	}
	codeProfile := req.CodeProfile
	if codeTemplate == "" || codeProfile == "" {
		return "", nil, fmt.Errorf(
			"dynamic print path requires code_template and code_profile (template=%q profile=%q)",
			codeTemplate, codeProfile,
		)
	}

	path := c.pathTemplate
	path = strings.ReplaceAll(path, "{{code_template}}", url.PathEscape(codeTemplate))
	path = strings.ReplaceAll(path, "{{code_profile}}", url.PathEscape(codeProfile))

	body, err := json.Marshal(dynamicPrintBody{
		RequestID:       req.RequestID,
		SourceSystem:    req.SourceSystem,
		SourceReference: req.SourceReference,
		PrinterCode:     req.PrinterCode,
		Payload:         req.Payload,
		Copies:          req.Copies,
		Metadata:        req.Metadata,
	})
	if err != nil {
		return "", nil, fmt.Errorf("marshal dynamic print body: %w", err)
	}
	return path, body, nil
}
