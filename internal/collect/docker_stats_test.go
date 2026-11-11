package collect

import "testing"

func TestDockerCPUPercent(t *testing.T) {
	stats := dockerStatsPayload{}
	stats.CPUStats.CPUUsage.TotalUsage = 2000
	stats.CPUStats.CPUUsage.PercpuUsage = []uint64{1, 1}
	stats.CPUStats.SystemCPUUsage = 10000
	stats.CPUStats.OnlineCPUs = 2
	stats.PreCPUStats.CPUUsage.TotalUsage = 1000
	stats.PreCPUStats.SystemCPUUsage = 5000
	got := dockerCPUPercent(stats)
	// (1000/5000)*2*100 = 40
	if got != 40 {
		t.Fatalf("dockerCPUPercent=%v want 40", got)
	}
}

func TestDockerCPUPercentNoDelta(t *testing.T) {
	stats := dockerStatsPayload{}
	if got := dockerCPUPercent(stats); got != 0 {
		t.Fatalf("got %v want 0", got)
	}
}

func TestDockerMemUsageSubtractsInactiveFile(t *testing.T) {
	stats := dockerStatsPayload{}
	stats.MemoryStats.Usage = 1000
	stats.MemoryStats.Stats = map[string]uint64{"inactive_file": 200}
	if got := dockerMemUsage(stats); got != 800 {
		t.Fatalf("got %d want 800", got)
	}
}

func TestCPUPctFromUsageDelta(t *testing.T) {
	// 500_000 usec over 1s = 50% of one core
	got := cpuPctFromUsageDelta(1_000_000, 1_500_000, 1.0)
	if got != 50 {
		t.Fatalf("got %v want 50", got)
	}
	if cpuPctFromUsageDelta(10, 5, 1.0) != 0 {
		t.Fatalf("expected 0 on rewind")
	}
}
