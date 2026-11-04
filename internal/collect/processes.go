package collect

import (
	"sort"
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

func collectProcesses(topN int) []ProcessInfo {
	procs, err := process.Processes()
	if err != nil {
		return nil
	}
	var all []ProcessInfo
	for _, p := range procs {
		name, _ := p.Name()
		cpuPct, _ := p.CPUPercent()
		memInfo, _ := p.MemoryInfo()
		var rss uint64
		if memInfo != nil {
			rss = memInfo.RSS
		}
		user, _ := p.Username()
		cmd, _ := p.Cmdline()
		if len(cmd) > 200 {
			cmd = cmd[:200]
		}
		all = append(all, ProcessInfo{
			PID:     p.Pid,
			Name:    name,
			User:    user,
			CPUPct:  round2(cpuPct),
			MemRSS:  rss,
			Cmdline: cmd,
		})
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].CPUPct == all[j].CPUPct {
			return all[i].MemRSS > all[j].MemRSS
		}
		return all[i].CPUPct > all[j].CPUPct
	})
	seen := map[int32]struct{}{}
	var out []ProcessInfo
	for _, p := range all {
		if len(out) >= topN {
			break
		}
		out = append(out, p)
		seen[p.PID] = struct{}{}
	}
	for _, p := range all {
		base := strings.ToLower(p.Name)
		if _, ok := interestingNames[base]; !ok {
			continue
		}
		if _, ok := seen[p.PID]; ok {
			continue
		}
		out = append(out, p)
		seen[p.PID] = struct{}{}
		if len(out) >= topN+15 {
			break
		}
	}
	return out
}
