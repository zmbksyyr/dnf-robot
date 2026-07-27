package marketapp

import (
	"net"
	"strings"
	"time"
)

func marketServiceShellCommand(bin string, args []string) string {
	parts := make([]string, 0, len(args)+2)
	parts = append(parts, shellQuote(bin))
	for _, arg := range args {
		parts = append(parts, shellQuote(arg))
	}
	service := strings.Join(parts, " ")
	return "nohup " + service + " >/dev/null 2>&1 &"
}

func shellQuote(v string) string {
	return "'" + strings.ReplaceAll(v, "'", "'\\''") + "'"
}

func tcpReady(addr string, timeout time.Duration) bool {
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}
