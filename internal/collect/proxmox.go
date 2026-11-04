package collect

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"monitoring-agent/internal/config"
)

func collectProxmox(cfg *config.Config) *ProxmoxInfo {
	if !isProxmoxHost() {
		return &ProxmoxInfo{Available: false, Guests: []ProxmoxGuest{}}
	}
	node := proxmoxLocalNode()
	guests, ok := collectProxmoxViaPvesh(cfg, node)
	if !ok {
		guests, ok = collectProxmoxViaCLI(cfg)
	}
	if !ok {
		return &ProxmoxInfo{Available: false, Node: node, Guests: []ProxmoxGuest{}}
	}
	sort.Slice(guests, func(i, j int) bool {
		if guests[i].Type != guests[j].Type {
			return guests[i].Type < guests[j].Type
		}
		return guests[i].VMID < guests[j].VMID
	})
	return &ProxmoxInfo{Available: true, Node: node, Guests: guests}
}

func isProxmoxHost() bool {
	st, err := os.Stat("/etc/pve")
	return err == nil && st.IsDir()
}

func proxmoxLocalNode() string {
	if target, err := os.Readlink("/etc/pve/local"); err == nil {
		base := filepath.Base(strings.TrimSuffix(target, "/"))
		if base != "" && base != "." && base != "local" {
			return base
		}
	}
	entries, err := os.ReadDir("/etc/pve/nodes")
	if err == nil {
		for _, e := range entries {
			if e.IsDir() {
				return e.Name()
			}
		}
	}
	host, _ := os.Hostname()
	if i := strings.IndexByte(host, '.'); i > 0 {
		return host[:i]
	}
	return host
}

type pveGuestRaw struct {
	VMID    any     `json:"vmid"`
	Name    string  `json:"name"`
	Status  string  `json:"status"`
	Type    string  `json:"type"`
	CPUs    any     `json:"cpus"`
	CPU     float64 `json:"cpu"`
	Mem     uint64  `json:"mem"`
	MaxMem  uint64  `json:"maxmem"`
	Disk    uint64  `json:"disk"`
	MaxDisk uint64  `json:"maxdisk"`
	Uptime  uint64  `json:"uptime"`
}

func collectProxmoxViaPvesh(cfg *config.Config, node string) ([]ProxmoxGuest, bool) {
	if _, err := exec.LookPath("pvesh"); err != nil {
		return nil, false
	}
	if node == "" {
		return nil, false
	}
	qemu, okQ := pveshGuests(cfg, "/nodes/"+node+"/qemu", "qemu")
	lxc, okL := pveshGuests(cfg, "/nodes/"+node+"/lxc", "lxc")
	if !okQ && !okL {
		return nil, false
	}
	out := make([]ProxmoxGuest, 0, len(qemu)+len(lxc))
	out = append(out, qemu...)
	out = append(out, lxc...)
	return out, true
}

func pveshGuests(cfg *config.Config, path, guestType string) ([]ProxmoxGuest, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "pvesh", "get", path, "--output-format", "json")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		cfg.Logf("pvesh %s: %v (%s)", path, err, strings.TrimSpace(stderr.String()))
		return nil, false
	}
	var raw []pveGuestRaw
	if err := json.Unmarshal(stdout.Bytes(), &raw); err != nil {
		cfg.Logf("pvesh decode %s: %v", path, err)
		return nil, false
	}
	out := make([]ProxmoxGuest, 0, len(raw))
	for _, g := range raw {
		out = append(out, ProxmoxGuest{
			VMID:         anyToInt(g.VMID),
			Name:         g.Name,
			Type:         guestType,
			Status:       g.Status,
			CPUs:         anyToInt(g.CPUs),
			CPU:          g.CPU,
			MemBytes:     g.Mem,
			MaxMemBytes:  g.MaxMem,
			DiskBytes:    g.Disk,
			MaxDiskBytes: g.MaxDisk,
			UptimeSec:    g.Uptime,
		})
	}
	return out, true
}

func collectProxmoxViaCLI(cfg *config.Config) ([]ProxmoxGuest, bool) {
	var out []ProxmoxGuest
	okAny := false
	if guests, ok := parseQmOrPctList(cfg, "qm"); ok {
		out = append(out, guests...)
		okAny = true
	}
	if guests, ok := parseQmOrPctList(cfg, "pct"); ok {
		out = append(out, guests...)
		okAny = true
	}
	return out, okAny
}

func parseQmOrPctList(cfg *config.Config, bin string) ([]ProxmoxGuest, bool) {
	if _, err := exec.LookPath(bin); err != nil {
		return nil, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "list")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		cfg.Logf("%s list: %v (%s)", bin, err, strings.TrimSpace(stderr.String()))
		return nil, false
	}
	guestType := "qemu"
	if bin == "pct" {
		guestType = "lxc"
	}
	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if len(lines) <= 1 {
		return []ProxmoxGuest{}, true
	}
	out := make([]ProxmoxGuest, 0, len(lines)-1)
	for _, line := range lines[1:] {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		vmid, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		g := ProxmoxGuest{VMID: vmid, Type: guestType}
		if guestType == "qemu" {
			// VMID NAME STATUS MEM(MB) BOOTDISK(GB) PID
			if len(fields) >= 3 {
				g.Name = fields[1]
				g.Status = fields[2]
			}
			if len(fields) >= 4 {
				if mb, err := strconv.ParseUint(fields[3], 10, 64); err == nil {
					g.MaxMemBytes = mb * 1024 * 1024
					if g.Status == "running" {
						g.MemBytes = g.MaxMemBytes
					}
				}
			}
			if len(fields) >= 5 {
				if gb, err := strconv.ParseFloat(fields[4], 64); err == nil {
					g.MaxDiskBytes = uint64(gb * 1024 * 1024 * 1024)
				}
			}
		} else {
			// VMID Status Lock Name  OR  VMID Status Name
			g.Status = fields[1]
			if len(fields) >= 4 {
				g.Name = fields[3]
			} else if len(fields) >= 3 {
				g.Name = fields[2]
			}
		}
		out = append(out, g)
	}
	return out, true
}

func anyToInt(v any) int {
	switch t := v.(type) {
	case float64:
		return int(t)
	case int:
		return t
	case int64:
		return int(t)
	case json.Number:
		n, _ := t.Int64()
		return int(n)
	case string:
		n, _ := strconv.Atoi(t)
		return n
	default:
		return 0
	}
}
