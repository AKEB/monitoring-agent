package main

import (
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"runtime/debug"
	"sync/atomic"
	"syscall"
	"time"

	"monitoring-agent/internal/collect"
	"monitoring-agent/internal/config"
	"monitoring-agent/internal/report"
	"monitoring-agent/internal/selfupdate"
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

	cfg.Logf("starting monitoring-agent version=%s interval=%ds auto_update=%v", cfg.AgentVersion, cfg.ReportIntervalSec, cfg.AutoUpdate)

	ticker := time.NewTicker(time.Duration(cfg.ReportIntervalSec) * time.Second)
	defer ticker.Stop()

	var updateOnce atomic.Bool
	var collectCycle uint64

	sendOnce := func() {
		r := collector.Collect()
		collectCycle++
		// Collection allocates large temporary buffers (proc tables, JSON).
		// Return scavenged pages to the OS periodically so cgroup RSS stays honest.
		if collectCycle%4 == 0 {
			runtime.GC()
			debug.FreeOSMemory()
		}
		resp, err := client.Send(r)
		if err != nil {
			cfg.Errorf("report failed: %v", err)
			return
		}
		if cfg.LastUpdateError != "" {
			// Cleared after a successful report that carried the error.
			cfg.LastUpdateError = ""
		}
		if resp == nil || resp.Update == nil || !cfg.AutoUpdate {
			return
		}
		if !updateOnce.CompareAndSwap(false, true) {
			return
		}
		cmd := *resp.Update
		cfg.Logf("self-update requested version=%s arch=%s", cmd.Version, cmd.Arch)
		if err := selfupdate.Apply(cmd, cfg.UpdateFromGitHub); err != nil {
			cfg.Errorf("self-update failed: %v", err)
			cfg.LastUpdateError = err.Error()
			updateOnce.Store(false)
			return
		}
		cfg.Logf("self-update installed %s; exiting for restart", cmd.Version)
		os.Exit(0)
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
