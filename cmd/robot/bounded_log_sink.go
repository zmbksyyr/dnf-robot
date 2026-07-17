package main

import (
	"flag"
	"fmt"
	"io"
	"robot/internal/foundation/config"
	"robot/internal/foundation/logfile"
	"strings"
)

var (
	boundedLogSinkPath   = flag.String("bounded-log-sink", "", "write stdin to a bounded rotating log")
	boundedLogMaxBytes   = flag.Int64("bounded-log-max-bytes", 0, "maximum bytes per bounded log file")
	boundedLogMaxBackups = flag.Int("bounded-log-backups", 0, "number of bounded log backups")
)

func boundedLogSinkRequested() bool {
	return strings.TrimSpace(*boundedLogSinkPath) != ""
}

func runBoundedLogSink(src io.Reader) error {
	maxBytes, backups, err := boundedLogSettings()
	if err != nil {
		return err
	}
	return logfile.CopyRotating(*boundedLogSinkPath, src, maxBytes, backups)
}

func boundedLogSettings() (int64, int, error) {
	maxBytes := *boundedLogMaxBytes
	backups := *boundedLogMaxBackups
	if maxBytes > 0 && backups > 0 {
		return maxBytes, backups, nil
	}
	configPath, _, err := runtimeConfigPaths()
	if err != nil {
		return 0, 0, fmt.Errorf("resolve bounded log config: %w", err)
	}
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return 0, 0, fmt.Errorf("load bounded log config: %w", err)
	}
	if maxBytes <= 0 {
		maxBytes = int64(cfg.LogMaxSizeMB) * 1024 * 1024
	}
	if backups <= 0 {
		backups = cfg.LogMaxBackups
	}
	return maxBytes, backups, nil
}
