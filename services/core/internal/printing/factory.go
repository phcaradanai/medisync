package printing

import (
	"github.com/adm-chura3inter/medisync/services/core/internal/platform/config"
)

// NewClientFromConfig returns the real or fake print_ops client based on config.
func NewClientFromConfig(cfg config.Config) Client {
	if cfg.PrintOpsFake {
		return NewFakeClient()
	}
	return NewClient(cfg)
}
