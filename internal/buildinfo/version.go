package buildinfo

// Version is set at build time, e.g.:
// go build -ldflags "-X monitoring-agent/internal/buildinfo.Version=v1.2.3" ...
var Version = ""
