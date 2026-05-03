package tests

import (
	"testing"

	"github.com/masoudx/monitoring24/internal/config"
)

func TestConfig_AuthEnabled(t *testing.T) {
	// given
	cfg := &config.Config{}

	// when
	off := cfg.AuthEnabled()

	// then
	if off {
		t.Fatalf("expected auth disabled when hash is nil")
	}

	// given
	cfg.BasicAuthHash = []byte("not-empty")

	// when
	on := cfg.AuthEnabled()

	// then
	if !on {
		t.Fatalf("expected auth enabled when hash is set")
	}
}

func TestConfig_Addr(t *testing.T) {
	// given
	cfg := &config.Config{Host: "127.0.0.1", Port: 47291}

	// when
	addr := cfg.Addr()

	// then
	if addr != "127.0.0.1:47291" {
		t.Fatalf("Addr: got %q", addr)
	}
}
