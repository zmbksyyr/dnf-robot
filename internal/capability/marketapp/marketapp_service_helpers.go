package marketapp

import (
	"net"
	"strconv"
	"strings"
	"time"
)

func marketServiceShellCommand(bin string, args []string, outputPath, sinkBin string, maxBytes int64, backups int) string {
	parts := make([]string, 0, len(args)+2)
	parts = append(parts, shellQuote(bin))
	for _, arg := range args {
		parts = append(parts, shellQuote(arg))
	}
	if outputPath == "" {
		outputPath = "/dev/null"
	}
	quotedOutput := shellQuote(outputPath)
	service := strings.Join(parts, " ")
	if strings.TrimSpace(sinkBin) == "" {
		return ": >" + quotedOutput + "; nohup " + service + " >>" + quotedOutput + " 2>&1 &"
	}
	sink := shellQuote(sinkBin) + " --bounded-log-sink " + quotedOutput +
		" --bounded-log-max-bytes " + strconv.FormatInt(maxBytes, 10) +
		" --bounded-log-backups " + strconv.Itoa(backups)
	pipeline := service + " 2>&1 | " + sink
	return ": >" + quotedOutput + "; nohup sh -c " + shellQuote(pipeline) + " >/dev/null 2>&1 &"
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
