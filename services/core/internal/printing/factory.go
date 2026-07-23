package printing

import (
	"github.com/nats-io/nats.go/jetstream"

	"github.com/adm-chura3inter/medisync/services/core/internal/platform/config"
)

// NewClientFromConfig returns the print_ops client based on config. When
// PrintOpsFake is set it returns the no-op fake. Otherwise it returns a
// dispatcher that routes each request to the HTTP or NATS transport (per-request
// override, falling back to cfg.PrintOpsTransport). js may be nil when only the
// HTTP transport is used; a NATS request then fails fast.
func NewClientFromConfig(cfg config.Config, js jetstream.JetStream) Client {
	if cfg.PrintOpsFake {
		return NewFakeClient()
	}
	return &dispatcherClient{
		http:             NewClient(cfg),
		nats:             newNATSClient(js, cfg.PrintOpsNATSSubject),
		defaultTransport: cfg.PrintOpsTransport,
	}
}
