package storage

import "testing"

func TestCombinedDiskAggBytes(t *testing.T) {
	// given
	disks := []DiskSample{
		{Used: 50, Total: 100},
		{Used: 25, Total: 100},
	}

	// when
	gotU, gotT := CombinedDiskAggBytes(disks)

	// then
	if gotU != 75 || gotT != 200 {
		t.Fatalf("got used=%d total=%d want 75 200", gotU, gotT)
	}

	// when
	u, to := CombinedDiskAggBytes(nil)

	// then
	if u != 0 || to != 0 {
		t.Fatalf("nil disks: got %d %d", u, to)
	}
}

func TestCombinedDiskUsagePct(t *testing.T) {
	// given
	disks := []DiskSample{
		{Used: 50, Total: 100},
		{Used: 25, Total: 100},
	}
	want := 100.0 * 75.0 / 200.0

	// when
	got := CombinedDiskUsagePct(disks)

	// then
	if got != want {
		t.Fatalf("got %v want %v", got, want)
	}

	// when
	emptyNil := CombinedDiskUsagePct(nil)

	// then
	if emptyNil != 0 {
		t.Fatal("empty disks should be 0")
	}

	// when
	zeroTotal := CombinedDiskUsagePct([]DiskSample{{Used: 1, Total: 0}})

	// then
	if zeroTotal != 0 {
		t.Fatal("zero total should be 0")
	}
}
