package collect

import "testing"

func TestCPUPctOfCapacity(t *testing.T) {
	// 500ms of CPU over 1s on 4 cores => 12.5% of capacity
	got := cpuPctOfCapacity(0, 500_000, 1.0, 4)
	if got != 12.5 {
		t.Fatalf("got %v want 12.5", got)
	}
	// One full core busy for 1s on 1 core => 100%
	if cpuPctOfCapacity(0, 1_000_000, 1.0, 1) != 100 {
		t.Fatalf("expected 100")
	}
	// Matches PVE-style: 40ms CPU / 1s / 4 cores = 1%
	if cpuPctOfCapacity(0, 40_000, 1.0, 4) != 1 {
		t.Fatalf("expected 1, got %v", cpuPctOfCapacity(0, 40_000, 1.0, 4))
	}
	if cpuPctOfCapacity(10, 5, 1.0, 4) != 0 {
		t.Fatalf("expected 0 on rewind")
	}
	if cpuPctOfCapacity(0, 1_000_000, 0, 4) != 0 {
		t.Fatalf("expected 0 on zero elapsed")
	}
}

func TestCPUPctFromUsageDeltaUnchanged(t *testing.T) {
	// systemd path stays "percent of one core" (can exceed 100)
	got := cpuPctFromUsageDelta(1_000_000, 1_500_000, 1.0)
	if got != 50 {
		t.Fatalf("got %v want 50", got)
	}
}
