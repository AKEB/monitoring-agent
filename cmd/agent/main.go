package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"monitoring-agent/internal/collect"
	"monitoring-agent/internal/config"
	"monitoring-agent/internal/report"
)

func main() {
	cfg := config.Get()
	if cfg.ServerHost == "" || cfg.AgentKeyHash == "" {
		fmt.Fprintln(os.Stderr, "SERVER_HOST and AGENT_KEY_HASH are required (e.g. from .env next to the binary).")
		os.Exit(1)
	}

	collector := collect.New(cfg)
	client := report.New(cfg)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	cfg.Logf("starting monitoring-agent version=%s interval=%ds", cfg.AgentVersion, cfg.ReportIntervalSec)

	ticker := time.NewTicker(time.Duration(cfg.ReportIntervalSec) * time.Second)
	defer ticker.Stop()

	sendOnce := func() {
		r := collector.Collect()
		if err := client.Send(r); err != nil {
			cfg.Errorf("report failed: %v", err)
		}
	}

	sendOnce()
	for {
		select {
		case <-ticker.C:
			sendOnce()
		case <-stop:
			fmt.Println("Exiting")
			return
		}
	}
}
