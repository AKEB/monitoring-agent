package collect

import (
	"bufio"
	"net"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"

	psnet "github.com/shirou/gopsutil/v4/net"
	"github.com/shirou/gopsutil/v4/process"
)

const maxListeners = 500

var (
	servicesOnce sync.Once
	servicesMap  map[string]string // "tcp/22" -> "ssh"
)

func collectListeners() []ListenerInfo {
	out := make([]ListenerInfo, 0)
	seen := map[string]struct{}{}
	procNames := map[int32]string{}

	appendKind := func(kind, protocol string, listenOnly bool) {
		conns, err := psnet.Connections(kind)
		if err != nil {
			return
		}
		for _, c := range conns {
			if listenOnly && !strings.EqualFold(c.Status, "LISTEN") {
				continue
			}
			if c.Laddr.Port == 0 || c.Laddr.Port > 65535 {
				continue
			}
			ip := normalizeListenIP(c.Laddr.IP)
			port := uint16(c.Laddr.Port)
			key := protocol + "|" + ip + "|" + strconv.Itoa(int(port))
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}

			info := ListenerInfo{
				Protocol: protocol,
				IP:       ip,
				Port:     port,
				PID:      c.Pid,
			}
			if c.Pid > 0 {
				name, ok := procNames[c.Pid]
				if !ok {
					name = processName(c.Pid)
					procNames[c.Pid] = name
				}
				info.Process = name
			}
			info.Service = lookupService(protocol, port)
			if info.Service == "" {
				info.Service = info.Process
			}
			out = append(out, info)
			if len(out) >= maxListeners {
				return
			}
		}
	}

	appendKind("tcp", "tcp", true)
	if len(out) < maxListeners {
		appendKind("udp", "udp", false)
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Protocol != out[j].Protocol {
			return out[i].Protocol < out[j].Protocol
		}
		if out[i].Port != out[j].Port {
			return out[i].Port < out[j].Port
		}
		return out[i].IP < out[j].IP
	})
	return out
}

func processName(pid int32) string {
	p, err := process.NewProcess(pid)
	if err != nil {
		return ""
	}
	name, err := p.Name()
	if err != nil {
		return ""
	}
	return name
}

func normalizeListenIP(ip string) string {
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return "0.0.0.0"
	}
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return ip
	}
	if v4 := parsed.To4(); v4 != nil {
		return v4.String()
	}
	return parsed.String()
}

func lookupService(protocol string, port uint16) string {
	servicesOnce.Do(loadServicesFile)
	if servicesMap == nil {
		return ""
	}
	return servicesMap[protocol+"/"+strconv.Itoa(int(port))]
}

func loadServicesFile() {
	servicesMap = map[string]string{}
	f, err := os.Open("/etc/services")
	if err != nil {
		return
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = strings.TrimSpace(line[:i])
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		name := fields[0]
		portProto := fields[1]
		parts := strings.SplitN(portProto, "/", 2)
		if len(parts) != 2 {
			continue
		}
		port, err := strconv.Atoi(parts[0])
		if err != nil || port < 1 || port > 65535 {
			continue
		}
		proto := strings.ToLower(parts[1])
		if proto != "tcp" && proto != "udp" {
			continue
		}
		key := proto + "/" + strconv.Itoa(port)
		if _, exists := servicesMap[key]; !exists {
			servicesMap[key] = name
		}
	}
}
