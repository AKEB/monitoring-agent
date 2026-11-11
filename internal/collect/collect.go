package collect

import (
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"monitoring-agent/internal/config"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/net"
)

type Collector struct {
	cfg *config.Config

	mu             sync.Mutex
	prevNet        map[string]net.IOCountersStat
	prevNetAt      time.Time
	authCursor     authCursor
	authSeen       map[string]struct{}
	systemdPrevCPU map[string]systemdCPUSample
	systemdPeaks   map[string]systemdPeak
}

func New(cfg *config.Config) *Collector {
	return &Collector{
		cfg:      cfg,
		prevNet:  map[string]net.IOCountersStat{},
		authSeen: map[string]struct{}{},
	}
}

func (c *Collector) Collect() Report {
	now := time.Now()
	r := Report{
		AgentKeyHash: c.cfg.AgentKeyHash,
		AgentVersion: c.cfg.AgentVersion,
		CollectedAt:  now.Unix(),
		UpdateError:  strings.TrimSpace(c.cfg.LastUpdateError),
		Host:         collectHost(),
		CPU:          collectCPU(),
		Memory:       collectMemory(),
		Swap:         collectSwap(),
		Disks:        collectDisks(),
		Network:      c.collectNetwork(now),
	}
	if c.cfg.CollectTemperatures {
		r.Temperatures = collectTemperatures()
	}
	if c.cfg.CollectDocker {
		r.Docker = collectDocker(c.cfg)
	}
	if c.cfg.CollectProxmox {
		r.Proxmox = collectProxmox(c.cfg)
	}
	if c.cfg.CollectSystemd {
		r.Systemd = c.collectSystemd()
	}
	if c.cfg.CollectAuth {
		r.AuthEvents = c.collectAuth()
	}
	if c.cfg.CollectProcesses {
		r.Processes = collectProcesses(c.cfg.ProcessTopN)
	}
	if c.cfg.CollectListeners {
		r.Listeners = collectListeners()
	}
	return r
}

func collectHost() HostInfo {
	info := HostInfo{OS: runtime.GOOS, Arch: runtime.GOARCH}
	hi, err := host.Info()
	if err == nil && hi != nil {
		info.Hostname = hi.Hostname
		info.Platform = hi.Platform
		info.PlatformVersion = hi.PlatformVersion
		info.Kernel = hi.KernelVersion
		info.UptimeSec = hi.Uptime
		info.Virtualization = hi.VirtualizationSystem
		if info.Virtualization == "" {
			info.Virtualization = hi.VirtualizationRole
		}
	}
	if info.Hostname == "" {
		info.Hostname, _ = os.Hostname()
	}
	if info.Virtualization == "" {
		info.Virtualization = detectVirtFallback()
	}
	if isProxmoxHost() {
		info.Virtualization = "proxmox"
	}
	return info
}

func detectVirtFallback() string {
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return "docker"
	}
	b, err := os.ReadFile("/proc/1/environ")
	if err == nil && strings.Contains(string(b), "container=") {
		return "container"
	}
	if _, err := os.Stat("/dev/lxd/sock"); err == nil {
		return "lxc"
	}
	return "unknown"
}

func collectCPU() CPUInfo {
	out := CPUInfo{}
	if cores, err := cpu.Counts(true); err == nil {
		out.Cores = cores
	}
	if avg, err := load.Avg(); err == nil && avg != nil {
		out.Load1 = avg.Load1
		out.Load5 = avg.Load5
		out.Load15 = avg.Load15
	}
	if pct, err := cpu.Percent(0, false); err == nil && len(pct) > 0 {
		out.UsagePct = round2(pct[0])
	}
	return out
}

func collectMemory() MemInfo {
	out := MemInfo{}
	vm, err := mem.VirtualMemory()
	if err != nil || vm == nil {
		return out
	}
	out.TotalBytes = vm.Total
	out.UsedBytes = vm.Used
	out.AvailableBytes = vm.Available
	out.UsedPct = round2(vm.UsedPercent)
	return out
}

func collectSwap() MemInfo {
	out := MemInfo{}
	sm, err := mem.SwapMemory()
	if err != nil || sm == nil {
		return out
	}
	out.TotalBytes = sm.Total
	out.UsedBytes = sm.Used
	out.UsedPct = round2(sm.UsedPercent)
	return out
}

func collectDisks() []DiskInfo {
	parts, err := disk.Partitions(false)
	if err != nil {
		return nil
	}
	seen := map[string]struct{}{}
	var out []DiskInfo
	skipFS := map[string]struct{}{
		"tmpfs": {}, "devtmpfs": {}, "overlay": {}, "squashfs": {},
		"proc": {}, "sysfs": {}, "cgroup": {}, "cgroup2": {},
		"devpts": {}, "securityfs": {}, "pstore": {}, "bpf": {},
		"tracefs": {}, "debugfs": {}, "fusectl": {}, "configfs": {},
		"mqueue": {}, "hugetlbfs": {}, "rpc_pipefs": {}, "nsfs": {},
		"autofs": {}, "binfmt_misc": {},
	}
	for _, p := range parts {
		if _, skip := skipFS[strings.ToLower(p.Fstype)]; skip {
			continue
		}
		if !strings.HasPrefix(p.Mountpoint, "/") {
			continue
		}
		if _, ok := seen[p.Mountpoint]; ok {
			continue
		}
		usage, err := disk.Usage(p.Mountpoint)
		if err != nil || usage == nil || usage.Total == 0 {
			continue
		}
		seen[p.Mountpoint] = struct{}{}
		out = append(out, DiskInfo{
			Mount:      p.Mountpoint,
			Device:     p.Device,
			FSType:     p.Fstype,
			TotalBytes: usage.Total,
			UsedBytes:  usage.Used,
			FreeBytes:  usage.Free,
			UsedPct:    round2(usage.UsedPercent),
		})
	}
	return out
}

func (c *Collector) collectNetwork(now time.Time) NetworkInfo {
	info := NetworkInfo{Interfaces: []NetIface{}}
	ifaces, err := net.Interfaces()
	if err != nil {
		return info
	}
	addrByName := map[string][]string{}
	for _, iface := range ifaces {
		if iface.Name == "lo" || strings.HasPrefix(iface.Name, "veth") || strings.HasPrefix(iface.Name, "br-") {
			continue
		}
		var addrs []string
		for _, a := range iface.Addrs {
			addrs = append(addrs, a.Addr)
		}
		addrByName[iface.Name] = addrs
	}
	counters, err := net.IOCounters(true)
	if err != nil {
		return info
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	elapsed := now.Sub(c.prevNetAt).Seconds()
	nextPrev := map[string]net.IOCountersStat{}
	for _, ctr := range counters {
		if ctr.Name == "lo" || strings.HasPrefix(ctr.Name, "veth") || strings.HasPrefix(ctr.Name, "br-") {
			continue
		}
		addrs := addrByName[ctr.Name]
		if len(addrs) == 0 {
			continue
		}
		ni := NetIface{
			Name:    ctr.Name,
			Addrs:   addrs,
			RxBytes: ctr.BytesRecv,
			TxBytes: ctr.BytesSent,
		}
		if prev, ok := c.prevNet[ctr.Name]; ok && elapsed > 0 {
			ni.RxBps = float64(ctr.BytesRecv-prev.BytesRecv) / elapsed
			ni.TxBps = float64(ctr.BytesSent-prev.BytesSent) / elapsed
			if ni.RxBps < 0 {
				ni.RxBps = 0
			}
			if ni.TxBps < 0 {
				ni.TxBps = 0
			}
		}
		info.Interfaces = append(info.Interfaces, ni)
		nextPrev[ctr.Name] = ctr
	}
	c.prevNet = nextPrev
	c.prevNetAt = now
	return info
}

func round2(v float64) float64 {
	return float64(int(v*100+0.5)) / 100
}
