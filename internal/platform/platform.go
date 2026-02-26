package platform

import "regexp"

// ListenEntry represents a TCP listening socket.
type ListenEntry struct {
	Port int
	PID  int
	Cmd  string
}

// Platform abstracts OS-specific process and network introspection.
type Platform interface {
	// FindPIDsByName returns PIDs whose binary path (argv[0]) matches re.
	FindPIDsByName(re *regexp.Regexp) []int
	// FindPIDsByArgs returns PIDs where any command-line argument matches re.
	// Unlike FindPIDsByName which only checks argv[0], this checks all args.
	FindPIDsByArgs(re *regexp.Regexp) []int
	// ListOpenFiles returns absolute file paths of all open FDs for a process.
	ListOpenFiles(pid int) []string
	// ReadProcessCwd returns the current working directory of a process.
	ReadProcessCwd(pid int) string
	// FindListenTCP returns all TCP LISTEN sockets on the host.
	FindListenTCP() []ListenEntry
}

// P is the platform-specific implementation, initialised by an init() in
// the platform_linux.go or platform_darwin.go file.
var P Platform
