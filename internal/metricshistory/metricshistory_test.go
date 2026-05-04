package metricshistory

import (
	"testing"
	"time"
)

func TestResolveQuery_clampsToAndMaxWindow(t *testing.T) {
	// given
	now := time.Unix(1_700_000_000, 0)
	to := now.Unix() + 3600
	from := to - int64((40 * 24 * time.Hour).Seconds())

	// when
	q, err := ResolveQuery("cpu", from, to, 60, now)

	// then
	if err != nil {
		t.Fatal(err)
	}
	if q.ToUnix != now.Unix() {
		t.Fatalf("expected to clamped to now, got %d want %d", q.ToUnix, now.Unix())
	}
	wantFrom := q.ToUnix - int64(MaxRange.Seconds())
	if q.FromUnix != wantFrom {
		t.Fatalf("from: got %d want %d", q.FromUnix, wantFrom)
	}
}

func TestResolveQuery_invalidKind(t *testing.T) {
	// given
	now := time.Now()

	// when
	_, err := ResolveQuery("swap", 1, 2, 60, now)

	// then
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestResolveQuery_invalidStep(t *testing.T) {
	// given
	now := time.Now()

	// when
	_, err := ResolveQuery("cpu", 1, 2, 61, now)

	// then
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestResolveQuery_fromAfterTo(t *testing.T) {
	// given
	now := time.Now()

	// when
	_, err := ResolveQuery("cpu", 100, 50, 60, now)

	// then
	if err == nil {
		t.Fatal("expected error")
	}
}
