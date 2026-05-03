package tests

import (
	"testing"
	"time"

	"github.com/masoudx/monitoring24/internal/sse"
)

func TestSSE_Broker_BroadcastNoPanicWithoutClients(t *testing.T) {
	// given
	b := sse.NewBroker()
	done := make(chan struct{})
	go b.Run(done)
	defer close(done)

	// when
	for i := 0; i < 20; i++ {
		b.BroadcastJSON("metrics", map[string]int{"n": i})
	}

	// then
	time.Sleep(20 * time.Millisecond)
}

func TestSSE_Broker_ClientCount(t *testing.T) {
	// given
	b := sse.NewBroker()
	done := make(chan struct{})
	go b.Run(done)
	defer close(done)

	// when
	n := b.ClientCount()

	// then
	if n != 0 {
		t.Fatalf("expected 0 clients, got %d", n)
	}
}
