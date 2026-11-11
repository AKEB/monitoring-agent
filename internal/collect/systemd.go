package collect

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

type systemdCPUSample struct {
	usageUsec uint64
	at        time.Time
}

type systemdPeak struct {
	cpuPct   float64
	memBytes uint64
}

func (c *Collector) collectSystemd() *SystemdInfo {
	if runtime.GOOS != "linux" {
		return &SystemdInfo{Available: false, Units: []SystemdUnit{}}
	}
	if _, err := os.Stat("/run/systemd/system"); err != nil {
		return &SystemdInfo{Available: false, Units: []SystemdUnit{}}
	}
	units, err := listSystemdServices()
	if err != nil || len(units) == 0 {
		c.cfg.Logf("systemd list: %v", err)
		return &SystemdInfo{Available: false, Units: []SystemdUnit{}}
	}

	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.systemdPrevCPU == nil {
		c.systemdPrevCPU = map[string]systemdCPUSample{}
	}
	if c.systemdPeaks == nil {
		c.systemdPeaks = map[string]systemdPeak{}
	}

	nextCPU := map[string]systemdCPUSample{}
	out := make([]SystemdUnit, 0, len(units))
	for _, u := range units {
		name := strings.TrimSpace(u.Name)
		if name == "" || !strings.HasSuffix(name, ".service") {
			continue
		}
		active := strings.ToLower(strings.TrimSpace(u.ActiveState))
		if active != "active" && active != "failed" && active != "activating" && active != "deactivating" && active != "inactive" {
			continue
		}

		usageUsec, memBytes := readUnitCgroup(name)
		cpuPct := 0.0
		if usageUsec > 0 {
			nextCPU[name] = systemdCPUSample{usageUsec: usageUsec, at: now}
			if prev, ok := c.systemdPrevCPU[name]; ok && now.After(prev.at) && usageUsec >= prev.usageUsec {
				elapsed := now.Sub(prev.at).Seconds()
				if elapsed > 0 {
					delta := float64(usageUsec - prev.usageUsec)
					cpuPct = round2((delta / 1e6) / elapsed * 100.0)
					if cpuPct < 0 {
						cpuPct = 0
					}
				}
			}
		}

		peak := c.systemdPeaks[name]
		if cpuPct > peak.cpuPct {
			peak.cpuPct = cpuPct
		}
		if memBytes > peak.memBytes {
			peak.memBytes = memBytes
		}
		c.systemdPeaks[name] = peak

		out = append(out, SystemdUnit{
			Name:         name,
			LoadState:    u.LoadState,
			ActiveState:  u.ActiveState,
			SubState:     u.SubState,
			CPUPct:       cpuPct,
			CPUPeakPct:   round2(peak.cpuPct),
			MemBytes:     memBytes,
			MemPeakBytes: peak.memBytes,
		})
	}
	c.systemdPrevCPU = nextCPU
	return &SystemdInfo{Available: true, Units: out}
}

type listedUnit struct {
	Name        string
	LoadState   string
	ActiveState string
	SubState    string
}

func listSystemdServices() ([]listedUnit, error) {
	cmd := exec.Command("systemctl", "list-units", "--type=service", "--all", "--no-legend", "--no-pager", "--plain")
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var units []listedUnit
	sc := bufio.NewScanner(strings.NewReader(string(out)))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		// systemctl may prefix with "● "
		line = strings.TrimPrefix(line, "● ")
		line = strings.TrimSpace(line)
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		name := fields[0]
		if !strings.HasSuffix(name, ".service") {
			continue
		}
		units = append(units, listedUnit{
			Name:        name,
			LoadState:   fields[1],
			ActiveState: fields[2],
			SubState:    fields[3],
		})
	}
	return units, sc.Err()
}

func readUnitCgroup(unit string) (usageUsec uint64, memBytes uint64) {
	paths := []string{
		filepath.Join("/sys/fs/cgroup/system.slice", unit),
		filepath.Join("/sys/fs/cgroup/systemd/system.slice", unit),
	}
	for _, base := range paths {
		if st, err := os.Stat(base); err != nil || !st.IsDir() {
			continue
		}
		if usageUsec == 0 {
			usageUsec = readCPUStatUsage(filepath.Join(base, "cpu.stat"))
			if usageUsec == 0 {
				usageUsec = readUintFile(filepath.Join(base, "cpuacct.usage"))
				// cpuacct.usage is nanoseconds
				if usageUsec > 0 {
					usageUsec = usageUsec / 1000
				}
			}
		}
		if memBytes == 0 {
			memBytes = readUintFile(filepath.Join(base, "memory.current"))
			if memBytes == 0 {
				memBytes = readUintFile(filepath.Join(base, "memory.usage_in_bytes"))
			}
		}
		if usageUsec > 0 || memBytes > 0 {
			return usageUsec, memBytes
		}
	}
	return 0, 0
}

func readCPUStatUsage(path string) uint64 {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "usage_usec ") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				v, _ := strconv.ParseUint(parts[1], 10, 64)
				return v
			}
		}
	}
	return 0
}

func readUintFile(path string) uint64 {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	v, err := strconv.ParseUint(strings.TrimSpace(string(b)), 10, 64)
	if err != nil {
		return 0
	}
	return v
}

// cpuPctFromUsageDelta is exported for tests.
func cpuPctFromUsageDelta(prevUsec, currUsec uint64, elapsedSec float64) float64 {
	if elapsedSec <= 0 || currUsec < prevUsec {
		return 0
	}
	delta := float64(currUsec - prevUsec)
	return round2((delta / 1e6) / elapsedSec * 100.0)
}
