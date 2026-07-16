package vending

import (
	"github.com/adm-chura3inter/medisync/services/core/internal/platform/config"
)

// NewClientFromConfig returns the real or fake vending client based on config.
// FulfillmentFake takes precedence over the deprecated VendingFake flag.
func NewClientFromConfig(cfg config.Config) Client {
	fake := cfg.FulfillmentFake || cfg.VendingFake
	if fake {
		return NewFakeClient()
	}
	return NewClient(cfg.VendingURL, cfg.VendingAPIBearerToken)
}
