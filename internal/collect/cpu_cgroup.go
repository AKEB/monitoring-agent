package collect

import (
	"bufio"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// cpuUsageFromCgroup returns CPU busy % of this OS instance's capacity (0..100+),
// matching Proxmox/LXC accounting via cgroup cpu.stat rather than host /proc/stat.
// ok is false on the first sample or when cgroup CPU accounting is unavailable.
func (c *Collector) cpuUsageFromCgroup(fallbackCores int) (pct float64, ok bool) {
	if runtime.GOOS != "linux" {
		return 0, false
	}
	usageUsec, quotaCores, found := readEnvCPUUsage()
	if !found || usageUsec == 0 {
		return 0, false
	}
	cores := quotaCores
	if cores <= 0 {
		cores = float64(fallbackCores)
	}
	if cores <= 0 {
		cores = 1
	}

	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	prev := c.prevCgroupCPU
	c.prevCgroupCPU = systemdCPUSample{usageUsec: usageUsec, at: now}
	if prev.usageUsec == 0 || !now.After(prev.at) || usageUsec < prev.usageUsec {
		return 0, false
	}
	elapsed := now.Sub(prev.at).Seconds()
	if elapsed <= 0 {
		return 0, false
	}
	return cpuPctOfCapacity(prev.usageUsec, usageUsec, elapsed, cores), true
}

// cpuPctOfCapacity: (CPU-seconds used / wall-seconds / cores) * 100.
func cpuPctOfCapacity(prevUsec, currUsec uint64, elapsedSec, cores float64) float64 {
	if elapsedSec <= 0 || currUsec < prevUsec || cores <= 0 {
		return 0
	}
	delta := float64(currUsec - prevUsec)
	pct := (delta / 1e6) / elapsedSec / cores * 100.0
	if pct < 0 {
		return 0
	}
	return round2(pct)
}

func readEnvCPUUsage() (usageUsec uint64, quotaCores float64, ok bool) {
	for _, base := range envCgroupBases() {
		if base == "" {
			continue
		}
		u := readCPUStatUsage(filepath.Join(base, "cpu.stat"))
		if u == 0 {
			// cgroup v1 cpuacct is nanoseconds
			if ns := readUintFile(filepath.Join(base, "cpuacct.usage")); ns > 0 {
				u = ns / 1000
			}
		}
		if u == 0 {
			continue
		}
		return u, readCPUQuotaCores(base), true
	}
	return 0, 0, false
}

// envCgroupBases returns candidate roots for "this machine/container", not the
// agent's own systemd slice (which would only measure the agent process).
func envCgroupBases() []string {
	var out []string
	seen := map[string]struct{}{}
	add := func(p string) {
		p = filepath.Clean(strings.TrimSpace(p))
		if p == "" || p == "." {
			return
		}
		if _, ok := seen[p]; ok {
			return
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}

	// Prefer PID 1's cgroup: on a shared host hierarchy this is the LXC/VM root
	// (not /system.slice/monitoring-agent.service). Empty/"/" means we already
	// sit at the cgroup namespace root (host or container with private ns).
	if rel := cgroupV2PathFromProc(1); rel != "" {
		add(filepath.Join("/sys/fs/cgroup", rel))
	}
	if rel := cgroupV1PathFromProc(1, "cpuacct"); rel != "" {
		add(filepath.Join("/sys/fs/cgroup/cpuacct", rel))
		add(filepath.Join("/sys/fs/cgroup/cpu,cpuacct", rel))
	}

	// Namespace root / host root fallback.
	add("/sys/fs/cgroup")
	add("/sys/fs/cgroup/cpuacct")
	add("/sys/fs/cgroup/cpu,cpuacct")
	return out
}

func cgroupV2PathFromProc(pid int) string {
	f, err := os.Open(filepath.Join("/proc", strconv.Itoa(pid), "cgroup"))
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		// v2: "0::/lxc/2006" or "0::/"
		if strings.HasPrefix(line, "0::") {
			rel := strings.TrimPrefix(line, "0::")
			if rel == "/" || rel == "" {
				return ""
			}
			return strings.TrimPrefix(rel, "/")
		}
	}
	return ""
}

func cgroupV1PathFromProc(pid int, controller string) string {
	f, err := os.Open(filepath.Join("/proc", strconv.Itoa(pid), "cgroup"))
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		// v1: "2:cpu,cpuacct:/lxc/2006"
		parts := strings.SplitN(sc.Text(), ":", 3)
		if len(parts) != 3 {
			continue
		}
		for _, c := range strings.Split(parts[1], ",") {
			if c == controller || c == "cpu" {
				rel := parts[2]
				if rel == "/" || rel == "" {
					return ""
				}
				return strings.TrimPrefix(rel, "/")
			}
		}
	}
	return ""
}

func readCPUQuotaCores(base string) float64 {
	// cgroup v2: "max 100000" or "400000 100000"
	b, err := os.ReadFile(filepath.Join(base, "cpu.max"))
	if err == nil {
		fields := strings.Fields(string(b))
		if len(fields) >= 2 && fields[0] != "max" {
			quota, err1 := strconv.ParseFloat(fields[0], 64)
			period, err2 := strconv.ParseFloat(fields[1], 64)
			if err1 == nil && err2 == nil && period > 0 && quota > 0 {
				return quota / period
			}
		}
	}
	// cgroup v1
	quota := readIntFile(filepath.Join(base, "cpu.cfs_quota_us"))
	period := readIntFile(filepath.Join(base, "cpu.cfs_period_us"))
	if quota > 0 && period > 0 {
		return float64(quota) / float64(period)
	}
	return 0
}

func readIntFile(path string) int64 {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	v, err := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
	if err != nil {
		return 0
	}
	return v
}
