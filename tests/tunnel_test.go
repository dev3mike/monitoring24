package tests

import (
	"testing"

	"github.com/masoudx/monitoring24/internal/tunnel"
)

func TestTunnel_TunnelNameFromArgs_NameFlag(t *testing.T) {
	// given
	args := []string{"/usr/bin/cloudflared", "tunnel", "--name", "my-tunnel", "run"}

	// when
	name := tunnel.TunnelNameFromArgs(args)

	// then
	if name != "my-tunnel" {
		t.Fatalf("got %q", name)
	}
}

func TestTunnel_TunnelNameFromArgs_RunSubcommand(t *testing.T) {
	// given
	args := []string{"cloudflared", "tunnel", "run", "edge-prod"}

	// when
	name := tunnel.TunnelNameFromArgs(args)

	// then
	if name != "edge-prod" {
		t.Fatalf("got %q", name)
	}
}

func TestTunnel_TunnelNameFromArgs_Empty(t *testing.T) {
	// given
	args := []string{"cloudflared", "version"}

	// when
	name := tunnel.TunnelNameFromArgs(args)

	// then
	if name != "" {
		t.Fatalf("expected empty, got %q", name)
	}
}
