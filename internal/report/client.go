package report

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"monitoring-agent/internal/collect"
	"monitoring-agent/internal/config"
	"monitoring-agent/internal/selfupdate"
)

type Client struct {
	cfg    *config.Config
	client *http.Client
}

type Response struct {
	Status   int
	Error    string
	ServerID int
	Update   *selfupdate.Command
}

func New(cfg *config.Config) *Client {
	return &Client{
		cfg: cfg,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (c *Client) Send(r collect.Report) (*Response, error) {
	body, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}
	url := c.cfg.ServerHost + "api/agent/report/"
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "monitoring-agent/"+c.cfg.AgentVersion)
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}
	var parsed struct {
		Status   int    `json:"status"`
		Error    string `json:"error"`
		ServerID int    `json:"server_id"`
		Update   *struct {
			Version string `json:"version"`
			URL     string `json:"url"`
			Sha256  string `json:"sha256"`
			Arch    string `json:"arch"`
		} `json:"update"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	if parsed.Status != 0 {
		if parsed.Error == "" {
			parsed.Error = "unknown error"
		}
		return nil, fmt.Errorf("api status=%d: %s", parsed.Status, parsed.Error)
	}
	out := &Response{
		Status:   parsed.Status,
		Error:    parsed.Error,
		ServerID: parsed.ServerID,
	}
	if parsed.Update != nil && strings.TrimSpace(parsed.Update.URL) != "" {
		out.Update = &selfupdate.Command{
			Version: strings.TrimSpace(parsed.Update.Version),
			URL:     strings.TrimSpace(parsed.Update.URL),
			Sha256:  strings.TrimSpace(parsed.Update.Sha256),
			Arch:    strings.TrimSpace(parsed.Update.Arch),
		}
	}
	c.cfg.Logf("report ok (%d bytes)", len(body))
	return out, nil
}
