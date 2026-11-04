package collect

type Report struct {
	AgentKeyHash  string         `json:"agent_key_hash"`
	AgentVersion  string         `json:"agent_version"`
	CollectedAt   int64          `json:"collected_at"`
	UpdateError   string         `json:"update_error,omitempty"`
	Host          HostInfo       `json:"host"`
	CPU           CPUInfo        `json:"cpu"`
	Memory        MemInfo        `json:"memory"`
	Swap          MemInfo        `json:"swap"`
	Disks         []DiskInfo     `json:"disks"`
	Network       NetworkInfo    `json:"network"`
	Docker        *DockerInfo    `json:"docker,omitempty"`
	AuthEvents    []AuthEvent    `json:"auth_events,omitempty"`
	Processes     []ProcessInfo  `json:"processes,omitempty"`
}

type HostInfo struct {
	Hostname        string `json:"hostname"`
	OS              string `json:"os"`
	Platform        string `json:"platform"`
	PlatformVersion string `json:"platform_version"`
	Kernel          string `json:"kernel"`
	Arch            string `json:"arch"`
	UptimeSec       uint64 `json:"uptime_sec"`
	Virtualization  string `json:"virtualization"`
}

type CPUInfo struct {
	Cores    int     `json:"cores"`
	Load1    float64 `json:"load1"`
	Load5    float64 `json:"load5"`
	Load15   float64 `json:"load15"`
	UsagePct float64 `json:"usage_pct"`
}

type MemInfo struct {
	TotalBytes     uint64  `json:"total_bytes"`
	UsedBytes      uint64  `json:"used_bytes"`
	AvailableBytes uint64  `json:"available_bytes,omitempty"`
	UsedPct        float64 `json:"used_pct"`
}

type DiskInfo struct {
	Mount      string  `json:"mount"`
	Device     string  `json:"device"`
	FSType     string  `json:"fstype"`
	TotalBytes uint64  `json:"total_bytes"`
	UsedBytes  uint64  `json:"used_bytes"`
	FreeBytes  uint64  `json:"free_bytes"`
	UsedPct    float64 `json:"used_pct"`
}

type NetworkInfo struct {
	Interfaces []NetIface `json:"interfaces"`
}

type NetIface struct {
	Name    string   `json:"name"`
	Addrs   []string `json:"addrs"`
	RxBytes uint64   `json:"rx_bytes"`
	TxBytes uint64   `json:"tx_bytes"`
	RxBps   float64  `json:"rx_bps"`
	TxBps   float64  `json:"tx_bps"`
}

type DockerInfo struct {
	Available  bool              `json:"available"`
	Containers []DockerContainer `json:"containers"`
}

type DockerContainer struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Image   string `json:"image"`
	State   string `json:"state"`
	Status  string `json:"status"`
	Created int64  `json:"created"`
	Ports   string `json:"ports"`
}

type AuthEvent struct {
	Time    int64  `json:"time"`
	User    string `json:"user"`
	From    string `json:"from"`
	Method  string `json:"method"`
	Success bool   `json:"success"`
	Raw     string `json:"raw,omitempty"`
}

type ProcessInfo struct {
	PID        int32   `json:"pid"`
	Name       string  `json:"name"`
	User       string  `json:"user"`
	CPUPct     float64 `json:"cpu_pct"`
	MemRSS     uint64  `json:"mem_rss"`
	Cmdline    string  `json:"cmdline,omitempty"`
}
