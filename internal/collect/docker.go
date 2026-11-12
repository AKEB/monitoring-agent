package collect

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"monitoring-agent/internal/config"
)

const dockerStatsConcurrency = 8

var (
	dockerClientOnce sync.Once
	dockerHTTP       *http.Client
)

func dockerClient() *http.Client {
	dockerClientOnce.Do(func() {
		dockerHTTP = &http.Client{
			Timeout: 8 * time.Second,
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					var d net.Dialer
					return d.DialContext(ctx, "unix", "/var/run/docker.sock")
				},
				MaxIdleConns:        8,
				MaxIdleConnsPerHost: 8,
				IdleConnTimeout:     30 * time.Second,
				DisableCompression:  true,
			},
		}
	})
	return dockerHTTP
}

func collectDocker(cfg *config.Config) *DockerInfo {
	sock := "/var/run/docker.sock"
	if _, err := os.Stat(sock); err != nil {
		return &DockerInfo{Available: false, Containers: []DockerContainer{}}
	}
	client := dockerClient()
	req, err := http.NewRequest(http.MethodGet, "http://localhost/containers/json?all=true", nil)
	if err != nil {
		cfg.Logf("docker request: %v", err)
		return &DockerInfo{Available: false, Containers: []DockerContainer{}}
	}
	resp, err := client.Do(req)
	if err != nil {
		cfg.Logf("docker dial: %v", err)
		return &DockerInfo{Available: false, Containers: []DockerContainer{}}
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		cfg.Logf("docker status %d", resp.StatusCode)
		return &DockerInfo{Available: false, Containers: []DockerContainer{}}
	}
	var raw []struct {
		ID      string   `json:"Id"`
		Names   []string `json:"Names"`
		Image   string   `json:"Image"`
		State   string   `json:"State"`
		Status  string   `json:"Status"`
		Created int64    `json:"Created"`
		Ports   []struct {
			IP          string `json:"IP"`
			PrivatePort int    `json:"PrivatePort"`
			PublicPort  int    `json:"PublicPort"`
			Type        string `json:"Type"`
		} `json:"Ports"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		cfg.Logf("docker decode: %v", err)
		return &DockerInfo{Available: false, Containers: []DockerContainer{}}
	}
	out := &DockerInfo{Available: true, Containers: make([]DockerContainer, 0, len(raw))}
	for _, c := range raw {
		name := ""
		if len(c.Names) > 0 {
			name = strings.TrimPrefix(c.Names[0], "/")
		}
		var ports []string
		for _, p := range c.Ports {
			if p.PublicPort > 0 {
				ports = append(ports, fmt.Sprintf("%s:%d->%d/%s", p.IP, p.PublicPort, p.PrivatePort, p.Type))
			} else {
				ports = append(ports, fmt.Sprintf("%d/%s", p.PrivatePort, p.Type))
			}
		}
		id := c.ID
		if len(id) > 12 {
			id = id[:12]
		}
		out.Containers = append(out.Containers, DockerContainer{
			ID:      id,
			Name:    name,
			Image:   c.Image,
			State:   c.State,
			Status:  c.Status,
			Created: c.Created,
			Ports:   strings.Join(ports, ", "),
		})
	}
	fillDockerStats(cfg, client, out.Containers)
	return out
}

func fillDockerStats(cfg *config.Config, client *http.Client, containers []DockerContainer) {
	sem := make(chan struct{}, dockerStatsConcurrency)
	var wg sync.WaitGroup
	for i := range containers {
		if !strings.EqualFold(containers[i].State, "running") {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(idx int) {
			defer wg.Done()
			defer func() { <-sem }()
			cpu, mem, limit, memPct, err := fetchContainerStats(client, containers[idx].ID)
			if err != nil {
				cfg.Logf("docker stats %s: %v", containers[idx].ID, err)
				return
			}
			containers[idx].CPUPct = cpu
			containers[idx].MemBytes = mem
			containers[idx].MemLimitBytes = limit
			containers[idx].MemPct = memPct
		}(i)
	}
	wg.Wait()
}

type dockerStatsPayload struct {
	CPUStats struct {
		CPUUsage struct {
			TotalUsage  uint64   `json:"total_usage"`
			PercpuUsage []uint64 `json:"percpu_usage"`
		} `json:"cpu_usage"`
		SystemCPUUsage uint64 `json:"system_cpu_usage"`
		OnlineCPUs     uint32 `json:"online_cpus"`
	} `json:"cpu_stats"`
	PreCPUStats struct {
		CPUUsage struct {
			TotalUsage uint64 `json:"total_usage"`
		} `json:"cpu_usage"`
		SystemCPUUsage uint64 `json:"system_cpu_usage"`
	} `json:"precpu_stats"`
	MemoryStats struct {
		Usage uint64            `json:"usage"`
		Limit uint64            `json:"limit"`
		Stats map[string]uint64 `json:"stats"`
	} `json:"memory_stats"`
}

func fetchContainerStats(client *http.Client, id string) (cpuPct float64, memBytes, memLimit uint64, memPct float64, err error) {
	url := fmt.Sprintf("http://localhost/containers/%s/stats?stream=false&one-shot=true", id)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, 0, 0, 0, fmt.Errorf("status %d", resp.StatusCode)
	}
	var stats dockerStatsPayload
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		return 0, 0, 0, 0, err
	}
	cpuPct = dockerCPUPercent(stats)
	memBytes = dockerMemUsage(stats)
	memLimit = stats.MemoryStats.Limit
	if memLimit > 0 {
		memPct = round2(float64(memBytes) / float64(memLimit) * 100.0)
	}
	// Drop large per-CPU slice promptly; only OnlineCPUs is needed after percent calc.
	stats.CPUStats.CPUUsage.PercpuUsage = nil
	stats.MemoryStats.Stats = nil
	return cpuPct, memBytes, memLimit, memPct, nil
}

func dockerCPUPercent(stats dockerStatsPayload) float64 {
	cpuDelta := float64(stats.CPUStats.CPUUsage.TotalUsage) - float64(stats.PreCPUStats.CPUUsage.TotalUsage)
	systemDelta := float64(stats.CPUStats.SystemCPUUsage) - float64(stats.PreCPUStats.SystemCPUUsage)
	if cpuDelta <= 0 || systemDelta <= 0 {
		return 0
	}
	online := float64(stats.CPUStats.OnlineCPUs)
	if online <= 0 {
		online = float64(len(stats.CPUStats.CPUUsage.PercpuUsage))
	}
	if online <= 0 {
		online = 1
	}
	return round2((cpuDelta / systemDelta) * online * 100.0)
}

func dockerMemUsage(stats dockerStatsPayload) uint64 {
	usage := stats.MemoryStats.Usage
	if stats.MemoryStats.Stats != nil {
		if cache, ok := stats.MemoryStats.Stats["inactive_file"]; ok && usage > cache {
			return usage - cache
		}
		if cache, ok := stats.MemoryStats.Stats["cache"]; ok && usage > cache {
			return usage - cache
		}
	}
	return usage
}
