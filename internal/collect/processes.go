package collect

import (
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/shirou/gopsutil/v4/process"
)

var interestingNames = map[string]struct{}{
	"sshd": {}, "ssh": {}, "dockerd": {}, "containerd": {}, "docker-proxy": {},
	"pvedaemon": {}, "pveproxy": {}, "pvescheduler": {}, "pve-cluster": {},
	"proxmox-backup": {}, "proxmox-backup-proxy": {},
	"nginx": {}, "apache2": {}, "httpd": {}, "mysqld": {}, "mariadbd": {},
	"postgres": {}, "redis-server": {}, "chronyd": {}, "systemd": {},
}

type procCandidate struct {
	pid    int32
	name   string
	cpuPct float64
	memRSS uint64
}

func collectProcesses(topN int) []ProcessInfo {
	procs, err := process.Processes()
	if err != nil {
		return nil
	}
	// First pass: cheap fields only (no Username/Cmdline — those allocate heavily).
	all := make([]procCandidate, 0, len(procs))
	for _, p := range procs {
		name, err := p.Name()
		if err != nil || name == "" {
			continue
		}
		cpuPct, _ := p.CPUPercent()
		var rss uint64
		if memInfo, err := p.MemoryInfo(); err == nil && memInfo != nil {
			rss = memInfo.RSS
		}
		all = append(all, procCandidate{
			pid:    p.Pid,
			name:   name,
			cpuPct: round2(cpuPct),
			memRSS: rss,
		})
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].cpuPct == all[j].cpuPct {
			return all[i].memRSS > all[j].memRSS
		}
		return all[i].cpuPct > all[j].cpuPct
	})

	seen := map[int32]struct{}{}
	selected := make([]procCandidate, 0, topN+15)
	for _, p := range all {
		if len(selected) >= topN {
			break
		}
		selected = append(selected, p)
		seen[p.pid] = struct{}{}
	}
	for _, p := range all {
		base := strings.ToLower(p.name)
		if _, ok := interestingNames[base]; !ok {
			continue
		}
		if _, ok := seen[p.pid]; ok {
			continue
		}
		selected = append(selected, p)
		seen[p.pid] = struct{}{}
		if len(selected) >= topN+15 {
			break
		}
	}

	out := make([]ProcessInfo, 0, len(selected))
	for _, p := range selected {
		info := ProcessInfo{
			PID:    p.pid,
			Name:   p.name,
			CPUPct: p.cpuPct,
			MemRSS: p.memRSS,
		}
		info.User = readProcUser(p.pid)
		info.Cmdline = readProcCmdline(p.pid, 200)
		out = append(out, info)
	}
	return out
}

func readProcCmdline(pid int32, maxLen int) string {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(int(pid)) + "/cmdline")
	if err != nil {
		// Non-Linux fallback.
		p, err := process.NewProcess(pid)
		if err != nil {
			return ""
		}
		cmd, _ := p.Cmdline()
		if len(cmd) > maxLen {
			return cmd[:maxLen]
		}
		return cmd
	}
	cmd := strings.ReplaceAll(string(b), "\x00", " ")
	cmd = strings.TrimSpace(cmd)
	if len(cmd) > maxLen {
		return cmd[:maxLen]
	}
	return cmd
}

func readProcUser(pid int32) string {
	p, err := process.NewProcess(pid)
	if err != nil {
		return ""
	}
	user, _ := p.Username()
	return user
}
