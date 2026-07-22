package vending

import (
	"testing"

	"github.com/adm-chura3inter/medisync/services/core/internal/platform/config"
)

func TestStaticRouterUsesExactImmutableKioskCode(t *testing.T) {
	router, err := NewRouterFromConfig(config.Config{
		VendingEndpointsJSON:   `{"00010001":"http://agent-one:3303","00010002":"http://agent-two:3303"}`,
		VendingAPIBearerToken: "secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	first, err := router.ClientFor("00010001")
	if err != nil {
		t.Fatal(err)
	}
	second, err := router.ClientFor("00010002")
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("different kiosk codes must never share the same routed client")
	}
	if _, err := router.ClientFor("00020001"); err == nil {
		t.Fatal("unconfigured kiosk must fail closed")
	}
}

func TestStaticRouterRejectsNonBusinessCodes(t *testing.T) {
	for _, code := range []string{"1", "DEMO-K1", "00010000", "000100010"} {
		t.Run(code, func(t *testing.T) {
			router, err := NewRouterFromConfig(config.Config{VendingFake: true})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := router.ClientFor(code); err == nil {
				t.Fatalf("expected %q to be rejected", code)
			}
		})
	}
}
