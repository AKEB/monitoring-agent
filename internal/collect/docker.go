package collect

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"monitoring-agent/internal/config"
)

func collectDocker(cfg *config.Config) *DockerInfo {
	sock := "/var/run/docker.sock"
	if _, err := os.Stat(sock); err != nil {
		return &DockerInfo{Available: false, Containers: []DockerContainer{}}
	}
	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", sock)
			},
		},
	}
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
	return out
}
