package report

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"monitoring-agent/internal/collect"
	"monitoring-agent/internal/config"
)

type Client struct {
	cfg    *config.Config
	client *http.Client
}

func New(cfg *config.Config) *Client {
	return &Client{
		cfg: cfg,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (c *Client) Send(r collect.Report) error {
	body, err := json.Marshal(r)
	if err != nil {
		return err
	}
	url := c.cfg.ServerHost + "api/agent/report/"
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "monitoring-agent/"+c.cfg.AgentVersion)
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}
	var parsed struct {
		Status int    `json:"status"`
		Error  string `json:"error"`
	}
	if err := json.Unmarshal(respBody, &parsed); err == nil {
		if parsed.Status != 0 {
			if parsed.Error == "" {
				parsed.Error = "unknown error"
			}
			return fmt.Errorf("api status=%d: %s", parsed.Status, parsed.Error)
		}
	}
	c.cfg.Logf("report ok (%d bytes)", len(body))
	return nil
}
