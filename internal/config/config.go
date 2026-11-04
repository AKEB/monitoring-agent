package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"monitoring-agent/internal/buildinfo"
)

type Config struct {
	Timezone          string
	ServerHost        string
	AgentKeyHash      string
	AgentVersion      string
	ReportIntervalSec int
	Debug             bool
	CollectDocker     bool
	CollectAuth       bool
	CollectProcesses  bool
	ProcessTopN       int
	AutoUpdate        bool
	UpdateFromGitHub  bool

	// LastUpdateError is attached to the next report after a failed self-update.
	LastUpdateError string
}

var (
	once sync.Once
	cfg  *Config
)

func Get() *Config {
	once.Do(func() {
		_ = loadDotEnvIfExists()
		cfg = &Config{
			Timezone:          getString("TZ", "UTC"),
			ServerHost:        normalizeHost(getString("SERVER_HOST", "")),
			AgentKeyHash:      getString("AGENT_KEY_HASH", ""),
			AgentVersion:      resolveAgentVersion(),
			ReportIntervalSec: getInt("REPORT_INTERVAL_SEC", 30),
			Debug:             getBool("DEBUG", false),
			CollectDocker:     getBool("COLLECT_DOCKER", true),
			CollectAuth:       getBool("COLLECT_AUTH", true),
			CollectProcesses:  getBool("COLLECT_PROCESSES", true),
			ProcessTopN:       getInt("PROCESS_TOP_N", 20),
			AutoUpdate:        getBool("AUTO_UPDATE", true),
			UpdateFromGitHub:  getBool("UPDATE_FROM_GITHUB", true),
		}
		if cfg.ReportIntervalSec < 15 {
			cfg.ReportIntervalSec = 15
		}
		if cfg.ReportIntervalSec > 600 {
			cfg.ReportIntervalSec = 600
		}
		if cfg.ProcessTopN < 5 {
			cfg.ProcessTopN = 5
		}
		if cfg.ProcessTopN > 100 {
			cfg.ProcessTopN = 100
		}
		_ = os.Setenv("TZ", cfg.Timezone)
		if loc, err := time.LoadLocation(cfg.Timezone); err == nil {
			time.Local = loc
		} else {
			time.Local = time.UTC
		}
		if cfg.Debug {
			cfg.Logf("agent_version=%q interval=%ds", cfg.AgentVersion, cfg.ReportIntervalSec)
		}
	})
	return cfg
}

func (c *Config) logPrefix() string {
	return fmt.Sprintf("[%s] PID=%d ", time.Now().Format("2006-01-02 15:04:05"), os.Getpid())
}

func (c *Config) Logf(format string, args ...any) {
	if !c.Debug {
		return
	}
	fmt.Fprintf(os.Stderr, "%s%s\n", c.logPrefix(), fmt.Sprintf(format, args...))
}

func (c *Config) Errorf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "%s%s\n", c.logPrefix(), fmt.Sprintf(format, args...))
}

func loadDotEnvIfExists() error {
	paths := []string{
		filepath.Join(getwdSafe(), ".env"),
		filepath.Join(filepath.Dir(getExecutableSafe()), ".env"),
	}
	for _, p := range paths {
		if _, err := os.Stat(p); err != nil {
			continue
		}
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		defer f.Close()
		s := bufio.NewScanner(f)
		for s.Scan() {
			line := strings.TrimSpace(s.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			parts := strings.SplitN(line, "=", 2)
			if len(parts) != 2 {
				continue
			}
			k := strings.TrimSpace(parts[0])
			v := strings.Trim(strings.TrimSpace(parts[1]), "\"'")
			if _, exists := os.LookupEnv(k); !exists {
				_ = os.Setenv(k, v)
			}
		}
		return s.Err()
	}
	return nil
}

func getwdSafe() string {
	wd, _ := os.Getwd()
	return wd
}

func getExecutableSafe() string {
	exe, _ := os.Executable()
	return exe
}

func normalizeHost(v string) string {
	if v == "" {
		return v
	}
	if strings.HasSuffix(v, "/") {
		return v
	}
	return v + "/"
}

func resolveAgentVersion() string {
	if v := strings.TrimSpace(getString("AGENT_VERSION", "")); v != "" {
		return v
	}
	if v := strings.TrimSpace(buildinfo.Version); v != "" {
		return v
	}
	return "local"
}

func getString(k, def string) string {
	v, ok := os.LookupEnv(k)
	if !ok {
		return def
	}
	return v
}

func getInt(k string, def int) int {
	v := getString(k, "")
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func getBool(k string, def bool) bool {
	v := strings.ToLower(getString(k, ""))
	if v == "" {
		return def
	}
	return v == "1" || v == "true" || v == "yes" || v == "on"
}
