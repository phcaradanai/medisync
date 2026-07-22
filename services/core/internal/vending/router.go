package vending

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/adm-chura3inter/medisync/services/core/internal/platform/config"
)

// Router selects exactly one vending agent from the immutable kiosk code.
type Router interface {
	ClientFor(kioskCode string) (Client, error)
}

type StaticRouter struct {
	mu        sync.Mutex
	endpoints map[string]string
	apiKey    string
	fake      bool
	clients   map[string]Client
}

func NewRouterFromConfig(cfg config.Config) (*StaticRouter, error) {
	router := &StaticRouter{
		endpoints: map[string]string{}, apiKey: cfg.VendingAPIBearerToken,
		fake: cfg.FulfillmentFake || cfg.VendingFake, clients: map[string]Client{},
	}
	if strings.TrimSpace(cfg.VendingEndpointsJSON) != "" {
		if err := json.Unmarshal([]byte(cfg.VendingEndpointsJSON), &router.endpoints); err != nil {
			return nil, fmt.Errorf("parse VENDING_ENDPOINTS_JSON: %w", err)
		}
	}
	for code, endpoint := range router.endpoints {
		if !validKioskCode(code) {
			return nil, fmt.Errorf("invalid kiosk code %q in VENDING_ENDPOINTS_JSON", code)
		}
		if strings.TrimSpace(endpoint) == "" {
			return nil, fmt.Errorf("empty vending endpoint for kiosk %s", code)
		}
	}
	return router, nil
}

func (r *StaticRouter) ClientFor(kioskCode string) (Client, error) {
	if !validKioskCode(kioskCode) {
		return nil, fmt.Errorf("invalid kiosk code %q", kioskCode)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if client := r.clients[kioskCode]; client != nil {
		return client, nil
	}
	if r.fake {
		client := NewFakeClient()
		r.clients[kioskCode] = client
		return client, nil
	}
	endpoint := strings.TrimSpace(r.endpoints[kioskCode])
	if endpoint == "" {
		return nil, fmt.Errorf("no vending agent configured for kiosk %s", kioskCode)
	}
	client := NewClient(endpoint, r.apiKey)
	r.clients[kioskCode] = client
	return client, nil
}

// OpenScannerEvents opens the QR/NFC SSE stream for exactly one kiosk code.
// The caller owns and must close the returned response body.
func (r *StaticRouter) OpenScannerEvents(ctx context.Context, kioskCode string) (*http.Response, error) {
	if !validKioskCode(kioskCode) {
		return nil, fmt.Errorf("invalid kiosk code %q", kioskCode)
	}
	r.mu.Lock()
	endpoint := strings.TrimSpace(r.endpoints[kioskCode])
	apiKey := r.apiKey
	r.mu.Unlock()
	if endpoint == "" {
		return nil, fmt.Errorf("no vending agent configured for kiosk %s", kioskCode)
	}
	return openScannerEvents(ctx, endpoint, apiKey)
}

func openScannerEvents(ctx context.Context, endpoint, apiKey string) (*http.Response, error) {
	client := NewClient(endpoint, apiKey)
	return client.OpenScannerEvents(ctx)
}

func validKioskCode(code string) bool {
	if len(code) != 8 || code[:4] == "0000" || code[4:] == "0000" {
		return false
	}
	for _, ch := range code {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
}

type singleClientRouter struct{ client Client }

func (r singleClientRouter) ClientFor(string) (Client, error) { return r.client, nil }
