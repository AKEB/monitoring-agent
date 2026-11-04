package collect

import (
	"bufio"
	"os"
	"regexp"
	"strings"
	"time"
)

type authCursor struct {
	path   string
	offset int64
}

var (
	reSSHAccepted = regexp.MustCompile(`(?i)Accepted\s+(\S+)\s+for\s+(\S+)\s+from\s+(\S+)`)
	reSSHFailed   = regexp.MustCompile(`(?i)Failed\s+(\S+)\s+for\s+(?:invalid user\s+)?(\S+)\s+from\s+(\S+)`)
	reSudo        = regexp.MustCompile(`(?i)sudo:\s+(\S+)\s+:`)
	reSessionOpen = regexp.MustCompile(`(?i)pam_unix\([^:]+:session\):\s+session opened for user\s+(\S+)`)
)

func (c *Collector) collectAuth() []AuthEvent {
	path := findAuthLog()
	if path == "" {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	st, err := f.Stat()
	if err != nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.authCursor.path != path || st.Size() < c.authCursor.offset {
		c.authCursor = authCursor{path: path, offset: 0}
		// first run: only seed cursor near end to avoid flooding history
		if st.Size() > 64*1024 {
			c.authCursor.offset = st.Size() - 64*1024
		}
	}
	if _, err := f.Seek(c.authCursor.offset, 0); err != nil {
		return nil
	}
	var events []AuthEvent
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		ev, ok := parseAuthLine(line)
		if !ok {
			continue
		}
		key := ev.fingerprint()
		if _, seen := c.authSeen[key]; seen {
			continue
		}
		c.authSeen[key] = struct{}{}
		events = append(events, ev)
	}
	off, _ := f.Seek(0, 1)
	c.authCursor.offset = off
	// cap seen map
	if len(c.authSeen) > 5000 {
		c.authSeen = map[string]struct{}{}
		for _, e := range events {
			c.authSeen[e.fingerprint()] = struct{}{}
		}
	}
	if len(events) > 50 {
		events = events[len(events)-50:]
	}
	return events
}

func (e AuthEvent) fingerprint() string {
	return strings.Join([]string{
		itoa(e.Time), e.User, e.From, e.Method, boolStr(e.Success), e.Raw,
	}, "|")
}

func findAuthLog() string {
	for _, p := range []string{"/var/log/auth.log", "/var/log/secure"} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func parseAuthLine(line string) (AuthEvent, bool) {
	ts := parseSyslogTime(line)
	if m := reSSHAccepted.FindStringSubmatch(line); m != nil {
		return AuthEvent{Time: ts, Method: "ssh/" + m[1], User: m[2], From: m[3], Success: true, Raw: truncate(line, 240)}, true
	}
	if m := reSSHFailed.FindStringSubmatch(line); m != nil {
		return AuthEvent{Time: ts, Method: "ssh/" + m[1], User: m[2], From: m[3], Success: false, Raw: truncate(line, 240)}, true
	}
	if m := reSessionOpen.FindStringSubmatch(line); m != nil {
		user := strings.TrimSuffix(m[1], "(")
		return AuthEvent{Time: ts, Method: "session", User: user, Success: true, Raw: truncate(line, 240)}, true
	}
	if m := reSudo.FindStringSubmatch(line); m != nil && strings.Contains(strings.ToLower(line), "command") {
		return AuthEvent{Time: ts, Method: "sudo", User: m[1], Success: true, Raw: truncate(line, 240)}, true
	}
	return AuthEvent{}, false
}

func parseSyslogTime(line string) int64 {
	// classic syslog: "Nov  4 19:10:01 ..."
	fields := strings.Fields(line)
	if len(fields) < 3 {
		return time.Now().Unix()
	}
	raw := strings.Join(fields[0:3], " ")
	t, err := time.ParseInLocation("Jan 2 15:04:05", raw, time.Local)
	if err != nil {
		t, err = time.ParseInLocation("Jan  2 15:04:05", raw, time.Local)
	}
	if err != nil {
		return time.Now().Unix()
	}
	now := time.Now()
	t = time.Date(now.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), 0, time.Local)
	if t.After(now.Add(24 * time.Hour)) {
		t = t.AddDate(-1, 0, 0)
	}
	return t.Unix()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func itoa(v int64) string {
	return strings.TrimSpace(strings.ReplaceAll(time.Unix(v, 0).Format("20060102150405"), " ", ""))
}

func boolStr(v bool) string {
	if v {
		return "1"
	}
	return "0"
}
