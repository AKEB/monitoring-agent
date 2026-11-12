package collect

import (
	"bufio"
	"encoding/hex"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"

	psnet "github.com/shirou/gopsutil/v4/net"
	"github.com/shirou/gopsutil/v4/process"
)

const maxListeners = 500

// tcpListenState is /proc/net/tcp state 0A (LISTEN).
const tcpListenState = 0x0A

var (
	servicesOnce sync.Once
	servicesMap  map[string]string // "tcp/22" -> "ssh"
)

type rawListener struct {
	protocol string
	ip       string
	port     uint16
	inode    uint64
}

func collectListeners() []ListenerInfo {
	if runtime.GOOS == "linux" {
		return collectListenersLinux()
	}
	return collectListenersGeneric()
}

// collectListenersLinux reads only listening sockets from /proc/net/{tcp,tcp6,udp,udp6}
// and resolves PIDs for those inodes only. Unlike gopsutil.Connections, it never
// materializes the full ESTABLISHED/UDP flow table (which balloons RSS on VPN/NAT hosts).
func collectListenersLinux() []ListenerInfo {
	raw := make([]rawListener, 0, 64)
	raw = appendProcNetListeners(raw, "/proc/net/tcp", "tcp", true)
	if len(raw) < maxListeners {
		raw = appendProcNetListeners(raw, "/proc/net/tcp6", "tcp", true)
	}
	if len(raw) < maxListeners {
		raw = appendProcNetListeners(raw, "/proc/net/udp", "udp", false)
	}
	if len(raw) < maxListeners {
		raw = appendProcNetListeners(raw, "/proc/net/udp6", "udp", false)
	}
	if len(raw) > maxListeners {
		raw = raw[:maxListeners]
	}

	need := make(map[uint64]struct{}, len(raw))
	for _, r := range raw {
		if r.inode > 0 {
			need[r.inode] = struct{}{}
		}
	}
	owners := resolveSocketInodes(need)

	out := make([]ListenerInfo, 0, len(raw))
	seen := map[string]struct{}{}
	for _, r := range raw {
		key := r.protocol + "|" + r.ip + "|" + strconv.Itoa(int(r.port))
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		info := ListenerInfo{
			Protocol: r.protocol,
			IP:       r.ip,
			Port:     r.port,
		}
		if own, ok := owners[r.inode]; ok {
			info.PID = own.pid
			info.Process = own.name
		}
		info.Service = lookupService(r.protocol, r.port)
		if info.Service == "" {
			info.Service = info.Process
		}
		out = append(out, info)
	}
	sortListeners(out)
	return out
}

func appendProcNetListeners(dst []rawListener, path, protocol string, tcpOnlyListen bool) []rawListener {
	f, err := os.Open(path)
	if err != nil {
		return dst
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	// Header line.
	if !sc.Scan() {
		return dst
	}
	for sc.Scan() {
		if len(dst) >= maxListeners {
			break
		}
		fields := strings.Fields(sc.Text())
		// local_address rem_address st ... inode (index 9)
		if len(fields) < 10 {
			continue
		}
		if tcpOnlyListen {
			st, err := strconv.ParseUint(fields[3], 16, 8)
			if err != nil || st != tcpListenState {
				continue
			}
		} else {
			// UDP "listeners": bound sockets with remote 0:0.
			if !isProcNetRemoteUnspecified(fields[2]) {
				continue
			}
		}
		ip, port, ok := decodeProcNetAddr(fields[1])
		if !ok || port == 0 {
			continue
		}
		inode, err := strconv.ParseUint(fields[9], 10, 64)
		if err != nil {
			inode = 0
		}
		dst = append(dst, rawListener{
			protocol: protocol,
			ip:       normalizeListenIP(ip),
			port:     port,
			inode:    inode,
		})
	}
	return dst
}

func isProcNetRemoteUnspecified(rem string) bool {
	parts := strings.SplitN(rem, ":", 2)
	if len(parts) != 2 {
		return false
	}
	if parts[1] != "0000" {
		return false
	}
	for _, c := range parts[0] {
		if c != '0' {
			return false
		}
	}
	return true
}

func decodeProcNetAddr(field string) (ip string, port uint16, ok bool) {
	parts := strings.SplitN(field, ":", 2)
	if len(parts) != 2 {
		return "", 0, false
	}
	port64, err := strconv.ParseUint(parts[1], 16, 16)
	if err != nil || port64 == 0 {
		return "", 0, false
	}
	raw, err := hex.DecodeString(parts[0])
	if err != nil {
		return "", 0, false
	}
	switch len(raw) {
	case 4:
		// /proc/net IPv4 is little-endian on Linux.
		ip = net.IPv4(raw[3], raw[2], raw[1], raw[0]).String()
	case 16:
		var b [16]byte
		for i := 0; i < 4; i++ {
			chunk := raw[i*4 : (i+1)*4]
			b[i*4] = chunk[3]
			b[i*4+1] = chunk[2]
			b[i*4+2] = chunk[1]
			b[i*4+3] = chunk[0]
		}
		ip = net.IP(b[:]).String()
	default:
		return "", 0, false
	}
	return ip, uint16(port64), true
}

type inodeOwner struct {
	pid  int32
	name string
}

func resolveSocketInodes(need map[uint64]struct{}) map[uint64]inodeOwner {
	out := make(map[uint64]inodeOwner, len(need))
	if len(need) == 0 {
		return out
	}
	procDir, err := os.Open("/proc")
	if err != nil {
		return out
	}
	defer procDir.Close()
	names, err := procDir.Readdirnames(-1)
	if err != nil {
		return out
	}
	for _, name := range names {
		if len(out) >= len(need) {
			break
		}
		pid64, err := strconv.ParseInt(name, 10, 32)
		if err != nil {
			continue
		}
		pid := int32(pid64)
		fdPath := filepath.Join("/proc", name, "fd")
		fdDir, err := os.Open(fdPath)
		if err != nil {
			continue
		}
		fds, err := fdDir.Readdirnames(-1)
		fdDir.Close()
		if err != nil {
			continue
		}
		var procName string
		named := false
		for _, fd := range fds {
			if len(out) >= len(need) {
				break
			}
			target, err := os.Readlink(filepath.Join(fdPath, fd))
			if err != nil || !strings.HasPrefix(target, "socket:[") || !strings.HasSuffix(target, "]") {
				continue
			}
			ino, err := strconv.ParseUint(target[len("socket:["):len(target)-1], 10, 64)
			if err != nil {
				continue
			}
			if _, want := need[ino]; !want {
				continue
			}
			if _, have := out[ino]; have {
				continue
			}
			if !named {
				procName = readProcComm(pid)
				named = true
			}
			out[ino] = inodeOwner{pid: pid, name: procName}
		}
	}
	return out
}

func readProcComm(pid int32) string {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(int(pid)) + "/comm")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// collectListenersGeneric is a fallback for non-Linux (dev/tests).
// Still avoids UID lookups; UDP is filtered to unbound remotes only.
func collectListenersGeneric() []ListenerInfo {
	out := make([]ListenerInfo, 0)
	seen := map[string]struct{}{}
	procNames := map[int32]string{}

	appendKind := func(kind, protocol string, listenOnly bool) {
		conns, err := psnet.ConnectionsWithoutUids(kind)
		if err != nil {
			return
		}
		for _, c := range conns {
			if listenOnly && !strings.EqualFold(c.Status, "LISTEN") {
				continue
			}
			if !listenOnly {
				// Approximate UDP listeners: no remote port.
				if c.Raddr.Port != 0 {
					continue
				}
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
	sortListeners(out)
	return out
}

func sortListeners(out []ListenerInfo) {
	sort.Slice(out, func(i, j int) bool {
		if out[i].Protocol != out[j].Protocol {
			return out[i].Protocol < out[j].Protocol
		}
		if out[i].Port != out[j].Port {
			return out[i].Port < out[j].Port
		}
		return out[i].IP < out[j].IP
	})
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
